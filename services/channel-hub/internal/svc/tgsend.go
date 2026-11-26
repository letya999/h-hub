package svc

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	credv1 "github.com/letya999/h-hub/contracts/cred/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/locale"
)

// tgMaxPhoto bounds a staged photo; base64 inflates ~4/3 and the
// httpapi body cap is 1 MiB, so the raw ceiling stays under it.
const tgMaxPhoto = 512 << 10

// tgStatusThrottle is the provider edit budget (blueprint: one edit
// per five seconds).
const tgStatusThrottle = 5 * time.Second

type tgPayload struct {
	MessageID int64  `json:"message_id"`
	FileID    string `json:"file_id"`
	FileSize  int64  `json:"file_size"`
}

func tgPayloadOf(raw string) tgPayload {
	var p tgPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return tgPayload{}
	}
	return p
}

// tgToken fetches the live bot token for a provider call — same
// lease path the poller uses.
func (ad *tgAdapter) tgToken(ctx context.Context) (string, error) {
	c, err := ad.credClient(ctx)
	if err != nil {
		return "", err
	}
	res, err := c.Acquire(ctx, credv1.AcquireReq{Ref: telegramTokenRef})
	if err != nil {
		return "", fmt.Errorf("acquire token: %w", err)
	}
	v := res.Fields["value"]
	if v == "" {
		return "", fmt.Errorf("acquire token: empty credential")
	}
	return v, nil
}

func (ad *tgAdapter) chatCredentials(ctx context.Context, id string) (string, int64, error) {
	chat, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("tg chat %q: %w", id, err)
	}
	tok, err := ad.tgToken(ctx)
	return tok, chat, err
}

// sendTelegram is the "send" effect for channel=telegram: one
// provider call, then the delivery machine acks or turns ambiguous
// on its own deadline.
func (ad *tgAdapter) sendTelegram(ctx context.Context, id string) (bool, error) {
	tx, err := ad.s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("tg send begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	var d deliveryRow
	err = tx.QueryRowContext(ctx, `SELECT m.state, d.id, d.kind, d.body, d.chat_id, d.topic, d.intake_id
		FROM fsm.machines m JOIN deliveries d ON d.id=m.id
		WHERE m.kind='delivery' AND m.id=$1 FOR UPDATE OF m`, id).
		Scan(&d.state, &d.id, &d.kind, &d.body, &d.chat, &d.topic, &d.intake)
	if err != nil {
		return false, fmt.Errorf("tg delivery read: %w", err)
	}
	if d.state == "sent" {
		return true, nil
	}
	if d.state != "sending" {
		return false, herrors.New(herrors.CodeConflict, "err.machine-state")
	}
	tok, chat, err := ad.chatCredentials(ctx, d.chat)
	if err != nil {
		return false, err
	}
	if err := ad.tgSendByKind(ctx, tx, tok, d, chat); err != nil {
		return false, err
	}
	if err := ad.s.delivery.Transition(ctx, tx, fsm.Request{
		ID: id, Event: "ack", Actor: "telegram"}, nil); err != nil {
		return false, fmt.Errorf("tg delivery ack: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("tg send commit: %w", err)
	}
	return true, nil
}

type deliveryRow struct{ state, id, kind, body, chat, topic, intake string }

// tgSendByKind performs the provider call for one delivery. A status
// delivery edits the already-posted phase message when the intake
// has one; a request delivery posts its prompt under an inline
// keyboard whose buttons carry the answer index.
func (ad *tgAdapter) tgSendByKind(ctx context.Context, tx *sql.Tx, tok string, d deliveryRow, chat int64) error {
	switch d.kind {
	case "status", "answer":
		return ad.tgSendStatus(ctx, tx, tok, d, chat)
	case "request":
		return ad.tgSendRequest(ctx, tx, tok, d, chat)
	}
	_, err := ad.api.sendMessage(ctx, tok, chat, d.topic, d.body)
	return err
}

// tgSendRequest posts one owner request with its option buttons; a
// question has no options and arrives as plain text — the link still
// routes the typed reply.
func (ad *tgAdapter) tgSendRequest(ctx context.Context, tx *sql.Tx, tok string, d deliveryRow, chat int64) error {
	var (
		requestID string
		opts      []byte
	)
	err := tx.QueryRowContext(ctx, `SELECT request_id,options FROM request_links
		WHERE delivery_id=$1`, d.id).Scan(&requestID, &opts)
	if err != nil {
		return fmt.Errorf("tg request link: %w", err)
	}
	var options []string
	if err := json.Unmarshal(opts, &options); err != nil {
		return fmt.Errorf("tg request options: %w", err)
	}
	if len(options) == 0 {
		_, err := ad.api.sendMessage(ctx, tok, chat, d.topic, d.body)
		return err
	}
	_, err = ad.api.sendRequest(ctx, tok, tgRequestMsg{
		Chat: chat, Topic: d.topic, Text: d.body,
		RequestID: requestID, Options: options})
	return err
}

// The first answer replaces the progress message. A delayed working
// delivery must never create or overwrite a message after completion.
func (ad *tgAdapter) tgSendStatus(ctx context.Context, tx *sql.Tx, tok string, d deliveryRow, chat int64) error {
	waiting, err := lockWaitingIntake(ctx, tx, d.intake)
	if err != nil {
		return err
	}
	if !waiting && d.kind == "status" && d.body == locale.T(locale.EN, "channel.phase.working") {
		return nil
	}
	var msg int64
	err = tx.QueryRowContext(ctx,
		`SELECT message_id FROM tg_status WHERE intake_id=$1`, d.intake).Scan(&msg)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		msg, err = ad.api.sendMessage(ctx, tok, chat, d.topic, d.body)
	case err == nil:
		err = ad.api.editMessageText(ctx, tok, chat, msg, d.body)
	default:
		return fmt.Errorf("tg status read: %w", err)
	}
	if err != nil {
		return err
	}
	phase := d.body
	if d.kind == "answer" {
		phase = locale.T(locale.EN, "channel.phase.done")
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO tg_status(intake_id,message_id,phase) VALUES($1,$2,$3)
		 ON CONFLICT(intake_id) DO UPDATE SET phase=$3, updated_at=now()`,
		d.intake, msg, phase); err != nil {
		return fmt.Errorf("tg status write: %w", err)
	}
	return nil
}

func lockWaitingIntake(ctx context.Context, tx *sql.Tx, intake string) (bool, error) {
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM fsm.machines
		WHERE kind='intake' AND id=$1 FOR UPDATE`, intake).Scan(&state); err != nil {
		return false, fmt.Errorf("tg intake state: %w", err)
	}
	return state == "waiting", nil
}

// Intermediate edits share the intake lock with the final delivery.
func (ad *tgAdapter) tgStatusPass(ctx context.Context, r inputRow, phase string) error {
	tx, err := ad.s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("tg progress begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	waiting, err := lockWaitingIntake(ctx, tx, r.id)
	if err != nil || !waiting {
		return err
	}
	var msg int64
	err = tx.QueryRowContext(ctx, `SELECT message_id FROM tg_status
		WHERE intake_id=$1 AND phase<>$2 AND updated_at<=$3`, r.id, phase,
		ad.s.now().Add(-tgStatusThrottle)).Scan(&msg)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("tg progress read: %w", err)
	}
	tok, chat, err := ad.chatCredentials(ctx, r.chat)
	if err != nil {
		return err
	}
	if err := ad.api.editMessageText(ctx, tok, chat, msg, phase); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE tg_status SET phase=$2, updated_at=now() WHERE intake_id=$1`,
		r.id, phase); err != nil {
		return fmt.Errorf("tg progress write: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("tg progress commit: %w", err)
	}
	return nil
}

// tgDelete removes the provider-side copy of a secret-looking
// message; the intake row keeps only the fact it was intercepted.
func (ad *tgAdapter) tgDelete(ctx context.Context, intakeID string) (bool, error) {
	r, err := inputOf(ctx, ad.s, intakeID)
	if err != nil {
		return false, err
	}
	tok, chat, err := ad.chatCredentials(ctx, r.chat)
	if err != nil {
		return false, err
	}
	p := tgPayloadOf(r.payload)
	if err := ad.api.deleteMessage(ctx, tok, chat, p.MessageID); err != nil {
		return false, err
	}
	return true, nil
}

// tgPhoto is the slow part of a photo intake: resolve the file, stage
// the bytes and admit the job — all inside the effect, never the
// polling loop.
func (ad *tgAdapter) tgPhoto(ctx context.Context, intakeID string) (bool, error) {
	r, err := inputOf(ctx, ad.s, intakeID)
	if err != nil {
		return false, err
	}
	if r.state != "processing" {
		return true, nil
	}
	p := tgPayloadOf(r.payload)
	if p.FileID == "" {
		return true, ad.s.completeText(ctx, r, locale.T(locale.EN, "channel.photo-no-vision"),
			"channel.phase.refused")
	}
	tok, err := ad.tgToken(ctx)
	if err != nil {
		return false, err
	}
	if p.FileSize > tgMaxPhoto {
		return true, ad.s.completeText(ctx, r, locale.T(locale.EN, "channel.photo-too-big"),
			"channel.phase.refused")
	}
	path, err := ad.api.filePath(ctx, tok, p.FileID)
	if err != nil {
		return false, err
	}
	data, err := ad.api.download(ctx, tok, path, tgMaxPhoto)
	if err != nil {
		return false, err
	}
	client, resolved, err := ad.s.taskClient(ctx, r.channel, r.external)
	if err != nil {
		return false, err
	}
	conv, err := (&threadOps{ad.s}).conversation(ctx, r)
	if err != nil {
		return false, err
	}
	input := r.body
	if input == "" {
		input = "photo"
	}
	job, err := client.CreateJob(ctx, tv1.CreateJobReq{
		Space: resolved.Space, Conversation: conv, Input: input, Idem: r.id,
		Attachments: []tv1.Attachment{
			{Mime: "image/jpeg", Data: base64.StdEncoding.EncodeToString(data)}}})
	if err != nil {
		return false, fmt.Errorf("create job: %w", err)
	}
	return true, ad.s.waitJob(ctx, r, job.ID)
}
