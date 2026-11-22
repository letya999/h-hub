package fsm

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// WriteTick upserts a watchdog heartbeat row in the fsm schema. The
// row proves the watchdog loop is alive; /healthz reads its age via
// TickAge and goes stale when the loop stops.
func WriteTick(ctx context.Context, db *sql.DB, schema, name string, at time.Time) error {
	if err := checkSchema(schema); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO `+schema+`.watchdog_ticks (name, at)
		VALUES ($1,$2) ON CONFLICT (name) DO UPDATE SET at=excluded.at`, name, at)
	if err != nil {
		return fmt.Errorf("watchdog tick: %w", err)
	}
	return nil
}

// TickAge reports how old the last watchdog tick is; a missing row is
// reported as the maximum duration so health never pretends to be
// fresh without a recorded tick.
func TickAge(ctx context.Context, db *sql.DB, schema, name string, now time.Time) (time.Duration, error) {
	if err := checkSchema(schema); err != nil {
		return 0, err
	}
	var at time.Time
	err := db.QueryRowContext(ctx,
		"SELECT at FROM "+schema+".watchdog_ticks WHERE name=$1", name).Scan(&at)
	if err == sql.ErrNoRows {
		return 1 << 62, nil
	}
	if err != nil {
		return 0, fmt.Errorf("watchdog tick age: %w", err)
	}
	return now.Sub(at), nil
}
