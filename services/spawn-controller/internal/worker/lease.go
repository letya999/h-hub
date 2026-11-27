package worker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// Lease identifies the only caller allowed to renew or release a generation.
type Lease struct {
	Owner, Space, Worker, Generation, ID string
}

// ChangeLease operates only on the authenticated lease's live generation.
func (r *Registry) ChangeLease(ctx context.Context, l Lease, event string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin worker %s: %w", event, err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	w, err := lock(ctx, tx, l.Worker)
	if errors.Is(err, sql.ErrNoRows) {
		return herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	if err != nil {
		return err
	}
	if err := verifyLease(w, l); err != nil {
		return err
	}
	if err := r.applyLeaseChange(ctx, tx, w, event); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit worker %s: %w", event, err)
	}
	return nil
}

func (r *Registry) applyLeaseChange(ctx context.Context, tx *sql.Tx, w Record, event string) error {
	if event != "renew" && event != "release" && event != "retire" {
		return herrors.New(herrors.CodeInvalid, "err.invalid-arg")
	}
	if event == "retire" {
		return r.stop(ctx, tx, w, "terminal-outcome-uncertain")
	}
	if event == "release" && w.State == idle {
		return nil
	}
	if w.State != leased || !w.currentAt(r.now()) {
		return claimConflict(w.State)
	}
	ttl := leaseTTL
	if event == "release" {
		ttl = idleTTL
	}
	return r.transition(ctx, tx, w, event, ttl)
}

func verifyLease(w Record, l Lease) error {
	if w.Owner != l.Owner || w.Space != l.Space {
		return herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	if w.Generation != l.Generation || w.Lease != l.ID {
		return herrors.New(herrors.CodeStale, "err.stale")
	}
	return nil
}
