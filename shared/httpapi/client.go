package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// Client is the shared outbound HTTP plumbing: bearer token, default
// timeout, size limits, and error-form decoding.
type Client struct {
	base  string
	token string
	hc    *http.Client
}

// NewClient builds a client with the shared default timeout.
func NewClient(base, token string) *Client {
	return NewClientTimeout(base, token, 5*time.Second)
}

// NewClientTimeout is NewClient with a caller-chosen timeout — model
// calls can legitimately take minutes, unlike contract ops.
func NewClientTimeout(base, token string, d time.Duration) *Client {
	return &Client{
		base:  base,
		token: token,
		hc:    &http.Client{Timeout: d},
	}
}

// Call describes one JSON request/response exchange.
type Call struct {
	Method string
	Path   string
	Req    any
	Resp   any
	Limit  int64
}

// Do performs a JSON request with the default body limit.
func (c *Client) Do(ctx context.Context, method, path string, req, resp any) error {
	return c.DoLimit(ctx, Call{
		Method: method, Path: path, Req: req, Resp: resp, Limit: MaxBody,
	})
}

// DoLimit is Do with an explicit response size limit.
func (c *Client) DoLimit(ctx context.Context, call Call) error {
	body, err := marshal(call.Req)
	if err != nil {
		return err
	}
	r, err := http.NewRequestWithContext(ctx, call.Method, c.base+call.Path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	r.Header.Set("Authorization", "Bearer "+c.token)
	r.Header.Set("Content-Type", "application/json")
	res, err := c.hc.Do(r)
	if err != nil {
		return herrors.Wrap(herrors.CodeUnavailable, "err.unavailable", err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort close
	return c.readResult(res, call.Resp, call.Limit)
}

func marshal(req any) ([]byte, error) {
	if req == nil {
		return nil, nil
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	return b, nil
}

func (c *Client) readResult(res *http.Response, resp any, limit int64) error {
	if res.StatusCode >= 400 {
		return decodeError(res)
	}
	if resp == nil {
		if _, err := io.Copy(io.Discard, io.LimitReader(res.Body, limit)); err != nil {
			return fmt.Errorf("drain: %w", err)
		}
		return nil
	}
	dec := json.NewDecoder(io.LimitReader(res.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(resp); err != nil {
		return herrors.Wrap(herrors.CodeUnavailable, "err.decode", err)
	}
	return nil
}

func decodeError(res *http.Response) error {
	var env errorEnvelope
	dec := json.NewDecoder(io.LimitReader(res.Body, MaxErrorBody))
	if err := dec.Decode(&env); err != nil {
		return herrors.New(herrors.CodeUnavailable, "err.unavailable")
	}
	e := herrors.New(herrors.Code(env.Err.Code), env.Err.Message)
	e.Retryable = env.Err.Retryable
	e.Details = env.Err.Details
	e.Args = env.Err.Args
	return e
}

// Copy is the in-process parity primitive: a JSON round-trip that
// behaves exactly like encode+decode over the wire.
func Copy(dst, src any) error {
	b, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("copy marshal: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("copy decode: %w", err)
	}
	return nil
}
