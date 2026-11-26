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
	ReplyTo  int64  `json:"reply_to,omitempty"`
}

func (r InjectReq) Valid() bool {
	return r.External != "" && r.UpdateID != "" && r.Text != "" && r.ReplyTo >= 0
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

// DeliverRequestReq asks the channel to put one owner request in
// front of its addressee. Addressing resolves Conversation first,
// then Channel+External; Kind renders the form (buttons or numbered
// text) without channel-hub knowing what is asked.
type DeliverRequestReq struct {
	Request      string   `json:"request"`
	Owner        string   `json:"owner"`
	Kind         string   `json:"kind"`
	Prompt       string   `json:"prompt"`
	Options      []string `json:"options,omitempty"`
	Deadline     int64    `json:"deadline"`
	Conversation string   `json:"conversation,omitempty"`
	Channel      string   `json:"channel,omitempty"`
	External     string   `json:"external,omitempty"`
	Chat         string   `json:"chat,omitempty"`
	Topic        string   `json:"topic,omitempty"`
}
type DeliverRequestResp struct {
	Delivery string `json:"delivery"`
}

// RetractRequestReq closes a request's open link and retires its
// unfinished delivery — the owner side of the request/delivery pair.
type RetractRequestReq struct {
	Request string `json:"request"`
}
type RetractRequestResp struct {
	OK bool `json:"ok"`
}

type Handlers struct {
	Inject         func(context.Context, InjectReq) (InjectResp, error)
	ReadOutbox     func(context.Context, ReadOutboxReq) (ReadOutboxResp, error)
	Alert          func(context.Context, AlertReq) (AlertResp, error)
	DeliverRequest func(context.Context, DeliverRequestReq) (DeliverRequestResp, error)
	RetractRequest func(context.Context, RetractRequestReq) (RetractRequestResp, error)
}

func Ops(h Handlers) map[string]httpapi.Handler {
	return map[string]httpapi.Handler{
		"Inject":         op(h.Inject),
		"ReadOutbox":     op(h.ReadOutbox),
		"Alert":          op(h.Alert),
		"DeliverRequest": op(h.DeliverRequest),
		"RetractRequest": op(h.RetractRequest)}
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
