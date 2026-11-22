//go:build hhube2e

// Package e2e is the phase-3 acceptance suite: task -> answer through
// CreateJob with real containers and the scripted model, plus the
// system invariants. Requires the running stack (just stack) and a
// docker socket.
package e2e

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/testkit"
)

var (
	taskURL  = testkit.Env("HHUB_TASK_URL", "http://task-engine:9000")
	spawnURL = testkit.Env("HHUB_SPAWN_URL", "http://spawn-controller:9000")
	adminDSN = testkit.Env("HHUB_ADMIN_DSN",
		"postgres://postgres@postgres:5432/postgres?sslmode=disable")
	// nonce makes idempotency keys unique per suite run: a second
	// run must create fresh jobs, not replay yesterday's idem hits.
	nonce = strconv.FormatInt(time.Now().Unix(), 36)
)

func idem(parts ...string) string {
	return strings.Join(append(parts, nonce), "-")
}

func taskClient() *tv1.Client {
	return &tv1.Client{T: tv1.HTTP{C: httpapi.NewClient(taskURL, "")}}
}

func spawnClient() *sv1.Client {
	return &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(spawnURL, "")}}
}

// ensureSpace creates the owner's space and returns its id — the
// value jobs name in their space field.
func ensureSpace(t *testing.T, owner, name string) string {
	t.Helper()
	res, err := spawnClient().CreateSpace(context.Background(), sv1.CreateSpaceReq{
		Owner: owner, Name: name, Template: "default"})
	if err != nil {
		t.Fatalf("create space %s: %v", name, err)
	}
	return res.ID
}

// waitJob polls a job into the wanted state.
func waitJob(t *testing.T, owner, id, want string, timeout time.Duration) tv1.JobInfo {
	t.Helper()
	var got tv1.JobInfo
	testkit.Poll(t, timeout, 500*time.Millisecond, func() bool {
		res, err := taskClient().GetJob(context.Background(),
			tv1.GetJobReq{Owner: owner, ID: id})
		if err != nil {
			return false
		}
		got = res.Job
		return got.State == want
	})
	return got
}

// runtimes lists live runtime containers (label hhub.managed).
func runtimes(t *testing.T) []testkit.ContainerSummary {
	t.Helper()
	cs, err := testkit.NewDocker(t).List(context.Background(), "hhub.managed")
	if err != nil {
		t.Fatalf("list runtimes: %v", err)
	}
	var out []testkit.ContainerSummary
	for _, c := range cs {
		if c.State == "running" {
			out = append(out, c)
		}
	}
	return out
}

// TestJobToAnswer is the core path: three owners, three spaces, three
// conversations — answers come back and no owner sees another's data.
//
// req: sys.tenant-isolation
func TestJobToAnswer(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.tenant-isolation")
	owners := []string{"alice", "boris", "cy"}
	var wg sync.WaitGroup
	for _, o := range owners {
		wg.Add(1)
		go func(o string) {
			defer wg.Done()
			space := ensureSpace(t, o, idem("s", o))
			res, err := taskClient().CreateJob(context.Background(), tv1.CreateJobReq{
				Owner: o, Space: space, Conversation: "c1",
				Input: "hello " + o, Idem: idem("j", o, "1")})
			if err != nil {
				t.Errorf("create job %s: %v", o, err)
				return
			}
			j := waitJob(t, o, res.ID, "succeeded", 3*time.Minute)
			if j.Result != "answer: hello "+o {
				t.Errorf("%s: result %q", o, j.Result)
			}
		}(o)
	}
	wg.Wait()
	// Tenant isolation: alice's listing contains only her job.
	res, err := taskClient().ListJobs(context.Background(), tv1.ListJobsReq{Owner: "alice"})
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
	owner := "kira"
	space := ensureSpace(t, owner, idem("s", owner))
	res, err := taskClient().CreateJob(context.Background(), tv1.CreateJobReq{
		Owner: owner, Space: space, Conversation: "c1",
		Input: "slow:120000 hold this turn", Idem: idem("j", "kira", "1")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitJob(t, owner, res.ID, "running", 2*time.Minute)
	d := testkit.NewDocker(t)
	var rt testkit.ContainerSummary
	testkit.Poll(t, 30*time.Second, 200*time.Millisecond, func() bool {
		if cs := runtimes(t); len(cs) > 0 {
			rt = cs[0]
			return true
		}
		return false
	})
	if err := d.Kill(context.Background(), rt.ID); err != nil {
		t.Fatalf("kill %s: %v", rt.ID, err)
	}
	j := waitJob(t, owner, res.ID, "uncertain", 90*time.Second)
	// Observation: uncertain does not move to queued/running and the
	// job keeps exactly one run — no retry.
	db := adminDB(t, "task_engine")
	testkit.Poll(t, 10*time.Second, time.Second, func() bool {
		st := jobState(t, db, res.ID)
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
	_ = j
}

// TestForgedToken proves a wrong-key token is refused on the service
// surface and a forged run token is refused on ReportRun.
//
// req: sys.forged-token
func TestForgedToken(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.forged-token")
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	tok := forge(t, key, "service", `{"sub":"task-engine","aud":["spawn-controller"],"exp":9999999999,"kid":"k"}`)
	c := &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(spawnURL, tok)}}
	_, err := c.EnsureRuntime(context.Background(),
		sv1.EnsureRuntimeReq{Space: "none", Digest: "x"})
	if err == nil {
		t.Fatalf("forged service token accepted")
	}
	runTok := forge(t, key, "run", `{"sub":"r","job":"j","run":"r","space":"s","owner":"o","aud":["task-engine"],"exp":9999999999,"kid":"k"}`)
	tc := &tv1.Client{T: tv1.HTTP{C: httpapi.NewClient(taskURL, runTok)}}
	if err := tc.ReportRun(context.Background(), tv1.ReportRunReq{
		Run: "r", Events: []tv1.RunEvent{{Seq: 1, Kind: "final-ok", Data: "x"}},
	}); err == nil {
		t.Fatalf("forged run token accepted")
	}
}

// forge signs a token with a random (non-issuer) key.
func forge(t *testing.T, key ed25519.PrivateKey, purpose, payload string) string {
	t.Helper()
	tok, err := auth.Sign(key, purpose, []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// TestServiceEdge proves a service op refuses a caller with no token;
// edge-level denial is covered by identity-hub's svc unit test.
//
// req: sys.service-edge
func TestServiceEdge(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.service-edge")
	c := &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(spawnURL, "")}}
	_, err := c.EnsureRuntime(context.Background(),
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
	owner := "nodb"
	space := ensureSpace(t, owner, idem("s", owner))
	if _, err := taskClient().CreateJob(context.Background(), tv1.CreateJobReq{
		Owner: owner, Space: space, Conversation: "c1",
		Input: "env check", Idem: idem("j", "nodb", "1")}); err != nil {
		t.Fatalf("create: %v", err)
	}
	var rt testkit.ContainerSummary
	testkit.Poll(t, 2*time.Minute, 500*time.Millisecond, func() bool {
		if cs := runtimes(t); len(cs) > 0 {
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
	for _, m := range insp.Mounts {
		if strings.Contains(m.Destination, "docker.sock") ||
			strings.Contains(m.Destination, "postgres") {
			t.Fatalf("forbidden mount in space container: %v", m)
		}
	}
}

// TestWorkerIntent proves the intent registry has rows for launched
// runtimes: intent precedes docker create.
//
// req: sys.worker-intent
func TestWorkerIntent(t *testing.T) {
	testkit.Req(t, "sys.worker-intent")
	db := adminDB(t, "spawn_controller")
	var n int
	if err := db.QueryRowContext(context.Background(),
		"SELECT count(*) FROM intents").Scan(&n); err != nil {
		t.Fatalf("intents: %v", err)
	}
	if n < 1 {
		t.Fatalf("no intent rows for launched runtimes")
	}
}

// TestStoreMissing proves a service refuses to start without its
// declared env (missing input is a refusal, not a quiet mode).
//
// req: sys.store-missing
func TestStoreMissing(t *testing.T) {
	testkit.Req(t, "sys.store-missing")
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
	owner := "reba"
	space := ensureSpace(t, owner, idem("s", owner))
	res, err := taskClient().CreateJob(context.Background(), tv1.CreateJobReq{
		Owner: owner, Space: space, Conversation: "c1",
		Input: "restart check", Idem: idem("j", "reba", "1")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitJob(t, owner, res.ID, "succeeded", 3*time.Minute)
	d := testkit.NewDocker(t)
	for _, name := range []string{"task-engine", "spawn-controller",
		"identity-hub", "postgres"} {
		if err := d.Restart(context.Background(), name); err != nil {
			t.Fatalf("restart %s: %v", name, err)
		}
	}
	// Wait for task-engine to answer again.
	testkit.Poll(t, 90*time.Second, time.Second, func() bool {
		res, err := http.Get(taskURL + "/healthz")
		if err != nil {
			return false
		}
		res.Body.Close()
		return res.StatusCode == http.StatusOK
	})
	j := waitJob(t, owner, res.ID, "succeeded", time.Minute)
	if j.Result != "answer: restart check" {
		t.Fatalf("lost result after restart: %q", j.Result)
	}
}

// adminDB opens the admin DSN against one service database; Postgres
// has no cross-database queries, so each service db is its own conn.
func adminDB(t *testing.T, name string) *sql.DB {
	t.Helper()
	u, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatalf("admin dsn parse: %v", err)
	}
	u.Path = "/" + name
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatalf("admin db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func jobState(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var st string
	if err := db.QueryRowContext(context.Background(),
		"SELECT state FROM fsm.machines WHERE kind='job' AND id=$1",
		id).Scan(&st); err != nil {
		return ""
	}
	return st
}
