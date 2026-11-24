//go:build hhube2e

// Package harness holds the helpers shared by the e2e packages:
// contract clients, space lifecycle, delivery polling and Docker
// probes. Tests dot-import it so the call sites read unchanged.
package harness

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/letya999/h-hub/cmd/hubctl/operator"
	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	mv1 "github.com/letya999/h-hub/contracts/model/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/testkit"
)

var (
	TaskURL     = testkit.Env("HHUB_TASK_URL", "http://task-engine:9000")
	SpawnURL    = testkit.Env("HHUB_SPAWN_URL", "http://spawn-controller:9000")
	IdentityURL = testkit.Env("HHUB_IDENTITY_URL", "http://identity-hub:9000")
	ModelURL    = testkit.Env("HHUB_MODEL_URL", "http://model-gateway:9000")
	AdminDSN    = testkit.Env("HHUB_ADMIN_DSN",
		"postgres://postgres@postgres:5432/postgres?sslmode=disable")
	// Nonce makes idempotency keys unique per suite run and per test
	// binary: two e2e packages run against one stack in parallel, so
	// the pid keeps their idem key spaces disjoint.
	Nonce       = strconv.FormatInt(time.Now().Unix(), 36) + "p" + strconv.Itoa(os.Getpid())
	ownerTokens sync.Map
)

func Idem(parts ...string) string {
	return strings.Join(append(parts, Nonce), "-")
}

type OwnerSpace struct{ Owner, Space string }

func TaskClient(t *testing.T, scope OwnerSpace) *tv1.Client {
	return &tv1.Client{T: tv1.HTTP{C: httpapi.NewClient(TaskURL, OwnerToken(t, scope, "task-engine"))}}
}

func SpawnClient(t *testing.T, scope OwnerSpace) *sv1.Client {
	return &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(SpawnURL, OwnerToken(t, scope, "spawn-controller"))}}
}

func ModelClient(t *testing.T, scope OwnerSpace) *mv1.Client {
	t.Helper()
	return &mv1.Client{T: mv1.HTTP{
		C: httpapi.NewClient(ModelURL, OwnerToken(t, scope, "model-gateway"))}}
}

func OwnerToken(t *testing.T, scope OwnerSpace, aud string) string {
	t.Helper()
	key := scope.Owner + "/" + scope.Space + "/" + aud
	if v, ok := ownerTokens.Load(key); ok {
		return v.(string)
	}
	priv, err := auth.LoadPrivateKey("/etc/hhub/keys/operator.key")
	if err != nil {
		t.Fatalf("operator key: %v", err)
	}
	claims, err := json.Marshal(idv1.OwnerClaims{
		Sub: "operator", Owner: scope.Owner, Space: scope.Space, Target: aud,
		Aud: []string{"identity-hub"}, Exp: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	opToken, err := auth.Sign(priv, "operator", claims)
	if err != nil {
		t.Fatal(err)
	}
	c := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(IdentityURL, opToken)}}
	res, err := c.ActAs(context.Background(), idv1.ActAsReq{Owner: scope.Owner, Space: scope.Space, Aud: aud})
	if err != nil {
		t.Fatalf("act as %s/%s for %s: %v", scope.Owner, scope.Space, aud, err)
	}
	v, _ := ownerTokens.LoadOrStore(key, res.Token)
	return v.(string)
}

// EnsureSpace creates the owner's space and returns its id — the
// value jobs name in their space field.
func EnsureSpace(t *testing.T, owner, name string) string {
	t.Helper()
	res, err := SpawnClient(t, OwnerSpace{owner, ""}).CreateSpace(context.Background(), sv1.CreateSpaceReq{
		Name: name, Template: "assistant"})
	if err != nil {
		t.Fatalf("create space %s: %v", name, err)
	}
	CleanupSpace(t, owner, res.ID)
	return res.ID
}

func CleanupSpace(t *testing.T, owner, space string) {
	t.Helper()
	t.Cleanup(func() {
		ownerTokens.Delete(owner + "/" + space + "/spawn-controller")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := SpawnClient(t, OwnerSpace{owner, space}).DestroySpace(ctx,
			sv1.DestroySpaceReq{ID: space}); err != nil {
			t.Errorf("destroy space %s: %v", space, err)
		}
	})
}

// WaitJob polls a job into the wanted state.
func WaitJob(t *testing.T, scope OwnerSpace, id, want string, timeout time.Duration) tv1.JobInfo {
	t.Helper()
	var got tv1.JobInfo
	testkit.Poll(t, timeout, 500*time.Millisecond, func() bool {
		res, err := TaskClient(t, scope).GetJob(context.Background(),
			tv1.GetJobReq{ID: id})
		if err != nil {
			return false
		}
		got = res.Job
		return got.State == want
	})
	return got
}

// Runtimes lists live runtime containers (label hhub.managed).
func Runtimes(t *testing.T) []testkit.ContainerSummary {
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

// AdminDB opens the admin DSN against one service database; Postgres
// has no cross-database queries, so each service db is its own conn.
func AdminDB(t *testing.T, name string) *sql.DB {
	t.Helper()
	u, err := url.Parse(AdminDSN)
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

func JobState(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var st string
	if err := db.QueryRowContext(context.Background(),
		"SELECT state FROM fsm.machines WHERE kind='job' AND id=$1",
		id).Scan(&st); err != nil {
		return ""
	}
	return st
}

func Forge(t *testing.T, key ed25519.PrivateKey, purpose, payload string) string {
	t.Helper()
	tok, err := auth.Sign(key, purpose, []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// --- mock channel ---

func MockClient(t *testing.T, external, op string) *cv1.Client {
	t.Helper()
	tok, err := operator.Token(external, "", op, "channel-hub", "/etc/hhub/keys/operator.key")
	if err != nil {
		t.Fatal(err)
	}
	return &cv1.Client{T: cv1.HTTP{C: httpapi.NewClient("http://channel-hub:9000", tok)}}
}

func AddUser(t *testing.T, id string) (string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/hubctl", "user", "add", id)
	cmd.Dir = "/src"
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hubctl user add: %v: %s", err, out)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 3 || fields[0] != id {
		t.Fatalf("hubctl user output: %q", out)
	}
	CleanupSpace(t, id, fields[1])
	return fields[1], fields[2]
}

func WaitMock(t *testing.T, external string, after int64, want string, timeout time.Duration) int64 {
	t.Helper()
	var seq int64
	testkit.Poll(t, timeout, 250*time.Millisecond, func() bool {
		res, err := MockClient(t, external, "ReadOutbox").ReadOutbox(context.Background(), cv1.ReadOutboxReq{External: external, After: after})
		if err != nil {
			return false
		}
		for _, m := range res.Messages {
			if strings.Contains(m.Body, want) {
				seq = m.Seq
				return true
			}
		}
		return false
	})
	return seq
}

func ForgedMock(t *testing.T, owner string) *cv1.Client {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := json.Marshal(idv1.OwnerClaims{Sub: "operator", Owner: owner, Target: "ReadOutbox", Aud: []string{"channel-hub"}, Exp: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := auth.Sign(key, "operator", claims)
	if err != nil {
		t.Fatal(err)
	}
	return &cv1.Client{T: cv1.HTTP{C: httpapi.NewClient("http://channel-hub:9000", tok)}}
}

// WaitOperatorDelivery polls the mock outbox for the alert row and
// then reads it back through the contract.
func WaitOperatorDelivery(t *testing.T, key, want string) {
	t.Helper()
	ctx := context.Background()
	db := AdminDB(t, "channel_hub")
	var seq int64
	testkit.Poll(t, 30*time.Second, 100*time.Millisecond, func() bool {
		return db.QueryRowContext(ctx, "SELECT seq FROM mock_outbox WHERE delivery_id=$1", "alert-"+key).Scan(&seq) == nil
	})
	out, err := MockClient(t, "operator", "ReadOutbox").ReadOutbox(ctx,
		cv1.ReadOutboxReq{External: "operator", After: seq - 1})
	if err != nil || len(out.Messages) == 0 || out.Messages[0].Seq != seq ||
		!strings.Contains(out.Messages[0].Body, want) {
		t.Fatalf("operator did not receive alert %d: %+v %v", seq, out, err)
	}
}

// --- docker / script probes ---

// ExecDocker runs a probe inside a live container via the mounted
// socket — the e2e runner carries the docker CLI.
func ExecDocker(id string, args ...string) ([]byte, error) {
	return exec.Command("docker", append([]string{"exec", id}, args...)...).CombinedOutput()
}

func DockerOutput(t *testing.T, args ...string) []byte {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v: %s", args[0], err, out)
	}
	return out
}

// PostScenario loads a scripted-model scenario; the test container
// posts to the model over the compose network.
func PostScenario(t *testing.T, body string, want int) {
	t.Helper()
	res, err := http.Post("http://scripted-model:9000/v1/scenarios", "application/yaml", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test response close
	if res.StatusCode != want {
		t.Fatalf("load scenario: %s", res.Status)
	}
}

// TaskServiceSpawnClient mints the task-engine -> spawn-controller
// service token through identity-hub, the same edge the runtime uses.
func TaskServiceSpawnClient(t *testing.T, space string) *sv1.Client {
	t.Helper()
	key, err := auth.LoadPrivateKey("/etc/hhub/services/task-engine/service.key")
	if err != nil {
		t.Fatal(err)
	}
	id := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(IdentityURL, "")}}
	tok, err := id.IssueServiceToken(context.Background(), "task-engine", "spawn-controller",
		Idem("wedge-spawn-token", space), key)
	if err != nil {
		t.Fatal(err)
	}
	return &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(SpawnURL, tok.Token)}}
}

// HubctlExit runs a hubctl subcommand and fails the test on nonzero
// exit; the runner mounts the sources read-only at /src.
func HubctlExit(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", append([]string{"run", "./cmd/hubctl"}, args...)...)
	cmd.Dir = "/src"
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hubctl %v: %v: %s", args, err, out)
	}
	return string(out)
}
