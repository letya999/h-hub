//go:build hhube2e

package lifecycle

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
)

func TestWorkerStopQuarantine(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.worker-stop-confirmed")
	ctx := t.Context()
	space := createWorkerSpace(t)
	c := WorkerControlClient(t)
	req := sv1.ClaimWorkerReq{Owner: "alice", Space: space.ID, Digest: space.Digest,
		Profile: "terminal", Key: Idem("worker-before-stop")}
	first, err := c.ClaimWorker(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	w := waitWorker(t, c, req, first)
	d := testkit.NewDocker(t)
	label := "hhub.generation=" + w.Generation
	real, name, foreign := obstructWorkerObservation(t, d, label)
	lease := sv1.WorkerLeaseReq{Owner: req.Owner, Space: req.Space, Worker: w.Worker,
		Generation: w.Generation, Lease: w.Lease, Action: sv1.RetireWorkerLease}
	if _, err := c.WorkerLease(ctx, lease); err != nil {
		t.Fatal(err)
	}
	waitWorkerQuarantine(t, w.Worker)
	req.Key = Idem("worker-while-quarantined")
	_, denied := c.ClaimWorker(ctx, req)
	old, observed := d.ContainerInspect(ctx, real)
	testkit.Deny(t, "sys.worker-stop-confirmed", denied != nil && herrors.CodeOf(denied) == herrors.CodeConflict &&
		observed == nil && old.State.Running)
	if err := d.Remove(ctx, foreign); err != nil {
		t.Fatal(err)
	}
	DockerOutput(t, "rename", real, name)
	checkWorkerStopped(t, d, label, w.Worker, "terminal-outcome-uncertain")
}

// A foreign container at the expected name makes the Engine's observation ambiguous.
func obstructWorkerObservation(t *testing.T, d *testkit.Docker, label string) (string, string, string) {
	t.Helper()
	ctx := t.Context()
	list, err := d.List(ctx, label)
	if err != nil || len(list) != 1 {
		t.Fatalf("inspect worker before stop: rows=%d, err=%v", len(list), err)
	}
	id := list[0].ID
	actual, err := d.ContainerInspect(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimPrefix(actual.Name, "/")
	mark := rand.Text()
	foreignLabel := "hhub.e2e-foreign=" + mark
	t.Cleanup(func() {
		cleanupWorkerTestContainers(t, d, foreignLabel)
		cleanupWorkerTestContainers(t, d, label)
	})
	DockerOutput(t, "rename", id, "hhub-worker-e2e-rename-"+mark)
	foreign, err := d.CreateRun(ctx, testkit.CreateSpec{
		Image: "hhub-task-engine:local", Name: name, Networks: []string{"none"},
		Labels: map[string]string{"hhub.e2e-foreign": mark},
	})
	if err != nil {
		t.Fatal(err)
	}
	return id, name, foreign
}

func waitWorkerQuarantine(t *testing.T, id string) {
	t.Helper()
	db := AdminDB(t, "spawn_controller")
	testkit.Poll(t, 8*time.Second, 50*time.Millisecond, func() bool {
		var status string
		err := db.QueryRowContext(t.Context(), "SELECT state FROM fsm.machines WHERE kind='worker' AND id=$1", id).
			Scan(&status)
		return err == nil && status == "quarantined"
	})
}

func cleanupWorkerTestContainers(t *testing.T, d *testkit.Docker, label string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := d.List(ctx, label)
	if err != nil {
		t.Error(err)
		return
	}
	for _, c := range rows {
		if err := d.Remove(ctx, c.ID); err != nil {
			t.Error(err)
		}
	}
}
