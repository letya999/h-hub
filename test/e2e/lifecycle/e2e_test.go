//go:build hhube2e

// Package e2e is the phase-3 acceptance suite: task -> answer through
// CreateJob with real containers and the scripted model, plus the
// system invariants. Requires the running stack (just stack) and a
// docker socket.
package lifecycle

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	_ "github.com/jackc/pgx/v5/stdlib"
	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// EnsureSpace creates the owner's space and returns its id — the
// value jobs name in their space field.

// WaitJob polls a job into the wanted state.

// Runtimes lists live runtime containers (label hhub.managed).

// TestJobToAnswer is the core path: three owners, three spaces, three
// conversations — answers come back and no owner sees another's data.
//
// req: sys.tenant-isolation
func TestJobToAnswer(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.tenant-isolation")
	testkit.Req(t, "adr.task-jobs.tenant-isolation")
	owners := []string{"alice", "boris", "cy"}
	var wg sync.WaitGroup
	var jobIDs, spaceIDs sync.Map
	for _, o := range owners {
		wg.Add(1)
		go func(o string) {
			defer wg.Done()
			space := EnsureSpace(t, o, Idem("s", o))
			spaceIDs.Store(o, space)
			res, err := TaskClient(t, OwnerSpace{Owner: o, Space: space}).CreateJob(context.Background(), tv1.CreateJobReq{
				Space: space, Conversation: "c1",
				Input: "hello " + o, Idem: Idem("j", o, "1")})
			if err != nil {
				t.Errorf("create job %s: %v", o, err)
				return
			}
			jobIDs.Store(o, res.ID)
			j := WaitJob(t, OwnerSpace{Owner: o, Space: space}, res.ID, "succeeded", 3*time.Minute)
			if j.Result != "answer: hello "+o {
				t.Errorf("%s: result %q", o, j.Result)
			}
		}(o)
	}
	wg.Wait()
	borisJob, ok := jobIDs.Load("boris")
	if !ok {
		t.Fatal("boris job missing")
	}
	aliceSpace, ok := spaceIDs.Load("alice")
	if !ok {
		t.Fatal("alice space missing")
	}
	alice := OwnerSpace{Owner: "alice", Space: aliceSpace.(string)}
	_, foreignErr := TaskClient(t, alice).GetJob(context.Background(), tv1.GetJobReq{ID: borisJob.(string)})
	testkit.Deny(t, "adr.task-jobs.tenant-isolation", herrors.CodeOf(foreignErr) == herrors.CodeNotFound)
	if _, err := TaskClient(t, alice).CancelJob(context.Background(),
		tv1.JobReq{ID: borisJob.(string)}); herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("alice cancelled boris job: %v", err)
	}
	db := AdminDB(t, "task_engine")
	var borisRun string
	if err := db.QueryRowContext(context.Background(), "SELECT id FROM runs WHERE job=$1",
		borisJob.(string)).Scan(&borisRun); err != nil {
		t.Fatalf("boris run: %v", err)
	}
	if _, err := TaskClient(t, alice).GetRun(context.Background(),
		tv1.GetRunReq{Run: borisRun}); herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("alice read boris run: %v", err)
	}
	borisSpace, ok := spaceIDs.Load("boris")
	if !ok {
		t.Fatal("boris space missing")
	}
	cross := OwnerSpace{Owner: "alice", Space: borisSpace.(string)}
	if _, err := SpawnClient(t, cross).GetSpace(context.Background(),
		sv1.GetSpaceReq{ID: borisSpace.(string)}); herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("alice read boris space: %v", err)
	}
	if _, err := SpawnClient(t, cross).DestroySpace(context.Background(),
		sv1.DestroySpaceReq{ID: borisSpace.(string)}); herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("alice destroyed boris space: %v", err)
	}
	if _, err := TaskClient(t, cross).CreateJob(context.Background(), tv1.CreateJobReq{
		Space: borisSpace.(string), Conversation: "c2", Input: "wrong space",
		Idem: Idem("cross-space")}); herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("alice created job in boris space: %v", err)
	}
	if _, err := TaskClient(t, cross).SetHold(context.Background(), tv1.SetHoldReq{
		Space: borisSpace.(string), Active: true}); herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("alice held boris space: %v", err)
	}
	if _, err := TaskClient(t, alice).CreateJob(context.Background(), tv1.CreateJobReq{
		Space: borisSpace.(string), Conversation: "c2", Input: "wrong token scope",
		Idem: Idem("body-space")}); herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("body space bypassed token scope: %v", err)
	}
	aliceJob, ok := jobIDs.Load("alice")
	if !ok {
		t.Fatal("alice job missing")
	}
	second := OwnerSpace{Owner: "alice", Space: EnsureSpace(t, "alice", Idem("s", "alice", "second"))}
	if _, err := TaskClient(t, second).GetJob(context.Background(),
		tv1.GetJobReq{ID: aliceJob.(string)}); herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("cross-space job read: %v", err)
	}
	if _, err := TaskClient(t, second).CreateJob(context.Background(), tv1.CreateJobReq{
		Space: second.Space, Conversation: "c1", Input: "replay other space",
		Idem: Idem("j", "alice", "1")}); herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("cross-space Idem replay: %v", err)
	}
	secondJobs, err := TaskClient(t, second).ListJobs(context.Background(), tv1.ListJobsReq{})
	if err != nil || len(secondJobs.Jobs) != 0 {
		t.Fatalf("cross-space list: %+v, %v", secondJobs.Jobs, err)
	}
	next, err := TaskClient(t, alice).CreateJob(context.Background(), tv1.CreateJobReq{
		Space: alice.Space, Conversation: "c1", Input: "second turn",
		Idem: Idem("j", "alice", "2")})
	if err != nil {
		t.Fatalf("second conversation turn: %v", err)
	}
	if got := WaitJob(t, alice, next.ID, "succeeded", 3*time.Minute); got.Result != "answer: second turn" {
		t.Fatalf("second conversation result: %q", got.Result)
	}
	// Tenant isolation: alice's listing contains only her jobs.
	res, err := TaskClient(t, alice).ListJobs(context.Background(), tv1.ListJobsReq{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, j := range res.Jobs {
		if !strings.HasPrefix(j.ID, "job-") {
			continue
		}
		if strings.Contains(j.Result, "boris") || strings.Contains(j.Result, "cy") {
			t.Fatalf("tenant leak: %v", j)
		}
	}
}

// TestKillUncertain kills a runtime mid-turn: the job goes uncertain
// and stays — never retried (uncertain is observed, not repeated).
//
// req: sys.kill-uncertain
func TestKillUncertain(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.kill-uncertain")
	testkit.Req(t, "adr.task-jobs.kill-to-uncertain")
	owner := "kira"
	space := EnsureSpace(t, owner, Idem("s", owner))
	res, err := TaskClient(t, OwnerSpace{Owner: owner, Space: space}).CreateJob(context.Background(), tv1.CreateJobReq{
		Space: space, Conversation: "c1",
		Input: "slow:120000 hold this turn", Idem: Idem("j", "kira", "1")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	WaitJob(t, OwnerSpace{Owner: owner, Space: space}, res.ID, "running", 2*time.Minute)
	d := testkit.NewDocker(t)
	spawnDB := AdminDB(t, "spawn_controller")
	var container string
	testkit.Poll(t, 30*time.Second, 200*time.Millisecond, func() bool {
		return spawnDB.QueryRowContext(context.Background(), `SELECT container FROM Runtimes
			WHERE space=$1 AND container<>'' ORDER BY created DESC LIMIT 1`, space).Scan(&container) == nil
	})
	if err := d.Kill(context.Background(), container); err != nil {
		t.Fatalf("kill %s: %v", container, err)
	}
	j := WaitJob(t, OwnerSpace{Owner: owner, Space: space}, res.ID, "uncertain", 90*time.Second)
	// Observation: uncertain does not move to queued/running and the
	// job keeps exactly one run — no retry.
	db := AdminDB(t, "task_engine")
	testkit.Poll(t, 10*time.Second, time.Second, func() bool {
		st := JobState(t, db, res.ID)
		return st == "uncertain"
	})
	var runs int
	if err := db.QueryRowContext(context.Background(),
		"SELECT count(*) FROM runs WHERE job=$1",
		res.ID).Scan(&runs); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if runs != 1 {
		t.Fatalf("expected 1 run, got %d", runs)
	}
	_, retryErr := TaskClient(t, OwnerSpace{Owner: owner, Space: space}).RetryJob(context.Background(), tv1.JobReq{ID: res.ID})
	testkit.Deny(t, "sys.kill-uncertain", herrors.CodeOf(retryErr) == herrors.CodeConflict)
	testkit.Deny(t, "adr.task-jobs.kill-to-uncertain", herrors.CodeOf(retryErr) == herrors.CodeConflict)
	_ = j
}

// TestForgedToken proves a wrong-key token is refused on the service
// surface and a forged run token is refused on ReportRun.
//
// req: sys.forged-token
func TestForgedToken(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.forged-token")
	testkit.Req(t, "adr.service-edges.forged-rejected")
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	ownerTok := Forge(t, key, "owner", `{"sub":"operator","owner":"alice","aud":["task-engine","spawn-controller"],"exp":9999999999}`)
	badTask := &tv1.Client{T: tv1.HTTP{C: httpapi.NewClient(TaskURL, ownerTok)}}
	_, err := badTask.GetJob(context.Background(), tv1.GetJobReq{ID: "job_none"})
	testkit.Deny(t, "adr.service-edges.forged-rejected", herrors.CodeOf(err) == herrors.CodeUnauthorized)
	if _, err := badTask.CreateJob(context.Background(), tv1.CreateJobReq{
		Space: "space_none", Conversation: "c", Input: "forged", Idem: "forged"}); herrors.CodeOf(err) != herrors.CodeUnauthorized {
		t.Fatalf("forged task create token: %v", err)
	}
	badSpawn := &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(SpawnURL, ownerTok)}}
	if _, err := badSpawn.GetSpace(context.Background(), sv1.GetSpaceReq{ID: "sp_none"}); herrors.CodeOf(err) != herrors.CodeUnauthorized {
		t.Fatalf("forged spawn owner token: %v", err)
	}
	if _, err := badSpawn.CreateSpace(context.Background(), sv1.CreateSpaceReq{
		Name: "forged", Template: "default"}); herrors.CodeOf(err) != herrors.CodeUnauthorized {
		t.Fatalf("forged spawn create token: %v", err)
	}
	tok := Forge(t, key, "service", `{"sub":"task-engine","aud":["spawn-controller"],"exp":9999999999,"kid":"k"}`)
	c := &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(SpawnURL, tok)}}
	_, err = c.EnsureRuntime(context.Background(),
		sv1.EnsureRuntimeReq{Space: "none", Digest: "x"})
	if err == nil {
		t.Fatalf("forged service token accepted")
	}
	runTok := Forge(t, key, "run", `{"sub":"r","job":"j","run":"r","space":"s","owner":"o","aud":["task-engine"],"exp":9999999999,"kid":"k"}`)
	tc := &tv1.Client{T: tv1.HTTP{C: httpapi.NewClient(TaskURL, runTok)}}
	if err := tc.ReportRun(context.Background(), tv1.ReportRunReq{
		Run: "r", Events: []tv1.RunEvent{{Seq: 1, Kind: "final-ok", Data: "x"}},
	}); err == nil {
		t.Fatalf("forged run token accepted")
	}
}

// Forge signs a token with a random (non-issuer) key.

// TestServiceEdge proves a service op refuses a caller with no token;
// edge-level denial is covered by identity-hub's svc unit test.
//
// req: sys.service-edge
func TestServiceEdge(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.service-edge")
	testkit.Req(t, "adr.service-edges.edge-required")
	c := &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(SpawnURL, "")}}
	_, err := c.CreateSpace(context.Background(), sv1.CreateSpaceReq{
		Name: "unauthenticated", Template: "default"})
	testkit.Deny(t, "adr.service-edges.edge-required", herrors.CodeOf(err) == herrors.CodeUnauthorized)
	_, err = c.EnsureRuntime(context.Background(),
		sv1.EnsureRuntimeReq{Space: "none", Digest: "x"})
	if err == nil {
		t.Fatalf("unauthenticated service call accepted")
	}
}

// TestSpaceNoDB inspects a live runtime container: the env allowlist
// holds no database variables and no docker socket is mounted.
//
// req: sys.space-no-db
func TestSpaceNoDB(t *testing.T) {
	testkit.Req(t, "sys.space-no-db")
	testkit.Req(t, "adr.spawn-runtime.space-no-db", "adr.harness-admit.config-readonly")
	owner := "nodb"
	space := EnsureSpace(t, owner, Idem("s", owner))
	if _, err := TaskClient(t, OwnerSpace{Owner: owner, Space: space}).CreateJob(context.Background(), tv1.CreateJobReq{
		Space: space, Conversation: "c1",
		Input: "env check", Idem: Idem("j", "nodb", "1")}); err != nil {
		t.Fatalf("create: %v", err)
	}
	var rt testkit.ContainerSummary
	testkit.Poll(t, 2*time.Minute, 500*time.Millisecond, func() bool {
		if cs := Runtimes(t); len(cs) > 0 {
			rt = cs[0]
			return true
		}
		return false
	})
	insp, err := testkit.NewDocker(t).ContainerInspect(context.Background(), rt.ID)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	for _, e := range insp.Config.Env {
		if strings.Contains(e, "DSN") || strings.Contains(e, "POSTGRES") ||
			strings.Contains(e, "PG") || strings.Contains(e, "DB") {
			t.Fatalf("database env in space container: %s", e)
		}
	}
	renderRO := false
	for _, m := range insp.Mounts {
		if m.Destination == "/etc/hhub/render" && !m.RW {
			renderRO = true
		}
		if strings.Contains(m.Destination, "docker.sock") ||
			strings.Contains(m.Destination, "postgres") {
			t.Fatalf("forbidden mount in space container: %v", m)
		}
	}
	if !renderRO {
		t.Fatal("rendered config volume is absent or writable")
	}
	_, dberr := ExecDocker(rt.ID, "wget", "-q", "-T", "3",
		"-O", "/dev/null", "http://postgres:5432/")
	testkit.Deny(t, "adr.spawn-runtime.space-no-db", dberr != nil)
	_, roerr := ExecDocker(rt.ID, "touch", "/etc/hhub/render/probe")
	testkit.Deny(t, "adr.harness-admit.config-readonly", roerr != nil)
}

// TestRuntimeIntent checks the runtime's own registry row. Ordering of
// intent versus Docker creation remains a deferred proof.
func TestRuntimeIntent(t *testing.T) {
	testkit.Req(t, "adr.spawn-runtime.intent-first")
	owner := "intent_" + Nonce
	space := EnsureSpace(t, owner, Idem("intent-space"))
	client := TaskServiceSpawnClient(t, space)
	var runtime string
	testkit.Poll(t, time.Minute, 250*time.Millisecond, func() bool {
		res, err := client.EnsureRuntime(context.Background(), sv1.EnsureRuntimeReq{Space: space})
		runtime = res.Runtime
		return err == nil && res.Ready && runtime != ""
	})
	db := AdminDB(t, "spawn_controller")
	var n int
	if err := db.QueryRowContext(context.Background(),
		"SELECT count(*) FROM intents WHERE runtime=$1 AND space=$2 AND kind='runtime'",
		runtime, space).Scan(&n); err != nil {
		t.Fatalf("intents: %v", err)
	}
	if n != 1 {
		t.Fatalf("runtime %s has %d intent rows", runtime, n)
	}
}

// TestMissingConfigRefused proves a service refuses to start without its
// declared env (missing input is a refusal, not a quiet mode).
func TestMissingConfigRefused(t *testing.T) {
	d := testkit.NewDocker(t)
	id, err := d.CreateRun(context.Background(), testkit.CreateSpec{
		Image:    "hhub-task-engine:local",
		Name:     "e2e-store-missing",
		Labels:   map[string]string{"hhub.test": "store-missing"},
		Networks: []string{"hhub"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	t.Cleanup(func() { d.Remove(context.Background(), id) })
	code, err := d.Wait(context.Background(), id)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code == 0 {
		logs, _ := d.Logs(context.Background(), id)
		t.Fatalf("service started without env: %s", logs)
	}
}

// TestStackRestart restarts the whole stack and proves durable state
// survives: a succeeded job still reads succeeded with its result.
//
// req: sys.stack-restart
func TestStackRestart(t *testing.T) {
	testkit.Req(t, "sys.stack-restart")
	testkit.Req(t, "adr.task-jobs.restart-durable")
	owner := "restart_" + Nonce
	space, code := AddUser(t, owner)
	if _, err := MockClient(t, owner, "Inject").Inject(context.Background(), cv1.InjectReq{
		External: owner, UpdateID: Idem("link", owner), Text: "/start " + code}); err != nil {
		t.Fatal(err)
	}
	cursor := WaitMock(t, owner, 0, "Account linked", 10*time.Second)
	res, err := TaskClient(t, OwnerSpace{Owner: owner, Space: space}).CreateJob(context.Background(), tv1.CreateJobReq{
		Space: space, Conversation: "c1",
		Input: "restart check", Idem: Idem("j", "reba", "1")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	WaitJob(t, OwnerSpace{Owner: owner, Space: space}, res.ID, "succeeded", 3*time.Minute)
	d := testkit.NewDocker(t)
	for _, name := range []string{"task-engine", "spawn-controller",
		"identity-hub", "channel-hub", "postgres"} {
		if err := d.Restart(context.Background(), name); err != nil {
			t.Fatalf("restart %s: %v", name, err)
		}
	}
	// Wait for task-engine and channel-hub to answer again.
	for _, base := range []string{TaskURL, "http://channel-hub:9000"} {
		testkit.Poll(t, 90*time.Second, time.Second, func() bool {
			res, err := http.Get(base + "/healthz")
			if err != nil {
				return false
			}
			res.Body.Close()
			return res.StatusCode == http.StatusOK
		})
	}
	j := WaitJob(t, OwnerSpace{Owner: owner, Space: space}, res.ID, "succeeded", time.Minute)
	if j.Result != "answer: restart check" {
		t.Fatalf("lost result after restart: %q", j.Result)
	}
	_, err = TaskClient(t, OwnerSpace{Owner: owner, Space: space}).CancelJob(context.Background(), tv1.JobReq{ID: res.ID})
	testkit.Deny(t, "sys.stack-restart", herrors.CodeOf(err) == herrors.CodeConflict)
	testkit.Deny(t, "adr.task-jobs.restart-durable", herrors.CodeOf(err) == herrors.CodeConflict)
	if _, err := MockClient(t, owner, "Inject").Inject(context.Background(), cv1.InjectReq{
		External: owner, UpdateID: Idem("after", owner), Text: "after restart"}); err != nil {
		t.Fatal(err)
	}
	WaitMock(t, owner, cursor, "answer: after restart", 3*time.Minute)
}

// AdminDB opens the admin DSN against one service database; Postgres
// has no cross-database queries, so each service db is its own conn.
