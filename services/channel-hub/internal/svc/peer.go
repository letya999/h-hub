package svc

import (
	"context"
	"fmt"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/httpapi"
)

func (s *Server) identityClient(ctx context.Context) (*idv1.Client, error) {
	nonce, err := randomID("nonce-")
	if err != nil {
		return nil, err
	}
	issuer := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(s.deps.IdentityURL, "")}}
	tok, err := issuer.IssueServiceToken(ctx, s.sub, "identity-hub", nonce, s.deps.SelfKey)
	if err != nil {
		return nil, fmt.Errorf("identity service token: %w", err)
	}
	return &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(s.deps.IdentityURL, tok.Token)}}, nil
}

func spawnClient(s *Server, ctx context.Context) (*sv1.Client, error) {
	id, err := s.identityClient(ctx)
	if err != nil {
		return nil, err
	}
	nonce, err := randomID("nonce-")
	if err != nil {
		return nil, err
	}
	tok, err := id.IssueServiceToken(ctx, s.sub, "spawn-controller", nonce, s.deps.SelfKey)
	if err != nil {
		return nil, fmt.Errorf("spawn service token: %w", err)
	}
	return &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(s.deps.SpawnURL, tok.Token)}}, nil
}

func (s *Server) taskClient(ctx context.Context, external string) (*tv1.Client, idv1.ResolveResp, error) {
	id, err := s.identityClient(ctx)
	if err != nil {
		return nil, idv1.ResolveResp{}, err
	}
	resolved, err := id.Resolve(ctx, idv1.ResolveReq{Channel: "mock", External: external})
	if err != nil {
		return nil, resolved, fmt.Errorf("resolve channel: %w", err)
	}
	tok, err := id.MintLinkToken(ctx, idv1.MintLinkTokenReq{Channel: "mock", External: external, Aud: "task-engine"})
	if err != nil {
		return nil, resolved, fmt.Errorf("mint link token: %w", err)
	}
	return &tv1.Client{T: tv1.HTTP{C: httpapi.NewClient(s.deps.TaskURL, tok.Token)}}, resolved, nil
}
