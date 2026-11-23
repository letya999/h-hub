//go:build hhubdb

package svc

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestLateReceiptRecordedWithoutReopening(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.late-receipt")
	ctx := context.Background()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	if err := fsm.EnsureSchema(ctx, db, "fsm"); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db, Migrations(), "migrations"); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, deps: Deps{Now: time.Now}, jobM: JobMachine(), runM: RunMachine()}
	jobID := fmt.Sprintf("late-job-%d", time.Now().UnixNano())
	runID := "late-run-" + jobID
	seedSettlingRun(t, ctx, db, s, jobID, runID)
	rep := &runReporter{s}
	for _, e := range []tv1.RunEvent{{Seq: 1, Kind: "start"}, {Seq: 2, Kind: "final-ok", Data: "answer"}, {Seq: 3, Kind: "progress", Data: "late"}} {
		if err := rep.applyEvent(ctx, runID, e); err != nil {
			t.Fatalf("event %d: %v", e.Seq, err)
		}
	}
	if err := rep.applyEvent(ctx, runID, tv1.RunEvent{Seq: 2, Kind: "final-ok", Data: "answer"}); err != nil {
		t.Fatalf("duplicate final: %v", err)
	}
	if err := rep.applyEvent(ctx, runID, tv1.RunEvent{Seq: 4, Kind: "final-fail", Data: "late failure"}); err != nil {
		t.Fatalf("late final: %v", err)
	}
	var state, result string
	if err := db.QueryRowContext(ctx, `SELECT m.state,j.result FROM jobs j JOIN fsm.machines m ON m.kind='job' AND m.id=j.id WHERE j.id=$1`, jobID).Scan(&state, &result); err != nil {
		t.Fatal(err)
	}
	if state != "succeeded" || result != "answer" {
		t.Fatalf("reopened job: %s %q", state, result)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM run_events WHERE run=$1`, runID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("late receipt missing: %d events", n)
	}
}

func seedSettlingRun(t *testing.T, ctx context.Context, db *sql.DB, s *Server, jobID, runID string) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // test rollback
	if _, err := tx.ExecContext(ctx, `INSERT INTO jobs(id,owner,space,conversation,input,idem,conv_seq)
		VALUES($1,'o','s','c','hello',$1,1)`, jobID); err != nil {
		t.Fatal(err)
	}
	if err := insertRun(ctx, tx, runRow{id: runID, job: jobID, owner: "o", space: "s", conv: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := s.jobM.Create(ctx, tx, clock{time.Now}, fsm.CreateReq{ID: jobID, Initial: "queued", Actor: "api"}); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"admit", "dispatch"} {
		if err := s.jobM.Transition(ctx, tx, fsm.Request{ID: jobID, Event: event, Actor: "task-engine"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.runM.Create(ctx, tx, clock{time.Now}, fsm.CreateReq{ID: runID, Initial: "dispatched", Actor: "task-engine"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
