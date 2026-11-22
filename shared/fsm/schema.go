package fsm

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed schema.sql
var schemaSQL string

// EnsureSchema creates the fsm schema and its uniform tables under the
// given name ("fsm" in production, a test schema in tests). It is
// idempotent and safe to run at every service boot; an advisory lock
// serializes concurrent callers because CREATE SCHEMA IF NOT EXISTS
// races under parallel first boots.
func EnsureSchema(ctx context.Context, db *sql.DB, schema string) error {
	if err := checkSchema(schema); err != nil {
		return err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("fsm schema conn: %w", err)
	}
	defer conn.Close() //nolint:errcheck // best-effort close
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock(735715)"); err != nil {
		return fmt.Errorf("fsm schema lock: %w", err)
	}
	defer conn.ExecContext(ctx, "SELECT pg_advisory_unlock(735715)") //nolint:errcheck // best-effort unlock
	ddl := strings.ReplaceAll(schemaSQL, "{{schema}}", schema)
	if _, err := conn.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("fsm schema: %w", err)
	}
	return nil
}
