//go:build hhube2e

package lifecycle

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net"
	"net/http"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	wv1 "github.com/letya999/h-hub/contracts/worker/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
)

func TestWorkerClaimAndDestroy(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.worker-intent", "sys.worker-tenant", "sys.space-no-db")
	checkUnregisteredLaunch(t)
	const samples = 5
	times := make([]time.Duration, 0, samples)
	for range samples {
		times = append(times, workerClaimAndDestroy(t))
	}
	slices.Sort(times)
	median := times[samples/2]
	t.Logf("cold worker median: %.3f s", median.Seconds())
	if median > 1500*time.Millisecond {
		t.Fatalf("cold worker median %s exceeds 1.5 s: %s", median, times)
	}
}

func workerClaimAndDestroy(t *testing.T) time.Duration {
	t.Helper()
	ctx := t.Context()
	space := createWorkerSpace(t)
	client := WorkerControlClient(t)
	req := sv1.ClaimWorkerReq{Owner: "boris", Space: space.ID, Digest: space.Digest,
		Profile: "terminal", Key: Idem("worker-claim")}
	_, cross := client.ClaimWorker(ctx, req)
	testkit.Deny(t, "sys.worker-tenant", cross != nil && herrors.CodeOf(cross) == herrors.CodeNotFound)
	req.Owner = "alice"
	began := time.Now()
	first, err := client.ClaimWorker(ctx, req)
	if err != nil || first.Ready || first.Generation == "" {
		t.Fatalf("first claim ready=%t generation=%q err=%v", first.Ready, first.Generation, err)
	}
	ready := waitWorker(t, client, req, first)
	checkWorkerTransport(t, ready)
	timing := time.Since(began)
	t.Logf("claim through first direct response: %.3f s", timing.Seconds())
	checkWorkerDatabaseRefusal(t, ready)
	d := testkit.NewDocker(t)
	label := "hhub.generation=" + first.Generation
	checkWorkerContainer(t, d, req, label, ready.SocketSlot)
	lease := sv1.WorkerLeaseReq{Owner: req.Owner, Space: req.Space, Worker: first.Worker,
		Generation: first.Generation, Lease: first.Lease, Action: sv1.RenewWorkerLease}
	if _, err := client.WorkerLease(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if _, err := SpawnClient(t, OwnerSpace{Owner: req.Owner, Space: req.Space}).DestroySpace(ctx,
		sv1.DestroySpaceReq{ID: req.Space}); err != nil {
		t.Fatal(err)
	}
	checkWorkerStopped(t, d, label, first.Worker, "space-destroy")
	return timing
}

func checkUnregisteredLaunch(t *testing.T) {
	t.Helper()
	db, ctx := AdminDB(t, "spawn_controller"), t.Context()
	key, generation := "missing-worker-"+rand.Text(), "missing-generation-"+rand.Text()
	payload, err := json.Marshal(generation)
	if err != nil {
		t.Fatal(err)
	}
	var seq int64
	if err := db.QueryRowContext(ctx, `INSERT INTO fsm.outbox (kind,machine_id,topic,payload)
		VALUES ('worker',$1,'worker-create',$2) RETURNING seq`, key, payload).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := db.ExecContext(ctx, "DELETE FROM fsm.outbox WHERE seq=$1", seq); err != nil {
			t.Error(err)
		}
	})
	testkit.Poll(t, 5*time.Second, 50*time.Millisecond, func() bool {
		var deferred bool
		err := db.QueryRowContext(ctx, "SELECT due_at > created_at FROM fsm.outbox WHERE seq=$1", seq).Scan(&deferred)
		return err == nil && deferred
	})
	rows, err := testkit.NewDocker(t).List(ctx, "hhub.generation="+generation)
	testkit.Deny(t, "sys.worker-intent", err == nil && len(rows) == 0)
}

func checkWorkerDatabaseRefusal(t *testing.T, ready sv1.ClaimWorkerResp) {
	t.Helper()
	addr, err := net.DefaultResolver.LookupHost(t.Context(), "postgres")
	if err != nil || len(addr) == 0 {
		t.Fatalf("resolve test database: %v", err)
	}
	const probe = "import socket,sys\ntry:\n socket.create_connection((sys.argv[1],5432),.3)\nexcept OSError:\n print('closed')"
	req := wv1.ExecReq{Argv: []string{"python3", "-c", probe, addr[0]}, TimeoutMS: 1000}
	peer := wv1.NewHTTPClient(wv1.SocketPath(ready.SocketSlot), ready.Token, 2*time.Second)
	var out wv1.ExecResp
	err = peer.DoLimit(t.Context(), httpapi.Call{Method: http.MethodPost, Path: wv1.ExecPath, Req: req,
		Resp: &out, Key: "db-isolation", Limit: wv1.MaxBody})
	testkit.Deny(t, "sys.space-no-db", err == nil && out.ExitCode == 0 && strings.TrimSpace(out.Stdout) == "closed")
}

func waitWorker(t *testing.T, client *sv1.Client, req sv1.ClaimWorkerReq, first sv1.ClaimWorkerResp) sv1.ClaimWorkerResp {
	t.Helper()
	var ready sv1.ClaimWorkerResp
	testkit.Poll(t, 8*time.Second, 50*time.Millisecond, func() bool {
		out, err := client.ClaimWorker(t.Context(), req)
		ready = out
		return err == nil && out.Ready && out.Generation == first.Generation && out.Lease == first.Lease &&
			out.SocketSlot > 0 && out.Token != ""
	})
	return ready
}

func checkWorkerTransport(t *testing.T, ready sv1.ClaimWorkerResp) {
	t.Helper()
	endpoint := wv1.SocketPath(ready.SocketSlot)
	wrong := wv1.NewHTTPClient(endpoint, "wrong-generation", time.Second)
	if err := wrong.Do(t.Context(), http.MethodGet, wv1.HealthPath, nil, nil); err == nil {
		t.Fatal("worker accepted another generation's connection")
	}
	peer := wv1.NewHTTPClient(endpoint, ready.Token, 2*time.Second)
	var res wv1.ExecResp
	req := wv1.ExecReq{Argv: []string{"printf", "%s", "direct worker connection"}, TimeoutMS: 500}
	if err := peer.DoLimit(t.Context(), httpapi.Call{Method: http.MethodPost, Path: wv1.ExecPath, Req: req,
		Resp: &res, Key: ready.Lease, Limit: wv1.MaxBody}); err != nil || res.Stdout != "direct worker connection" {
		t.Fatalf("direct worker exec = %+v, %v", res, err)
	}
}

func createWorkerSpace(t *testing.T) sv1.CreateSpaceResp {
	t.Helper()
	scope := OwnerSpace{Owner: "alice"}
	space, err := SpawnClient(t, scope).CreateSpace(t.Context(),
		sv1.CreateSpaceReq{Name: "worker-space-" + rand.Text(), Template: "assistant"})
	if err != nil {
		t.Fatal(err)
	}
	scope.Space = space.ID
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := SpawnClient(t, scope).DestroySpace(ctx, sv1.DestroySpaceReq{ID: space.ID})
		if err != nil && herrors.CodeOf(err) != herrors.CodeNotFound {
			t.Errorf("destroy worker space: %v", err)
		}
	})
	return space
}

func checkWorkerContainer(t *testing.T, d *testkit.Docker, req sv1.ClaimWorkerReq, label string, slot int) {
	t.Helper()
	ctx := t.Context()
	rows, err := d.List(ctx, label)
	if err != nil || len(rows) != 1 {
		t.Fatalf("worker generation count %d, %v", len(rows), err)
	}
	c, err := d.ContainerInspect(ctx, rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	db := AdminDB(t, "spawn_controller")
	var intentAt time.Time
	if err := db.QueryRowContext(ctx, `SELECT created FROM intents WHERE kind='worker' AND runtime=$1
		AND owner=$2 AND space=$3 AND digest=$4 AND profile=$5`, strings.TrimPrefix(label, "hhub.generation="),
		req.Owner, req.Space, req.Digest, req.Profile).Scan(&intentAt); err != nil {
		t.Fatal(err)
	}
	if !intentAt.Before(c.Created) || !c.State.Running {
		t.Fatalf("worker intent at %s, Docker creation at %s, running=%t", intentAt, c.Created, c.State.Running)
	}
	checkWorkerIsolation(t, c, req.Space, slot)
}

func checkWorkerIsolation(t *testing.T, c testkit.Inspect, space string, slot int) {
	t.Helper()
	if _, closed := c.NetworkSettings.Networks["none"]; !closed || len(c.NetworkSettings.Networks) != 1 {
		t.Fatalf("worker has a network route: %v", c.NetworkSettings.Networks)
	}
	if !c.HostConfig.ReadonlyRootfs || !slices.Equal(c.HostConfig.CapDrop, []string{"ALL"}) {
		t.Fatal("worker escaped its root filesystem or capability limits")
	}
	if !slices.Equal(c.Config.Env, []string{"PATH=/usr/local/bin:/usr/bin:/bin"}) {
		t.Fatal("worker received environment beyond PATH")
	}
	for _, m := range c.HostConfig.Mounts {
		if m.Target == path.Dir(wv1.WorkerSocket) && m.VolumeOptions.Subpath == wv1.SlotDirectory(slot) {
			continue
		}
		expected := path.Join("workspaces", space, strings.TrimPrefix(m.Target, "/workspace/"))
		if m.VolumeOptions.Subpath != expected || m.ReadOnly == (m.Target == "/workspace/files") {
			t.Fatalf("incorrect worker mount %+v", m)
		}
	}
	if len(c.HostConfig.Mounts) != 6 {
		t.Fatalf("worker has %d mounts", len(c.HostConfig.Mounts))
	}
}

func checkWorkerStopped(t *testing.T, d *testkit.Docker, label, worker, wantReason string) {
	t.Helper()
	ctx := t.Context()
	db := AdminDB(t, "spawn_controller")
	testkit.Poll(t, 8*time.Second, 50*time.Millisecond, func() bool {
		var state, reason string
		err := db.QueryRowContext(ctx, `SELECT m.state, i.stop_reason FROM fsm.machines m
			JOIN intents i ON i.kind=m.kind AND i.id=m.id WHERE m.kind='worker' AND m.id=$1`,
			worker).Scan(&state, &reason)
		if err != nil || state != "absent" || reason != wantReason {
			return false
		}
		rows, err := d.List(ctx, label)
		return err == nil && len(rows) == 0
	})
}
