//go:build hhubdb

package fsm

import (
	"bufio"
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

// TestKillVictim is the crash-test victim spawned by
// TestKillBetweenCommitAndEffect. In the parent process it returns at
// once; in the victim it commits a transition with an outbox effect,
// reports the commit, and waits to be killed before any effect runs.
func TestKillVictim(t *testing.T) {
	env, ok := testkit.Victim(t)
	if !ok {
		return
	}
	ctx := context.Background()
	conn, err := store.Connect(ctx, env["DSN"])
	if err != nil {
		t.Fatalf("victim connect: %v", err)
	}
	spec := testSpec()
	spec.Kind = env["KIND"]
	spec.Schema = env["SCHEMA"]
	m, err := New(spec)
	if err != nil {
		t.Fatalf("victim spec: %v", err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("victim tx: %v", err)
	}
	if err := m.Create(ctx, tx, sysClock{},
		CreateReq{ID: "m1", Initial: "queued", Actor: "victim"}); err != nil {
		t.Fatalf("victim create: %v", err)
	}
	err = m.Transition(ctx, tx, Request{
		ID: "m1", Event: "start", Actor: "victim",
		Effects: []Effect{{Topic: "victim-effect", Payload: []byte("bang")}},
	}, nil)
	if err != nil {
		t.Fatalf("victim transition: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("victim commit: %v", err)
	}
	if _, err := os.Stdout.WriteString("COMMITTED\n"); err != nil {
		t.Fatalf("victim report: %v", err)
	}
	select {} // die by SIGKILL here: the effect must never run in-process
}

// TestKillBetweenCommitAndEffect kills a victim process with SIGKILL
// between transition commit and effect execution, then proves from a
// fresh process that the state and the outbox intent survived, and the
// pending effect applies exactly once under its idempotency key (seq).
func TestKillBetweenCommitAndEffect(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.kill-uncertain", "adr.fsm.outbox-same-tx")
	conn, schema := db(t)
	m := machine(t, schema)
	cmd, out := testkit.Spawn(t, "TestKillVictim", map[string]string{
		"DSN":    testkit.DSN(),
		"SCHEMA": schema,
		"KIND":   m.Kind(),
	})
	waitLine(t, out, "COMMITTED")
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill victim: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("victim exited cleanly — the kill never happened")
	}
	if got := stateOf(t, conn, m, "m1"); got != "active" {
		t.Fatalf("state=%s after kill", got)
	}
	if n := count(t, conn, "SELECT count(*) FROM "+schema+
		".outbox WHERE done_at IS NULL"); n != 1 {
		t.Fatalf("pending outbox=%d, want the committed intent", n)
	}
	applyOutbox(t, conn, schema)
	applyOutbox(t, conn, schema) // replay must not duplicate the effect
	if n := count(t, conn, "SELECT count(*) FROM "+schema+".kill_effects"); n != 1 {
		t.Fatalf("effects=%d, want exactly once", n)
	}
	if n := count(t, conn, "SELECT count(*) FROM "+schema+
		".outbox WHERE done_at IS NOT NULL"); n != 1 {
		t.Fatalf("done outbox=%d", n)
	}
}

// waitLine fails when the victim does not print want within the
// deadline — a dead victim must not hang the suite.
func waitLine(t *testing.T, r *bufio.Reader, want string) {
	t.Helper()
	got := make(chan string, 1)
	go func() {
		line, _ := r.ReadString('\n')
		got <- line
	}()
	select {
	case line := <-got:
		if strings.TrimSpace(line) != want {
			t.Fatalf("victim reported %q, want %q", line, want)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("victim never reported %q", want)
	}
}

// applyOutbox is the survivor-side executor: every pending intent
// lands in the effects table under its seq idempotency key, then is
// marked done. A replay is a no-op, so the effect happens once even
// if the executor itself dies between the two writes.
func applyOutbox(t *testing.T, conn *sql.DB, schema string) {
	t.Helper()
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS "+
		schema+".kill_effects (outbox_seq bigint PRIMARY KEY, payload bytea)"); err != nil {
		t.Fatal(err)
	}
	rows, err := conn.QueryContext(ctx, "SELECT seq, payload FROM "+
		schema+".outbox WHERE done_at IS NULL ORDER BY seq")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close
	for rows.Next() {
		var seq int64
		var payload []byte
		if err := rows.Scan(&seq, &payload); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO "+schema+
			".kill_effects (outbox_seq, payload) VALUES ($1,$2) "+
			"ON CONFLICT (outbox_seq) DO NOTHING", seq, payload); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, "UPDATE "+schema+
			".outbox SET done_at=now() WHERE seq=$1", seq); err != nil {
			t.Fatal(err)
		}
	}
}
