package svc

import (
	"context"
	"fmt"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
)

// setModelOp switches models.default inside the space's own effective
// spec: the route must already be declared in the passport's routes
// table — the command cannot invent policy, only select it. The
// recompiled spec gets a new digest; the next run picks it up without
// restarting the runtime.
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
	p, err := specPassports(sp)
	if err != nil {
		return sv1.SetModelResp{}, err
	}
	p.normalize(s.deps.ModelURL)
	found := false
	for _, rt := range p.Models.Routes {
		if rt.Name == r.Default {
			found = true
		}
	}
	if !found {
		return sv1.SetModelResp{}, herrors.New(herrors.CodeInvalid, //nolint:wrapcheck // shared error form
			"err.invalid-arg").With("route", r.Default)
	}
	p.Models.Default = r.Default
	spec, digest, err := compile(p)
	if err != nil {
		return sv1.SetModelResp{}, err
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE spaces SET spec=$2, digest=$3 WHERE id=$1`, sp.id, spec, digest); err != nil {
		return sv1.SetModelResp{}, fmt.Errorf("set model: %w", err)
	}
	return sv1.SetModelResp{Digest: digest}, nil
}
