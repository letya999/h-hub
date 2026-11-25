//go:build hhubdb

package svc

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

// waitingFixture builds a running job/run pair and reports the
// owner-request event the runtime journals when an ownerreq tool
// comes back pending.
func waitingFixture(t *testing.T) (*Server, *sql.DB, string, string) {
	t.Helper()
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
	j := jobRow{id: jobID, owner: "o", space: "s", conv: "c", input: "hi", idem: jobID}
	if err := s.disp.bindRun(ctx, j, runID, "digest", sv1.EnsureRuntimeResp{
		Runtime: "rt-test", Endpoint: "http://runtime"}); err != nil {
		t.Fatal(err)
	}
	if err := s.rep.applyEvent(ctx, runID, tv1.RunEvent{Seq: 1, Kind: "start"}); err != nil {
		t.Fatal(err)
	}
	return s, db, jobID, runID
}

func TestOwnerRequestEventWaitsJob(t *testing.T) {
	t.Parallel()
	s, db, jobID, runID := waitingFixture(t)
	ctx := context.Background()
	deadline := time.Now().Add(90 * time.Second).Unix()
	ev := tv1.RunEvent{Seq: 2, Kind: "owner-request",
		Data: `{"request":"oqr-x","deadline":` + jsonInt(deadline) + `}`}
	if err := s.rep.applyEvent(ctx, runID, ev); err != nil {
		t.Fatal(err)
	}
	if state, err := s.jobM.State(ctx, db, jobID); err != nil || state != "waiting_owner" {
		t.Fatalf("state %q %v", state, err)
	}
	if err := s.rep.applyEvent(ctx, runID, tv1.RunEvent{Seq: 3, Kind: "progress"}); err != nil {
		t.Fatal(err)
	}
	var req string
	var dl time.Time
	if err := db.QueryRowContext(ctx, `SELECT j.owner_request, m.deadline_at FROM jobs j
		JOIN fsm.machines m ON m.kind='job' AND m.id=j.id WHERE j.id=$1`, jobID).
		Scan(&req, &dl); err != nil {
		t.Fatal(err)
	}
	if req != "oqr-x" {
		t.Fatalf("bound request %q", req)
	}
	if dl.Unix() != deadline {
		t.Fatalf("deadline %v want %v", dl.Unix(), deadline)
	}
}

func TestFinalSettlesRunButJobKeepsWaiting(t *testing.T) {
	t.Parallel()
	s, db, jobID, runID := waitingFixture(t)
	ctx := context.Background()
	var jobDeadline time.Time
	if err := db.QueryRowContext(ctx, "SELECT deadline_at FROM fsm.machines WHERE kind='job' AND id=$1", jobID).
		Scan(&jobDeadline); err != nil {
		t.Fatal(err)
	}
	// The run asks with one minute left on the job and gives the human two.
	s.deps.Now = func() time.Time { return jobDeadline.Add(-time.Minute) }
	deadline := jobDeadline.Add(time.Minute).Unix()
	ev := tv1.RunEvent{Seq: 2, Kind: "owner-request",
		Data: `{"request":"oqr-x","deadline":` + jsonInt(deadline) + `}`}
	if err := s.rep.applyEvent(ctx, runID, ev); err != nil {
		t.Fatal(err)
	}
	if err := s.rep.applyEvent(ctx, runID, tv1.RunEvent{
		Seq: 3, Kind: "final-ok", Data: "i asked the owner"}); err != nil {
		t.Fatal(err)
	}
	if state, err := s.jobM.State(ctx, db, jobID); err != nil || state != "waiting_owner" {
		t.Fatalf("job state %q %v", state, err)
	}
	if state, err := s.runM.State(ctx, db, runID); err != nil || state != "settled" {
		t.Fatalf("run state %q %v", state, err)
	}
	var released *time.Time
	if err := db.QueryRowContext(ctx,
		"SELECT claim_released_at FROM runs WHERE id=$1", runID).Scan(&released); err != nil {
		t.Fatal(err)
	}
	if released == nil {
		t.Fatal("conversation claim still held: the lane must stay free")
	}
	turn, err := s.ops.getRequestTurn(ctx, tv1.RequestTurnReq{Job: jobID, Run: runID, Request: "oqr-x"})
	if err != nil || turn.Done || turn.Introduction != "i asked the owner" ||
		turn.Conversation != "c" || turn.Deadline != jobDeadline.Unix() {
		t.Fatalf("waiting turn: %+v %v, want job deadline %d", turn, err, jobDeadline.Unix())
	}
}

func TestRequestTurnClosesForEveryStoppedJob(t *testing.T) {
	t.Parallel()
	for state, events := range map[string][]string{
		"succeeded": {"settle-ok"}, "failed": {"settle-fail"},
		"cancelled": {"cancel"}, "quarantined": {"lost", "expire"},
	} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			s, _, job, run := waitingFixture(t)
			ctx := context.Background()
			req := tv1.RequestTurnReq{Job: job, Run: run, Request: "unreported"}
			if got, err := s.ops.getRequestTurn(ctx, req); err != nil || got != (tv1.RequestTurnResp{}) {
				t.Fatalf("unreported request during turn: %+v %v", got, err)
			}
			for _, event := range events {
				if err := s.disp.withJobTx(ctx, job, event, "test", nil); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := s.ops.getRequestTurn(ctx, req); err != nil || !got.Done || got.Introduction != "" {
				t.Fatalf("request after %s: %+v %v", state, got, err)
			}
		})
	}
}

func TestResumeReturnsJobToQueue(t *testing.T) {
	t.Parallel()
	s, db, jobID, runID := waitingFixture(t)
	ctx := context.Background()
	deadline := time.Now().Add(90 * time.Second).Unix()
	ev := tv1.RunEvent{Seq: 2, Kind: "owner-request",
		Data: `{"request":"oqr-x","deadline":` + jsonInt(deadline) + `}`}
	if err := s.rep.applyEvent(ctx, runID, ev); err != nil {
		t.Fatal(err)
	}
	if err := s.run.finishWaiting(ctx, jobID, "resume", ""); err != nil {
		t.Fatal(err)
	}
	if state, err := s.jobM.State(ctx, db, jobID); err != nil || state != "queued" {
		t.Fatalf("state %q %v", state, err)
	}
}

func TestDenialFailsJobWithReason(t *testing.T) {
	t.Parallel()
	s, db, jobID, runID := waitingFixture(t)
	ctx := context.Background()
	deadline := time.Now().Add(90 * time.Second).Unix()
	ev := tv1.RunEvent{Seq: 2, Kind: "owner-request",
		Data: `{"request":"oqr-x","deadline":` + jsonInt(deadline) + `}`}
	if err := s.rep.applyEvent(ctx, runID, ev); err != nil {
		t.Fatal(err)
	}
	if err := s.run.finishWaiting(ctx, jobID, "settle-fail", "owner denied the request"); err != nil {
		t.Fatal(err)
	}
	if state, err := s.jobM.State(ctx, db, jobID); err != nil || state != "failed" {
		t.Fatalf("state %q %v", state, err)
	}
	var result string
	if err := db.QueryRowContext(ctx,
		"SELECT COALESCE(result,'') FROM jobs WHERE id=$1", jobID).Scan(&result); err != nil {
		t.Fatal(err)
	}
	if result != "owner denied the request" {
		t.Fatalf("result %q", result)
	}
}

func TestWaitingLaneIsFree(t *testing.T) {
	t.Parallel()
	s, db, _, runID := waitingFixture(t)
	ctx := context.Background()
	deadline := time.Now().Add(90 * time.Second).Unix()
	ev := tv1.RunEvent{Seq: 2, Kind: "owner-request",
		Data: `{"request":"oqr-x","deadline":` + jsonInt(deadline) + `}`}
	if err := s.rep.applyEvent(ctx, runID, ev); err != nil {
		t.Fatal(err)
	}
	if err := s.rep.applyEvent(ctx, runID, tv1.RunEvent{
		Seq: 3, Kind: "final-ok", Data: "asked"}); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // test rollback after commit
	if _, err := tx.ExecContext(ctx, `INSERT INTO conversations(owner,space,conversation,next_seq)
		VALUES('o','s','c',3)`); err != nil {
		t.Fatal(err)
	}
	nextID := newID("jobnext")
	if _, err := tx.ExecContext(ctx, `INSERT INTO jobs(id,owner,space,conversation,input,idem,conv_seq)
		VALUES($1,'o','s','c','next',$1,2)`, nextID); err != nil {
		t.Fatal(err)
	}
	if err := s.jobM.Create(ctx, tx, clock{time.Now}, fsm.CreateReq{
		ID: nextID, Initial: "queued", Actor: "api"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	read, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback() //nolint:errcheck // test rollback after commit
	next, err := claimable(ctx, read)
	if err != nil {
		t.Fatal(err)
	}
	if next.id != nextID {
		t.Fatalf("lane blocked behind waiting job: claimed %q", next.id)
	}
}

func jsonInt(n int64) string {
	return strconv.FormatInt(n, 10)
}
