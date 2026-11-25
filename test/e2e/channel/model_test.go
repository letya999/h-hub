//go:build hhube2e

package channel

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	mv1 "github.com/letya999/h-hub/contracts/model/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// ExecDocker runs a probe inside a live container via the mounted
// socket — the e2e runner carries the docker CLI.

// TestGatewayAccountsSpend proves the turn went through the gateway:
// the scripted usage is measured, settled and readable via Usage.
//
// req: sys.model-usage
func TestGatewayAccountsSpend(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.model-usage")
	owner := "u6_" + Nonce
	space := EnsureSpace(t, owner, Idem("s", owner))
	res, err := TaskClient(t, OwnerSpace{Owner: owner, Space: space}).CreateJob(context.Background(),
		tv1.CreateJobReq{Space: space, Conversation: "c1",
			Input: "measure me", Idem: Idem("j", owner, "1")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	j := WaitJob(t, OwnerSpace{Owner: owner, Space: space}, res.ID, "succeeded", 3*time.Minute)
	if j.Result != "answer: measure me" {
		t.Fatalf("result %q", j.Result)
	}
	var u mv1.UsageResp
	testkit.Poll(t, 30*time.Second, 250*time.Millisecond, func() bool {
		u, err = ModelClient(t, OwnerSpace{Owner: owner, Space: space}).Usage(context.Background(), mv1.UsageReq{})
		return err == nil && u.Spent > 0
	})
	if u.DailyLimit <= 0 || u.RunLimit <= 0 {
		t.Fatalf("ceilings missing: %+v", u)
	}
	db := AdminDB(t, "model_gateway")
	var events int
	if err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM spend_events WHERE owner=$1 AND measured`, owner).
		Scan(&events); err != nil {
		t.Fatalf("spend events: %v", err)
	}
	if events == 0 {
		t.Fatal("no measured spend event recorded")
	}
	_, cerr := ModelClient(t, OwnerSpace{Owner: owner, Space: space}).Complete(context.Background(),
		mv1.CompleteReq{Run: "r", Request: json.RawMessage(
			`{"model":"scripted","messages":[{"role":"user","content":"hi"}]}`)})
	testkit.Deny(t, "sys.model-usage", herrors.CodeOf(cerr) == herrors.CodeUnauthorized)
}

// TestForgedRunTokenRefused proves Complete is bound to a run token
// verified against the issuer key: a token signed by a foreign key is
// refused before any upstream call or reservation.
//
// req: sys.forged-token
func TestForgedRunTokenRefused(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.forged-token")
	testkit.Req(t, "adr.model-gateway.run-token-bound")
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	tok := Forge(t, key, "run", `{"sub":"r","job":"j","run":"r","space":"s","owner":"o","aud":["model-gateway"],"exp":9999999999,"kid":"k"}`)
	c := &mv1.Client{T: mv1.HTTP{C: httpapi.NewClient(ModelURL, tok)}}
	_, err := c.Complete(context.Background(), mv1.CompleteReq{
		Run: "r", Request: json.RawMessage(`{"model":"scripted","messages":[{"role":"user","content":"hi"}]}`)})
	testkit.Deny(t, "adr.model-gateway.run-token-bound", herrors.CodeOf(err) == herrors.CodeUnauthorized)
	if err == nil {
		t.Fatal("forged run token accepted")
	}
}

// TestDailyQuotaRefusal burns the daily ceiling on the scripted model
// and proves the next turn is refused with a clear error, not a hang.
//
// req: sys.model-quota
func TestDailyQuotaRefusal(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.model-quota")
	owner := "q6_" + Nonce
	space := EnsureSpace(t, owner, Idem("s", owner))
	burn, err := TaskClient(t, OwnerSpace{Owner: owner, Space: space}).CreateJob(context.Background(),
		tv1.CreateJobReq{Space: space, Conversation: "c1",
			Input: "burn:999990 quota probe", Idem: Idem("j", owner, "burn")})
	if err != nil {
		t.Fatalf("create burn: %v", err)
	}
	WaitJob(t, OwnerSpace{Owner: owner, Space: space}, burn.ID, "succeeded", 3*time.Minute)
	over, err := TaskClient(t, OwnerSpace{Owner: owner, Space: space}).CreateJob(context.Background(),
		tv1.CreateJobReq{Space: space, Conversation: "c1",
			Input: "over the ceiling", Idem: Idem("j", owner, "over")})
	if err != nil {
		t.Fatalf("create over: %v", err)
	}
	j := WaitJob(t, OwnerSpace{Owner: owner, Space: space}, over.ID, "failed", 3*time.Minute)
	refused := strings.Contains(j.Result, "token budget exceeded")
	if !refused {
		t.Fatalf("refusal is not readable: %q", j.Result)
	}
	testkit.Deny(t, "sys.model-quota", refused)
}

// TestModelChannelCommands drives /usage, /model list and /model
// switch through the mock channel; the switched route answers on the
// same warm runtime — no restart.
//
// req: sys.model-usage
// req: sys.model-route
func TestModelChannelCommands(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.model-usage")
	testkit.Req(t, "sys.model-route")
	testkit.Req(t, "adr.model-gateway.spec-per-call")
	owner := "mc6_" + Nonce
	_, code := AddUser(t, owner)
	if _, err := MockClient(t, owner, "Inject").Inject(context.Background(), cv1.InjectReq{
		External: owner, UpdateID: Idem("link", owner), Text: "/start " + code}); err != nil {
		t.Fatal(err)
	}
	cursor := WaitMock(t, owner, 0, "Account linked", 10*time.Second)
	inject := func(text string) {
		t.Helper()
		if _, err := MockClient(t, owner, "Inject").Inject(context.Background(), cv1.InjectReq{
			External: owner, UpdateID: Idem("cmd", owner, text), Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	inject("/usage")
	cursor = WaitMock(t, owner, cursor, "Spent today:", 15*time.Second)
	inject("/model")
	cursor = WaitMock(t, owner, cursor, "scripted", 15*time.Second)
	inject("/model no-such")
	prev := cursor
	cursor = WaitMock(t, owner, cursor, "Unknown route", 15*time.Second)
	testkit.Deny(t, "sys.model-route", cursor > prev)
	testkit.Deny(t, "adr.model-gateway.spec-per-call", cursor > prev)
	inject("/model scripted-alt")
	cursor = WaitMock(t, owner, cursor, "switched", 15*time.Second)
	inject("still routed")
	WaitMock(t, owner, cursor, "answer: still routed", 3*time.Minute)
}

// TestSpaceReachesOnlyModelGateway execs probes inside the live
// runtime: the gateway answers, every other name fails to resolve on
// the space-zone network.
//
// req: sys.space-egress
func TestSpaceReachesOnlyModelGateway(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.space-egress")
	owner := "eg_" + Nonce
	space := EnsureSpace(t, owner, Idem("s", owner))
	res, err := TaskClient(t, OwnerSpace{Owner: owner, Space: space}).CreateJob(context.Background(),
		tv1.CreateJobReq{Space: space, Conversation: "c1",
			Input: "slow:60000 net probe", Idem: Idem("j", owner, "1")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	spawnDB := AdminDB(t, "spawn_controller")
	var container string
	testkit.Poll(t, 2*time.Minute, 500*time.Millisecond, func() bool {
		return spawnDB.QueryRowContext(context.Background(), `SELECT container FROM Runtimes
			WHERE space=$1 AND container<>'' ORDER BY created DESC LIMIT 1`, space).Scan(&container) == nil
	})
	probe := func(host string) error {
		_, err := ExecDocker(container, "curl", "-fsS", "--max-time", "3",
			"-o", "/dev/null", "http://"+host+"/healthz")
		return err
	}
	if err := probe("model-gateway:9000"); err != nil {
		t.Fatalf("runtime cannot reach model-gateway: %v", err)
	}
	refused := 0
	for _, denied := range []string{"postgres:5432", "identity-hub:9000",
		"channel-hub:9000", "cliproxy:8317"} {
		if err := probe(denied); err == nil {
			t.Fatalf("space runtime reached %s", denied)
		}
		refused++
	}
	testkit.Deny(t, "sys.space-egress", refused == 4)
	WaitJob(t, OwnerSpace{Owner: owner, Space: space}, res.ID, "succeeded", 2*time.Minute)
}

// TestProviderSecretsAbsent writes canary credentials into the two
// volumes a real cliproxy deployment uses — the route key file inside
// the gateway's keys volume and the OAuth session in cliproxy-auth —
// then proves they surface in no log, env, inspect output, dump or
// answer.
//
// req: sys.secret-leak
func TestProviderSecretsAbsent(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.secret-leak")
	routeCanary := "E2E-CLIPROXY-" + Nonce
	oauthCanary := "E2E-OAUTH-" + Nonce
	write := func(volume, dir, file, value string) {
		t.Helper()
		out, err := exec.Command("docker", "run", "--rm",
			"--entrypoint", "sh", "-v", volume+":/k",
			"hhub-hubctl:local", "-c",
			"mkdir -p /k/"+dir+" && printf %s '"+value+"' > /k/"+dir+"/"+file).
			CombinedOutput()
		if err != nil {
			t.Fatalf("canary write %s: %v: %s", file, err, out)
		}
	}
	write("hhub_keys-model-gateway", "routes", "cliproxy.key", routeCanary)
	write("hhub_cliproxy-auth", "session", "oauth.json", oauthCanary)
	canaries := []string{routeCanary, oauthCanary}
	leaks := func(where string, blob []byte) {
		t.Helper()
		for _, c := range canaries {
			if strings.Contains(string(blob), c) {
				t.Fatalf("%s in %s", c, where)
			}
		}
	}
	for _, c := range []string{"model-gateway", "channel-hub", "task-engine"} {
		leaks("logs "+c, DockerOutput(t, "logs", c))
		leaks("inspect "+c, DockerOutput(t, "inspect", c))
	}
	for _, db := range []string{"model_gateway", "identity_hub", "task_engine"} {
		leaks("dump "+db, DockerOutput(t, "exec", "postgres", "pg_dump", "-U", "postgres", db))
	}
	owner := "sl6_" + Nonce
	_, code := AddUser(t, owner)
	if _, err := MockClient(t, owner, "Inject").Inject(context.Background(), cv1.InjectReq{
		External: owner, UpdateID: Idem("link", owner), Text: "/start " + code}); err != nil {
		t.Fatal(err)
	}
	WaitMock(t, owner, 0, "Account linked", 10*time.Second)
	res, err := MockClient(t, owner, "ReadOutbox").ReadOutbox(context.Background(),
		cv1.ReadOutboxReq{External: owner})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range res.Messages {
		leaks("answer", []byte(m.Body))
	}
}
