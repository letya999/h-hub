package ownerreq

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"slices"
	"time"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	orv1 "github.com/letya999/h-hub/contracts/ownerreq/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
)

// Handlers exposes the contract operation table.
func (s *Server) Handlers() orv1.Handlers {
	return orv1.Handlers{CreateRequest: s.createRequest,
		AnswerRequest: s.answerRequest, GetRequest: s.getRequest,
		BindRequest: s.bindRequest}
}

// caller is the verified op caller: a run token carries claims the
// addressee binds to; a service token names its addressee.
type caller struct {
	run    bool
	claims idv1.RunClaims
}

func errRequestNotFound() error {
	return herrors.New(herrors.CodeNotFound, "err.not-found"). //nolint:wrapcheck // shared error form
									With("kind", "owner_request")
}

func errClosed(state string) error {
	return herrors.New(herrors.CodeConflict, "err.request-closed"). //nolint:wrapcheck // shared error form
									With("state", state)
}

// callerOf binds the principal the mux verifier accepted. A run
// token must name tool-hub in its audience, like every run op here.
func callerOf(ctx context.Context) (caller, error) {
	p := httpapi.PrincipalFrom(ctx)
	if p == nil {
		return caller{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	if p.Purpose != "run" {
		return caller{}, nil
	}
	if !slices.Contains(p.Aud, "tool-hub") {
		return caller{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	c, err := idv1.ParseRunClaims(p.Payload)
	if err != nil {
		return caller{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	return caller{run: true, claims: c}, nil
}

// createRequest opens one request. Run callers are bound to their
// claims and may ask only question or choice — approval comes from
// services by their own rules, never from the model.
func (s *Server) createRequest(ctx context.Context, r orv1.CreateReq) (orv1.CreateResp, error) {
	var out orv1.CreateResp
	c, err := callerOf(ctx)
	if err != nil {
		return out, err
	}
	return s.create(ctx, c, r)
}

// addressee binds the request's target to what the caller proved:
// run claims pin owner, space, job and run — the conversation
// arrives later through BindRequest, because the mint wire stays
// frozen for the rollback baseline; a service caller names the
// addressee explicitly.
func addressee(c caller, r orv1.CreateReq) (request, error) {
	rec := request{kind: r.Kind, prompt: r.Prompt, options: r.Options}
	if c.run {
		if r.Kind == orv1.KindApproval {
			return rec, herrors.New(herrors.CodeForbidden, "err.request-kind")
		}
		rec.owner, rec.space = c.claims.Owner, c.claims.Space
		rec.job, rec.run = c.claims.Job, c.claims.Run
		return rec, nil
	}
	rec.owner, rec.space = r.Owner, r.Space
	rec.channel, rec.external, rec.chat, rec.topic = r.Channel, r.External, r.Chat, r.Topic
	rec.conv = r.Conversation
	if rec.owner == "" || rec.space == "" ||
		(rec.conv == "" && (rec.channel == "" || rec.external == "")) {
		return rec, herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
											With("leaf", "addressee")
	}
	return rec, nil
}

// create is the shared create path for the contract op and the
// agent's ask/choose tools.
func (s *Server) create(ctx context.Context, c caller, r orv1.CreateReq) (orv1.CreateResp, error) {
	var out orv1.CreateResp
	if err := orv1.ValidateCreate(r); err != nil {
		return out, err //nolint:wrapcheck // contract error form
	}
	if r.TTLSec > int64(maxTTL.Seconds()) {
		return out, herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
											With("leaf", "ttl_sec")
	}
	rec, err := addressee(c, r)
	if err != nil {
		return out, err
	}
	if rec.kind == orv1.KindApproval {
		rec.options = orv1.ApprovalOptions
	}
	mode, err := s.deps.Mode.ApprovalsMode(ctx, rec.space)
	if err != nil {
		return out, fmt.Errorf("approvals mode: %w", err)
	}
	rec.id, err = randomID()
	if err != nil {
		return out, err
	}
	now := s.deps.Now()
	state, err := s.commitRequest(ctx, rec, r.TTLSec, mode, now)
	if err != nil {
		return out, err
	}
	return orv1.CreateResp{Request: rec.id, State: state,
		Deadline: now.Add(time.Duration(r.TTLSec) * time.Second).Unix()}, nil
}

// commitRequest writes the machine, the row and the mode decision in
// one transaction: ask arms the delivery effect when the addressee
// is already deliverable — a run-created request waits for its
// binding — other modes resolve the request at once.
func (s *Server) commitRequest(ctx context.Context, rec request,
	ttlSec int64, mode string, now time.Time) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("request begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	if err := s.m.Create(ctx, tx, clock{s.deps.Now},
		fsm.CreateReq{ID: rec.id, Initial: "pending", Actor: "owner_request"}); err != nil {
		return "", fmt.Errorf("request create: %w", err)
	}
	if err := armDeadline(ctx, tx, rec.id,
		now.Add(time.Duration(ttlSec)*time.Second)); err != nil {
		return "", err
	}
	if err := insert(ctx, tx, rec); err != nil {
		return "", err
	}
	state := "pending"
	if rec.kind == orv1.KindApproval && mode != "ask" {
		if err := s.autoAnswer(ctx, tx, rec, mode, now); err != nil {
			return "", err
		}
		state = "answered"
	} else if deliverable(rec) {
		if err := armDelivery(ctx, tx, rec.id); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("request commit: %w", err)
	}
	return state, nil
}

// deliverable tells whether the row carries enough targeting for the
// delivery effect: a conversation, or a direct channel binding.
func deliverable(rec request) bool {
	return rec.conv != "" || (rec.channel != "" && rec.external != "")
}

// armDelivery inserts the delivery effect row inside tx.
func armDelivery(ctx context.Context, tx *sql.Tx, id string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO fsm.outbox
		(kind, machine_id, topic, payload) VALUES ('owner_request',$1,'deliver-request',$2)`,
		id, id); err != nil {
		return fmt.Errorf("request effect: %w", err)
	}
	return nil
}

// bindRequest pins one pending request to its conversation and arms
// the delivery effect in the same transaction. Only services bind —
// task-engine, when the runtime's journal event names the request;
// the run token cannot carry the conversation because the mint wire
// stays frozen for the rollback baseline. A repeat with the same
// conversation is the idempotent success, never a second effect.
func (s *Server) bindRequest(ctx context.Context, r orv1.BindReq) (orv1.BindResp, error) {
	var out orv1.BindResp
	if err := herrors.Require(r.Request != "" && r.Conversation != "",
		"err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	c, err := callerOf(ctx)
	if err != nil {
		return out, err
	}
	if c.run {
		return out, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	rec, err := s.loadFresh(ctx, r.Request)
	if err != nil {
		return out, err
	}
	if rec.state != "pending" {
		return out, errClosed(rec.state)
	}
	if rec.conv == r.Conversation {
		return orv1.BindResp{Request: rec.id, State: rec.state}, nil
	}
	if rec.conv != "" {
		return out, herrors.New(herrors.CodeConflict, "err.request-bound")
	}
	return bindTx(ctx, s.db, r)
}

// bindTx pins the conversation under the row lock and arms the
// delivery effect in the same transaction; the lock serializes
// racing binds, so a loser reads the winner's value as a conflict.
func bindTx(ctx context.Context, db *sql.DB, r orv1.BindReq) (orv1.BindResp, error) {
	var out orv1.BindResp
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return out, fmt.Errorf("bind begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	var cur string
	if err := tx.QueryRowContext(ctx,
		`SELECT conversation FROM owner_requests WHERE id=$1 FOR UPDATE`,
		r.Request).Scan(&cur); err != nil {
		return out, fmt.Errorf("bind lock: %w", err)
	}
	if cur == r.Conversation {
		return orv1.BindResp{Request: r.Request, State: "pending"}, nil
	}
	if cur != "" {
		return out, herrors.New(herrors.CodeConflict, "err.request-bound")
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE owner_requests SET conversation=$1 WHERE id=$2`,
		r.Conversation, r.Request); err != nil {
		return out, fmt.Errorf("bind write: %w", err)
	}
	if err := armDelivery(ctx, tx, r.Request); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, fmt.Errorf("bind commit: %w", err)
	}
	return orv1.BindResp{Request: r.Request, State: "pending"}, nil
}

// autoAnswer resolves an approval at once under a non-ask mode: the
// canonical answer is yes — the mode permits the action, it never
// picks for the human. The row records who answered (the mode); no
// delivery is armed.
func (s *Server) autoAnswer(ctx context.Context, tx *sql.Tx, rec request, mode string, now time.Time) error {
	hook := func(ctx context.Context, tx *sql.Tx, _ fsm.Change) error {
		return recordAnswer(ctx, tx, rec.id, "yes", "mode:"+mode)
	}
	err := s.m.Transition(ctx, tx, fsm.Request{
		ID: rec.id, Event: "answer", Actor: "mode:" + mode, At: now}, hook)
	if err != nil {
		return fmt.Errorf("request auto: %w", err)
	}
	return nil
}

// landed is the answer the proof admitted: the request, the
// canonical answer and its machine event.
type landed struct {
	rec    request
	answer string
	event  string
}

// answerable proves one answer may land: the request is pending and
// unexpired, the channel binding verifies to the addressee, and the
// raw text maps to a canonical answer and machine event.
func (s *Server) answerable(ctx context.Context, r orv1.AnswerReq) (landed, error) {
	var out landed
	rec, err := s.loadFresh(ctx, r.Request)
	if err != nil {
		return out, err
	}
	if rec.state != "pending" {
		return out, errClosed(rec.state)
	}
	owner, err := s.resolveOwner(ctx, r.Channel, r.External)
	if err != nil {
		return out, err
	}
	if owner != rec.owner {
		return out, herrors.New(herrors.CodeForbidden, "err.request-addressee")
	}
	out.rec = rec
	out.answer, out.event, err = orv1.CanonicalAnswer(rec.kind, rec.options, r.Answer)
	if err != nil {
		return landed{}, err //nolint:wrapcheck // contract error form
	}
	return out, nil
}

// answerRequest applies the addressee's answer exactly once: the
// request must be pending and unexpired, the channel binding must
// verify to the addressee, and the transition goes through CAS.
func (s *Server) answerRequest(ctx context.Context, r orv1.AnswerReq) (orv1.AnswerResp, error) {
	var out orv1.AnswerResp
	if err := herrors.Require(r.Request != "" && r.Channel != "" && r.External != "",
		"err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	l, err := s.answerable(ctx, r)
	if err != nil {
		return out, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, fmt.Errorf("answer begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	by := r.Channel + "/" + r.External
	hook := func(ctx context.Context, tx *sql.Tx, _ fsm.Change) error {
		return recordAnswer(ctx, tx, l.rec.id, l.answer, by)
	}
	err = s.m.Transition(ctx, tx, fsm.Request{ID: l.rec.id, Event: l.event, Actor: by,
		Effects: []fsm.Effect{{Topic: "retract-request", Payload: []byte(l.rec.id)}}}, hook)
	if err != nil {
		return out, errClosed("pending")
	}
	if err := tx.Commit(); err != nil {
		return out, fmt.Errorf("answer commit: %w", err)
	}
	state := "answered"
	if l.event == "deny" {
		state = "denied"
	}
	return orv1.AnswerResp{Request: l.rec.id, State: state}, nil
}

// getRequest reads one request, firing its deadline first when due
// so an expired request reports expired, not a stale pending.
func (s *Server) getRequest(ctx context.Context, r orv1.GetReq) (orv1.GetResp, error) {
	var out orv1.GetResp
	if err := herrors.Require(r.Request != "", "err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	c, err := callerOf(ctx)
	if err != nil {
		return out, err
	}
	rec, err := s.loadFresh(ctx, r.Request)
	if err != nil {
		return out, err
	}
	if c.run && (c.claims.Owner != rec.owner || c.claims.Space != rec.space) {
		return out, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	return orv1.GetResp{Request: rec.id, Kind: rec.kind, Owner: rec.owner,
		Space: rec.space, State: rec.state, Answer: rec.answer,
		Deadline: rec.deadline.Unix()}, nil
}

// loadFresh reads a request after firing its deadline when due — the
// deadline is the machine's, the sweep is only the backstop.
func (s *Server) loadFresh(ctx context.Context, id string) (request, error) {
	rec, err := s.load(ctx, id)
	if err != nil {
		return rec, err
	}
	if rec.state != "pending" || s.deps.Now().Before(rec.deadline) {
		return rec, nil
	}
	if _, err := s.m.SweepRow(ctx, s.db, id, s.deps.Now(), s.retractOnFinal); err != nil {
		return rec, fmt.Errorf("request expiry: %w", err)
	}
	return s.load(ctx, id)
}

// retractOnFinal arms the delivery-side retraction when a deadline
// transition lands the request in a final state.
func (s *Server) retractOnFinal(ctx context.Context, tx *sql.Tx, ch fsm.Change) error {
	if !s.m.Terminal(ch.To) {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO fsm.outbox
		(kind, machine_id, topic, payload) VALUES ('owner_request',$1,'retract-request',$2)`,
		ch.ID, ch.ID)
	if err != nil {
		return fmt.Errorf("retract effect: %w", err)
	}
	return nil
}

// resolveOwner maps a channel binding to its verified owner via
// identity — the only proof an answer may lean on.
func (s *Server) resolveOwner(ctx context.Context, channel, external string) (string, error) {
	tok, err := s.deps.Tokens.Token(ctx, "identity-hub")
	if err != nil {
		return "", fmt.Errorf("identity token: %w", err)
	}
	cl := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(s.deps.IdentityURL, tok)}}
	res, err := cl.Resolve(ctx, idv1.ResolveReq{Channel: channel, External: external})
	if err != nil {
		return "", fmt.Errorf("binding resolve: %w", err)
	}
	return res.Owner, nil
}

// clock adapts Deps.Now to fsm.Clock.
type clock struct{ now func() time.Time }

func (c clock) Now() time.Time { return c.now() }

func randomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("id random: %w", err)
	}
	return "req-" + hex.EncodeToString(raw[:]), nil
}
