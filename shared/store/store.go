// Package store is the single Postgres access layer: one driver (pgx),
// embedded numbered migrations under an advisory lock, and temporary
// test schemas via testkit.
package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // single shared driver
)

// Connect opens a pgx-backed pool and verifies it.
func Connect(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open pgx: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close() //nolint:errcheck // best-effort close
		return nil, fmt.Errorf("ping: %w", err)
	}
	return db, nil
}
