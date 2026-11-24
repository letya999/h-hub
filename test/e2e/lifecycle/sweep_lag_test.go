//go:build hhube2e

package lifecycle

import (
	"context"
	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
	"testing"
	"time"
)

func TestSweepLagOperatorAlert(t *testing.T) {
	testkit.Req(t, "sys.sweep-lag")
	ctx := context.Background()
	owner := "lag_" + Nonce
	space := EnsureSpace(t, owner, Idem("lag-space"))
	scope := OwnerSpace{Owner: owner, Space: space}
	client := TaskClient(t, scope)
	if _, err := client.SetHold(ctx, tv1.SetHoldReq{Space: space, Active: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := TaskClient(t, scope).SetHold(ctx, tv1.SetHoldReq{Space: space, Active: false}); err != nil {
			t.Error(err)
		}
	})
	job, err := client.CreateJob(ctx, tv1.CreateJobReq{Space: space, Conversation: "c1",
		Input: "overdue", Idem: Idem("lag-job")})
	if err != nil {
		t.Fatal(err)
	}
	db := AdminDB(t, "task_engine")
	// Backdate far enough that this job is always the oldest pending
	// deadline: the sweeper alerts on the oldest row only, and other
	// tests' overdue machines must not outrank it.
	result, err := db.ExecContext(ctx, `UPDATE fsm.machines SET deadline_at=now()-interval '1 day'
		WHERE kind='job' AND id=$1 AND state='queued'`, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("held job not queued: rows=%d err=%v", n, err)
	}
	// The durable intent lands first; the cross-service delivery
	// follows through the task outbox and channel outbox.
	testkit.Poll(t, 30*time.Second, 100*time.Millisecond, func() bool {
		var count int
		err := db.QueryRowContext(ctx, "SELECT count(*) FROM operator_alerts WHERE key=$1",
			"sweep-lag-"+job.ID).Scan(&count)
		return err == nil && count == 1
	})
	WaitOperatorDelivery(t, "sweep-lag-"+job.ID, "Job deadlines are overdue")
	_, err = MockClient(t, "operator", "ReadOutbox").Alert(ctx,
		cv1.AlertReq{Key: "denied-" + job.ID, Code: "sweep-lag"})
	testkit.Deny(t, "sys.sweep-lag", herrors.CodeOf(err) == herrors.CodeUnauthorized)
}
