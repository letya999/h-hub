// Package v1 is the spawn-controller contract: spaces built from
// layer templates, passports compiled into one effective spec with a
// digest, and the runtime lifecycle claim that precedes every
// container launch (blueprint §2.1).
package v1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/letya999/h-hub/shared/httpapi"
)

// CreateSpaceReq provisions a space from a layer template. Phase 3 has
// one built-in template; Name selects it.
type CreateSpaceReq struct {
	Name     string `json:"name"`
	Template string `json:"template"`
}

// CreateSpaceResp acknowledges a space.
type CreateSpaceResp struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

// SpaceInfo describes a space and its live runtime.
type SpaceInfo struct {
	ID       string `json:"id"`
	Owner    string `json:"owner"`
	Name     string `json:"name"`
	Digest   string `json:"digest"`
	Runtime  string `json:"runtime"`
	Endpoint string `json:"endpoint"`
}

// GetSpaceReq reads one space scoped to its owner.
type GetSpaceReq struct {
	ID string `json:"id"`
}

// GetSpaceResp is the space read result.
type GetSpaceResp struct {
	Space SpaceInfo `json:"space"`
}

// DestroySpaceReq removes a space and drains its runtime.
type DestroySpaceReq struct {
	ID string `json:"id"`
}

// DestroySpaceResp acknowledges destruction.
type DestroySpaceResp struct {
	OK bool `json:"ok"`
}

// EnsureRuntimeReq asks for a running runtime for a space; the digest
// binds the claim to the spec revision the caller holds.
type EnsureRuntimeReq struct {
	Space  string `json:"space"`
	Digest string `json:"digest"`
}

// EnsureRuntimeResp returns the runtime endpoint once it is warm.
type EnsureRuntimeResp struct {
	Runtime  string `json:"runtime"`
	Endpoint string `json:"endpoint"`
	Digest   string `json:"digest"`
	Ready    bool   `json:"ready"`
}

// ResolveSpecReq compiles (or reads the persisted) effective spec.
type ResolveSpecReq struct {
	Space string `json:"space"`
}

// ResolveSpecResp carries the spec bytes and their digest.
type ResolveSpecResp struct {
	Spec   string `json:"spec"`
	Digest string `json:"digest"`
	Owner  string `json:"owner"`
}

// ReleaseRuntimeReq marks a runtime free after a run settles.
type ReleaseRuntimeReq struct {
	Runtime string `json:"runtime"`
}

// ReleaseRuntimeResp acknowledges the release.
type ReleaseRuntimeResp struct {
	OK bool `json:"ok"`
}

// DrainRuntimeReq asks to stop the runtime of one owned space.
type DrainRuntimeReq struct {
	Space string `json:"space"`
}

// DrainRuntimeResp reports the state requested by the operator.
type DrainRuntimeResp struct {
	State string `json:"state"`
}

// WatchdogStatusReq reads the spawn watchdog without changing it.
type WatchdogStatusReq struct{}

// WatchdogStatusResp identifies one stale tick for deduplicated alerts.
type WatchdogStatusResp struct {
	Stale bool   `json:"stale"`
	Tick  string `json:"tick"`
}

// SetModelReq switches the space's default model route to one of the
// routes declared in its effective models passport.
type SetModelReq struct {
	Space   string `json:"space"`
	Default string `json:"default"`
}

// SetModelResp reports the recompiled spec's digest.
type SetModelResp struct {
	Digest string `json:"digest"`
}

// Handlers is the server-side operation table.
type Handlers struct {
	CreateSpace    func(context.Context, CreateSpaceReq) (CreateSpaceResp, error)
	GetSpace       func(context.Context, GetSpaceReq) (GetSpaceResp, error)
	DestroySpace   func(context.Context, DestroySpaceReq) (DestroySpaceResp, error)
	EnsureRuntime  func(context.Context, EnsureRuntimeReq) (EnsureRuntimeResp, error)
	ResolveSpec    func(context.Context, ResolveSpecReq) (ResolveSpecResp, error)
	ReleaseRuntime func(context.Context, ReleaseRuntimeReq) (ReleaseRuntimeResp, error)
	DrainRuntime   func(context.Context, DrainRuntimeReq) (DrainRuntimeResp, error)
	WatchdogStatus func(context.Context, WatchdogStatusReq) (WatchdogStatusResp, error)
	SetModel       func(context.Context, SetModelReq) (SetModelResp, error)
}

// Ops maps operation names to request handlers.
func Ops(h Handlers) map[string]httpapi.Handler {
	return map[string]httpapi.Handler{
		"CreateSpace":    op(h.CreateSpace),
		"GetSpace":       op(h.GetSpace),
		"DestroySpace":   op(h.DestroySpace),
		"EnsureRuntime":  op(h.EnsureRuntime),
		"ResolveSpec":    op(h.ResolveSpec),
		"ReleaseRuntime": op(h.ReleaseRuntime),
		"DrainRuntime":   op(h.DrainRuntime),
		"WatchdogStatus": op(h.WatchdogStatus),
		"SetModel":       op(h.SetModel),
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
