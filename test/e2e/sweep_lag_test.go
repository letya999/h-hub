//go:build hhube2e

package e2e

import (
	"context"
	"testing"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestSweepLagOperatorAlert(t *testing.T) {
	testkit.Req(t, "sys.sweep-lag")
	ctx := context.Background()
	owner := "lag_" + nonce
	space := ensureSpace(t, owner, idem("lag-space"))
	scope := ownerSpace{owner, space}
	client := taskClient(t, scope)
	if _, err := client.SetHold(ctx, tv1.SetHoldReq{Space: space, Active: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := taskClient(t, scope).SetHold(ctx, tv1.SetHoldReq{Space: space, Active: false}); err != nil {
			t.Error(err)
		}
	})
	job, err := client.CreateJob(ctx, tv1.CreateJobReq{Space: space, Conversation: "c1",
		Input: "overdue", Idem: idem("lag-job")})
	if err != nil {
		t.Fatal(err)
	}
	db := adminDB(t, "task_engine")
	result, err := db.ExecContext(ctx, `UPDATE fsm.machines SET deadline_at=now()-interval '1 minute'
		WHERE kind='job' AND id=$1 AND state='queued'`, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("held job not queued: rows=%d err=%v", n, err)
	}
	waitOperatorDelivery(t, "sweep-lag-"+job.ID, "Job deadlines are overdue")
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM operator_alerts WHERE key=$1",
		"sweep-lag-"+job.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("lag alert intent: count=%d err=%v", count, err)
	}
	_, err = mockClient(t, "operator", "ReadOutbox").Alert(ctx,
		cv1.AlertReq{Key: "denied-" + job.ID, Code: "sweep-lag"})
	testkit.Deny(t, "sys.sweep-lag", herrors.CodeOf(err) == herrors.CodeUnauthorized)
}
