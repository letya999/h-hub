package svc

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	"github.com/letya999/h-hub/services/spawn-controller/internal/backend"
	"github.com/letya999/h-hub/services/spawn-controller/internal/worker"
	"github.com/letya999/h-hub/shared/edges"
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
// ticks until ctx ends. Each gets its own goroutine: a deep outbox
// (real Docker calls) must not starve the deadline sweeper.
func (s *Server) Loops(ctx context.Context, l *log.L) {
	go every(ctx, outboxEvery, func() { s.outboxPass(ctx, l) })
	go every(ctx, reconcileEvery, func() { s.reconcilePass(ctx, l) })
	go every(ctx, reconcileEvery, func() { reconcileWorkers(s, ctx, l) })
	go every(ctx, tickEvery, func() {
		if err := fsm.WriteTick(ctx, s.db, "fsm", tickName, s.deps.Now()); err != nil {
			l.Error(ctx, "watchdog tick", err)
		}
	})
	<-ctx.Done()
}

// every runs f on each tick until ctx ends; a slow pass drops its
// missed ticks instead of piling up.
func every(ctx context.Context, d time.Duration, f func()) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f()
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
			if !errors.Is(err, errRestartBusy) {
				l.Error(ctx, "effect "+f.Topic, err)
			}
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
	case notifyRestart, restartIdle:
		return runRestartEffect(s, ctx, f)
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
	case "render-reset", "remove-sessions":
		return resetSpace(s, ctx, f)
	case worker.StopTopic:
		return reapWorkerEffect(s, ctx, f)
	case worker.LaunchTopic:
		return startWorkerEffect(s, ctx, f)
	default:
		return fmt.Errorf("unknown topic %q", f.Topic)
	}
}

func resetSpace(s *Server, ctx context.Context, f fsm.OutEffect) error {
	var e resetEffect
	if err := json.Unmarshal(f.Payload, &e); err != nil {
		return fmt.Errorf("reset payload: %w", err)
	}
	dir := filepath.Join(s.deps.RenderDir, e.Space)
	if f.Topic == "remove-sessions" {
		live, err := s.workers.SpaceHasLive(ctx, e.Space)
		if err != nil {
			return fmt.Errorf("check workspace mounts: %w", err)
		}
		if live {
			return fmt.Errorf("space %s still has a worker generation", e.Space)
		}
		if err := s.deps.Docker.RemoveSessions(ctx, e.Space); err != nil {
			return fmt.Errorf("remove space sessions: %w", err)
		}
		dir = filepath.Join(s.deps.RenderDir, workspaceStorage, e.Space)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clear space storage: %w", err)
	}
	return nil
}

// dockerCreate renders the config, launches the hardened container
// through the backend and binds the runtime row — after this the
// reconcile pass drives starting -> warm.
func (s *Server) dockerCreate(ctx context.Context, e createEffect) error {
	state, err := s.runM.State(ctx, s.db, e.Runtime)
	if err != nil {
		return fmt.Errorf("create state: %w", err)
	}
	if state != "starting" {
		return nil
	}
	sp, err := getSpaceAny(ctx, s.db, e.Space)
	if err != nil {
		return err
	}
	p, err := specPassports(sp)
	if err != nil {
		return err
	}
	name := "hhub-" + e.Runtime
	cid, err := launchHermes(s, ctx, name, sp, p)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	if err := setContainer(ctx, tx, e.Runtime, cid, "http://"+name+":9000"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func launchHermes(s *Server, ctx context.Context, name string, sp spaceRow, p Passports) (string, error) {
	blocked, err := worker.BlocksWorkspace(ctx, s.db, sp.id, sp.digest)
	if err != nil {
		return "", fmt.Errorf("runtime workspace: %w", err)
	}
	if blocked {
		return "", errRestartBusy
	}
	if err := s.requireImage(p.Stack.Image); err != nil {
		return "", err
	}
	adapter, err := s.deps.Docker.ImageID(ctx, s.deps.Image)
	if err != nil {
		return "", fmt.Errorf("resolve adapter image: %w", err)
	}
	dir := filepath.Join(sp.id, sp.digest)
	tools, err := s.projectTools(ctx, sp, p)
	if err != nil {
		return "", err
	}
	if err := s.renderConfig(sp, p, tools); err != nil {
		return "", err
	}
	workspace, err := prepareWorkspace(s.deps.RenderDir, sp.id, p.Workspace)
	if err != nil {
		return "", err
	}
	cid, err := s.deps.Docker.CreateAndStart(ctx, backend.Launch{
		Name:      name,
		Image:     p.Stack.Image,
		Adapter:   adapter,
		Space:     sp.id,
		Digest:    sp.digest,
		UID:       p.Stack.UID,
		Env:       []string{"HHUB_SUB=harness-runtime", "HHUB_CONFIG=/etc/hhub/render/config.json"},
		MemBytes:  memBytes(p.Stack.Mem),
		NanoCPUs:  nanoCPUs(p.Stack.CPU),
		Pids:      p.Stack.Pids,
		Network:   s.deps.Network,
		RenderVol: s.deps.RenderVol,
		ConfigDir: dir,
		Workspace: workspace,
	})
	if err != nil {
		return "", fmt.Errorf("docker create: %w", err)
	}
	return cid, nil
}

// renderConfig writes the space's effective harness config and the
// issuer public key into the shared render volume; runtimes mount
// the directory read-only.
func (s *Server) renderConfig(sp spaceRow, p Passports, tools thv1.ProjectionResp) error {
	dir := filepath.Join(s.deps.RenderDir, sp.id, sp.digest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("render dir: %w", err)
	}
	if err := writeHermesConfig(dir, tools); err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		ConfigPassport
		Space string      `json:"space"`
		Edges edges.Table `json:"edges,omitempty"`
	}{p.Config, sp.id, s.deps.RuntimeEdges})
	if err != nil {
		return fmt.Errorf("render config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o644); err != nil {
		return fmt.Errorf("render config: %w", err)
	}
	pub, err := x509.MarshalPKIXPublicKey(s.deps.Issuer)
	if err != nil {
		return fmt.Errorf("issuer pub: %w", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})
	if err := os.WriteFile(filepath.Join(dir, "issuer.pub"), pemBytes, 0o644); err != nil {
		return fmt.Errorf("issuer pub: %w", err)
	}
	// Workspace files from the workspace passport land under files/;
	// the runtime sees them read-only at /etc/hhub/render/<space>/files.
	for name, body := range p.Workspace.Files {
		target := filepath.Join(dir, "files", filepath.Clean(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("workspace dir: %w", err)
		}
		if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
			return fmt.Errorf("workspace file %s: %w", name, err)
		}
	}
	return renderSkillShowcase(dir, p.Workspace.Skills)
}

func (s *Server) dockerRemove(ctx context.Context, e rmEffect) error {
	container := e.Container
	if container == "" {
		container = "hhub-" + e.Runtime
	}
	if err := s.deps.Docker.Remove(ctx, container); err != nil {
		return fmt.Errorf("docker rm: %w", err)
	}
	state, err := s.runM.State(ctx, s.db, e.Runtime)
	if err != nil {
		return fmt.Errorf("rm state: %w", err)
	}
	if state == "absent" {
		return nil
	}
	return s.withRuntimeTx(ctx, e.Runtime, "reaped", "outbox")
}

// reconcilePass inspects live runtimes against the daemon and drives
// state that only observation can settle.
func (s *Server) reconcilePass(ctx context.Context, l *log.L) {
	if _, err := s.runM.Sweep(ctx, s.db, s.deps.Now(), 32, runtimeSweepHook); err != nil {
		l.Error(ctx, "runtime sweep", err)
	}
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
	if err := reapOrphans(ctx, s.db, s.deps.Docker, l); err != nil {
		l.Error(ctx, "orphan janitor", err)
	}
}

func runtimeSweepHook(ctx context.Context, tx *sql.Tx, ch fsm.Change) error {
	if ch.To != "draining" && ch.To != "failed" {
		return nil
	}
	var container string
	if err := tx.QueryRowContext(ctx,
		"SELECT container FROM runtimes WHERE id=$1", ch.ID).Scan(&container); err != nil {
		return fmt.Errorf("sweep container: %w", err)
	}
	if container == "" {
		return nil
	}
	payload, err := json.Marshal(rmEffect{Runtime: ch.ID, Container: container})
	if err != nil {
		return fmt.Errorf("sweep removal: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO fsm.outbox
		(kind,machine_id,topic,payload) VALUES ('runtime',$1,'docker-rm',$2)`, ch.ID, payload); err != nil {
		return fmt.Errorf("sweep outbox: %w", err)
	}
	return nil
}

func reapOrphans(ctx context.Context, db *sql.DB, docker *backend.Docker, l *log.L) error {
	containers, err := docker.ManagedContainers(ctx)
	if err != nil {
		return fmt.Errorf("list managed: %w", err)
	}
	for _, c := range containers {
		var exists bool
		runtimeID := strings.TrimPrefix(c.Runtime, "hhub-")
		if err := db.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM runtimes WHERE id=$1)", runtimeID).Scan(&exists); err != nil {
			return fmt.Errorf("orphan registry lookup: %w", err)
		}
		if exists {
			continue
		}
		if err := docker.Remove(ctx, c.ID); err != nil {
			return fmt.Errorf("remove orphan %s: %w", c.ID, err)
		}
		l.Info(ctx, "orphan runtime removed", "container", c.ID, "reason", "missing registry row")
	}
	return nil
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
	if st == "starting" && rt.container == "" {
		return nil
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
	case st == "starting":
		ok, err := s.deps.Docker.Healthy(ctx, rt.container)
		if err != nil {
			return fmt.Errorf("health: %w", err)
		}
		if ok {
			return s.withRuntimeTx(ctx, rt.id, "ready", "reconcile")
		}
	}
	return nil
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
