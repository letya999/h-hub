package svc

import (
	"context"

	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
)

// admitCall answers whether one call is allowed. A denial is a
// normal answer with its receipt; only transport-level failures are
// errors.
func (s *Server) admitCall(ctx context.Context, r thv1.AdmitCallReq) (thv1.AdmitCallResp, error) {
	if err := herrors.Require(r.Run != "" && r.Digest != "" && r.Toolset != "" && r.Tool != "",
		"err.invalid-arg"); err != nil {
		return thv1.AdmitCallResp{}, err //nolint:wrapcheck // shared error form
	}
	d, err := s.admit(ctx, r.Run, r.Digest, r.Toolset, r.Tool)
	if err != nil {
		return thv1.AdmitCallResp{}, err
	}
	out := thv1.AdmitCallResp{Allowed: d.allowed, Receipt: d.receipt}
	if !d.allowed {
		out.Reason = "err.tool-denied"
	}
	return out, nil
}

// projection lists the tools the model may see under the caller's
// spec: every selected toolset, each tool filtered by the spec's
// grants. An ungranted tool does not exist for the model — it is
// absent, not refused. An empty projection is itself a refusal.
func (s *Server) projection(ctx context.Context, r thv1.ProjectionReq) (thv1.ProjectionResp, error) {
	if err := r.Validate(); err != nil {
		return thv1.ProjectionResp{}, err //nolint:wrapcheck // contract error
	}
	if r.Space != "" {
		return s.launchProjection(ctx, r)
	}
	claims, err := s.runClaims(ctx, r.Run, r.Digest)
	if err != nil {
		return thv1.ProjectionResp{}, err
	}
	spec, specDigest, err := s.spec(ctx, claims.Space)
	if err != nil {
		return thv1.ProjectionResp{}, err
	}
	if p, ok, err := s.cachedProjection(ctx, specDigest); err != nil {
		return thv1.ProjectionResp{}, err
	} else if ok {
		return p, nil
	}
	p, err := project(spec)
	if err != nil {
		return thv1.ProjectionResp{}, err
	}
	if len(p.Core)+len(p.Extended)+len(p.Native) == 0 {
		return thv1.ProjectionResp{}, thv1.EmptyProjection() //nolint:wrapcheck // preserve the typed contract refusal
	}
	p.Digest = specDigest
	if err := s.cacheProjection(ctx, claims.Space, specDigest, p); err != nil {
		return thv1.ProjectionResp{}, err
	}
	return p, nil
}

func (s *Server) launchProjection(ctx context.Context, r thv1.ProjectionReq) (thv1.ProjectionResp, error) {
	principal := httpapi.PrincipalFrom(ctx)
	if principal == nil || principal.Purpose != "service" || principal.Sub != "spawn-controller" {
		return thv1.ProjectionResp{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	spec, digest, err := s.spec(ctx, r.Space)
	if err != nil {
		return thv1.ProjectionResp{}, err
	}
	if digest != r.Digest {
		return thv1.ProjectionResp{}, herrors.New(herrors.CodeStale, "err.stale")
	}
	p, err := project(spec)
	if err != nil {
		return thv1.ProjectionResp{}, err
	}
	p.Digest = digest
	for _, name := range spec.Tools.Names() {
		if spec.Tools.Executor(name) != "toolhub" {
			continue
		}
		for _, definition := range registry[name].Tools {
			p.Catalog = append(p.Catalog, toolInfo(name, definition))
		}
	}
	return p, nil
}

// invoke runs one call through tool-hub, after
// the same admission AdmitCall would give. A denial is forbidden.
func (s *Server) invoke(ctx context.Context, r thv1.InvokeReq) (thv1.InvokeResp, error) {
	if err := herrors.Require(r.Run != "" && r.Digest != "" && r.Toolset != "" && r.Tool != "",
		"err.invalid-arg"); err != nil {
		return thv1.InvokeResp{}, err //nolint:wrapcheck // shared error form
	}
	d, err := s.admit(ctx, r.Run, r.Digest, r.Toolset, r.Tool)
	if err != nil {
		return thv1.InvokeResp{}, err
	}
	if !d.allowed {
		return thv1.InvokeResp{}, herrors.New(herrors.CodeForbidden, "err.tool-denied")
	}
	ts := registry[r.Toolset]
	res, err := toolhubExec(ctx, s,
		toolCall{ts: ts, name: r.Tool, claims: d.claims, args: r.Arguments})
	if err != nil {
		return thv1.InvokeResp{}, err
	}
	return thv1.InvokeResp{Result: res, Receipt: d.receipt}, nil
}
