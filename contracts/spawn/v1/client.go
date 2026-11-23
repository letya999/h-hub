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

// CreateSpace provisions a space from a template.
func (c *Client) CreateSpace(ctx context.Context, r CreateSpaceReq) (CreateSpaceResp, error) {
	var out CreateSpaceResp
	err := c.call(ctx, "CreateSpace", r, &out)
	return out, err
}

// GetSpace reads one space.
func (c *Client) GetSpace(ctx context.Context, r GetSpaceReq) (GetSpaceResp, error) {
	var out GetSpaceResp
	err := c.call(ctx, "GetSpace", r, &out)
	return out, err
}

// DestroySpace removes a space.
func (c *Client) DestroySpace(ctx context.Context, r DestroySpaceReq) (DestroySpaceResp, error) {
	var out DestroySpaceResp
	err := c.call(ctx, "DestroySpace", r, &out)
	return out, err
}

// EnsureRuntime claims a warm runtime for a space.
func (c *Client) EnsureRuntime(ctx context.Context, r EnsureRuntimeReq) (EnsureRuntimeResp, error) {
	var out EnsureRuntimeResp
	err := c.call(ctx, "EnsureRuntime", r, &out)
	return out, err
}

// ResolveSpec reads the persisted effective spec.
func (c *Client) ResolveSpec(ctx context.Context, r ResolveSpecReq) (ResolveSpecResp, error) {
	var out ResolveSpecResp
	err := c.call(ctx, "ResolveSpec", r, &out)
	return out, err
}

// ReleaseRuntime marks a runtime free after a run.
func (c *Client) ReleaseRuntime(ctx context.Context, r ReleaseRuntimeReq) (ReleaseRuntimeResp, error) {
	var out ReleaseRuntimeResp
	err := c.call(ctx, "ReleaseRuntime", r, &out)
	return out, err
}

// DrainRuntime requests removal of one owned space's runtime.
func (c *Client) DrainRuntime(ctx context.Context, r DrainRuntimeReq) (DrainRuntimeResp, error) {
	var out DrainRuntimeResp
	err := c.call(ctx, "DrainRuntime", r, &out)
	return out, err
}

// WatchdogStatus reads the last spawn watchdog tick.
func (c *Client) WatchdogStatus(ctx context.Context, r WatchdogStatusReq) (WatchdogStatusResp, error) {
	var out WatchdogStatusResp
	err := c.call(ctx, "WatchdogStatus", r, &out)
	return out, err
}

// SetModel switches the space's default model route.
func (c *Client) SetModel(ctx context.Context, r SetModelReq) (SetModelResp, error) {
	var out SetModelResp
	err := c.call(ctx, "SetModel", r, &out)
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
