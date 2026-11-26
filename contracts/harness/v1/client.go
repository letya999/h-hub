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

// SubmitRun admits a run.
func (c *Client) SubmitRun(ctx context.Context, r SubmitRunReq) (SubmitRunResp, error) {
	var out SubmitRunResp
	err := c.call(ctx, "SubmitRun", r, &out)
	return out, err
}

// CancelRun aborts an admitted run.
func (c *Client) CancelRun(ctx context.Context, r CancelRunReq) (CancelRunResp, error) {
	var out CancelRunResp
	err := c.call(ctx, "CancelRun", r, &out)
	return out, err
}

func (c *Client) PrepareRestart(ctx context.Context, r PrepareRestartReq) (PrepareRestartResp, error) {
	var out PrepareRestartResp
	err := c.call(ctx, "PrepareRestart", r, &out)
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
