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

func (c *Client) Acquire(ctx context.Context, r AcquireReq) (AcquireResp, error) {
	var out AcquireResp
	err := c.T.Call(ctx, "Acquire", r, &out)
	if err != nil {
		return out, fmt.Errorf("acquire: %w", err)
	}
	return out, nil
}

func (c *Client) Revoke(ctx context.Context, r RevokeReq) (RevokeResp, error) {
	var out RevokeResp
	err := c.T.Call(ctx, "Revoke", r, &out)
	if err != nil {
		return out, fmt.Errorf("revoke: %w", err)
	}
	return out, nil
}

func (c *Client) ListConnections(ctx context.Context, r ListConnectionsReq) (ListConnectionsResp, error) {
	var out ListConnectionsResp
	err := c.T.Call(ctx, "ListConnections", r, &out)
	if err != nil {
		return out, fmt.Errorf("list connections: %w", err)
	}
	return out, nil
}

func (c *Client) SetSecret(ctx context.Context, r SetSecretReq) (SetSecretResp, error) {
	var out SetSecretResp
	err := c.T.Call(ctx, "SetSecret", r, &out)
	if err != nil {
		return out, fmt.Errorf("set secret: %w", err)
	}
	return out, nil
}

func (c *Client) Snapshot(ctx context.Context, r SnapshotReq) (SnapshotResp, error) {
	var out SnapshotResp
	err := c.T.Call(ctx, "Snapshot", r, &out)
	if err != nil {
		return out, fmt.Errorf("snapshot: %w", err)
	}
	return out, nil
}
