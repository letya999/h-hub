//go:build hhube2e

package e2e

import (
	"context"
	"testing"
	"time"

	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestRuntimeDeadlineReapsContainer(t *testing.T) {
	t.Parallel()
	owner := "sweep_" + nonce
	space := ensureSpace(t, owner, idem("sweep-space"))
	scope := ownerSpace{owner, space}
	job, err := taskClient(t, scope).CreateJob(context.Background(), tv1.CreateJobReq{
		Space: space, Conversation: "c1", Input: "idle sweep", Idem: idem("sweep-job"),
	})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, scope, job.ID, "succeeded", 3*time.Minute)
	db := adminDB(t, "spawn_controller")
	var runtime, state string
	testkit.Poll(t, 20*time.Second, 250*time.Millisecond, func() bool {
		err := db.QueryRowContext(context.Background(), `SELECT r.id,m.state FROM runtimes r
			JOIN fsm.machines m ON m.kind='runtime' AND m.id=r.id
			WHERE r.space=$1 ORDER BY r.created DESC LIMIT 1`, space).Scan(&runtime, &state)
		return err == nil && state == "warm"
	})
	if _, err := db.ExecContext(context.Background(), `UPDATE fsm.machines
		SET deadline_at=now()-interval '1 second' WHERE kind='runtime' AND id=$1`, runtime); err != nil {
		t.Fatal(err)
	}
	d := testkit.NewDocker(t)
	var containers int
	var listErr error
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("runtime=%s state=%s containers=%d docker_error=%v", runtime, state, containers, listErr)
		}
	})
	testkit.Poll(t, 10*time.Second, 250*time.Millisecond, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := db.QueryRowContext(ctx,
			"SELECT state FROM fsm.machines WHERE kind='runtime' AND id=$1", runtime).Scan(&state); err != nil {
			return false
		}
		cs, err := d.List(ctx, "hhub.runtime=hhub-"+runtime)
		listErr, containers = err, len(cs)
		return err == nil && state == "absent" && containers == 0
	})
}
