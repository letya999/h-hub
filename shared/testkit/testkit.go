// Package testkit is the shared test infrastructure: requirement
// markers for the trace gate, temporary Postgres schemas, and
// goroutine-leak checks.
package testkit

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"testing"
	"time"
)

var reqRe = regexp.MustCompile(`^(adr|cap|sys)\.[a-z0-9-]+(\.[a-z0-9-]+)?$`)

// Req marks a test as a check for requirement IDs. IDs must use the
// trace format (adr.<slug>.<invariant>, cap.<name>, sys.<name>); the
// trace gate verifies each ID resolves to a real requirement.
func Req(t *testing.T, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if !reqRe.MatchString(id) {
			t.Fatalf("testkit.Req: malformed id %q", id)
		}
	}
}

// DSN returns the test Postgres DSN. The check recipe sets it after
// bringing postgres up; tests that need a database run only under the
// hhubdb build tag inside just check.
func DSN() string {
	if v := os.Getenv("HHUB_TEST_DSN"); v != "" {
		return v
	}
	return "postgres://postgres@postgres:5432/postgres?sslmode=disable"
}

// TempSchema creates a scratch schema, points a single-connection pool
// at it via search_path, and drops it on test cleanup. This matches the
// owner decision: temporary schemas in the same container.
func TempSchema(t *testing.T, ctx context.Context, driver, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatalf("temp schema open: %v", err)
	}
	db.SetMaxOpenConns(1)
	name := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+name); err != nil {
		db.Close() //nolint:errcheck // best-effort close
		t.Fatalf("temp schema create: %v", err)
	}
	if _, err := db.ExecContext(ctx, "SET search_path TO "+name); err != nil {
		db.Close() //nolint:errcheck // best-effort close
		t.Fatalf("temp schema search_path: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), //nolint:errcheck // best-effort cleanup
			"DROP SCHEMA "+name+" CASCADE")
		db.Close() //nolint:errcheck // best-effort close
	})
	return db
}

// Goroutines snapshots the live goroutine count.
func Goroutines() int { return runtime.NumGoroutine() }

// CheckLeak fails the test when more goroutines are alive than before.
func CheckLeak(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("goroutine leak: before=%d after=%d", before, runtime.NumGoroutine())
}
