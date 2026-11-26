package svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
)

// layerAuth checks the caller may touch the space's layers: the
// platform role is operator-only (sub "operator"), the user role
// needs an owner-scoped claim on this space.
func layerAuth(scope idv1.OwnerClaims, sp spaceRow, role string) error {
	switch role {
	case "platform":
		if scope.Sub != "operator" {
			return herrors.New(herrors.CodeForbidden, "err.forbidden")
		}
	case "user":
		if scope.Owner != sp.owner || scope.Space != sp.id {
			return herrors.New(herrors.CodeForbidden, "err.forbidden")
		}
	default:
		return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
										With("role", role)
	}
	return nil
}

// submitLayersOp stores an immutable layer revision after the same
// compile the activation will run — a widening lower layer is
// refused here, not at apply. Submitted leaves overlay the role's
// latest revision; omitted leaves keep their stored values.
func submitLayersOp(s *Server, ctx context.Context, r sv1.SubmitLayersReq) (sv1.SubmitLayersResp, error) {
	scope, err := idv1.ScopeFrom(ctx)
	if err != nil {
		return sv1.SubmitLayersResp{}, fmt.Errorf("scope: %w", err)
	}
	sp, err := layerSpace(ctx, s.db, scope, r.Space)
	if err != nil {
		return sv1.SubmitLayersResp{}, err
	}
	if err := layerAuth(scope, sp, r.Role); err != nil {
		return sv1.SubmitLayersResp{}, err
	}
	docs, err := parseDocs(r.Docs)
	if err != nil {
		return sv1.SubmitLayersResp{}, err
	}
	if err := s.checkImagePatch(docs); err != nil {
		return sv1.SubmitLayersResp{}, err
	}
	if err := ensureLayers(s, ctx, sp); err != nil {
		return sv1.SubmitLayersResp{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sv1.SubmitLayersResp{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	rev, seq, err := storeRevision(s, ctx, tx, revRow{
		space: sp.id, role: r.Role,
		author: scope.Sub, at: s.deps.Now()}, docs)
	if err != nil {
		return sv1.SubmitLayersResp{}, err
	}
	chain, err := candidateChain(ctx, tx, sp.id)
	if err != nil {
		return sv1.SubmitLayersResp{}, err
	}
	if _, err := compileSet(chain); err != nil {
		return sv1.SubmitLayersResp{}, err
	}
	if err := tx.Commit(); err != nil {
		return sv1.SubmitLayersResp{}, fmt.Errorf("commit: %w", err)
	}
	return sv1.SubmitLayersResp{Revision: rev, Seq: seq}, nil
}

// storeRevision overlays patch leaves onto the role's latest
// revision and inserts the result as a new immutable revision.
func storeRevision(s *Server, ctx context.Context, tx *sql.Tx, r revRow, patch DocSet) (string, int, error) {
	if _, err := spaceForUpdate(ctx, tx, r.space); err != nil {
		return "", 0, err
	}
	base, err := latestRoleDocs(ctx, tx, r.space, r.role)
	if err != nil {
		return "", 0, err
	}
	for _, leaf := range registry.Leaves {
		if v, ok := leafValue(patch, leaf.Path); ok {
			setLeaf(base, leaf.Path, v)
		}
	}
	r.docs = base
	return insertRevision(ctx, tx, r)
}

// previewOp compiles the candidate set and reports the diff and the
// effect an activation would apply — no writes, no side effects.
func previewOp(s *Server, ctx context.Context, r sv1.PreviewReq) (sv1.PreviewResp, error) {
	scope, err := idv1.ScopeFrom(ctx)
	if err != nil {
		return sv1.PreviewResp{}, fmt.Errorf("scope: %w", err)
	}
	sp, err := layerSpace(ctx, s.db, scope, r.Space)
	if err != nil {
		return sv1.PreviewResp{}, err
	}
	if err := ensureLayers(s, ctx, sp); err != nil {
		return sv1.PreviewResp{}, err
	}
	chain, err := candidateChain(ctx, s.db, sp.id)
	if err != nil {
		return sv1.PreviewResp{}, err
	}
	cand, err := compileSet(chain)
	if err != nil {
		return sv1.PreviewResp{}, err
	}
	diff, effect, err := specDiff(sp, cand.spec, chain)
	if err != nil {
		return sv1.PreviewResp{}, err
	}
	return sv1.PreviewResp{Base: sp.digest, Candidate: cand.digest,
		Effect: effect, Diff: diff}, nil
}

// activateOp applies exactly the candidate the caller previewed: CAS
// on the base digest, recompile must reproduce the candidate digest,
// then the declared effect runs inside the same transaction.
func activateOp(s *Server, ctx context.Context, r sv1.ActivateReq) (sv1.ActivateResp, error) {
	scope, err := idv1.ScopeFrom(ctx)
	if err != nil {
		return sv1.ActivateResp{}, fmt.Errorf("scope: %w", err)
	}
	sp, err := layerSpace(ctx, s.db, scope, r.Space)
	if err != nil {
		return sv1.ActivateResp{}, err
	}
	if err := ensureLayers(s, ctx, sp); err != nil {
		return sv1.ActivateResp{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sv1.ActivateResp{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	cur, err := spaceForUpdate(ctx, tx, sp.id)
	if err != nil {
		return sv1.ActivateResp{}, err
	}
	if cur.digest != r.Base {
		return sv1.ActivateResp{}, staleErr("digest", r.Base)
	}
	digest, effect, err := applyCandidate(s, ctx, tx, cur,
		actReq{target: r.Candidate, author: scope.Sub})
	if err != nil {
		return sv1.ActivateResp{}, err
	}
	if err := tx.Commit(); err != nil {
		return sv1.ActivateResp{}, fmt.Errorf("commit: %w", err)
	}
	return sv1.ActivateResp{Digest: digest, Effect: effect}, nil
}

// applyCandidate recompiles the latest chain and applies it when the
// digest still matches the previewed candidate.
func applyCandidate(s *Server, ctx context.Context, tx *sql.Tx, cur spaceRow,
	req actReq) (string, string, error) {
	chain, err := candidateChain(ctx, tx, cur.id)
	if err != nil {
		return "", "", err
	}
	cand, err := compileSet(chain)
	if err != nil {
		return "", "", err
	}
	if cand.digest != req.target {
		return "", "", staleErr("candidate", req.target)
	}
	effect, err := applyCompiled(s, ctx, tx, cur, specApply{
		chain: chain, spec: cand.spec, digest: cand.digest,
		explain: cand.explain, author: req.author,
	})
	return cand.digest, effect, err
}

func applyCompiled(s *Server, ctx context.Context, tx *sql.Tx, cur spaceRow, candidate specApply) (string, error) {
	_, effect, err := specDiff(cur, candidate.spec, candidate.chain)
	if err != nil {
		return "", err
	}
	candidate.effect = effect
	if err := applySpecTx(s, ctx, tx, cur, candidate); err != nil {
		return "", err
	}
	cur.digest = candidate.digest
	if err := applyEffectTx(s, ctx, tx, cur, effect); err != nil {
		return "", err
	}
	return effect, nil
}

// rollbackOp re-activates a previous digest: the stored revision set
// for that activation re-passes the full compile before the pointer
// moves — rollback goes through the same checks as apply.
func rollbackOp(s *Server, ctx context.Context, r sv1.RollbackReq) (sv1.RollbackResp, error) {
	scope, err := idv1.ScopeFrom(ctx)
	if err != nil {
		return sv1.RollbackResp{}, fmt.Errorf("scope: %w", err)
	}
	sp, err := layerSpace(ctx, s.db, scope, r.Space)
	if err != nil {
		return sv1.RollbackResp{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sv1.RollbackResp{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	cur, err := spaceForUpdate(ctx, tx, sp.id)
	if err != nil {
		return sv1.RollbackResp{}, err
	}
	digest, effect, err := applyRollback(s, ctx, tx, cur,
		actReq{target: r.Digest, author: scope.Sub})
	if err != nil {
		return sv1.RollbackResp{}, err
	}
	if err := tx.Commit(); err != nil {
		return sv1.RollbackResp{}, fmt.Errorf("commit: %w", err)
	}
	return sv1.RollbackResp{Digest: digest, Effect: effect}, nil
}

// applyRollback reconstructs the chain stored for a past activation
// and re-applies it through the same compile and effect path.
func applyRollback(s *Server, ctx context.Context, tx *sql.Tx, cur spaceRow,
	req actReq) (string, string, error) {
	revs, err := activationRevisions(ctx, tx, cur.id, req.target)
	if err != nil {
		return "", "", err
	}
	chain, err := revisionsChain(ctx, tx, revs)
	if err != nil {
		return "", "", err
	}
	cand, err := compileSet(chain)
	if err != nil {
		return "", "", err
	}
	if cand.digest != req.target {
		return "", "", staleErr("digest", req.target)
	}
	effect, err := applyCompiled(s, ctx, tx, cur, specApply{
		chain: chain, spec: cand.spec, digest: cand.digest,
		explain: cand.explain, author: req.author,
	})
	return cand.digest, effect, err
}

// explainOp reads the persisted attribution of the active digest.
func explainOp(s *Server, ctx context.Context, r sv1.ExplainReq) (sv1.ExplainResp, error) {
	scope, err := idv1.ScopeFrom(ctx)
	if err != nil {
		return sv1.ExplainResp{}, fmt.Errorf("scope: %w", err)
	}
	sp, err := layerSpace(ctx, s.db, scope, r.Space)
	if err != nil {
		return sv1.ExplainResp{}, err
	}
	if err := ensureLayers(s, ctx, sp); err != nil {
		return sv1.ExplainResp{}, err
	}
	var raw []byte
	err = s.db.QueryRowContext(ctx, `SELECT explain FROM effective_specs
		WHERE space=$1 AND digest=$2`, sp.id, sp.digest).Scan(&raw)
	if err != nil {
		return sv1.ExplainResp{}, fmt.Errorf("explain: %w", err)
	}
	out := sv1.ExplainResp{Digest: sp.digest}
	if err := json.Unmarshal(raw, &out.Layers); err != nil {
		return out, fmt.Errorf("explain decode: %w", err)
	}
	p, err := specPassports(sp)
	if err != nil {
		return out, err
	}
	if err := p.Workspace.validateLayout(); err != nil {
		return out, err
	}
	out.Workspace = p.Workspace.tree()
	return out, nil
}

// layerSpace loads a space either by owner scope or as operator.
func layerSpace(ctx context.Context, db *sql.DB, scope idv1.OwnerClaims, id string) (spaceRow, error) {
	if scope.Sub == "operator" {
		return getSpaceAny(ctx, db, id)
	}
	return getSpace(ctx, db, scope.Owner, id)
}

// actReq is the target digest + author for one apply path.
type actReq struct {
	target string
	author string
}

// specApply carries one activation's inputs through the tx.
type specApply struct {
	chain   layerSet
	spec    Passports
	explain map[string]string
	digest  string
	effect  string
	author  string
}

// applySpecTx stores the effective spec, logs the activation and CAS-
// updates the space's digest — the pointer move is one statement.
func applySpecTx(s *Server, ctx context.Context, tx *sql.Tx, sp spaceRow, a specApply) error {
	raw, err := json.Marshal(a.spec)
	if err != nil {
		return fmt.Errorf("spec marshal: %w", err)
	}
	if err := insertSpec(ctx, tx, sp.id, a, s.deps.Now()); err != nil {
		return err
	}
	if err := insertActivation(ctx, tx, sp.id, a, s.deps.Now()); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE spaces SET spec=$2, digest=$3 WHERE id=$1`,
		sp.id, raw, a.digest)
	if err != nil {
		return fmt.Errorf("digest cas: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return herrors.New(herrors.CodeStale, "err.stale")
	}
	return nil
}

// applyEffectTx runs the declared effect inside the activation tx:
// respawn and recreate drain the runtime through the FSM, recreate
// additionally resets the rendered world.
func applyEffectTx(s *Server, ctx context.Context, tx *sql.Tx, sp spaceRow, effect string) error {
	if effect == "live" || effect == "" {
		return nil
	}
	if _, err := drainRuntimeTx(s, ctx, tx, sp.id, "passport"); err != nil {
		return err
	}
	if effect == "recreate" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO fsm.outbox
			(kind, machine_id, topic, payload) VALUES ('space',$1,'render-reset',$2)`,
			sp.id, mustJSON(resetEffect{Space: sp.id})); err != nil {
			return fmt.Errorf("effect reset: %w", err)
		}
	}
	return nil
}

// staleErr is the shared refusal for CAS and digest mismatches.
func staleErr(key, v string) error {
	return herrors.New(herrors.CodeStale, "err.stale"). //nolint:wrapcheck // shared error form
								With(key, v)
}
