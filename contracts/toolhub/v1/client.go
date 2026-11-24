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

// AdmitCall asks whether one tool call is allowed.
func (c *Client) AdmitCall(ctx context.Context, r AdmitCallReq) (AdmitCallResp, error) {
	var out AdmitCallResp
	err := c.call(ctx, "AdmitCall", r, &out)
	return out, err
}

// Projection lists the tools visible for the spec digest.
func (c *Client) Projection(ctx context.Context, r ProjectionReq) (ProjectionResp, error) {
	var out ProjectionResp
	err := c.call(ctx, "Projection", r, &out)
	return out, err
}

// Invoke runs one admitted call through the executor.
func (c *Client) Invoke(ctx context.Context, r InvokeReq) (InvokeResp, error) {
	var out InvokeResp
	err := c.call(ctx, "Invoke", r, &out)
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
