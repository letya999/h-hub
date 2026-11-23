package svc

import (
	"context"
	"fmt"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	mv1 "github.com/letya999/h-hub/contracts/model/v1"
)

// usage reports the caller's spend for today against the passport
// ceilings — the /usage channel command reads exactly this.
func (s *Server) usage(ctx context.Context, _ mv1.UsageReq) (mv1.UsageResp, error) {
	scope, err := idv1.ScopeFrom(ctx)
	if err != nil {
		return mv1.UsageResp{}, fmt.Errorf("scope: %w", err)
	}
	models, err := s.spec(ctx, scope.Space)
	if err != nil {
		return mv1.UsageResp{}, err
	}
	spent, reserved, err := usageToday(ctx, s.db, scope.Owner)
	if err != nil {
		return mv1.UsageResp{}, err
	}
	return mv1.UsageResp{
		Spent: spent, Reserved: reserved,
		DailyLimit: models.DailyTokens, RunLimit: models.RunTokens,
	}, nil
}

// routes lists the selectable routes and the active default, both
// from the caller's effective passport.
func (s *Server) routes(ctx context.Context, _ mv1.RoutesReq) (mv1.RoutesResp, error) {
	scope, err := idv1.ScopeFrom(ctx)
	if err != nil {
		return mv1.RoutesResp{}, fmt.Errorf("scope: %w", err)
	}
	models, err := s.spec(ctx, scope.Space)
	if err != nil {
		return mv1.RoutesResp{}, err
	}
	out := mv1.RoutesResp{Default: models.Default}
	for _, r := range models.Routes {
		out.Routes = append(out.Routes, mv1.RouteInfo{Name: r.Name, Model: r.Model})
	}
	return out, nil
}
