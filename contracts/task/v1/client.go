package v1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/letya999/h-hub/shared/httpapi"
)

// Transport is the call plumbing below the typed client.
type Transport interface {
	Call(ctx context.Context, op string, req, resp any) error
}

// HTTP calls a peer over the shared client.
type HTTP struct{ C *httpapi.Client }

// Call posts one operation request.
func (t HTTP) Call(ctx context.Context, op string, req, resp any) error {
	err := t.C.Do(ctx, http.MethodPost, "/v1/"+op, req, resp)
	if err != nil {
		return fmt.Errorf("call %s: %w", op, err)
	}
	return nil
}

// Client is the typed caller surface of the contract.
type Client struct{ T Transport }

// CreateJob submits a job.
func (c *Client) CreateJob(ctx context.Context, r CreateJobReq) (CreateJobResp, error) {
	var out CreateJobResp
	err := c.call(ctx, "CreateJob", r, &out)
	return out, err
}

// GetJob reads one job.
func (c *Client) GetJob(ctx context.Context, r GetJobReq) (GetJobResp, error) {
	var out GetJobResp
	err := c.call(ctx, "GetJob", r, &out)
	return out, err
}

// ListJobs lists an owner's jobs.
func (c *Client) ListJobs(ctx context.Context, r ListJobsReq) (ListJobsResp, error) {
	var out ListJobsResp
	err := c.call(ctx, "ListJobs", r, &out)
	return out, err
}

// CancelJob cancels a non-final job.
func (c *Client) CancelJob(ctx context.Context, r JobReq) (JobInfo, error) {
	var out JobInfo
	err := c.call(ctx, "CancelJob", r, &out)
	return out, err
}

// RetryJob requeues a failed/uncertain/waiting job (operator action).
func (c *Client) RetryJob(ctx context.Context, r JobReq) (JobInfo, error) {
	var out JobInfo
	err := c.call(ctx, "RetryJob", r, &out)
	return out, err
}

// UnquarantineJob releases a quarantined job back to the queue.
func (c *Client) UnquarantineJob(ctx context.Context, r JobReq) (JobInfo, error) {
	var out JobInfo
	err := c.call(ctx, "UnquarantineJob", r, &out)
	return out, err
}

// SetHold toggles an admin hold on a space.
func (c *Client) SetHold(ctx context.Context, r SetHoldReq) (SetHoldResp, error) {
	var out SetHoldResp
	err := c.call(ctx, "SetHold", r, &out)
	return out, err
}

// ReportRun appends run events (run-token authenticated).
func (c *Client) ReportRun(ctx context.Context, r ReportRunReq) error {
	return c.call(ctx, "ReportRun", r, nil)
}

// ReportRunReceipt returns the binding from the same authenticated journal operation.
func (c *Client) ReportRunReceipt(ctx context.Context, r ReportRunReq) (ReportRunResp, error) {
	var out ReportRunResp
	err := c.call(ctx, "ReportRun", r, &out)
	return out, err
}

// GetRun reads the run journal after a cursor.
func (c *Client) GetRun(ctx context.Context, r GetRunReq) (GetRunResp, error) {
	var out GetRunResp
	err := c.call(ctx, "GetRun", r, &out)
	return out, err
}

// call routes one op through the transport and wraps transport
// failures once, keeping the shared error form in the chain.
func (c *Client) call(ctx context.Context, op string, req, resp any) error {
	if err := c.T.Call(ctx, op, req, resp); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}
