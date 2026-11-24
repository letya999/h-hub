package svc

import (
	"context"
	"encoding/json"
	"fmt"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
)

// setModelOp switches models.default through the layer mechanism:
// the user layer gains a revision pinning the choice leaf, then a
// preview+activate runs in one call. The route must already exist in
// the passport table — the command selects, it cannot widen policy.
func setModelOp(s *Server, ctx context.Context, r sv1.SetModelReq) (sv1.SetModelResp, error) {
	scope, err := idv1.ScopeFrom(ctx)
	if err != nil {
		return sv1.SetModelResp{}, fmt.Errorf("scope: %w", err)
	}
	if r.Space == "" || scope.Space != r.Space || r.Default == "" {
		return sv1.SetModelResp{}, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	sp, err := getSpace(ctx, s.db, scope.Owner, r.Space)
	if err != nil {
		return sv1.SetModelResp{}, err
	}
	if err := modelExists(sp, s.deps.ModelURL, r.Default); err != nil {
		return sv1.SetModelResp{}, err
	}
	return applyUserLeaf(s, ctx, sp, "models.default", r.Default)
}

// modelExists refuses a route name the passport table does not list.
func modelExists(sp spaceRow, modelURL, name string) error {
	p, err := specPassports(sp)
	if err != nil {
		return err
	}
	p.normalize(modelURL)
	for _, rt := range p.Models.Routes {
		if rt.Name == name {
			return nil
		}
	}
	return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
									With("route", name)
}

// applyUserLeaf writes one user-layer leaf and runs submit + preview
// + activate — the standard path for owner-selectable values.
func applyUserLeaf(s *Server, ctx context.Context, sp spaceRow,
	path string, v any) (sv1.SetModelResp, error) {
	space := sp.id
	if err := ensureLayers(s, ctx, sp); err != nil {
		return sv1.SetModelResp{}, err
	}
	layer, err := latestUserDocs(ctx, s.db, sp.id)
	if err != nil {
		return sv1.SetModelResp{}, err
	}
	setLeaf(layer, path, v)
	docs, err := json.Marshal(layer)
	if err != nil {
		return sv1.SetModelResp{}, fmt.Errorf("docs: %w", err)
	}
	if _, err := submitLayersOp(s, ctx, sv1.SubmitLayersReq{
		Space: space, Role: "user", Docs: docs}); err != nil {
		return sv1.SetModelResp{}, err
	}
	prev, err := previewOp(s, ctx, sv1.PreviewReq{Space: space})
	if err != nil {
		return sv1.SetModelResp{}, err
	}
	act, err := activateOp(s, ctx, sv1.ActivateReq{
		Space: space, Base: prev.Base, Candidate: prev.Candidate})
	if err != nil {
		return sv1.SetModelResp{}, err
	}
	return sv1.SetModelResp{Digest: act.Digest}, nil
}
