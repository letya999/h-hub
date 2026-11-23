package svc

import (
	"context"
	"errors"
	"slices"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	mv1 "github.com/letya999/h-hub/contracts/model/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
)

// complete is the relay: run claims bind the caller to one run, the
// effective passport picks the route, the estimate is reserved under
// both ceilings, the upstream answers, and the measured spend
// settles. No reservation, no upstream call.
func (s *Server) complete(ctx context.Context, r mv1.CompleteReq) (mv1.CompleteResp, error) {
	claims, err := s.runClaims(ctx, r.Run)
	if err != nil {
		return mv1.CompleteResp{}, err
	}
	if len(r.Request) == 0 {
		return mv1.CompleteResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	models, err := s.spec(ctx, claims.Space)
	if err != nil {
		return mv1.CompleteResp{}, err
	}
	route, err := models.route(models.Default)
	if err != nil {
		return mv1.CompleteResp{}, err
	}
	est := models.Reserve
	if err := reserve(ctx, s.db, reservation{owner: claims.Owner,
		run: claims.Run, estimate: est,
		daily: models.DailyTokens, runLimit: models.RunTokens}); err != nil {
		if errors.Is(err, errOverBudget) {
			return mv1.CompleteResp{}, herrors.New(herrors.CodeForbidden, "err.quota")
		}
		return mv1.CompleteResp{}, err
	}
	body, tokens, err := s.callUpstream(ctx, route, r.Request)
	if err != nil {
		// The reservation is returned uncharged: a failed upstream
		// consumed nothing. The sweep still covers a lost process.
		if serr := settle(ctx, s.db, settlement{owner: claims.Owner,
			run: claims.Run, route: route.Name, reserved: est}); serr != nil && s.l != nil {
			s.l.Error(ctx, "settle after upstream failure", serr)
		}
		return mv1.CompleteResp{}, herrors.Wrap(herrors.CodeUnavailable, "err.unavailable", err)
	}
	charged, measured := tokens, true
	if charged <= 0 {
		charged, measured = est, false
	}
	if err := settle(ctx, s.db, settlement{owner: claims.Owner,
		run: claims.Run, route: route.Name, reserved: est,
		charged: charged, measured: measured}); err != nil {
		return mv1.CompleteResp{}, err
	}
	return mv1.CompleteResp{Response: body}, nil
}

// runClaims binds the run token to the request: the token must name
// this run and carry model-gateway in its audience.
func (s *Server) runClaims(ctx context.Context, run string) (idv1.RunClaims, error) {
	p := httpapi.PrincipalFrom(ctx)
	if p == nil {
		return idv1.RunClaims{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	if !slices.Contains(p.Aud, s.sub) {
		return idv1.RunClaims{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	c, err := idv1.ParseRunClaims(p.Payload)
	if err != nil || c.Run != run {
		return idv1.RunClaims{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	return c, nil
}
