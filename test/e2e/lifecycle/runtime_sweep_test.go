//go:build hhube2e

package lifecycle

import (
	"context"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
	"testing"
	"time"
)

func TestRuntimeDeadlineReapsContainer(t *testing.T) {
	t.Parallel()
	owner := "sweep_" + Nonce
	space := EnsureSpace(t, owner, Idem("sweep-space"))
	scope := OwnerSpace{Owner: owner, Space: space}
	job, err := TaskClient(t, scope).CreateJob(context.Background(), tv1.CreateJobReq{
		Space: space, Conversation: "c1", Input: "idle sweep", Idem: Idem("sweep-job"),
	})
	if err != nil {
		t.Fatal(err)
	}
	WaitJob(t, scope, job.ID, "succeeded", 3*time.Minute)
	db := AdminDB(t, "spawn_controller")
	var runtime, state string
	testkit.Poll(t, 20*time.Second, 250*time.Millisecond, func() bool {
		err := db.QueryRowContext(context.Background(), `SELECT r.id,m.state FROM Runtimes r
			JOIN fsm.machines m ON m.kind='runtime' AND m.id=r.id
			WHERE r.space=$1 ORDER BY r.created DESC LIMIT 1`, space).Scan(&runtime, &state)
		return err == nil && state == "warm"
	})
	// Job settle commits the release-runtime effect in the same tx,
	// but the outbox applies it later; a release on a warm runtime
	// re-arms the idle deadline via Touch (busy is never entered).
	// Draining the effect first keeps the UPDATE below the last
	// deadline writer — a late Touch would silently undo the pin.
	taskDB := AdminDB(t, "task_engine")
	testkit.Poll(t, 20*time.Second, 250*time.Millisecond, func() bool {
		var pending int
		err := taskDB.QueryRowContext(context.Background(), `SELECT count(*) FROM fsm.outbox
			WHERE topic='release-runtime' AND done_at IS NULL
			AND convert_from(payload,'UTF8')::jsonb->>'runtime'=$1`, runtime).Scan(&pending)
		return err == nil && pending == 0
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
	// deadline -> draining -> docker-rm -> reaped crosses the real
	// daemon: under parallel e2e load one slow Remove alone eats the
	// old 10s window.
	testkit.Poll(t, 30*time.Second, 250*time.Millisecond, func() bool {
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
