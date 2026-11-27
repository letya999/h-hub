package worker

import (
	"context"
	"database/sql"
	"fmt"
)

const spaceWorkers = `SELECT i.id FROM intents i JOIN fsm.machines m ON m.kind=i.kind AND m.id=i.id
	WHERE i.kind='worker' AND i.space=$1 AND m.state<>'absent'`

// BlocksWorkspace protects mount preparation while another revision can
// still change directories that may become mount roots.
func BlocksWorkspace(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, space, digest string) (bool, error) {
	var found bool
	if err := q.QueryRowContext(ctx, "SELECT EXISTS("+spaceWorkers+" AND i.digest<>$2)", space, digest).
		Scan(&found); err != nil {
		return false, fmt.Errorf("check worker workspace revision: %w", err)
	}
	return found, nil
}

// SpaceHasLive reports whether a generation still owns workspace mounts.
func (r *Registry) SpaceHasLive(ctx context.Context, space string) (bool, error) {
	var found bool
	if err := r.db.QueryRowContext(ctx, "SELECT EXISTS("+spaceWorkers+")", space).Scan(&found); err != nil {
		return false, fmt.Errorf("inspect space worker registry: %w", err)
	}
	return found, nil
}

// DrainSpace records stop intents in the caller's space destruction transaction.
func (r *Registry) DrainSpace(ctx context.Context, tx *sql.Tx, space string) error {
	keys, err := spaceWorkerIDs(ctx, tx, space)
	if err != nil {
		return err
	}
	for _, id := range keys {
		w, err := lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := r.stop(ctx, tx, w, "space-destroy"); err != nil {
			return err
		}
	}
	return nil
}

func spaceWorkerIDs(ctx context.Context, tx *sql.Tx, space string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, spaceWorkers+" ORDER BY i.id", space)
	if err != nil {
		return nil, fmt.Errorf("list space workers: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close
	var keys []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan space worker: %w", err)
		}
		keys = append(keys, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read space workers: %w", err)
	}
	return keys, nil
}
