package svc

// request.go: the delivery half of the owner-request pair (phase 10).
// channel-hub renders a request (buttons or numbered text), routes
// the reply back to tool-hub and closes the pair on retraction. The
// request's meaning and lifecycle belong to tool-hub — the link row
// here is routing data with a deadline, not a second state machine.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	orv1 "github.com/letya999/h-hub/contracts/ownerreq/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/locale"
)

// requestOps is the request path's method set — delivery, links and
// answer routing live on it so Server stays under the method cap;
// binding resolution rides commandOps' resolveExternal (rule 3).
type requestOps struct{ *Server }

// reqTarget is where one request lands: mock binds on the external
// handle, telegram on the (chat, topic) conversation key.
type reqTarget struct {
	channel, external, chat, topic string
}

// deliverRequest admits one owner request: resolves its channel
// target, verifies the addressee's binding when an external handle
// is known, and queues the delivery with the link that routes the
// reply. A newer link on the same target supersedes an open one.
func (o *requestOps) deliverRequest(ctx context.Context, r cv1.DeliverRequestReq) (cv1.DeliverRequestResp, error) {
	if err := validDeliver(r); err != nil {
		return cv1.DeliverRequestResp{}, err
	}
	t, err := o.requestTarget(ctx, r)
	if err != nil {
		return cv1.DeliverRequestResp{}, err
	}
	if err := o.verifyAddressee(ctx, t, r.Owner); err != nil {
		return cv1.DeliverRequestResp{}, err
	}
	id, err := randomID("d-")
	if err != nil {
		return cv1.DeliverRequestResp{}, err
	}
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return cv1.DeliverRequestResp{}, fmt.Errorf("request begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if err := o.insertRequestDelivery(ctx, tx, id, t, requestText(r)); err != nil {
		return cv1.DeliverRequestResp{}, err
	}
	if err := insertRequestLink(ctx, tx, id, t, r); err != nil {
		return cv1.DeliverRequestResp{}, err
	}
	if err := tx.Commit(); err != nil {
		return cv1.DeliverRequestResp{}, fmt.Errorf("request commit: %w", err)
	}
	return cv1.DeliverRequestResp{Delivery: id}, nil
}

// validDeliver enforces the contract-level shape a delivery needs.
func validDeliver(r cv1.DeliverRequestReq) error {
	if err := herrors.Require(r.Request != "" && r.Owner != "" && r.Prompt != "" &&
		r.Deadline > 0, "err.invalid-arg"); err != nil {
		return err //nolint:wrapcheck // shared error form
	}
	if r.Kind != orv1.KindQuestion && r.Kind != orv1.KindChoice && r.Kind != orv1.KindApproval {
		return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
										With("kind", r.Kind)
	}
	return nil
}

// verifyAddressee proves the delivery targets the request's owner:
// where the target names an external handle, its identity binding
// must resolve to the owner. A conversation-resolved telegram
// target names no handle — the answerer is verified on reply.
func (o *requestOps) verifyAddressee(ctx context.Context, t reqTarget, owner string) error {
	if t.external == "" {
		return nil
	}
	resolved, err := (&commandOps{o.Server}).resolveExternal(ctx, t.channel, t.external)
	if err != nil {
		return err
	}
	if resolved.Owner != owner {
		return herrors.New(herrors.CodeForbidden, "err.request-addressee")
	}
	return nil
}

// requestTarget resolves where the request is delivered: the
// conversation key first, then the explicit channel address.
func (o *requestOps) requestTarget(ctx context.Context, r cv1.DeliverRequestReq) (reqTarget, error) {
	if r.Conversation != "" {
		if t, ok, err := o.convTarget(ctx, r.Conversation); err != nil || ok {
			return t, err
		}
	}
	if r.Channel == "mock" && r.External != "" {
		return reqTarget{channel: "mock", external: r.External}, nil
	}
	if r.Channel == "telegram" && r.Chat != "" {
		return reqTarget{channel: "telegram", external: r.External,
			chat: r.Chat, topic: r.Topic}, nil
	}
	return reqTarget{}, herrors.New(herrors.CodeNotFound, "err.request-target")
}

// convTarget maps a conversation id to its channel address; not
// found is ok=false so explicit addressing still applies.
func (o *requestOps) convTarget(ctx context.Context, conv string) (reqTarget, bool, error) {
	var ext string
	err := o.db.QueryRowContext(ctx, `SELECT external_id FROM mock_threads
		WHERE conversation_id=$1`, conv).Scan(&ext)
	if err == nil {
		return reqTarget{channel: "mock", external: ext}, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return reqTarget{}, false, fmt.Errorf("thread lookup: %w", err)
	}
	var chat, topic string
	err = o.db.QueryRowContext(ctx, `SELECT chat_id,topic FROM tg_threads
		WHERE conversation_id=$1`, conv).Scan(&chat, &topic)
	if err == nil {
		return reqTarget{channel: "telegram", chat: chat, topic: topic}, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return reqTarget{}, false, fmt.Errorf("tg thread lookup: %w", err)
	}
	return reqTarget{}, false, nil
}

// requestText renders the channel body: a bare prompt for a
// question, a numbered list for a choice or approval — the mock
// channel answers with the number, telegram carries the same text
// under its buttons.
func requestText(r cv1.DeliverRequestReq) string {
	if r.Kind == orv1.KindQuestion || len(r.Options) == 0 {
		return r.Prompt
	}
	var b strings.Builder
	b.WriteString(r.Prompt)
	for i, o := range r.Options {
		fmt.Fprintf(&b, "\n%d. %s", i+1, o)
	}
	return b.String()
}

// insertRequestDelivery queues the delivery row and its machine in
// the caller's transaction — the same pair enqueueDelivery writes
// for intake answers.
func (o *requestOps) insertRequestDelivery(ctx context.Context, tx *sql.Tx,
	id string, t reqTarget, body string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO deliveries
		(id,external_id,kind,body,channel,chat_id,topic,intake_id)
		VALUES($1,$2,'request',$3,$4,$5,$6,'')`,
		id, t.external, body, t.channel, t.chat, t.topic); err != nil {
		return fmt.Errorf("request delivery insert: %w", err)
	}
	if err := o.delivery.Create(ctx, tx, clock{o.deps.Now},
		fsm.CreateReq{ID: id, Initial: "queued", Actor: "channel-hub"}); err != nil {
		return fmt.Errorf("request delivery create: %w", err)
	}
	if err := o.delivery.Transition(ctx, tx, fsm.Request{ID: id, Event: "send",
		Actor:   "channel-hub",
		Effects: []fsm.Effect{{Topic: "send", Payload: []byte(id)}}}, nil); err != nil {
		return fmt.Errorf("request delivery send: %w", err)
	}
	return nil
}

// insertRequestLink supersedes any open link on the same target and
// writes the new one — the newest request owns the next reply.
func insertRequestLink(ctx context.Context, tx *sql.Tx,
	deliveryID string, t reqTarget, r cv1.DeliverRequestReq) error {
	if _, err := tx.ExecContext(ctx, `UPDATE request_links SET deadline_at=now()
		WHERE channel=$1 AND external_id=$2 AND chat_id=$3 AND topic=$4
		  AND answered_at IS NULL AND deadline_at > now()`,
		t.channel, t.external, t.chat, t.topic); err != nil {
		return fmt.Errorf("link supersede: %w", err)
	}
	opts, err := json.Marshal(r.Options)
	if err != nil {
		return fmt.Errorf("link options: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO request_links
		(request_id,delivery_id,channel,external_id,chat_id,topic,owner,options,deadline_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,to_timestamp($9))`,
		r.Request, deliveryID, t.channel, t.external, t.chat, t.topic,
		r.Owner, opts, r.Deadline); err != nil {
		return fmt.Errorf("link insert: %w", err)
	}
	return nil
}

// retractRequest closes the pair's channel side: the link expires
// at once and a delivery still in flight retires on its timeout
// edge — never left hanging (sys.seam-onesided).
func (o *requestOps) retractRequest(ctx context.Context, r cv1.RetractRequestReq) (cv1.RetractRequestResp, error) {
	if err := herrors.Require(r.Request != "", "err.invalid-arg"); err != nil {
		return cv1.RetractRequestResp{}, err //nolint:wrapcheck // shared error form
	}
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return cv1.RetractRequestResp{}, fmt.Errorf("retract begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	var delivery string
	err = tx.QueryRowContext(ctx, `UPDATE request_links SET deadline_at=now()
		WHERE request_id=$1 AND answered_at IS NULL
		RETURNING delivery_id`, r.Request).Scan(&delivery)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return cv1.RetractRequestResp{}, fmt.Errorf("retract link: %w", err)
	}
	if err := o.retireDelivery(ctx, tx, delivery); err != nil {
		return cv1.RetractRequestResp{}, err
	}
	if err := tx.Commit(); err != nil {
		return cv1.RetractRequestResp{}, fmt.Errorf("retract commit: %w", err)
	}
	return cv1.RetractRequestResp{OK: true}, nil
}

// retireDelivery lands a non-final request delivery on its timeout
// edge inside the retract transaction — never left hanging.
func (o *requestOps) retireDelivery(ctx context.Context, tx *sql.Tx, id string) error {
	if id == "" {
		return nil
	}
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM fsm.machines
		WHERE kind='delivery' AND id=$1`, id).Scan(&state); err != nil {
		return fmt.Errorf("retract delivery: %w", err)
	}
	if state != "queued" && state != "sending" {
		return nil
	}
	if err := o.delivery.Transition(ctx, tx, fsm.Request{
		ID: id, Event: "timeout", Actor: "channel-hub"}, nil); err != nil {
		return fmt.Errorf("retract retire: %w", err)
	}
	return nil
}

// linkRow is one open request_links row joined to its target.
type linkRow struct {
	request, owner, external string
	options                  []string
}

// openLink finds the newest link still accepting replies on the
// update's target: mock binds on the external handle, telegram on
// (chat, topic) — the answerer's handle is verified separately.
func (o *requestOps) openLink(ctx context.Context, r inputRow) (linkRow, bool, error) {
	var (
		l    linkRow
		opts []byte
	)
	err := o.db.QueryRowContext(ctx, `SELECT request_id,owner,external_id,options
		FROM request_links
		WHERE channel=$1 AND answered_at IS NULL AND deadline_at > $2
		  AND ((external_id <> '' AND external_id = $3)
		    OR (external_id = '' AND chat_id = $4 AND topic = $5))
		ORDER BY created_at DESC LIMIT 1`,
		r.channel, o.now(), r.external, r.chat, r.topic).
		Scan(&l.request, &l.owner, &l.external, &opts)
	if errors.Is(err, sql.ErrNoRows) {
		return l, false, nil
	}
	if err != nil {
		return l, false, fmt.Errorf("link lookup: %w", err)
	}
	if err := json.Unmarshal(opts, &l.options); err != nil {
		return l, false, fmt.Errorf("link options: %w", err)
	}
	return l, true, nil
}

// tryAnswer routes a plain-text update to an open request link when
// one exists. Telegram verifies the answerer's binding is the
// addressee — in a shared topic another member's text is an ordinary
// message, not an answer.
func (o *requestOps) tryAnswer(ctx context.Context, r inputRow) (bool, error) {
	l, ok, err := o.openLink(ctx, r)
	if err != nil || !ok {
		return ok, err
	}
	if l.external == "" {
		resolved, err := (&commandOps{o.Server}).resolveExternal(ctx, r.channel, r.external)
		if err != nil {
			return false, err
		}
		if resolved.Owner != l.owner {
			return false, nil
		}
	}
	return true, o.answerLink(ctx, r, l, r.body)
}

// answerByID routes a telegram callback to the request its button
// carried. The button names the request; the link still must be
// open and the presser must be the addressee.
func (o *requestOps) answerByID(ctx context.Context, r inputRow) error {
	req, choice := answerPayload(r.payload)
	if req == "" {
		return o.refuseClosed(ctx, r)
	}
	owner, open, err := o.linkByID(ctx, req, r.channel, r.chat)
	if err != nil {
		return err
	}
	resolved, err := (&commandOps{o.Server}).resolveExternal(ctx, r.channel, r.external)
	if err != nil {
		return err
	}
	if !open || resolved.Owner != owner {
		return o.refuseClosed(ctx, r)
	}
	l := linkRow{request: req, external: r.external}
	return o.answerLink(ctx, r, l, strconv.Itoa(choice))
}

// answerPayload unpacks the callback payload the intake row carries;
// anything that does not name a request yields "".
func answerPayload(raw string) (string, int) {
	var p struct {
		Request string `json:"request"`
		Choice  int    `json:"choice"`
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return "", 0
	}
	return p.Request, p.Choice
}

// linkByID reads one link's owner and openness for the callback
// path — the button names the request, the chat confirms where.
func (o *requestOps) linkByID(ctx context.Context, request, channel, chat string) (string, bool, error) {
	var (
		owner string
		open  bool
	)
	err := o.db.QueryRowContext(ctx, `SELECT owner,
		(answered_at IS NULL AND deadline_at > $4) FROM request_links
		WHERE request_id=$1 AND channel=$2 AND chat_id=$3`,
		request, channel, chat, o.now()).
		Scan(&owner, &open)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("callback link: %w", err)
	}
	return owner, open, nil
}

// refuseClosed completes the intake with the closed-request notice.
func (o *requestOps) refuseClosed(ctx context.Context, r inputRow) error {
	return o.completeText(ctx, r, locale.T(locale.EN, "channel.request-closed"),
		"channel.phase.refused")
}

// answerLink forwards one answer to tool-hub and completes the
// intake with the outcome's localized notice. The link is marked
// only on an accepted answer — a refused one keeps the request open
// or closes it with the request itself.
func (o *requestOps) answerLink(ctx context.Context, r inputRow, l linkRow, raw string) error {
	client, err := o.ownerreqClient(ctx)
	if err != nil {
		return err
	}
	_, err = client.AnswerRequest(ctx, orv1.AnswerReq{
		Request: l.request, Channel: r.channel, External: r.external, Answer: raw})
	switch herrors.CodeOf(err) {
	case "":
		return o.completeAnswer(ctx, r, l, "channel.request-accepted")
	case herrors.CodeInvalid:
		return o.completeText(ctx, r, locale.T(locale.EN, "channel.request-invalid"),
			"channel.phase.refused")
	case herrors.CodeConflict, herrors.CodeForbidden:
		return o.completeText(ctx, r, locale.T(locale.EN, "channel.request-closed"),
			"channel.phase.refused")
	default:
		return fmt.Errorf("answer request: %w", err)
	}
}

// completeAnswer marks the link answered and completes the intake
// with the accepted notice in one transaction.
func (o *requestOps) completeAnswer(ctx context.Context, r inputRow, l linkRow, key string) error {
	parts := renderAnswer(r.channel, locale.T(locale.EN, key))
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("answer begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	hook := func(ctx context.Context, tx *sql.Tx, _ fsm.Change) error {
		if _, err := tx.ExecContext(ctx, `UPDATE request_links SET answered_at=now()
			WHERE request_id=$1`, l.request); err != nil {
			return fmt.Errorf("link answer: %w", err)
		}
		return enqueueDeliveries(ctx, o.Server, tx, r, parts)
	}
	if err := o.intake.Transition(ctx, tx, fsm.Request{ID: r.id, Event: "complete",
		Actor: "channel-hub"}, hook); err != nil {
		return fmt.Errorf("answer intake: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("answer commit: %w", err)
	}
	return nil
}

// ownerreqClient builds a tool-hub client with a service token.
func (o *requestOps) ownerreqClient(ctx context.Context) (*orv1.Client, error) {
	id, err := o.identityClient(ctx)
	if err != nil {
		return nil, err
	}
	nonce, err := randomID("nonce-")
	if err != nil {
		return nil, err
	}
	tok, err := id.IssueServiceToken(ctx, o.sub, "tool-hub", nonce, o.deps.SelfKey)
	if err != nil {
		return nil, fmt.Errorf("tool service token: %w", err)
	}
	return &orv1.Client{T: orv1.HTTP{
		C: httpapi.NewClient(o.deps.ToolURL, tok.Token)}}, nil
}
