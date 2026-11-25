// Package ownerreq is tool-hub's owner-request module: the one
// mechanism the system uses to ask the human — a question, a choice
// or an approval (phase 10, blueprint §2.5). The module owns the
// request machine pending -> answered|denied|expired: answers apply
// exactly once by CAS, expiry is a refusal, and the addressee is the
// user whose action is asked, proven by the channel binding — never
// by a request-body claim.
package ownerreq

import (
	"context"
	"database/sql"
	"time"

	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/log"
)

const (
	// ceiling bounds the machine deadline; a request's own TTL arms
	// inside it at create.
	ceiling = 10 * time.Minute
	// maxTTL caps a per-request TTL below the runtime idle deadline
	// so a waiting run never outlives its container.
	maxTTL = 5 * time.Minute
	// defaultTTL is the ask/choose tool deadline.
	defaultTTL = 2 * time.Minute
)

// TokenSource issues service tokens for the module's outbound calls;
// svc's tokenSource satisfies it.
type TokenSource interface {
	Token(ctx context.Context, aud string) (string, error)
}

// ModeResolver reads the spec's config.approvals.mode; svc's spec
// resolution satisfies it.
type ModeResolver interface {
	ApprovalsMode(ctx context.Context, space string) (string, error)
}

// Deps are the module's collaborators.
type Deps struct {
	IdentityURL string
	ChannelURL  string
	Now         func() time.Time
	Tokens      TokenSource
	Mode        ModeResolver
	RequestTurn func(context.Context, tv1.RequestTurnReq) (tv1.RequestTurnResp, error)
}

// Server is the module: the request machine, the request table and
// the delivery loops.
type Server struct {
	db   *sql.DB
	m    *fsm.Machine
	deps Deps
	l    *log.L
}

// New builds the module; a malformed machine spec is a build defect.
func New(db *sql.DB, deps Deps, l *log.L) *Server {
	m, err := fsm.New(fsm.Spec{
		Kind:  "owner_request",
		Final: []string{"answered", "denied", "expired"},
		Edges: []fsm.Edge{
			{From: "pending", Event: "answer", To: "answered"},
			{From: "pending", Event: "deny", To: "denied"},
			{From: "pending", Event: "expire", To: "expired"},
		},
		Deadlines: map[string]fsm.Deadline{
			"pending": {Event: "expire", Actor: "owner_request", TTL: ceiling},
		},
	})
	if err != nil {
		panic("ownerreq machine: " + err.Error())
	}
	return &Server{db: db, m: m, deps: deps, l: l}
}
