package svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// reservation is one estimate take under the daily and per-run
// ceilings.
type reservation struct {
	owner, run string
	estimate   int64
	daily      int64
	runLimit   int64
}

// reserve atomically takes an estimate under both ceilings: one
// conditional upsert on (owner, day) and one on run, in a single
// transaction. Zero rows updated means the ceiling is already met —
// a refusal, never a silent clamp.
func reserve(ctx context.Context, db *sql.DB, a reservation) error {
	if a.estimate > a.daily || a.estimate > a.runLimit {
		return errOverBudget
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reserve begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	day := time.Now().UTC().Truncate(24 * time.Hour)
	if err := bump(ctx, tx, `INSERT INTO usage_daily(owner, day, reserved)
		VALUES ($1,$2,$3) ON CONFLICT (owner,day) DO UPDATE
		SET reserved = usage_daily.reserved + $3
		WHERE usage_daily.spent + usage_daily.reserved + $3 <= $4`,
		a.owner, day, a.estimate, a.daily); err != nil {
		return err
	}
	if err := bump(ctx, tx, `INSERT INTO usage_runs(run, owner, day, reserved)
		VALUES ($1,$2,$3,$4) ON CONFLICT (run) DO UPDATE
		SET reserved = usage_runs.reserved + $4, reserved_at = now()
		WHERE usage_runs.spent + usage_runs.reserved + $4 <= $5`,
		a.run, a.owner, day, a.estimate, a.runLimit); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reserve commit: %w", err)
	}
	return nil
}

// errOverBudget marks a conditional update that matched no row.
var errOverBudget = errors.New("over budget")

func bump(ctx context.Context, tx *sql.Tx, q string, args ...any) error {
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("reserve update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reserve rows: %w", err)
	}
	if n == 0 {
		return errOverBudget
	}
	return nil
}

// settlement is one reserve return plus the charged spend.
type settlement struct {
	owner, run, route string
	reserved, charged int64
	measured          bool
}

// settle returns the unused reservation and charges the measured (or
// estimated) spend, then appends the spend event — one transaction
// for both counters and the ledger row.
func settle(ctx context.Context, db *sql.DB, a settlement) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("settle begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	day, err := runDay(ctx, tx, a.run)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE usage_daily
		SET spent = spent + $3, reserved = GREATEST(0, reserved - $4)
		WHERE owner=$1 AND day=$2`, a.owner, day, a.charged, a.reserved); err != nil {
		return fmt.Errorf("settle daily: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE usage_runs
		SET spent = spent + $2, reserved = GREATEST(0, reserved - $3)
		WHERE run=$1`, a.run, a.charged, a.reserved); err != nil {
		return fmt.Errorf("settle run: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO spend_events
		(run, owner, route, reserved, charged, measured)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		a.run, a.owner, a.route, a.reserved, a.charged, a.measured); err != nil {
		return fmt.Errorf("settle event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("settle commit: %w", err)
	}
	return nil
}

// runDay reads the day the run's reservation was booked under, so the
// settle lands on the same counter row across midnight.
func runDay(ctx context.Context, tx *sql.Tx, run string) (time.Time, error) {
	var day time.Time
	if err := tx.QueryRowContext(ctx,
		`SELECT day FROM usage_runs WHERE run=$1`, run).Scan(&day); err != nil {
		return day, fmt.Errorf("run day: %w", err)
	}
	return day, nil
}

// usageToday reads today's charged and reserved counters for /usage.
func usageToday(ctx context.Context, db *sql.DB, owner string) (spent, reserved int64, err error) {
	err = db.QueryRowContext(ctx, `SELECT spent, reserved FROM usage_daily
		WHERE owner=$1 AND day=$2`, owner,
		time.Now().UTC().Truncate(24*time.Hour)).Scan(&spent, &reserved)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("usage read: %w", err)
	}
	return spent, reserved, nil
}

// sweepReservations converts reservations abandoned by a crash into
// charged spend at their estimate; a live call still settles its own.
const reserveTTL = 10 * time.Minute

func sweepReservations(ctx context.Context, db *sql.DB, before time.Time) error {
	_, err := db.ExecContext(ctx, `WITH stale AS (
		SELECT run, owner, day, reserved FROM usage_runs
		WHERE reserved > 0 AND reserved_at < $1
		FOR UPDATE
	), upd AS (
		UPDATE usage_runs u SET spent = u.spent + s.reserved, reserved = 0
		FROM stale s WHERE u.run = s.run
	)
		UPDATE usage_daily d
		SET spent = d.spent + s.reserved,
		    reserved = GREATEST(0, d.reserved - s.reserved)
		FROM stale s WHERE d.owner = s.owner AND d.day = s.day`,
		before.UTC())
	if err != nil {
		return fmt.Errorf("reservation sweep: %w", err)
	}
	return nil
}
