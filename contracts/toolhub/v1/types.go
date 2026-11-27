// Package v1 is the tool-hub contract: the admission point for tool
// calls. A runtime calls AdmitCall before invoking, Projection to
// learn which tools the model may see for a spec digest, and Invoke
// to run an admitted call through the executor (blueprint §2.5).
package v1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
)

// Receipt is the admission receipt: proof that a decision was made,
// which capabilities were checked and when.
type Receipt struct {
	ID   string    `json:"id"`
	At   time.Time `json:"at"`
	Caps []string  `json:"caps"`
}

// ToolInfo is one tool as the model sees it: name, human text and
// the argument schema.
type ToolInfo struct {
	Toolset     string          `json:"toolset"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// AdmitCallReq asks whether one call is allowed for the caller's
// effective spec. Digest binds the decision to one activation.
type AdmitCallReq struct {
	Run     string `json:"run"`
	Digest  string `json:"digest"`
	Toolset string `json:"toolset"`
	Tool    string `json:"tool"`
}

// AdmitCallResp is the decision plus its receipt. A denial is a
// normal answer, not a transport error: Allowed false with Reason
// naming the refusal.
type AdmitCallResp struct {
	Allowed bool    `json:"allowed"`
	Reason  string  `json:"reason,omitempty"`
	Receipt Receipt `json:"receipt"`
}

// ProjectionReq asks for the tools the model may see under digest.
type ProjectionReq struct {
	Run    string `json:"run,omitempty"`
	Space  string `json:"space,omitempty"` // service-authorized container preparation
	Digest string `json:"digest"`
}

// Validate requires exactly one scope for a projection.
func (r ProjectionReq) Validate() error {
	if r.Run != "" && r.Space != "" {
		return herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	return herrors.Require(r.Digest != "" && (r.Run != "" || r.Space != ""), "err.invalid-arg") //nolint:wrapcheck // contract error
}

// EmptyProjection preserves the reason for a tools-only refusal across HTTP.
func EmptyProjection() *herrors.Error {
	return herrors.New(herrors.CodeForbidden, "err.tool-projection-empty").With("empty_projection", true)
}

func IsEmptyProjection(err error) bool {
	var e *herrors.Error
	return errors.As(err, &e) && e.Code == herrors.CodeForbidden && e.Details["empty_projection"] == true
}

// ProjectionResp lists the projected tools in the two sections of
// the tools passport, plus the spec digest they were computed from —
// a run can see its spec moved under it. An empty projection is
// refused by the service rather than answered with two empty lists.
type ProjectionResp struct {
	Native   []string   `json:"native,omitempty"`
	Catalog  []ToolInfo `json:"catalog,omitempty"` // selected definitions for the host renderer
	Digest   string     `json:"digest"`
	Core     []ToolInfo `json:"core"`
	Extended []ToolInfo `json:"extended"`
}

// InvokeReq runs one call through the executor after admission.
// Arguments is the verbatim tool-call argument object.
type InvokeReq struct {
	Run       string          `json:"run"`
	Digest    string          `json:"digest"`
	Toolset   string          `json:"toolset"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Key       string          `json:"key,omitempty"`
}

// InvokeResp is the executor result plus the admission receipt.
type InvokeResp struct {
	Result  json.RawMessage `json:"result"`
	Receipt Receipt         `json:"receipt"`
}

// Handlers is the server-side operation table.
type Handlers struct {
	AdmitCall        func(context.Context, AdmitCallReq) (AdmitCallResp, error)
	Projection       func(context.Context, ProjectionReq) (ProjectionResp, error)
	SkillPrompt      func(context.Context, SkillPromptReq) (SkillPromptResp, error)
	Invoke           func(context.Context, InvokeReq) (InvokeResp, error)
	NotifyProjection func(context.Context, sv1.RestartReq) (sv1.RestartResp, error)
}

// Ops maps operation names to request handlers.
func Ops(h Handlers) map[string]httpapi.Handler {
	return map[string]httpapi.Handler{
		"AdmitCall":        op(h.AdmitCall),
		"Projection":       op(h.Projection),
		"SkillPrompt":      op(h.SkillPrompt),
		"Invoke":           op(h.Invoke),
		"NotifyProjection": op(h.NotifyProjection),
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
