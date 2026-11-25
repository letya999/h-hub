package svc

import (
	"context"
	"encoding/json"
	"fmt"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	"github.com/letya999/h-hub/shared/httpapi"
)

// spec resolves the caller's current effective spec from
// spawn-controller — the single owner of policy. A live grant change
// lands here on the next call without a restart.
func (s *Server) spec(ctx context.Context, space string) (specView, string, error) {
	tok, err := s.toks.Token(ctx, "spawn-controller")
	if err != nil {
		return specView{}, "", err
	}
	sc := &sv1.Client{T: sv1.HTTP{
		C: httpapi.NewClient(s.deps.SpawnURL, tok)}}
	res, err := sc.ResolveSpec(ctx, sv1.ResolveSpecReq{Space: space})
	if err != nil {
		return specView{}, "", fmt.Errorf("resolve spec: %w", err)
	}
	var v specView
	if err := json.Unmarshal([]byte(res.Spec), &v); err != nil {
		return specView{}, "", fmt.Errorf("spec decode: %w", err)
	}
	return v, res.Digest, nil
}

// Token issues and caches service tokens per audience.
func (t *tokenSource) Token(ctx context.Context, aud string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.cache[aud]
	if ok && c.exp > t.now().Unix()+60 {
		return c.token, nil
	}
	t.seq++
	cl := clientNoAuth(t.url)
	res, err := cl.IssueServiceToken(ctx, t.sub, aud,
		fmt.Sprintf("%s-%d-%d", t.sub, t.now().Unix(), t.seq), t.key)
	if err != nil {
		return "", fmt.Errorf("issue service token %s: %w", aud, err)
	}
	t.cache[aud] = cached{token: res.Token, exp: res.ExpiresAt}
	return res.Token, nil
}
