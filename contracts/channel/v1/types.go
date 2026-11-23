// Package v1 is the private mock-channel contract.
package v1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/letya999/h-hub/shared/httpapi"
)

type InjectReq struct {
	External string `json:"external"`
	UpdateID string `json:"update_id"`
	Text     string `json:"text"`
}
type InjectResp struct {
	ID string `json:"id"`
}
type ReadOutboxReq struct {
	External string `json:"external"`
	After    int64  `json:"after"`
}
type OutMessage struct {
	Seq  int64  `json:"seq"`
	Kind string `json:"kind"`
	Body string `json:"body"`
}
type ReadOutboxResp struct {
	Messages []OutMessage `json:"messages"`
}

type AlertReq struct {
	Key  string `json:"key"`
	Code string `json:"code"`
}
type AlertResp struct {
	OK bool `json:"ok"`
}

type Handlers struct {
	Inject     func(context.Context, InjectReq) (InjectResp, error)
	ReadOutbox func(context.Context, ReadOutboxReq) (ReadOutboxResp, error)
	Alert      func(context.Context, AlertReq) (AlertResp, error)
}

func Ops(h Handlers) map[string]httpapi.Handler {
	return map[string]httpapi.Handler{"Inject": op(h.Inject), "ReadOutbox": op(h.ReadOutbox), "Alert": op(h.Alert)}
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
