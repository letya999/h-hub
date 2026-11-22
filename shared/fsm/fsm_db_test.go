//go:build hhubdb

package fsm

import (
	"context"
	"database/sql"
	stderrors "errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

// db opens a pool and gives the test its own scratch schema with the
// fsm tables in it; the schema is dropped on cleanup, so tests never
// share or clean tables by hand.
func db(t *testing.T) (*sql.DB, string) {
	t.Helper()
	ctx := context.Background()
	conn, err := store.Connect(ctx, testkit.DSN())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Logf("conn close: %v", err)
		}
	})
	schema := testkit.ScratchSchema(t, ctx, conn)
	if err := EnsureSchema(ctx, conn, schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return conn, schema
}

func machine(t *testing.T, schema string) *Machine {
	t.Helper()
	spec := testSpec()
	spec.Schema = schema
	spec.Kind = "t_" + strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, strings.ToLower(t.Name()))
	m, err := New(spec)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func create(t *testing.T, conn *sql.DB, m *Machine, id string) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	err = m.Create(ctx, tx, sysClock{}, CreateReq{ID: id, Initial: "queued", Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

type sysClock struct{}

func (sysClock) Now() time.Time { return time.Now().UTC() }

func transition(conn *sql.DB, m *Machine, r Request) error {
	ctx := context.Background()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	if err := m.Transition(ctx, tx, r, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func stateOf(t *testing.T, conn *sql.DB, m *Machine, id string) string {
	t.Helper()
	var s string
	err := conn.QueryRowContext(context.Background(),
		"SELECT state FROM "+m.table("machines")+" WHERE kind=$1 AND id=$2",
		m.Kind(), id).Scan(&s)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func count(t *testing.T, conn *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := conn.QueryRowContext(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTransitionWritesAll(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.fsm.cas-version", "adr.fsm.history-same-tx", "adr.fsm.outbox-same-tx")
	conn, schema := db(t)
	m := machine(t, schema)
	create(t, conn, m, "m1")
	err := transition(conn, m, Request{
		ID: "m1", Event: "start", Actor: "owner",
		Effects: []Effect{{Topic: "notify", Payload: []byte("hi")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, conn, m, "m1"); got != "active" {
		t.Fatalf("state=%s", got)
	}
	if n := count(t, conn,
		"SELECT count(*) FROM "+m.table("history")+
			" WHERE kind=$1 AND id='m1' AND to_state='active'", m.Kind()); n != 1 {
		t.Fatalf("history rows=%d", n)
	}
	if n := count(t, conn,
		"SELECT count(*) FROM "+m.table("outbox")+
			" WHERE kind=$1 AND machine_id='m1' AND topic='notify'", m.Kind()); n != 1 {
		t.Fatalf("outbox rows=%d", n)
	}
}

func TestForbiddenWritesNothing(t *testing.T) {
	t.Parallel()
	conn, schema := db(t)
	m := machine(t, schema)
	create(t, conn, m, "m1")
	err := transition(conn, m, Request{ID: "m1", Event: "finish", Actor: "owner"})
	if !stderrors.Is(err, ErrBadTransition) {
		t.Fatalf("want ErrBadTransition, got %v", err)
	}
	if got := stateOf(t, conn, m, "m1"); got != "queued" {
		t.Fatalf("state moved to %s", got)
	}
	if n := count(t, conn,
		"SELECT count(*) FROM "+m.table("history")+
			" WHERE kind=$1 AND id='m1'", m.Kind()); n != 1 {
		t.Fatalf("history rows=%d want only create", n)
	}
}

func TestRollbackLeavesNothing(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.fsm.outbox-same-tx", "sys.kill-uncertain")
	conn, schema := db(t)
	m := machine(t, schema)
	ctx := context.Background()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = m.Create(ctx, tx, sysClock{}, CreateReq{ID: "m1", Initial: "queued", Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	err = m.Transition(ctx, tx, Request{
		ID: "m1", Event: "start", Actor: "owner",
		Effects: []Effect{{Topic: "t", Payload: []byte("p")}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if n := count(t, conn,
		"SELECT count(*) FROM "+m.table("machines")+
			" WHERE kind=$1 AND id='m1'", m.Kind()); n != 0 {
		t.Fatal("machine row survived rollback")
	}
	if n := count(t, conn,
		"SELECT count(*) FROM "+m.table("outbox")+
			" WHERE kind=$1 AND machine_id='m1'", m.Kind()); n != 0 {
		t.Fatal("outbox intent survived rollback")
	}
}

func TestCASRace(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.fsm.cas-version")
	conn, schema := db(t)
	m := machine(t, schema)
	create(t, conn, m, "m1")
	ctx := context.Background()
	tx1, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	tx2, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback() //nolint:errcheck // best-effort close
	s1, v1, err := m.readRow(ctx, tx1, "m1")
	if err != nil {
		t.Fatal(err)
	}
	s2, v2, err := m.readRow(ctx, tx2, "m1")
	if err != nil {
		t.Fatal(err)
	}
	chg1 := Change{ID: "m1", From: s1, Event: "start", To: "active", Actor: "a"}
	if err := m.commitTransition(ctx, tx1, v1, chg1, nil); err != nil {
		t.Fatal(err)
	}
	if err := tx1.Commit(); err != nil {
		t.Fatal(err)
	}
	chg2 := Change{ID: "m1", From: s2, Event: "start", To: "active", Actor: "b"}
	err = m.commitTransition(ctx, tx2, v2, chg2, nil)
	if !stderrors.Is(err, ErrStale) {
		t.Fatalf("CAS loser must get ErrStale, got %v", err)
	}
	if got := stateOf(t, conn, m, "m1"); got != "active" {
		t.Fatalf("state=%s", got)
	}
	if n := count(t, conn,
		"SELECT count(*) FROM "+m.table("history")+
			" WHERE kind=$1 AND id='m1' AND event='start'", m.Kind()); n != 1 {
		t.Fatalf("exactly one start transition, got %d", n)
	}
}

func TestDeadlineSweep(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.fsm.deadline-nonfinal", "sys.sweep-lag")
	conn, schema := db(t)
	m := machine(t, schema)
	create(t, conn, m, "m1")
	now := time.Now().UTC().Add(2 * time.Minute)
	rows, err := m.DueRows(context.Background(), conn, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].DeadlineEvent != "timeout" {
		t.Fatalf("due=%+v", rows)
	}
	n, err := m.Sweep(context.Background(), conn, now, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("swept=%d", n)
	}
	if got := stateOf(t, conn, m, "m1"); got != "failed" {
		t.Fatalf("state=%s", got)
	}
}

func TestSweepSkipsMovedOn(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.fsm.sweep-reread")
	conn, schema := db(t)
	m := machine(t, schema)
	create(t, conn, m, "m1")
	err := transition(conn, m, Request{ID: "m1", Event: "start", Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := m.SweepRow(context.Background(), conn, "m1",
		time.Now().UTC().Add(2*time.Minute), nil)
	if err != nil {
		t.Fatal(err)
	}
	if done {
		t.Fatal("sweep must not fire on a moved-on row")
	}
	if got := stateOf(t, conn, m, "m1"); got != "active" {
		t.Fatalf("state=%s", got)
	}
}

func TestConcurrentTransitionsOneWins(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.fsm.cas-version")
	conn, schema := db(t)
	m := machine(t, schema)
	create(t, conn, m, "m1")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, actor := range []string{"a", "b"} {
		wg.Add(1)
		go func(i int, actor string) {
			defer wg.Done()
			errs[i] = transition(conn, m,
				Request{ID: "m1", Event: "start", Actor: actor})
		}(i, actor)
	}
	wg.Wait()
	if got := stateOf(t, conn, m, "m1"); got != "active" {
		t.Fatalf("state=%s", got)
	}
	if n := count(t, conn,
		"SELECT count(*) FROM "+m.table("history")+
			" WHERE kind=$1 AND id='m1' AND event='start'", m.Kind()); n != 1 {
		t.Fatalf("one winner expected, history=%d (errs=%v)", n, errs)
	}
}

func TestLeaseContention(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.fsm.lease-atomic", "adr.fsm.single-sweeper")
	conn, schema := db(t)
	ctx := context.Background()
	name := "lease_" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "_")
	now := time.Now().UTC()
	req := func(owner string, at time.Time) LeaseReq {
		return LeaseReq{Name: name, Owner: owner, TTL: time.Minute, Now: at}
	}
	ok, err := AcquireLease(ctx, conn, schema, req("a", now))
	if err != nil || !ok {
		t.Fatalf("first acquire ok=%v err=%v", ok, err)
	}
	ok, err = AcquireLease(ctx, conn, schema, req("b", now))
	if err != nil || ok {
		t.Fatalf("contended acquire ok=%v err=%v", ok, err)
	}
	ok, err = AcquireLease(ctx, conn, schema, req("b", now.Add(2*time.Minute)))
	if err != nil || !ok {
		t.Fatalf("expired acquire ok=%v err=%v", ok, err)
	}
	if err := ReleaseLease(ctx, conn, schema, name, "a"); err != nil {
		t.Fatal(err)
	}
	ok, err = AcquireLease(ctx, conn, schema, req("a", now))
	if err != nil || ok {
		t.Fatal("lease must still belong to b")
	}
	if err := ReleaseLease(ctx, conn, schema, name, "b"); err != nil {
		t.Fatal(err)
	}
}
