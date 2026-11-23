package svc

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
)

// handlers implements the contract's operation table.
func (s *Server) handlers() sv1.Handlers {
	return sv1.Handlers{
		CreateSpace:    s.createSpace,
		GetSpace:       s.getSpace,
		DestroySpace:   s.destroySpace,
		EnsureRuntime:  s.ensureRuntime,
		ResolveSpec:    s.resolveSpec,
		ReleaseRuntime: s.releaseRuntime,
		WatchdogStatus: func(ctx context.Context, r sv1.WatchdogStatusReq) (sv1.WatchdogStatusResp, error) {
			return watchdogStatus(s, ctx, r)
		},
	}
}

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("rand: " + err.Error())
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

type clock struct{ f func() time.Time }

func (c clock) Now() time.Time { return c.f() }

func (s *Server) createSpace(ctx context.Context, r sv1.CreateSpaceReq) (sv1.CreateSpaceResp, error) {
	scope, err := idv1.ScopeFrom(ctx)
	if err != nil {
		return sv1.CreateSpaceResp{}, fmt.Errorf("scope: %w", err)
	}
	if scope.Space != "" {
		return sv1.CreateSpaceResp{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	if r.Name == "" || r.Template != "default" {
		return sv1.CreateSpaceResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	owner := scope.Owner
	// The passport pins the image by digest: the configured tag
	// resolves to the daemon's immutable id at compile time, so the
	// spec digest binds image content, not a moving tag.
	image, err := s.deps.Docker.ImageID(ctx, s.deps.Image)
	if err != nil {
		return sv1.CreateSpaceResp{}, fmt.Errorf("image pin: %w", err)
	}
	// Idempotent on the natural key (owner,name): a repeat returns the
	// existing space — its spec stays pinned at creation time.
	if sp, err := spaceByName(ctx, s.db, owner, r.Name); err != nil {
		return sv1.CreateSpaceResp{}, err
	} else if sp.id != "" {
		return sv1.CreateSpaceResp{ID: sp.id, Digest: sp.digest}, nil
	}
	spec, digest, err := compile(defaultPassports(
		image, s.deps.ModelURL, s.deps.TaskURL,
		Limits{Mem: "256m", CPU: "1.0", Pids: 256}))
	if err != nil {
		return sv1.CreateSpaceResp{}, err
	}
	id := newID("space")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sv1.CreateSpaceResp{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	if err := insertSpace(ctx, tx, spaceRow{
		id: id, owner: owner, name: r.Name,
		template: r.Template, spec: spec, digest: digest}); err != nil {
		return sv1.CreateSpaceResp{}, err
	}
	if err := tx.Commit(); err != nil {
		return sv1.CreateSpaceResp{}, fmt.Errorf("commit: %w", err)
	}
	return sv1.CreateSpaceResp{ID: id, Digest: digest}, nil
}

func (s *Server) getSpace(ctx context.Context, r sv1.GetSpaceReq) (sv1.GetSpaceResp, error) {
	scope, err := idv1.ScopeFrom(ctx)
	if err != nil {
		return sv1.GetSpaceResp{}, fmt.Errorf("scope: %w", err)
	}
	if scope.Space == "" || scope.Space != r.ID {
		return sv1.GetSpaceResp{}, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	sp, err := getSpace(ctx, s.db, scope.Owner, scope.Space)
	if err != nil {
		return sv1.GetSpaceResp{}, err
	}
	out := sv1.GetSpaceResp{Space: sv1.SpaceInfo{
		ID: sp.id, Owner: sp.owner, Name: sp.name, Digest: sp.digest}}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sv1.GetSpaceResp{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	if rt, ok, err := runtimeForSpace(ctx, tx, sp.id); err != nil {
		return sv1.GetSpaceResp{}, err
	} else if ok {
		out.Space.Runtime = rt.id
		out.Space.Endpoint = rt.endpoint
	}
	return out, nil
}

// destroySpace drains the runtime and removes the domain rows; the
// docker removal rides the outbox in the same transaction.
func (s *Server) destroySpace(ctx context.Context, r sv1.DestroySpaceReq) (sv1.DestroySpaceResp, error) {
	scope, err := idv1.ScopeFrom(ctx)
	if err != nil {
		return sv1.DestroySpaceResp{}, fmt.Errorf("scope: %w", err)
	}
	if scope.Space == "" || scope.Space != r.ID {
		return sv1.DestroySpaceResp{}, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	sp, err := getSpace(ctx, s.db, scope.Owner, scope.Space)
	if err != nil {
		return sv1.DestroySpaceResp{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sv1.DestroySpaceResp{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	if rt, ok, err := runtimeForSpace(ctx, tx, sp.id); err != nil {
		return sv1.DestroySpaceResp{}, err
	} else if ok {
		if err := s.drainRuntime(ctx, tx, rt); err != nil {
			return sv1.DestroySpaceResp{}, err
		}
	}
	if err := deleteSpace(ctx, tx, sp.id); err != nil {
		return sv1.DestroySpaceResp{}, err
	}
	if err := tx.Commit(); err != nil {
		return sv1.DestroySpaceResp{}, fmt.Errorf("commit: %w", err)
	}
	return sv1.DestroySpaceResp{OK: true}, nil
}

// drainRuntime moves an active runtime to draining with a docker-rm
// effect; already-idle states are left to their own deadlines.
func (s *Server) drainRuntime(ctx context.Context, tx *sql.Tx, rt runtimeRow) error {
	payload := mustJSON(rmEffect{Runtime: rt.id, Container: rt.container})
	err := s.runM.Transition(ctx, tx, fsm.Request{
		ID: rt.id, Event: "drain", Actor: "api", At: s.deps.Now(),
		Effects: []fsm.Effect{{Topic: "docker-rm", Payload: payload}}}, nil)
	if errors.Is(err, fsm.ErrBadTransition) {
		return nil // absent/quarantined/etc: nothing running to drain
	}
	if err != nil {
		return fmt.Errorf("drain: %w", err)
	}
	return nil
}

func (s *Server) resolveSpec(ctx context.Context, r sv1.ResolveSpecReq) (sv1.ResolveSpecResp, error) {
	sp, err := getSpaceAny(ctx, s.db, r.Space)
	if err != nil {
		return sv1.ResolveSpecResp{}, err
	}
	return sv1.ResolveSpecResp{Spec: string(sp.spec), Digest: sp.digest, Owner: sp.owner}, nil
}

func (s *Server) ensureRuntime(ctx context.Context, r sv1.EnsureRuntimeReq) (sv1.EnsureRuntimeResp, error) {
	sp, err := getSpaceAny(ctx, s.db, r.Space)
	if err != nil {
		return sv1.EnsureRuntimeResp{}, err
	}
	if r.Digest != "" && r.Digest != sp.digest {
		stale := herrors.New(herrors.CodeStale, "err.stale").
			With("digest", r.Digest)
		return sv1.EnsureRuntimeResp{}, stale //nolint:wrapcheck // shared error form
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sv1.EnsureRuntimeResp{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	rt, ok, err := runtimeForSpace(ctx, tx, sp.id)
	if err != nil {
		return sv1.EnsureRuntimeResp{}, err
	}
	if !ok {
		if err := s.claimRuntime(ctx, tx, sp); err != nil {
			return sv1.EnsureRuntimeResp{}, err
		}
		if err := tx.Commit(); err != nil {
			return sv1.EnsureRuntimeResp{}, fmt.Errorf("commit: %w", err)
		}
		return sv1.EnsureRuntimeResp{Digest: sp.digest}, nil
	}
	st, err := s.runM.State(ctx, s.db, rt.id)
	if err != nil {
		return sv1.EnsureRuntimeResp{}, fmt.Errorf("runtime state: %w", err)
	}
	out, err := s.ensureByState(ctx, tx, sp, rt, st)
	if err != nil {
		return sv1.EnsureRuntimeResp{}, err
	}
	if err := tx.Commit(); err != nil {
		return sv1.EnsureRuntimeResp{}, fmt.Errorf("commit: %w", err)
	}
	return out, nil
}

// claimRuntime inserts the runtime row, its machine, the intent and
// the docker-create effect — all in one transaction.
func (s *Server) claimRuntime(ctx context.Context, tx *sql.Tx, sp spaceRow) error {
	p, err := specPassports(sp)
	if err != nil {
		return err
	}
	rtID := newID("rt")
	if err := insertRuntime(ctx, tx, runtimeRow{
		id: rtID, space: sp.id, digest: sp.digest}); err != nil {
		return err
	}
	if err := s.runM.Create(ctx, tx, clock{s.deps.Now},
		fsm.CreateReq{ID: rtID, Initial: "absent", Actor: "api"}); err != nil {
		return fmt.Errorf("runtime machine: %w", err)
	}
	payload := mustJSON(createEffect{Runtime: rtID, Space: sp.id})
	if err := s.runM.Transition(ctx, tx, fsm.Request{
		ID: rtID, Event: "claim", Actor: "api", At: s.deps.Now(),
		Effects: []fsm.Effect{{Topic: "docker-create", Payload: payload}}}, nil); err != nil {
		return fmt.Errorf("claim: %w", err)
	}
	return writeIntent(ctx, tx, intentRow{
		id: newID("intent"), runtime: rtID, space: sp.id,
		digest: sp.digest, image: p.Stack.Image})
}

// ensureByState answers EnsureRuntime from the current machine state.
func (s *Server) ensureByState(ctx context.Context, tx *sql.Tx, sp spaceRow,
	rt runtimeRow, st string) (sv1.EnsureRuntimeResp, error) {
	switch st {
	case "warm":
		return sv1.EnsureRuntimeResp{Runtime: rt.id, Endpoint: rt.endpoint,
			Digest: sp.digest, Ready: true}, nil
	case "busy":
		return sv1.EnsureRuntimeResp{Digest: sp.digest}, nil
	case "missing", "failed":
		// A lost container is re-claimed in place; the machine edge
		// carries it absent->starting equivalent.
		if err := s.runM.Transition(ctx, tx, fsm.Request{
			ID: rt.id, Event: "claim", Actor: "api", At: s.deps.Now(),
			Effects: []fsm.Effect{{Topic: "docker-create",
				Payload: mustJSON(createEffect{Runtime: rt.id, Space: sp.id})}}}, nil); err != nil {
			return sv1.EnsureRuntimeResp{}, fmt.Errorf("re-claim: %w", err)
		}
		return sv1.EnsureRuntimeResp{Digest: sp.digest}, nil
	case "starting":
		return sv1.EnsureRuntimeResp{Digest: sp.digest}, nil
	default:
		// draining / quarantined: nothing to give the caller.
		conflict := herrors.New(herrors.CodeConflict, "err.machine-state").
			With("state", st)
		return sv1.EnsureRuntimeResp{}, conflict //nolint:wrapcheck // shared error form
	}
}

func (s *Server) releaseRuntime(ctx context.Context, r sv1.ReleaseRuntimeReq) (sv1.ReleaseRuntimeResp, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sv1.ReleaseRuntimeResp{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	err = s.runM.Transition(ctx, tx, fsm.Request{
		ID: r.Runtime, Event: "release", Actor: "api", At: s.deps.Now()}, nil)
	if errors.Is(err, fsm.ErrBadTransition) {
		// Warm/absent is already released — idempotent ack.
		err = s.runM.Touch(ctx, tx, r.Runtime, s.deps.Now())
	}
	if err != nil {
		return sv1.ReleaseRuntimeResp{}, fmt.Errorf("release: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return sv1.ReleaseRuntimeResp{}, fmt.Errorf("commit: %w", err)
	}
	return sv1.ReleaseRuntimeResp{OK: true}, nil
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("json: " + err.Error())
	}
	return b
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	age, err := fsm.TickAge(r.Context(), s.db, "fsm", "spawn-controller", s.deps.Now())
	if err != nil || age > 15*time.Second {
		w.WriteHeader(http.StatusServiceUnavailable)
		httpapi.WriteJSON(w, map[string]string{"status": "stale"})
		return
	}
	httpapi.WriteJSON(w, map[string]string{"status": "ok"})
}
