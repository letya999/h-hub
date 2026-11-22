package svc

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/letya999/h-hub/services/spawn-controller/internal/backend"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/log"
)

const (
	reconcileEvery = 2 * time.Second
	outboxEvery    = 250 * time.Millisecond
	tickEvery      = 5 * time.Second
	tickName       = "spawn-controller"
)

type createEffect struct {
	Runtime string `json:"runtime"`
	Space   string `json:"space"`
}

type rmEffect struct {
	Runtime   string `json:"runtime"`
	Container string `json:"container"`
}

// Loops runs the outbox executor, the reconcile pass and watchdog
// ticks until ctx ends.
func (s *Server) Loops(ctx context.Context, l *log.L) {
	out := time.NewTicker(outboxEvery)
	defer out.Stop()
	rec := time.NewTicker(reconcileEvery)
	defer rec.Stop()
	tick := time.NewTicker(tickEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-out.C:
			s.outboxPass(ctx, l)
		case <-rec.C:
			s.reconcilePass(ctx, l)
		case <-tick.C:
			if err := fsm.WriteTick(ctx, s.db, "fsm", tickName, s.deps.Now()); err != nil {
				l.Error(ctx, "watchdog tick", err)
			}
		}
	}
}

func (s *Server) outboxPass(ctx context.Context, l *log.L) {
	fx, err := fsm.DueEffects(ctx, s.db, "fsm", s.deps.Now(), 16)
	if err != nil {
		l.Error(ctx, "outbox read", err)
		return
	}
	for _, f := range fx {
		if err := s.execEffect(ctx, f); err != nil {
			l.Error(ctx, "effect "+f.Topic, err)
			if err := fsm.DeferEffect(ctx, s.db, "fsm", f.Seq,
				s.deps.Now().Add(5*time.Second)); err != nil {
				l.Error(ctx, "effect defer", err)
			}
			continue
		}
		if err := fsm.MarkDone(ctx, s.db, "fsm", f.Seq); err != nil {
			l.Error(ctx, "effect done", err)
		}
	}
}

func (s *Server) execEffect(ctx context.Context, f fsm.OutEffect) error {
	switch f.Topic {
	case "docker-create":
		var e createEffect
		if err := json.Unmarshal(f.Payload, &e); err != nil {
			return fmt.Errorf("create payload: %w", err)
		}
		return s.dockerCreate(ctx, e)
	case "docker-rm":
		var e rmEffect
		if err := json.Unmarshal(f.Payload, &e); err != nil {
			return fmt.Errorf("rm payload: %w", err)
		}
		return s.dockerRemove(ctx, e)
	default:
		return fmt.Errorf("unknown topic %q", f.Topic)
	}
}

// dockerCreate renders the config, launches the hardened container
// through the backend and binds the runtime row — after this the
// reconcile pass drives starting -> warm.
func (s *Server) dockerCreate(ctx context.Context, e createEffect) error {
	sp, err := getSpaceAny(ctx, s.db, e.Space)
	if err != nil {
		return err
	}
	p, err := specPassports(sp)
	if err != nil {
		return err
	}
	if err := renderConfig(s.deps.RenderDir, s.deps.Issuer, sp.id, p); err != nil {
		return err
	}
	name := "hhub-" + e.Runtime
	cid, err := s.deps.Docker.CreateAndStart(ctx, backend.Launch{
		Name:      name,
		Image:     p.Stack.Image,
		UID:       p.Stack.UID,
		Env:       harnessEnv(sp.id),
		MemBytes:  memBytes(p.Stack.Mem),
		NanoCPUs:  nanoCPUs(p.Stack.CPU),
		Pids:      p.Stack.Pids,
		Network:   s.deps.Network,
		RenderVol: s.deps.RenderVol,
	})
	if err != nil {
		return fmt.Errorf("docker create: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	if err := setContainer(ctx, tx, e.Runtime, cid,
		"http://"+name+":9000"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// renderConfig writes the space's effective harness config and the
// issuer public key into the shared render volume; runtimes mount
// the directory read-only.
func renderConfig(renderDir string, issuer ed25519.PublicKey, space string, p Passports) error {
	dir := filepath.Join(renderDir, space)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("render dir: %w", err)
	}
	data, err := json.Marshal(p.Config)
	if err != nil {
		return fmt.Errorf("render config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o644); err != nil {
		return fmt.Errorf("render config: %w", err)
	}
	pub, err := x509.MarshalPKIXPublicKey(issuer)
	if err != nil {
		return fmt.Errorf("issuer pub: %w", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})
	if err := os.WriteFile(filepath.Join(dir, "issuer.pub"), pemBytes, 0o644); err != nil {
		return fmt.Errorf("issuer pub: %w", err)
	}
	return nil
}

// harnessEnv is the exact environment allowlist for a runtime
// container: nothing else crosses the boundary.
func harnessEnv(space string) []string {
	return []string{
		"HHUB_SUB=harness-runtime",
		"HHUB_CONFIG=/etc/hhub/render/" + space + "/config.json",
	}
}

func (s *Server) dockerRemove(ctx context.Context, e rmEffect) error {
	if e.Container != "" {
		if err := s.deps.Docker.Remove(ctx, e.Container); err != nil {
			return fmt.Errorf("docker rm: %w", err)
		}
	}
	return s.withRuntimeTx(ctx, e.Runtime, "reaped", "outbox")
}

// reconcilePass inspects live runtimes against the daemon and drives
// state that only observation can settle.
func (s *Server) reconcilePass(ctx context.Context, l *log.L) {
	rts, err := liveRuntimes(ctx, s.db)
	if err != nil {
		l.Error(ctx, "reconcile read", err)
		return
	}
	for _, rt := range rts {
		if err := s.reconcileOne(ctx, rt); err != nil {
			l.Error(ctx, "reconcile "+rt.id, err)
		}
	}
}

func liveRuntimes(ctx context.Context, db *sql.DB) ([]runtimeRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT r.id, r.space, r.digest,
		r.container, r.endpoint FROM runtimes r
		JOIN fsm.machines m ON m.kind='runtime' AND m.id=r.id
		WHERE m.state IN ('starting','warm','busy','draining')`)
	if err != nil {
		return nil, fmt.Errorf("live runtimes: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close
	var out []runtimeRow
	for rows.Next() {
		var r runtimeRow
		if err := rows.Scan(&r.id, &r.space, &r.digest,
			&r.container, &r.endpoint); err != nil {
			return nil, fmt.Errorf("scan runtime: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("live runtimes: %w", err)
	}
	return out, nil
}

// reconcileOne checks one runtime's container against its state.
func (s *Server) reconcileOne(ctx context.Context, rt runtimeRow) error {
	st, err := s.runM.State(ctx, s.db, rt.id)
	if err != nil {
		return fmt.Errorf("runtime state: %w", err)
	}
	exists, running, err := s.deps.Docker.Inspect(ctx, rt.container)
	if err != nil {
		return fmt.Errorf("inspect: %w", err)
	}
	switch {
	case st == "draining":
		if exists {
			if err := s.deps.Docker.Remove(ctx, rt.container); err != nil {
				return fmt.Errorf("docker rm: %w", err)
			}
			return nil
		}
		return s.withRuntimeTx(ctx, rt.id, "reaped", "reconcile")
	case !exists, !running:
		return s.withRuntimeTx(ctx, rt.id, "lost", "reconcile")
	case st == "starting" && healthy(ctx, rt.endpoint):
		return s.withRuntimeTx(ctx, rt.id, "ready", "reconcile")
	}
	return nil
}

// healthy probes the runtime's health endpoint on the internal net.
func healthy(ctx context.Context, endpoint string) bool {
	if endpoint == "" {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		endpoint+"/healthz", nil)
	if err != nil {
		return false
	}
	hc := &http.Client{Timeout: 2 * time.Second}
	res, err := hc.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close() //nolint:errcheck // best-effort close
	if _, err := io.Copy(io.Discard, res.Body); err != nil {
		return false
	}
	return res.StatusCode == http.StatusOK
}

// withRuntimeTx applies one runtime event in its own transaction;
// a forbidden edge is a no-op (already moved on).
func (s *Server) withRuntimeTx(ctx context.Context, id, event, actor string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	err = s.runM.Transition(ctx, tx, fsm.Request{
		ID: id, Event: event, Actor: actor, At: s.deps.Now()}, nil)
	if errors.Is(err, fsm.ErrBadTransition) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: %w", event, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// memBytes parses a "256m"-style limit to bytes.
func memBytes(s string) int64 {
	mul := int64(1)
	v := strings.TrimSpace(s)
	switch {
	case strings.HasSuffix(v, "g"):
		mul, v = 1<<30, v[:len(v)-1]
	case strings.HasSuffix(v, "m"):
		mul, v = 1<<20, v[:len(v)-1]
	case strings.HasSuffix(v, "k"):
		mul, v = 1<<10, v[:len(v)-1]
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 256 << 20
	}
	return n * mul
}

// nanoCPUs parses "1.0"-style CPUs to nano CPUs.
func nanoCPUs(s string) int64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f <= 0 {
		return 1e9
	}
	return int64(f * 1e9)
}
