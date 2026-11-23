// Package v1 is the model-gateway contract: an OpenAI-compatible
// relay with per-user spend accounting. Runtimes call Complete with a
// run token; channels read Usage and Routes with an owner token
// (blueprint §2.14).
package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/letya999/h-hub/shared/httpapi"
)

// CompleteReq relays one OpenAI chat-completions request. Request is
// the verbatim upstream body; the gateway selects the upstream from
// the caller's effective models passport and may rewrite "model".
type CompleteReq struct {
	Run     string          `json:"run"`
	Request json.RawMessage `json:"request"`
}

// CompleteResp is the verbatim upstream response body.
type CompleteResp struct {
	Response json.RawMessage `json:"response"`
}

// UsageReq reads the caller's spend counters for today.
type UsageReq struct{}

// UsageResp reports today's charged and reserved spend against the
// ceilings of the effective models passport. Units are tokens.
type UsageResp struct {
	Spent      int64 `json:"spent"`
	Reserved   int64 `json:"reserved"`
	DailyLimit int64 `json:"daily_limit"`
	RunLimit   int64 `json:"run_limit"`
}

// RoutesReq lists the routes of the caller's effective passport.
type RoutesReq struct{}

// RouteInfo is one named model route.
type RouteInfo struct {
	Name  string `json:"name"`
	Model string `json:"model"`
}

// RoutesResp names the active default and every selectable route.
type RoutesResp struct {
	Default string      `json:"default"`
	Routes  []RouteInfo `json:"routes"`
}

// Handlers is the server-side operation table.
type Handlers struct {
	Complete func(context.Context, CompleteReq) (CompleteResp, error)
	Usage    func(context.Context, UsageReq) (UsageResp, error)
	Routes   func(context.Context, RoutesReq) (RoutesResp, error)
}

// Ops maps operation names to request handlers.
func Ops(h Handlers) map[string]httpapi.Handler {
	return map[string]httpapi.Handler{
		"Complete": op(h.Complete),
		"Usage":    op(h.Usage),
		"Routes":   op(h.Routes),
	}
}

func op[Req, Resp any](fn func(context.Context, Req) (Resp, error)) httpapi.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req Req
		if err := httpapi.Decode(r, &req); err != nil {
			return fmt.Errorf("decode: %w", err)
		}
		resp, err := fn(r.Context(), req)
		if err != nil {
			return err
		}
		httpapi.WriteJSON(w, resp)
		return nil
	}
}
