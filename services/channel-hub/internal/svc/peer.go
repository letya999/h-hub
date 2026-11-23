package svc

import (
	"context"
	"fmt"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	mv1 "github.com/letya999/h-hub/contracts/model/v1"
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

// linkToken mints an owner-scoped link token for one callee audience;
// the same delegation channel-hub uses for job ops now covers the
// model ops declared for the space.
func (s *commandOps) linkToken(ctx context.Context, external, aud string) (string, error) {
	id, err := s.identityClient(ctx)
	if err != nil {
		return "", err
	}
	tok, err := id.MintLinkToken(ctx, idv1.MintLinkTokenReq{
		Channel: "mock", External: external, Aud: aud})
	if err != nil {
		return "", fmt.Errorf("mint link token %s: %w", aud, err)
	}
	return tok.Token, nil
}

func (s *commandOps) modelClient(ctx context.Context, external string) (*mv1.Client, error) {
	tok, err := s.linkToken(ctx, external, "model-gateway")
	if err != nil {
		return nil, err
	}
	return &mv1.Client{T: mv1.HTTP{C: httpapi.NewClient(s.deps.ModelURL, tok)}}, nil
}

func (s *commandOps) spawnOwnerClient(ctx context.Context, external string) (*sv1.Client, error) {
	tok, err := s.linkToken(ctx, external, "spawn-controller")
	if err != nil {
		return nil, err
	}
	return &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(s.deps.SpawnURL, tok)}}, nil
}
