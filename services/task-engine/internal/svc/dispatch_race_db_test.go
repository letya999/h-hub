//go:build hhubdb

package svc

import (
	"context"
	"testing"
	"time"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestRunCanFinishBeforeSubmitReturns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	if err := fsm.EnsureSchema(ctx, db, "fsm"); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db, Migrations(), "migrations"); err != nil {
		t.Fatal(err)
	}
	s := New("task-engine", db, Deps{Now: time.Now})
	jobID, runID := newID("job"), newID("run")
	j := jobRow{id: jobID, owner: "o", space: "s", conv: "c", input: "hi", idem: jobID}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // test rollback after commit
	if _, err := tx.ExecContext(ctx, `INSERT INTO jobs(id,owner,space,conversation,input,idem,conv_seq)
		VALUES($1,'o','s','c','hi',$1,1)`, jobID); err != nil {
		t.Fatal(err)
	}
	if err := s.jobM.Create(ctx, tx, clock{time.Now}, fsm.CreateReq{ID: jobID, Initial: "queued", Actor: "api"}); err != nil {
		t.Fatal(err)
	}
	if err := s.jobM.Transition(ctx, tx, fsm.Request{ID: jobID, Event: "admit", Actor: "dispatcher"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := s.disp.bindRun(ctx, j, runID, "digest", sv1.EnsureRuntimeResp{
		Runtime: "rt-test", Endpoint: "http://runtime"}); err != nil {
		t.Fatal(err)
	}
	for _, e := range []tv1.RunEvent{{Seq: 1, Kind: "start"}, {Seq: 2, Kind: "final-ok", Data: "answer"}} {
		if err := s.rep.applyEvent(ctx, runID, e); err != nil {
			t.Fatalf("run reported before SubmitRun returned: %v", err)
		}
	}
	if state, err := s.jobM.State(ctx, db, jobID); err != nil || state != "succeeded" {
		t.Fatalf("early completed job: %q %v", state, err)
	}
}
