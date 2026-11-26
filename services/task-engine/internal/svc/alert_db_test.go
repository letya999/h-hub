//go:build hhubdb

package svc

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLagAlertAndDeadlineTransitionCommitTogether(t *testing.T) {
	t.Parallel()
	s, db, job, _ := waitingFixture(t)
	lp := loopRunner{s: s}
	ctx := context.Background()
	now := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	statements := []string{
		"UPDATE fsm.machines SET deadline_at='1999-12-31' WHERE kind='job' AND id=$1",
		"ALTER TABLE operator_alerts ADD CONSTRAINT refuse_lag CHECK (code <> 'sweep-lag')",
	}
	if _, err := db.ExecContext(ctx, statements[0], job); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, statements[1]); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(ctx, "DELETE FROM fsm.machines WHERE kind='job' AND id=$1", job); err != nil {
			t.Error(err)
		}
	})
	if err := lp.alertSweepLag(ctx, now); err == nil || !strings.Contains(err.Error(), "refuse_lag") {
		t.Fatalf("expected the alert write to fail: %v", err)
	}
	state, err := s.jobM.State(ctx, db, job)
	if err != nil || state != "running" {
		t.Fatalf("failed alert left job %s, want running: %v", state, err)
	}
	if _, err := db.ExecContext(ctx, "ALTER TABLE operator_alerts DROP CONSTRAINT refuse_lag"); err != nil {
		t.Fatal(err)
	}
	if err := lp.alertSweepLag(ctx, now); err != nil {
		t.Fatal(err)
	}
	state, err = s.jobM.State(ctx, db, job)
	if err != nil || state != "uncertain" {
		t.Fatalf("committed alert left job %s, want uncertain: %v", state, err)
	}
	var count int
	err = db.QueryRowContext(ctx, `SELECT count(*) FROM operator_alerts a JOIN fsm.outbox o
		ON o.kind='alert' AND o.machine_id=a.key AND o.topic='operator-alert' WHERE a.key=$1`,
		"sweep-lag-"+job).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("committed alert and outbox count %d, want 1: %v", count, err)
	}
}
