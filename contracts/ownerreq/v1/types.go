// Package v1 is the owner-request contract: the one mechanism the
// system uses to ask the human — a question, a choice or an approval.
// tool-hub owns the machine (pending -> answered|denied|expired);
// other services create requests only through this contract, and
// channel-hub delivers them and returns the answer (blueprint §2.5).
package v1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/letya999/h-hub/shared/httpapi"
)

// Request kinds: who may create which is enforced by tool-hub —
// run tokens (the agent) create only question and choice, never
// approval; approvals come from services by their own rules.
const (
	KindQuestion = "question"
	KindChoice   = "choice"
	KindApproval = "approval"
)

// Request states are the machine's; expired means refusal, not a
// retryable wait.
const (
	StatePending  = "pending"
	StateAnswered = "answered"
	StateDenied   = "denied"
	StateExpired  = "expired"
)

// Approval option labels are fixed; a "no" answer maps to denied.
var ApprovalOptions = []string{"yes", "no"}

// CreateReq names the addressee and the question. Run-token callers
// are bound by their claims (owner, space, conversation); service
// callers name the addressee explicitly. Delivery targets resolve in
// order: Conversation, then Channel+External.
type CreateReq struct {
	Owner        string   `json:"owner,omitempty"`
	Space        string   `json:"space,omitempty"`
	Channel      string   `json:"channel,omitempty"`
	External     string   `json:"external,omitempty"`
	Chat         string   `json:"chat,omitempty"`
	Topic        string   `json:"topic,omitempty"`
	Conversation string   `json:"conversation,omitempty"`
	Kind         string   `json:"kind"`
	Prompt       string   `json:"prompt"`
	Options      []string `json:"options,omitempty"`
	TTLSec       int64    `json:"ttl_sec"`
}

// CreateResp returns the request identity and state — the only
// fields a model may ever see. No links, no nonces.
type CreateResp struct {
	Request  string `json:"request"`
	State    string `json:"state"`
	Deadline int64  `json:"deadline"`
}

// AnswerReq delivers one answer. The channel binding is verified
// against the addressee; a non-pending request refuses.
type AnswerReq struct {
	Request  string `json:"request"`
	Channel  string `json:"channel"`
	External string `json:"external"`
	Answer   string `json:"answer"`
}

// AnswerResp reports the state the transition reached.
type AnswerResp struct {
	Request string `json:"request"`
	State   string `json:"state"`
}

// GetReq asks for one request's state.
type GetReq struct {
	Request string `json:"request"`
}

// GetResp is the full record the creator or channel sees.
type GetResp struct {
	Request  string `json:"request"`
	Kind     string `json:"kind"`
	Owner    string `json:"owner"`
	Space    string `json:"space"`
	State    string `json:"state"`
	Answer   string `json:"answer,omitempty"`
	Deadline int64  `json:"deadline"`
}

// Handlers is the server-side operation table.
type Handlers struct {
	CreateRequest func(context.Context, CreateReq) (CreateResp, error)
	AnswerRequest func(context.Context, AnswerReq) (AnswerResp, error)
	GetRequest    func(context.Context, GetReq) (GetResp, error)
}

// Ops maps operation names to request handlers.
func Ops(h Handlers) map[string]httpapi.Handler {
	return map[string]httpapi.Handler{
		"CreateRequest": op(h.CreateRequest),
		"AnswerRequest": op(h.AnswerRequest),
		"GetRequest":    op(h.GetRequest),
	}
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
