package svc

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/letya999/h-hub/shared/fsm"
)

func (s *Server) conversation(ctx context.Context, external string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("thread begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	res, err := tx.ExecContext(ctx, `INSERT INTO mock_threads(external_id,conversation_id) VALUES($1,$2)
		ON CONFLICT(external_id) DO NOTHING`, external, "mock-"+external)
	if err != nil {
		return "", fmt.Errorf("thread insert: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("thread rows: %w", err)
	}
	if n > 0 {
		if err := s.thread.Create(ctx, tx, clock{s.deps.Now}, fsm.CreateReq{ID: "thread-" + external, Initial: "active", Actor: "channel-hub"}); err != nil {
			return "", fmt.Errorf("thread create: %w", err)
		}
	}
	var conv string
	if err := tx.QueryRowContext(ctx, `SELECT conversation_id FROM mock_threads WHERE external_id=$1`, external).Scan(&conv); err != nil {
		return "", fmt.Errorf("thread read: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("thread commit: %w", err)
	}
	return conv, nil
}

func (s *Server) newConversation(ctx context.Context, external, intakeID string) error {
	if _, err := s.conversation(ctx, external); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("new thread begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	hook := func(ctx context.Context, tx *sql.Tx, _ fsm.Change) error {
		if _, err := tx.ExecContext(ctx, `UPDATE mock_threads SET conversation_id=$1 WHERE external_id=$2`, "conv-"+intakeID, external); err != nil {
			return fmt.Errorf("new conversation: %w", err)
		}
		return nil
	}
	if err := s.thread.Transition(ctx, tx, fsm.Request{ID: "thread-" + external, Event: "new", Actor: "channel-hub"}, hook); err != nil {
		return fmt.Errorf("thread new: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("thread new commit: %w", err)
	}
	return nil
}
