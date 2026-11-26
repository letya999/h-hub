// Package v1 is the spawn-controller contract: spaces built from
// layer templates, passports compiled into one effective spec with a
// digest, and the runtime lifecycle claim that precedes every
// container launch (blueprint §2.1).
package v1

import (
	"context"
	"encoding/json"
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

// SubmitLayersReq stores one immutable layer revision. Role is a
// name from the registry chain; Docs is the layer's passport
// documents as a JSON object keyed by section.
type SubmitLayersReq struct {
	Space string          `json:"space"`
	Role  string          `json:"role"`
	Docs  json.RawMessage `json:"docs"`
}

// SubmitLayersResp acknowledges the stored revision.
type SubmitLayersResp struct {
	Revision string `json:"revision"`
	Seq      int    `json:"seq"`
}

// PreviewReq compiles the current candidate set without effects.
type PreviewReq struct {
	Space string `json:"space"`
}

// LeafDiff is one leaf changed between the base and the candidate.
type LeafDiff struct {
	Path   string `json:"path"`
	Class  string `json:"class"`
	Effect string `json:"effect"`
	Layer  string `json:"layer"`
	From   string `json:"from"`
	To     string `json:"to"`
}

// PreviewResp is the dry-run result activate must be pointed at.
type PreviewResp struct {
	Base      string     `json:"base"`
	Candidate string     `json:"candidate"`
	Effect    string     `json:"effect"`
	Diff      []LeafDiff `json:"diff"`
}

// ActivateReq CAS-activates the exact candidate preview showed.
type ActivateReq struct {
	Space     string `json:"space"`
	Base      string `json:"base"`
	Candidate string `json:"candidate"`
}

// ActivateResp reports the new effective digest and applied effect.
type ActivateResp struct {
	Digest string `json:"digest"`
	Effect string `json:"effect"`
}

// RollbackReq re-activates a previously applied digest; the stored
// revision set re-passes the same compile checks.
type RollbackReq struct {
	Space  string `json:"space"`
	Digest string `json:"digest"`
}

// RollbackResp reports the restored digest and applied effect.
type RollbackResp struct {
	Digest string `json:"digest"`
	Effect string `json:"effect"`
}

// ExplainReq asks which layer supplied each leaf of the live spec.
type ExplainReq struct {
	Space string `json:"space"`
}

// ExplainResp is the persisted attribution of the active digest.
type ExplainResp struct {
	Digest    string            `json:"digest"`
	Layers    map[string]string `json:"layers"`
	Workspace []WorkspaceBranch `json:"workspace,omitempty"`
}

type WorkspaceNode struct {
	Path string `json:"path"`
	Mode string `json:"mode,omitempty"`
	Note string `json:"note,omitempty"`
}

type WorkspaceBranch struct {
	WorkspaceNode
	Children []WorkspaceBranch `json:"children,omitempty"`
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
	SubmitLayers   func(context.Context, SubmitLayersReq) (SubmitLayersResp, error)
	Preview        func(context.Context, PreviewReq) (PreviewResp, error)
	Activate       func(context.Context, ActivateReq) (ActivateResp, error)
	Rollback       func(context.Context, RollbackReq) (RollbackResp, error)
	Explain        func(context.Context, ExplainReq) (ExplainResp, error)
	Workspace      func(context.Context, WorkspaceReq) (WorkspaceResp, error)
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
		"SubmitLayers":   op(h.SubmitLayers),
		"Preview":        op(h.Preview),
		"Activate":       op(h.Activate),
		"Rollback":       op(h.Rollback),
		"Explain":        op(h.Explain),
		"Workspace":      op(h.Workspace),
		"EditConfig":     op(h.EditConfig),
		"RestartRequest": op(h.RestartRequest),
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
