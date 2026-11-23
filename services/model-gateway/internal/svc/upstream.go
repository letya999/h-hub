package svc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// upstreamTimeout bounds one provider call; the caller's own deadline
// usually arrives first and cancels the hop.
const upstreamTimeout = 300 * time.Second

// upstreamBody is the OpenAI surface the gateway rewrites before
// relaying: the route pins the model name, never the caller's body.
func rewriteModel(body []byte, model string) ([]byte, error) {
	if model == "" {
		return body, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("request decode: %w", err)
	}
	raw, err := json.Marshal(model)
	if err != nil {
		return nil, fmt.Errorf("model encode: %w", err)
	}
	doc["model"] = raw
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("request encode: %w", err)
	}
	return out, nil
}

// routeKey reads the bearer credential for a route from the keys
// volume; the file is the only place the secret ever exists (never
// env, never logs, never a dump). An absent file refuses the call
// before it can hit an unauthenticated upstream. The name is a
// basename inside routes/ — a spec cannot reach outside it.
func (s *Server) routeKey(route routeView) (string, error) {
	if route.KeyFile == "" {
		return "", nil
	}
	name := filepath.Base(route.KeyFile)
	if name != route.KeyFile || name == "." || name == ".." {
		return "", fmt.Errorf("route key %q: invalid name", route.KeyFile)
	}
	data, err := os.ReadFile(filepath.Join(s.deps.KeysDir, "routes", name))
	if err != nil {
		return "", fmt.Errorf("route key %s: %w", name, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// callUpstream posts the rewritten body to the route and returns the
// verbatim response plus the measured token spend — zero when the
// upstream reports no usage and the estimate is charged instead.
func (s *Server) callUpstream(ctx context.Context, route routeView,
	body []byte) (resp []byte, tokens int64, err error) {
	out, err := rewriteModel(body, route.Model)
	if err != nil {
		return nil, 0, err
	}
	key, err := s.routeKey(route)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		route.URL+"/v1/chat/completions", bytes.NewReader(out))
	if err != nil {
		return nil, 0, fmt.Errorf("upstream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	hc := &http.Client{Timeout: upstreamTimeout}
	res, err := hc.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("upstream call: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort close
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("upstream read: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("upstream status %d", res.StatusCode)
	}
	return data, parseUsage(data), nil
}

// parseUsage reads the OpenAI usage block; absence means the caller
// is charged at the estimate (measured stays false upstream).
func parseUsage(body []byte) int64 {
	var doc struct {
		Usage struct {
			Prompt     int64 `json:"prompt_tokens"`
			Completion int64 `json:"completion_tokens"`
			Total      int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return 0
	}
	if doc.Usage.Total > 0 {
		return doc.Usage.Total
	}
	return doc.Usage.Prompt + doc.Usage.Completion
}
