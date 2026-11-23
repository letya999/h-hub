package v1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/letya999/h-hub/shared/httpapi"
)

type Transport interface {
	Call(context.Context, string, any, any) error
}
type HTTP struct{ C *httpapi.Client }

func (t HTTP) Call(ctx context.Context, op string, req, resp any) error {
	if err := t.C.Do(ctx, http.MethodPost, "/v1/"+op, req, resp); err != nil {
		return fmt.Errorf("call %s: %w", op, err)
	}
	return nil
}

type Client struct{ T Transport }

func (c *Client) Inject(ctx context.Context, r InjectReq) (InjectResp, error) {
	var out InjectResp
	err := c.T.Call(ctx, "Inject", r, &out)
	if err != nil {
		return out, fmt.Errorf("inject: %w", err)
	}
	return out, nil
}
func (c *Client) ReadOutbox(ctx context.Context, r ReadOutboxReq) (ReadOutboxResp, error) {
	var out ReadOutboxResp
	err := c.T.Call(ctx, "ReadOutbox", r, &out)
	if err != nil {
		return out, fmt.Errorf("read outbox: %w", err)
	}
	return out, nil
}
