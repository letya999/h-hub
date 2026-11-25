package svc

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/letya999/h-hub/shared/fsm"
)

// threadOps is the conversation-key path's method set, kept off
// Server for the method cap (commandOps and requestOps follow the
// same shape).
type threadOps struct{ *Server }

// conversation resolves the durable conversation id for the row's
// channel key: mock keys on the external handle, telegram on
// (chat, topic) — the blueprint conversation key.
func (o *threadOps) conversation(ctx context.Context, r inputRow) (string, error) {
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("thread begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	var conv string
	if r.channel == "telegram" {
		conv, err = tgConversation(ctx, tx, r)
	} else {
		conv, err = o.mockConversation(ctx, tx, r)
	}
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("thread commit: %w", err)
	}
	return conv, nil
}

// tgConversation keys telegram conversations on (chat, topic); the
// row is plain data, the thread itself has no machine.
func tgConversation(ctx context.Context, tx *sql.Tx, r inputRow) (string, error) {
	var conv string
	def := "tg-" + r.chat + "-" + r.topic
	if _, err := tx.ExecContext(ctx, `INSERT INTO tg_threads(chat_id,topic,conversation_id)
		VALUES($1,$2,$3) ON CONFLICT(chat_id,topic) DO NOTHING`,
		r.chat, r.topic, def); err != nil {
		return "", fmt.Errorf("tg thread insert: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT conversation_id FROM tg_threads
		WHERE chat_id=$1 AND topic=$2`, r.chat, r.topic).Scan(&conv); err != nil {
		return "", fmt.Errorf("tg thread read: %w", err)
	}
	return conv, nil
}

// mockConversation keys mock threads on the external handle and
// creates the thread machine on first sight.
func (o *threadOps) mockConversation(ctx context.Context, tx *sql.Tx, r inputRow) (string, error) {
	var conv string
	res, err := tx.ExecContext(ctx, `INSERT INTO mock_threads(external_id,conversation_id) VALUES($1,$2)
		ON CONFLICT(external_id) DO NOTHING`, r.external, "mock-"+r.external)
	if err != nil {
		return "", fmt.Errorf("thread insert: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("thread rows: %w", err)
	}
	if n > 0 {
		if err := o.thread.Create(ctx, tx, clock{o.deps.Now},
			fsm.CreateReq{ID: "thread-" + r.external, Initial: "active", Actor: "channel-hub"}); err != nil {
			return "", fmt.Errorf("thread create: %w", err)
		}
	}
	if err := tx.QueryRowContext(ctx, `SELECT conversation_id FROM mock_threads WHERE external_id=$1`, r.external).Scan(&conv); err != nil {
		return "", fmt.Errorf("thread read: %w", err)
	}
	return conv, nil
}

// newConversation rotates the conversation id for the row's channel
// key. Mock threads ride their machine; telegram rows are plain data
// with no lifecycle (rule 16 concerns state, a rotation marker is not).
func (o *threadOps) newConversation(ctx context.Context, r inputRow) error {
	if _, err := o.conversation(ctx, r); err != nil {
		return err
	}
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("new thread begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if r.channel == "telegram" {
		if _, err := tx.ExecContext(ctx, `UPDATE tg_threads SET conversation_id=$1
			WHERE chat_id=$2 AND topic=$3`, "conv-"+r.id, r.chat, r.topic); err != nil {
			return fmt.Errorf("tg new conversation: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("tg thread commit: %w", err)
		}
		return nil
	}
	hook := func(ctx context.Context, tx *sql.Tx, _ fsm.Change) error {
		if _, err := tx.ExecContext(ctx, `UPDATE mock_threads SET conversation_id=$1 WHERE external_id=$2`, "conv-"+r.id, r.external); err != nil {
			return fmt.Errorf("new conversation: %w", err)
		}
		return nil
	}
	if err := o.thread.Transition(ctx, tx, fsm.Request{ID: "thread-" + r.external, Event: "new", Actor: "channel-hub"}, hook); err != nil {
		return fmt.Errorf("thread new: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("thread new commit: %w", err)
	}
	return nil
}
