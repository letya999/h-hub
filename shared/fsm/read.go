package fsm

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// State reads the current state of one machine row.
func (m *Machine) State(ctx context.Context, db *sql.DB, id string) (string, error) {
	var state string
	err := db.QueryRowContext(ctx,
		"SELECT state FROM "+m.table("machines")+" WHERE kind=$1 AND id=$2",
		m.kind, id).Scan(&state)
	if err == sql.ErrNoRows {
		return "", herrors.New(herrors.CodeNotFound, "err.not-found"). //nolint:wrapcheck // shared error form
										With("kind", m.kind).With("id", id)
	}
	if err != nil {
		return "", fmt.Errorf("machine state: %w", err)
	}
	return state, nil
}

// Touch re-arms the deadline of the machine's current state inside the
// caller's transaction. It is the idle-deadline primitive (reports
// renew the deadline instead of resetting a whole-run timeout).
func (m *Machine) Touch(ctx context.Context, tx *sql.Tx, id string, now time.Time) error {
	var state string
	var version int64
	err := tx.QueryRowContext(ctx,
		"SELECT state, version FROM "+m.table("machines")+" WHERE kind=$1 AND id=$2",
		m.kind, id).Scan(&state, &version)
	if err != nil {
		return fmt.Errorf("touch read: %w", err)
	}
	d, ok := m.deadlines[state]
	if !ok {
		return nil
	}
	at := now.Add(d.TTL)
	res, err := tx.ExecContext(ctx, `UPDATE `+m.table("machines")+`
		SET deadline_at=$1, version=version+1, updated_at=now()
		WHERE kind=$2 AND id=$3 AND state=$4 AND version=$5`,
		at, m.kind, id, state, version)
	if err != nil {
		return fmt.Errorf("touch: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return ErrStale.With("kind", m.kind).With("id", id) //nolint:wrapcheck // shared error form
	}
	return nil
}
