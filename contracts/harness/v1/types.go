// Package v1 is the harness-runtime contract: run admission and
// cancellation (blueprint §2.11). Only task-engine calls it; every
// request carries a run token minted for the launch.
package v1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/letya999/h-hub/shared/httpapi"
)

// Attachment is one binary input part for the model; Data is base64.
type Attachment struct {
	Mime string `json:"mime"`
	Data string `json:"data"`
}

// SubmitRunReq admits a run: the admission row binds the run id, the
// job it serves and the spec digest it was launched under. Input is
// the job payload — ephemeral, never durably stored.
type SubmitRunReq struct {
	Run         string       `json:"run"`
	Job         string       `json:"job"`
	Space       string       `json:"space"`
	Input       string       `json:"input"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// SubmitRunResp acknowledges admission.
type SubmitRunResp struct {
	Accepted bool `json:"accepted"`
}

// CancelRunReq aborts an admitted run.
type CancelRunReq struct {
	Run string `json:"run"`
}

// CancelRunResp acknowledges cancellation.
type CancelRunResp struct {
	OK bool `json:"ok"`
}

type PrepareRestartReq struct {
	Space string `json:"space"`
	Key   string `json:"key"`
}

type PrepareRestartResp struct {
	Idle bool `json:"idle"`
}

// Handlers is the server-side operation table.
type Handlers struct {
	SubmitRun      func(context.Context, SubmitRunReq) (SubmitRunResp, error)
	CancelRun      func(context.Context, CancelRunReq) (CancelRunResp, error)
	PrepareRestart func(context.Context, PrepareRestartReq) (PrepareRestartResp, error)
}

// Ops maps operation names to request handlers.
func Ops(h Handlers) map[string]httpapi.Handler {
	return map[string]httpapi.Handler{
		"SubmitRun":      op(h.SubmitRun),
		"CancelRun":      op(h.CancelRun),
		"PrepareRestart": op(h.PrepareRestart),
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
