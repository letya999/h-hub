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

// CreateJobReq submits a job. Idem is the caller's idempotency key:
// replaying it returns the existing job instead of a duplicate.
type CreateJobReq struct {
	Owner        string `json:"owner"`
	Space        string `json:"space"`
	Conversation string `json:"conversation"`
	Input        string `json:"input"`
	Idem         string `json:"idem"`
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
	Created string `json:"created"`
}

// GetJobReq reads one job scoped to its owner.
type GetJobReq struct {
	Owner string `json:"owner"`
	ID    string `json:"id"`
}

// GetJobResp is the job read result.
type GetJobResp struct {
	Job JobInfo `json:"job"`
}

// ListJobsReq lists an owner's jobs.
type ListJobsReq struct {
	Owner string `json:"owner"`
}

// ListJobsResp is the listing result.
type ListJobsResp struct {
	Jobs []JobInfo `json:"jobs"`
}

// JobReq is the shared shape of single-job operator actions
// (cancel, retry, unquarantine).
type JobReq struct {
	Owner string `json:"owner"`
	ID    string `json:"id"`
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

// ClaimNextReq asks the dispatcher to claim one due job.
type ClaimNextReq struct{}

// ClaimNextResp names the claimed job (empty when none was due).
type ClaimNextResp struct {
	Job string `json:"job"`
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
	ClaimNext       func(context.Context, ClaimNextReq) (ClaimNextResp, error)
	ReportRun       func(context.Context, ReportRunReq) (struct{}, error)
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
		"ClaimNext":       op(h.ClaimNext),
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
