package v1

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// ValidateCreate enforces the contract-level shape every
// implementation must refuse the same way.
func ValidateCreate(r CreateReq) error {
	switch r.Kind {
	case KindQuestion:
	case KindChoice:
		if len(r.Options) < 2 {
			return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
											With("leaf", "options")
		}
	case KindApproval:
		if len(r.Options) != 0 {
			return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
											With("leaf", "options")
		}
	default:
		return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
										With("kind", r.Kind)
	}
	if r.Prompt == "" {
		return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
										With("leaf", "prompt")
	}
	if r.TTLSec <= 0 {
		return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
										With("leaf", "ttl_sec")
	}
	return nil
}

// CanonicalAnswer maps a raw channel answer to the stored value and
// the machine event: "answer" or "deny". One mapping for the fake,
// the service and every channel.
func CanonicalAnswer(kind string, options []string, raw string) (answer, event string, err error) {
	raw = strings.TrimSpace(raw)
	switch kind {
	case KindQuestion:
		if raw == "" {
			return "", "", herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
												With("leaf", "answer")
		}
		return raw, "answer", nil
	case KindChoice:
		if n, e := strconv.Atoi(raw); e == nil && n >= 1 && n <= len(options) {
			return options[n-1], "answer", nil
		}
		for _, o := range options {
			if raw == o {
				return o, "answer", nil
			}
		}
		return "", "", herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
											With("leaf", "answer")
	case KindApproval:
		switch strings.ToLower(raw) {
		case "yes", "1", "да":
			return "yes", "answer", nil
		case "no", "2", "нет":
			return "no", "deny", nil
		}
		return "", "", herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
											With("leaf", "answer")
	}
	return "", "", herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
										With("kind", kind)
}

// fakeReq is one stored request row.
type fakeReq struct {
	CreateReq
	id       string
	state    string
	answer   string
	deadline time.Time
}

// Fake is an in-memory owner-request service for tests: bindings map
// "channel/external" to the verified owner the channel reports.
type Fake struct {
	mu       sync.Mutex
	bindings map[string]string
	reqs     map[string]*fakeReq
	seq      int
}

// NewFake builds a fake; bindings maps "channel/external" to owner.
func NewFake(bindings map[string]string) *Fake {
	return &Fake{bindings: bindings, reqs: map[string]*fakeReq{}}
}

// Handlers exposes the fake as the contract operation table.
func (f *Fake) Handlers() Handlers {
	return Handlers{
		CreateRequest: f.create,
		AnswerRequest: f.answer,
		GetRequest:    f.get,
		BindRequest:   f.bind,
	}
}

func (f *Fake) create(_ context.Context, r CreateReq) (CreateResp, error) {
	var out CreateResp
	if err := ValidateCreate(r); err != nil {
		return out, err
	}
	if r.Owner == "" || r.Space == "" {
		return out, herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
											With("leaf", "owner")
	}
	if r.Conversation == "" && (r.Channel == "" || r.External == "") {
		return out, herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
											With("leaf", "addressee")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	id := fmt.Sprintf("req_%d", f.seq)
	if r.Kind == KindApproval {
		r.Options = ApprovalOptions
	}
	deadline := time.Now().UTC().Add(time.Duration(r.TTLSec) * time.Second)
	f.reqs[id] = &fakeReq{CreateReq: r, id: id, state: StatePending, deadline: deadline}
	return CreateResp{Request: id, State: StatePending, Deadline: deadline.Unix()}, nil
}

func (f *Fake) answer(_ context.Context, r AnswerReq) (AnswerResp, error) {
	var out AnswerResp
	if err := herrors.Require(r.Request != "" && r.Channel != "" && r.External != "",
		"err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	q, ok := f.reqs[r.Request]
	if !ok {
		return out, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	f.expireLocked(q)
	if q.state != StatePending {
		return out, herrors.New(herrors.CodeConflict, "err.request-closed"). //nolint:wrapcheck // shared error form
											With("state", q.state)
	}
	if f.bindings[r.Channel+"/"+r.External] != q.Owner {
		return out, herrors.New(herrors.CodeForbidden, "err.request-addressee")
	}
	ans, ev, err := CanonicalAnswer(q.Kind, q.Options, r.Answer)
	if err != nil {
		return out, err
	}
	q.answer = ans
	q.state = StateAnswered
	if ev == "deny" {
		q.state = StateDenied
	}
	return AnswerResp{Request: q.id, State: q.state}, nil
}

func (f *Fake) get(_ context.Context, r GetReq) (GetResp, error) {
	var out GetResp
	if err := herrors.Require(r.Request != "", "err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	q, ok := f.reqs[r.Request]
	if !ok {
		return out, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	f.expireLocked(q)
	out = GetResp{Request: q.id, Kind: q.Kind, Owner: q.Owner, Space: q.Space,
		State: q.state, Answer: q.answer, Deadline: q.deadline.Unix()}
	return out, nil
}

// bind pins the conversation once: a repeat with the same value is
// the idempotent success, a different one conflicts.
func (f *Fake) bind(_ context.Context, r BindReq) (BindResp, error) {
	var out BindResp
	if err := herrors.Require(r.Request != "" && r.Conversation != "",
		"err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	q, ok := f.reqs[r.Request]
	if !ok {
		return out, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	f.expireLocked(q)
	if q.state != StatePending {
		return out, herrors.New(herrors.CodeConflict, "err.request-closed"). //nolint:wrapcheck // shared error form
											With("state", q.state)
	}
	if q.Conversation != "" && q.Conversation != r.Conversation {
		return out, herrors.New(herrors.CodeConflict, "err.request-bound")
	}
	q.Conversation = r.Conversation
	return BindResp{Request: q.id, State: q.state}, nil
}

// expireLocked applies the lazy deadline: past its deadline a pending
// request is expired — the sweep only carries the delivery effect.
func (f *Fake) expireLocked(q *fakeReq) {
	if q.state == StatePending && !time.Now().UTC().Before(q.deadline) {
		q.state = StateExpired
	}
}
