package fsm

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// OutEffect is one pending outbox row as the executor reads it.
type OutEffect struct {
	Seq       int64
	Kind      string
	MachineID string
	Topic     string
	Payload   []byte
}

// DueEffects lists pending outbox rows of a schema in commit order.
// Applying an effect is the executor's job; rows stay pending until
// MarkDone or Defer so a crash between commit and apply replays them.
func DueEffects(ctx context.Context, db *sql.DB, schema string,
	now time.Time, limit int) ([]OutEffect, error) {
	if err := checkSchema(schema); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT seq, kind, machine_id, topic, payload
		FROM `+schema+`.outbox
		WHERE done_at IS NULL AND due_at <= $1 ORDER BY seq LIMIT $2`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("due effects: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close
	var out []OutEffect
	for rows.Next() {
		var e OutEffect
		if err := rows.Scan(&e.Seq, &e.Kind, &e.MachineID, &e.Topic, &e.Payload); err != nil {
			return nil, fmt.Errorf("scan effect: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("due effects: %w", err)
	}
	return out, nil
}

// MarkDone records a successful apply.
func MarkDone(ctx context.Context, db *sql.DB, schema string, seq int64) error {
	if err := checkSchema(schema); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx,
		"UPDATE "+schema+".outbox SET done_at=now() WHERE seq=$1", seq)
	if err != nil {
		return fmt.Errorf("mark effect done: %w", err)
	}
	return nil
}

// DeferEffect reschedules an effect whose apply failed retryably.
func DeferEffect(ctx context.Context, db *sql.DB, schema string,
	seq int64, at time.Time) error {
	if err := checkSchema(schema); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx,
		"UPDATE "+schema+".outbox SET due_at=$1 WHERE seq=$2", at, seq)
	if err != nil {
		return fmt.Errorf("defer effect: %w", err)
	}
	return nil
}
