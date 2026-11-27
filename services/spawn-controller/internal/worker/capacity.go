package worker

import (
	"context"
	"database/sql"
	"fmt"

	herrors "github.com/letya999/h-hub/shared/errors"
)

func (r *Registry) checkCapacity(ctx context.Context, tx *sql.Tx, key Key) error {
	if err := r.limits.Validate(); err != nil {
		return fmt.Errorf("worker capacity: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext('hhub-worker-capacity'))"); err != nil {
		return fmt.Errorf("lock worker capacity: %w", err)
	}
	blocked, err := BlocksWorkspace(ctx, tx, key.Space, key.Digest)
	if err != nil {
		return err
	}
	if blocked {
		return herrors.New(herrors.CodeCapacity, "err.worker-layout-busy")
	}
	var total, user int
	err = tx.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER (WHERE i.owner=$1)
		FROM intents i JOIN fsm.machines m ON m.kind=i.kind AND m.id=i.id
		WHERE i.kind='worker' AND m.state<>'absent'`, key.Owner).Scan(&total, &user)
	if err != nil {
		return fmt.Errorf("count worker generations: %w", err)
	}
	if total >= r.limits.MaxTotal || user >= r.limits.MaxPerUser {
		return herrors.New(herrors.CodeCapacity, "err.worker-capacity")
	}
	return nil
}

// availableSlot runs while the capacity transaction lock is held.
func availableSlot(ctx context.Context, tx *sql.Tx, maxTotal int) (int, error) {
	var slot sql.NullInt32
	err := tx.QueryRowContext(ctx, `SELECT min(slot) FROM generate_series(1,$1) AS slot
		WHERE NOT EXISTS (SELECT 1 FROM intents WHERE kind='worker' AND worker_slot=slot)`,
		maxTotal).Scan(&slot)
	if err != nil {
		return 0, fmt.Errorf("find free worker socket slot: %w", err)
	}
	if !slot.Valid {
		return 0, herrors.New(herrors.CodeCapacity, "err.worker-capacity")
	}
	return int(slot.Int32), nil
}
