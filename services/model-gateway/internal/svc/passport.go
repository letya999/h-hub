package svc

import (
	"context"
	"encoding/json"
	"fmt"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
)

// specView decodes the models block of the effective spec owned by
// spawn-controller. The gateway resolves it per request — no local
// policy copy is kept (rule 4).
type specView struct {
	Models modelsView `json:"models"`
}

type modelsView struct {
	Default     string      `json:"default"`
	Routes      []routeView `json:"routes"`
	DailyTokens int64       `json:"daily_tokens"`
	RunTokens   int64       `json:"run_tokens"`
	Reserve     int64       `json:"reserve"`
}

type routeView struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Model  string `json:"model"`
	KeyRef string `json:"key_ref"`
	Vision bool   `json:"vision"`
}

func (m modelsView) route(name string) (routeView, error) {
	for _, r := range m.Routes {
		if r.Name == name {
			if r.URL == "" {
				return r, herrors.New(herrors.CodeInvalid, "err.invalid-arg")
			}
			return r, nil
		}
	}
	return routeView{}, herrors.New(herrors.CodeNotFound, "err.not-found").With("route", name) //nolint:wrapcheck // shared error form
}

// spec resolves the caller's effective models passport from
// spawn-controller — the single owner of policy.
func (s *Server) spec(ctx context.Context, space string) (modelsView, error) {
	tok, err := s.toks.token(ctx, "spawn-controller")
	if err != nil {
		return modelsView{}, err
	}
	sc := &sv1.Client{T: sv1.HTTP{
		C: httpapi.NewClient(s.deps.SpawnURL, tok)}}
	res, err := sc.ResolveSpec(ctx, sv1.ResolveSpecReq{Space: space})
	if err != nil {
		return modelsView{}, fmt.Errorf("resolve spec: %w", err)
	}
	var v specView
	if err := json.Unmarshal([]byte(res.Spec), &v); err != nil {
		return modelsView{}, fmt.Errorf("spec decode: %w", err)
	}
	if err := herrors.Require(v.Models.Reserve > 0 &&
		len(v.Models.Routes) > 0, "err.invalid-arg"); err != nil {
		return modelsView{}, err //nolint:wrapcheck // shared error form
	}
	return v.Models, nil
}

// token issues and caches service tokens per audience.
func (t *tokenSource) token(ctx context.Context, aud string) (string, error) {
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
