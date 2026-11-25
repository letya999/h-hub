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

// CreateRequest opens one request against its addressee.
func (c *Client) CreateRequest(ctx context.Context, r CreateReq) (CreateResp, error) {
	var out CreateResp
	err := c.call(ctx, "CreateRequest", r, &out)
	return out, err
}

// AnswerRequest applies the addressee's answer exactly once.
func (c *Client) AnswerRequest(ctx context.Context, r AnswerReq) (AnswerResp, error) {
	var out AnswerResp
	err := c.call(ctx, "AnswerRequest", r, &out)
	return out, err
}

// GetRequest reads one request's state.
func (c *Client) GetRequest(ctx context.Context, r GetReq) (GetResp, error) {
	var out GetResp
	err := c.call(ctx, "GetRequest", r, &out)
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
