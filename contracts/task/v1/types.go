// Package v1 is the task-engine contract: the durable job queue and
// the run event journal (blueprint §2.3). Channels submit jobs; the
// runtime reports events; operators drive job actions.
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

// CreateJobReq submits a job. Idem is the caller's idempotency key:
// replaying it returns the existing job instead of a duplicate.
type CreateJobReq struct {
	Space        string       `json:"space"`
	Conversation string       `json:"conversation"`
	Input        string       `json:"input"`
	Idem         string       `json:"idem"`
	Attachments  []Attachment `json:"attachments,omitempty"`
}

// CreateJobResp acknowledges a job.
type CreateJobResp struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

// JobInfo is one job in a listing.
type JobInfo struct {
	ID      string `json:"id"`
	Space   string `json:"space"`
	State   string `json:"state"`
	Result  string `json:"result"`
	Done    bool   `json:"done"`
	Created string `json:"created"`
}

// GetJobReq reads one job scoped to its owner.
type GetJobReq struct {
	ID string `json:"id"`
}

// GetJobResp is the job read result.
type GetJobResp struct {
	Job JobInfo `json:"job"`
}

// ListJobsReq lists an owner's jobs.
type ListJobsReq struct{}

// ListJobsResp is the listing result.
type ListJobsResp struct {
	Jobs []JobInfo `json:"jobs"`
}

// JobReq is the shared shape of single-job operator actions
// (cancel, retry, unquarantine).
type JobReq struct {
	ID string `json:"id"`
}

// SetHoldReq toggles an administrative hold on a space.
type SetHoldReq struct {
	Space  string `json:"space"`
	Active bool   `json:"active"`
	Note   string `json:"note"`
}

// SetHoldResp returns the new hold epoch.
type SetHoldResp struct {
	Epoch int64 `json:"epoch"`
}

// RunEvent is one append to the run journal (seq orders within a run).
type RunEvent struct {
	Seq  int64  `json:"seq"`
	Kind string `json:"kind"`
	Data string `json:"data"`
}

// ReportRunReq appends events to a run (run-token authenticated).
type ReportRunReq struct {
	Run    string     `json:"run"`
	Events []RunEvent `json:"events"`
}

// ReportRunResp identifies the bound conversation when a start is acknowledged.
type ReportRunResp struct {
	ConversationKey string `json:"conversation_key,omitempty"`
}

// GetRunReq reads run events after a cursor position.
type GetRunReq struct {
	Run  string `json:"run"`
	From int64  `json:"from"`
}

// GetRunResp returns the journal slice and run state.
type GetRunResp struct {
	State  string     `json:"state"`
	Events []RunEvent `json:"events"`
}

// Handlers is the server-side operation table.
type Handlers struct {
	CreateJob       func(context.Context, CreateJobReq) (CreateJobResp, error)
	GetJob          func(context.Context, GetJobReq) (GetJobResp, error)
	ListJobs        func(context.Context, ListJobsReq) (ListJobsResp, error)
	CancelJob       func(context.Context, JobReq) (JobInfo, error)
	RetryJob        func(context.Context, JobReq) (JobInfo, error)
	UnquarantineJob func(context.Context, JobReq) (JobInfo, error)
	SetHold         func(context.Context, SetHoldReq) (SetHoldResp, error)
	ReportRun       func(context.Context, ReportRunReq) (ReportRunResp, error)
	GetRun          func(context.Context, GetRunReq) (GetRunResp, error)
}

// Ops maps operation names to request handlers.
func Ops(h Handlers) map[string]httpapi.Handler {
	return map[string]httpapi.Handler{
		"CreateJob":       op(h.CreateJob),
		"GetJob":          op(h.GetJob),
		"ListJobs":        op(h.ListJobs),
		"CancelJob":       op(h.CancelJob),
		"RetryJob":        op(h.RetryJob),
		"UnquarantineJob": op(h.UnquarantineJob),
		"SetHold":         op(h.SetHold),
		"ReportRun":       op(h.ReportRun),
		"GetRun":          op(h.GetRun),
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
