package ownerreq

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	orv1 "github.com/letya999/h-hub/contracts/ownerreq/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
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
	action string
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

// CreateRunRequest binds a question or choice to the admitted run's claims.
func CreateRunRequest(s *Server, ctx context.Context, claims idv1.RunClaims, r orv1.CreateReq) (orv1.CreateResp, error) {
	r.TTLSec = int64(defaultTTL.Seconds())
	return s.create(ctx, caller{run: true, claims: claims}, r)
}

// addressee binds the request's target to what the caller proved:
// run claims pin owner, space, job and run — the conversation
// arrives later through BindRequest, because the mint wire stays
// frozen for the rollback baseline; a service caller names the
// addressee explicitly.
func addressee(c caller, r orv1.CreateReq) (request, error) {
	rec := request{kind: r.Kind, prompt: r.Prompt, options: r.Options}
	if c.run {
		if r.Kind == orv1.KindApproval && c.action == "" {
			return rec, herrors.New(herrors.CodeForbidden, "err.request-kind")
		}
		rec.owner, rec.space = c.claims.Owner, c.claims.Space
		rec.job, rec.run = c.claims.Job, c.claims.Run
		rec.action = c.action
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

// Run requests watch their job even if the runtime never reports the request ID.
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
	switch {
	case rec.kind == orv1.KindApproval && mode != "ask":
		err = s.autoAnswer(ctx, tx, rec, mode, now)
		state = "answered"
	case rec.run != "":
		err = armEffect(ctx, tx, rec.id, "check-request-job")
	default:
		err = armEffect(ctx, tx, rec.id, "deliver-request")
	}
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("request commit: %w", err)
	}
	return state, nil
}

func armEffect(ctx context.Context, tx *sql.Tx, id, topic string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO fsm.outbox
		(kind, machine_id, topic, payload) VALUES ('owner_request',$1,$2,$3)`,
		id, topic, id); err != nil {
		return fmt.Errorf("request effect: %w", err)
	}
	return nil
}

// bindRequest pins the conversation and schedules delivery once, under the row lock.
func (s *Server) bindRequest(ctx context.Context, r orv1.BindReq) (orv1.BindResp, error) {
	var out orv1.BindResp
	if err := herrors.Require(r.Request != "" && r.Conversation != "",
		"err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	if p := httpapi.PrincipalFrom(ctx); p == nil || p.Purpose != "service" {
		return out, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	return bindTx(ctx, s.db, r, s.deps.Now())
}

func bindTx(ctx context.Context, db *sql.DB, r orv1.BindReq, now time.Time) (orv1.BindResp, error) {
	var out orv1.BindResp
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return out, fmt.Errorf("bind begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	var cur, state string
	var deadline sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT q.conversation,m.state,m.deadline_at
		FROM owner_requests q JOIN fsm.machines m ON m.kind='owner_request' AND m.id=q.id
		WHERE q.id=$1 FOR UPDATE OF m,q`, r.Request).Scan(&cur, &state, &deadline)
	if errors.Is(err, sql.ErrNoRows) {
		return out, errRequestNotFound()
	}
	if err != nil {
		return out, fmt.Errorf("bind lock: %w", err)
	}
	if state != "pending" {
		return out, errClosed(state)
	}
	if !now.Before(deadline.Time) {
		return out, errClosed("expired")
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
	if err := armEffect(ctx, tx, r.Request, "deliver-request"); err != nil {
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

// answerable verifies the pending request and its addressee before mapping the reply.
func (s *Server) answerable(ctx context.Context, r orv1.AnswerReq) (answer, event string, err error) {
	rec, err := s.loadFresh(ctx, r.Request)
	if err != nil {
		return "", "", err
	}
	if rec.state != "pending" || (rec.run != "" && rec.turn.Deadline == 0) {
		return "", "", errClosed(rec.state)
	}
	owner, err := s.resolveOwner(ctx, r.Channel, r.External)
	if err != nil {
		return "", "", err
	}
	if owner != rec.owner {
		return "", "", herrors.New(herrors.CodeForbidden, "err.request-addressee")
	}
	return orv1.CanonicalAnswer(rec.kind, rec.options, r.Answer) //nolint:wrapcheck // contract error form
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
	answer, event, err := s.answerable(ctx, r)
	if err != nil {
		return out, err
	}
	by := r.Channel + "/" + r.External
	hook := func(ctx context.Context, tx *sql.Tx, _ fsm.Change) error {
		return recordAnswer(ctx, tx, r.Request, answer, by)
	}
	err = s.closeRequest(ctx, fsm.Request{ID: r.Request, Event: event, Actor: by}, hook)
	if alreadyClosed(err) {
		return out, herrors.New(herrors.CodeConflict, "err.request-closed")
	}
	if err != nil {
		return out, err
	}
	state := "answered"
	if event == "deny" {
		state = "denied"
	}
	return orv1.AnswerResp{Request: r.Request, State: state}, nil
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

// loadFresh reconciles a pending request before a read, answer or delivery.
func (s *Server) loadFresh(ctx context.Context, id string) (request, error) {
	rec, err := s.load(ctx, id)
	if err != nil || rec.state != "pending" {
		return rec, err
	}
	if rec.run != "" && s.deps.Now().Before(rec.deadline) {
		rec.turn, err = s.deps.RequestTurn(ctx, tv1.RequestTurnReq{Job: rec.job, Run: rec.run, Request: id})
		if err != nil {
			return rec, fmt.Errorf("request turn: %w", err)
		}
		if at := time.Unix(rec.turn.Deadline, 0); rec.turn.Deadline > 0 && at.Before(rec.deadline) {
			rec.deadline = at
		}
	}
	if !rec.turn.Done && s.deps.Now().Before(rec.deadline) {
		return rec, nil
	}
	err = s.closeRequest(ctx, fsm.Request{ID: id, Event: "expire", Actor: "owner_request"}, nil)
	if err != nil && !alreadyClosed(err) {
		return rec, err
	}
	return s.load(ctx, id)
}

func alreadyClosed(err error) bool {
	return errors.Is(err, fsm.ErrBadTransition) || errors.Is(err, fsm.ErrStale)
}

func (s *Server) closeRequest(ctx context.Context, r fsm.Request, hook fsm.Hook) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("request close begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	r.At = s.deps.Now()
	r.Effects = []fsm.Effect{{Topic: "retract-request", Payload: []byte(r.ID)}}
	if err := s.m.Transition(ctx, tx, r, hook); err != nil {
		return fmt.Errorf("request %s: %w", r.Event, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("request close commit: %w", err)
	}
	return nil
}

// retractOnFinal runs inside the expiry transaction.
func retractOnFinal(ctx context.Context, tx *sql.Tx, ch fsm.Change) error {
	return armEffect(ctx, tx, ch.ID, "retract-request")
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
