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
		CreateSpace: func(ctx context.Context, r sv1.CreateSpaceReq) (sv1.CreateSpaceResp, error) {
			return createSpaceOp(s, ctx, r)
		},
		GetSpace: func(ctx context.Context, r sv1.GetSpaceReq) (sv1.GetSpaceResp, error) {
			return getSpaceOp(s, ctx, r)
		},
		DestroySpace: func(ctx context.Context, r sv1.DestroySpaceReq) (sv1.DestroySpaceResp, error) {
			return destroySpaceOp(s, ctx, r)
		},
		EnsureRuntime: func(ctx context.Context, r sv1.EnsureRuntimeReq) (sv1.EnsureRuntimeResp, error) {
			return ensureRuntimeOp(s, ctx, r)
		},
		ResolveSpec: func(ctx context.Context, r sv1.ResolveSpecReq) (sv1.ResolveSpecResp, error) {
			return resolveSpecOp(s, ctx, r)
		},
		ReleaseRuntime: func(ctx context.Context, r sv1.ReleaseRuntimeReq) (sv1.ReleaseRuntimeResp, error) {
			return releaseRuntimeOp(s, ctx, r)
		},
		DrainRuntime: func(ctx context.Context, r sv1.DrainRuntimeReq) (sv1.DrainRuntimeResp, error) {
			return drainRuntimeOp(s, ctx, r)
		},
		WatchdogStatus: func(ctx context.Context, r sv1.WatchdogStatusReq) (sv1.WatchdogStatusResp, error) {
			return watchdogStatus(s, ctx, r)
		},
		SetModel: func(ctx context.Context, r sv1.SetModelReq) (sv1.SetModelResp, error) {
			return setModelOp(s, ctx, r)
		},
		SubmitLayers: func(ctx context.Context, r sv1.SubmitLayersReq) (sv1.SubmitLayersResp, error) {
			return submitLayersOp(s, ctx, r)
		},
		Preview: func(ctx context.Context, r sv1.PreviewReq) (sv1.PreviewResp, error) {
			return previewOp(s, ctx, r)
		},
		Activate: func(ctx context.Context, r sv1.ActivateReq) (sv1.ActivateResp, error) {
			return activateOp(s, ctx, r)
		},
		Rollback: func(ctx context.Context, r sv1.RollbackReq) (sv1.RollbackResp, error) {
			return rollbackOp(s, ctx, r)
		},
		Explain: func(ctx context.Context, r sv1.ExplainReq) (sv1.ExplainResp, error) {
			return explainOp(s, ctx, r)
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

func createSpaceOp(s *Server, ctx context.Context, r sv1.CreateSpaceReq) (sv1.CreateSpaceResp, error) {
	scope, err := idv1.ScopeFrom(ctx)
	if err != nil {
		return sv1.CreateSpaceResp{}, fmt.Errorf("scope: %w", err)
	}
	if err := createArgs(scope, r); err != nil {
		return sv1.CreateSpaceResp{}, err
	}
	owner := scope.Owner
	// Idempotent on the natural key (owner,name): a repeat returns the
	// existing space — its spec stays pinned at creation time.
	if sp, err := spaceByName(ctx, s.db, owner, r.Name); err != nil {
		return sv1.CreateSpaceResp{}, err
	} else if sp.id != "" {
		return sv1.CreateSpaceResp{ID: sp.id, Digest: sp.digest}, nil
	}
	// The space's layers: the shipped platform layer plus per-space
	// facts, then the template folder as the user layer. The first
	// activation compiles them into the effective spec.
	chain, err := spaceLayers(s, owner, s.deps.Hermes, r.Template)
	if err != nil {
		return sv1.CreateSpaceResp{}, err
	}
	cand, err := compileSet(chain)
	if err != nil {
		return sv1.CreateSpaceResp{}, err
	}
	id := newID("space")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sv1.CreateSpaceResp{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	err = insertNewSpace(s, ctx, tx, spaceRow{
		id: id, owner: owner, name: r.Name,
		template: r.Template}, specApply{
		chain: chain, spec: cand.spec, digest: cand.digest,
		explain: cand.explain, effect: "live", author: "create"})
	if err != nil {
		return sv1.CreateSpaceResp{}, err
	}
	if err := tx.Commit(); err != nil {
		return sv1.CreateSpaceResp{}, fmt.Errorf("commit: %w", err)
	}
	return sv1.CreateSpaceResp{ID: id, Digest: cand.digest}, nil
}

// createArgs enforces the request contract: only an unscoped owner
// may create, and both fields are required.
func createArgs(scope idv1.OwnerClaims, r sv1.CreateSpaceReq) error {
	if scope.Space != "" {
		return herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	if r.Name == "" || r.Template == "" {
		return herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	return nil
}

// spaceLayers assembles the creation-time layer chain: the shipped
// platform layer plus per-space facts, then the template folder.
func spaceLayers(s *Server, owner, image, template string) (layerSet, error) {
	platDocs, err := platformLayer(s, owner, image)
	if err != nil {
		return nil, err
	}
	userDocs, err := templateLayer(template)
	if err != nil {
		return nil, err
	}
	return layerSet{
		{role: "platform", docs: platDocs},
		{role: "user", docs: userDocs},
	}, nil
}

// insertNewSpace persists the space row, both creation revisions and
// the first activation — the whole birth in one transaction.
func insertNewSpace(s *Server, ctx context.Context, tx *sql.Tx, sp spaceRow,
	a specApply) error {
	specRaw, err := json.Marshal(a.spec)
	if err != nil {
		return fmt.Errorf("spec marshal: %w", err)
	}
	sp.spec, sp.digest = specRaw, a.digest
	if err := insertSpace(ctx, tx, sp); err != nil {
		return err
	}
	at := s.deps.Now()
	for _, l := range a.chain {
		if _, _, err := insertRevision(ctx, tx, revRow{
			space: sp.id, role: l.role, docs: l.docs,
			author: a.author, at: at}); err != nil {
			return err
		}
	}
	a.chain, err = candidateChain(ctx, tx, sp.id)
	if err != nil {
		return err
	}
	return applySpecTx(s, ctx, tx, sp, a)
}

func getSpaceOp(s *Server, ctx context.Context, r sv1.GetSpaceReq) (sv1.GetSpaceResp, error) {
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
func destroySpaceOp(s *Server, ctx context.Context, r sv1.DestroySpaceReq) (sv1.DestroySpaceResp, error) {
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
		if err := drainRuntime(s, ctx, tx, rt); err != nil {
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
func drainRuntime(s *Server, ctx context.Context, tx *sql.Tx, rt runtimeRow) error {
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

// resolveSpec reads the persisted effective spec, upgrading a
// phase-5 document to the phase-6 models shape once so the digest
// keeps binding real stored bytes.
func resolveSpecOp(s *Server, ctx context.Context, r sv1.ResolveSpecReq) (sv1.ResolveSpecResp, error) {
	sp, err := s.spaceForRun(ctx, r.Space)
	if err != nil {
		return sv1.ResolveSpecResp{}, err
	}
	p, err := specPassports(sp)
	if err != nil {
		return sv1.ResolveSpecResp{}, err
	}
	upgraded, err := p.normalize(s.deps.ModelURL)
	if err != nil {
		return sv1.ResolveSpecResp{}, err
	}
	if !upgraded {
		return sv1.ResolveSpecResp{Spec: string(sp.spec), Digest: sp.digest, Owner: sp.owner}, nil
	}
	spec, digest, err := compile(p)
	if err != nil {
		return sv1.ResolveSpecResp{}, err
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE spaces SET spec=$2, digest=$3 WHERE id=$1`, sp.id, spec, digest); err != nil {
		return sv1.ResolveSpecResp{}, fmt.Errorf("spec upgrade: %w", err)
	}
	return sv1.ResolveSpecResp{Spec: string(spec), Digest: digest, Owner: sp.owner}, nil
}

// staleDigest reports whether a caller-pinned spec digest no longer
// matches the space's current effective digest — the refusal condition
// for runtime creation against a changed image.
func staleDigest(req, current string) bool { return req != "" && req != current }

// admissionReady refuses a run whose spec cannot be read or selects
// toolsets without an admission url — fail closed at the run boundary.
func (s *Server) admissionReady(sp spaceRow) error {
	p, err := specPassports(sp)
	if err != nil {
		return err
	}
	if err := s.requireImage(p.Stack.Image); err != nil {
		return err
	}
	return toolAdmission(p)
}

func (s *Server) requireImage(image string) error {
	if image == "" || image != s.deps.Hermes {
		return herrors.New(herrors.CodeInvalid, "err.invalid-arg").With("leaf", "stack.image") //nolint:wrapcheck // shared error form
	}
	return nil
}

// toolAdmission refuses a run whose spec selects toolsets but names no
// tool admission url — fail closed, never a quiet tool-less mode. A
// spec selecting nothing runs without tools by design.
func toolAdmission(p Passports) error {
	if len(p.Tools.Core)+len(p.Tools.Extended) > 0 && p.Config.ToolURL == "" {
		return herrors.New(herrors.CodeInvalid, "err.tool-admission-missing")
	}
	return nil
}

func ensureRuntimeOp(s *Server, ctx context.Context, r sv1.EnsureRuntimeReq) (sv1.EnsureRuntimeResp, error) {
	if _, err := s.spaceForRun(ctx, r.Space); err != nil {
		return sv1.EnsureRuntimeResp{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sv1.EnsureRuntimeResp{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	sp, err := spaceForUpdate(ctx, tx, r.Space)
	if err != nil {
		return sv1.EnsureRuntimeResp{}, err
	}
	if staleDigest(r.Digest, sp.digest) {
		stale := herrors.New(herrors.CodeStale, "err.stale").
			With("digest", r.Digest)
		return sv1.EnsureRuntimeResp{}, stale //nolint:wrapcheck // shared error form
	}
	if err := s.admissionReady(sp); err != nil {
		return sv1.EnsureRuntimeResp{}, err
	}
	out, err := s.runtimeForLaunch(ctx, tx, sp)
	if err != nil {
		return sv1.EnsureRuntimeResp{}, err
	}
	if err := tx.Commit(); err != nil {
		return sv1.EnsureRuntimeResp{}, fmt.Errorf("commit: %w", err)
	}
	return out, nil
}

func (s *Server) runtimeForLaunch(ctx context.Context, tx *sql.Tx, sp spaceRow) (sv1.EnsureRuntimeResp, error) {
	rt, ok, err := runtimeForSpace(ctx, tx, sp.id)
	if err != nil {
		return sv1.EnsureRuntimeResp{}, err
	}
	if !ok {
		return sv1.EnsureRuntimeResp{Digest: sp.digest}, claimRuntime(s, ctx, tx, sp)
	}
	var st, actor string
	if err := tx.QueryRowContext(ctx, `SELECT state, actor FROM fsm.machines
		WHERE kind='runtime' AND id=$1`, rt.id).Scan(&st, &actor); err != nil {
		return sv1.EnsureRuntimeResp{}, fmt.Errorf("runtime state: %w", err)
	}
	return ensureByState(s, ctx, tx, sp, rtState{row: rt, state: st, actor: actor})
}

// claimRuntime inserts the runtime row, its machine, the intent and
// the docker-create effect — all in one transaction.
func claimRuntime(s *Server, ctx context.Context, tx *sql.Tx, sp spaceRow) error {
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

// rtState pairs a runtime row with its machine state and the actor
// of the transition that landed it there.
type rtState struct {
	row   runtimeRow
	state string
	actor string
}

// ensureByState answers EnsureRuntime from the current machine state.
func ensureByState(s *Server, ctx context.Context, tx *sql.Tx, sp spaceRow,
	rs rtState) (sv1.EnsureRuntimeResp, error) {
	rt, st := rs.row, rs.state
	notReady := sv1.EnsureRuntimeResp{Digest: sp.digest}
	switch st {
	case "warm":
		return sv1.EnsureRuntimeResp{Runtime: rt.id, Endpoint: rt.endpoint,
			Digest: sp.digest, Ready: true}, nil
	case "starting", "busy":
		return notReady, nil
	case "draining":
		// Sweep and passport drains are transient teardown the caller
		// polls past; an operator drain refuses a blind re-activate
		// (adr.spawn-runtime.digest-pins-image).
		if rs.actor == "sweep" || rs.actor == "passport" {
			return notReady, nil
		}
	case "absent", "missing", "failed":
		if rt.digest != sp.digest {
			return notReady, claimRuntime(s, ctx, tx, sp)
		}
		// A reaped or lost container is re-claimed in place; the
		// machine edge carries it to starting.
		if err := s.runM.Transition(ctx, tx, fsm.Request{
			ID: rt.id, Event: "claim", Actor: "api", At: s.deps.Now(),
			Effects: []fsm.Effect{{Topic: "docker-create",
				Payload: mustJSON(createEffect{Runtime: rt.id, Space: sp.id})}}}, nil); err != nil {
			return sv1.EnsureRuntimeResp{}, fmt.Errorf("re-claim: %w", err)
		}
		return notReady, nil
	}
	// Operator-drained and quarantined runtimes refuse: an operator
	// releases them, no silent wait.
	conflict := herrors.New(herrors.CodeConflict, "err.machine-state").
		With("state", st)
	return sv1.EnsureRuntimeResp{}, conflict //nolint:wrapcheck // shared error form
}

func releaseRuntimeOp(s *Server, ctx context.Context, r sv1.ReleaseRuntimeReq) (sv1.ReleaseRuntimeResp, error) {
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

func healthz(s *Server, w http.ResponseWriter, r *http.Request) {
	age, err := fsm.TickAge(r.Context(), s.db, "fsm", "spawn-controller", s.deps.Now())
	if err != nil || age > 15*time.Second {
		w.WriteHeader(http.StatusServiceUnavailable)
		httpapi.WriteJSON(w, map[string]string{"status": "stale"})
		return
	}
	httpapi.WriteJSON(w, map[string]string{"status": "ok"})
}
