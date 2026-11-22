// Package testkit is the shared test infrastructure: requirement
// markers for the trace gate, temporary Postgres schemas, and
// goroutine-leak checks.
package testkit

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
// owner decision: temporary schemas in the same container. The schema
// name is returned so tests can build qualified identifiers. Use
// ScratchSchema instead when a test needs a multi-connection pool.
func TempSchema(t *testing.T, ctx context.Context, driver, dsn string) (*sql.DB, string) {
	t.Helper()
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatalf("temp schema open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		db.Close() //nolint:errcheck // best-effort close, runs after the drop
	})
	name := ScratchSchema(t, ctx, db)
	if _, err := db.ExecContext(ctx, "SET search_path TO "+name); err != nil {
		t.Fatalf("temp schema search_path: %v", err)
	}
	return db, name
}

// ScratchSchema creates a uniquely named schema on an open pool and
// drops it on test cleanup. Unlike TempSchema it sets no search_path:
// callers qualify table names explicitly, and the pool may serve
// several connections (needed by concurrency tests).
func ScratchSchema(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	name := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+name); err != nil {
		t.Fatalf("scratch schema create: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), //nolint:errcheck // best-effort cleanup
			"DROP SCHEMA "+name+" CASCADE")
	})
	return name
}

const (
	victimMarker  = "HHUB_TESTKIT_VICTIM"
	victimPayload = "HHUB_TESTKIT_PAYLOAD"
)

// Spawn re-runs this test binary executing only the named test, with
// env entries passed to the victim as one JSON payload. The named test is
// the crash-test victim; it detects its role through Victim. The
// returned command is started and its stdout buffered for sync
// markers; cleanup kills a survivor.
func Spawn(t *testing.T, testName string, env map[string]string) (*exec.Cmd, *bufio.Reader) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
	payload, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("spawn env: %v", err)
	}
	cmd.Env = append(cmd.Environ(), victimMarker+"=1", victimPayload+"="+string(payload))
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("spawn stdout: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn start: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill() //nolint:errcheck // best-effort kill of a survivor
	})
	return cmd, bufio.NewReader(out)
}

// Victim reports whether this process is a Spawn-launched victim and
// returns the env map Spawn was given. A victim test starts with
// `env, ok := testkit.Victim(t)` and
// returns early when ok is false — a plain return, never a skip.
func Victim(t *testing.T) (map[string]string, bool) {
	t.Helper()
	if os.Getenv(victimMarker) != "1" {
		return nil, false
	}
	var env map[string]string
	if err := json.Unmarshal([]byte(os.Getenv(victimPayload)), &env); err != nil {
		t.Fatalf("victim env: %v", err)
	}
	return env, true
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
