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

// Complete relays one OpenAI request through the gateway.
func (c *Client) Complete(ctx context.Context, r CompleteReq) (CompleteResp, error) {
	var out CompleteResp
	err := c.call(ctx, "Complete", r, &out)
	return out, err
}

// Usage reads the caller's spend counters.
func (c *Client) Usage(ctx context.Context, r UsageReq) (UsageResp, error) {
	var out UsageResp
	err := c.call(ctx, "Usage", r, &out)
	return out, err
}

// Routes lists the caller's selectable model routes.
func (c *Client) Routes(ctx context.Context, r RoutesReq) (RoutesResp, error) {
	var out RoutesResp
	err := c.call(ctx, "Routes", r, &out)
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
