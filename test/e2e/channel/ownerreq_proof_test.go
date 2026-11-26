//go:build hhube2e

package channel

import (
	"context"
	"testing"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	orv1 "github.com/letya999/h-hub/contracts/ownerreq/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/auth"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/locale"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
)

// svcToolClient mints the service -> tool-hub edge token the same way
// the stack does and builds the owner-request client carrying it.
func svcToolClient(t *testing.T, service, mark string) *orv1.Client {
	t.Helper()
	key, err := auth.LoadPrivateKey("/etc/hhub/services/" + service + "/service.key")
	if err != nil {
		t.Fatalf("%s key: %v", service, err)
	}
	id := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(IdentityURL, "")}}
	tok, err := id.IssueServiceToken(context.Background(), service, "tool-hub",
		Idem("svc-tool", service, mark), key)
	if err != nil {
		t.Fatalf("service token: %v", err)
	}
	return &orv1.Client{T: orv1.HTTP{C: httpapi.NewClient(ToolURL, tok.Token)}}
}

// runReqClient mints a run token — the agent's identity — and builds
// the owner-request client with it.
func runReqClient(t *testing.T, scope OwnerSpace, job, run string) *orv1.Client {
	t.Helper()
	key, err := auth.LoadPrivateKey("/etc/hhub/services/task-engine/service.key")
	if err != nil {
		t.Fatalf("task-engine key: %v", err)
	}
	ctx := context.Background()
	id := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(IdentityURL, "")}}
	svc, err := id.IssueServiceToken(ctx, "task-engine", "identity-hub",
		Idem("mint", run), key)
	if err != nil {
		t.Fatalf("service token: %v", err)
	}
	mint := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(IdentityURL, svc.Token)}}
	tok, err := mint.MintRunToken(ctx, idv1.MintRunTokenReq{
		Job: job, Run: run, Space: scope.Space, Owner: scope.Owner,
		Digest: "digest", Aud: []string{"tool-hub"}})
	if err != nil {
		t.Fatalf("run token: %v", err)
	}
	return &orv1.Client{T: orv1.HTTP{C: httpapi.NewClient(ToolURL, tok.Token)}}
}

// linkedOwner registers one mock owner and returns its handle.
func linkedOwner(t *testing.T, mark string) (string, string) {
	t.Helper()
	owner := "mock_orp_" + mark + Nonce
	space, code := AddUser(t, owner)
	if _, err := MockClient(t, owner, "Inject").Inject(context.Background(),
		cv1.InjectReq{External: owner, UpdateID: Idem("link", mark), Text: "/start " + code}); err != nil {
		t.Fatalf("link inject: %v", err)
	}
	WaitMock(t, owner, 0, "Account linked", 10*time.Second)
	return owner, space
}

func createRequest(t *testing.T, cl *orv1.Client, owner, space, kind, prompt string, ttl int64) string {
	t.Helper()
	res, err := cl.CreateRequest(context.Background(), orv1.CreateReq{
		Owner: owner, Space: space, Channel: "mock", External: owner, Kind: kind, Prompt: prompt, TTLSec: ttl})
	if err != nil {
		t.Fatalf("create %s: %v", kind, err)
	}
	return res.Request
}

func TestOwnerApprovalOutcomes(t *testing.T) {
	t.Parallel()
	mark := "appr"
	owner, space := linkedOwner(t, mark)
	cl := svcToolClient(t, "task-engine", mark)
	yesID := createRequest(t, cl, owner, space, "approval", "format the disk?", 300)
	cursor := WaitMock(t, owner, 0, "format the disk?", 30*time.Second)
	noID := createRequest(t, cl, owner, space, "approval", "wipe the archive?", 300)
	next := WaitMock(t, owner, cursor, "wipe the archive?", 30*time.Second)
	if _, err := MockClient(t, owner, "Inject").Inject(context.Background(),
		cv1.InjectReq{External: owner, UpdateID: Idem("yes"), Text: "1", ReplyTo: cursor}); err != nil {
		t.Fatal(err)
	}
	testkit.Poll(t, 30*time.Second, 250*time.Millisecond, func() bool {
		res, err := cl.GetRequest(context.Background(), orv1.GetReq{Request: yesID})
		return err == nil && res.State == "answered" && res.Answer == "yes"
	})
	accepted := WaitMock(t, owner, next, locale.T(locale.EN, "channel.request-accepted"), 30*time.Second)
	if _, err := MockClient(t, owner, "Inject").Inject(context.Background(),
		cv1.InjectReq{External: owner, UpdateID: Idem("repeated-yes"), Text: "1", ReplyTo: cursor}); err != nil {
		t.Fatal(err)
	}
	WaitMock(t, owner, accepted, locale.T(locale.EN, "channel.request-closed"), 30*time.Second)
	pending, err := cl.GetRequest(context.Background(), orv1.GetReq{Request: noID})
	if err != nil || pending.State != "pending" {
		t.Fatalf("repeated answer touched the next request: %+v %v", pending, err)
	}
	if _, err := MockClient(t, owner, "Inject").Inject(context.Background(),
		cv1.InjectReq{External: owner, UpdateID: Idem("no"), Text: "2", ReplyTo: next}); err != nil {
		t.Fatal(err)
	}
	testkit.Poll(t, 30*time.Second, 250*time.Millisecond, func() bool {
		res, err := cl.GetRequest(context.Background(), orv1.GetReq{Request: noID})
		return err == nil && res.State == "denied"
	})
}

func TestOwnerRequestSelfAnswer(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.owner-request-self-answer")
	mark := "self"
	owner, space := linkedOwner(t, mark)
	cl := svcToolClient(t, "task-engine", mark)
	id := createRequest(t, cl, owner, space, "question", "pick?", 300)
	agent := runReqClient(t, OwnerSpace{Owner: owner, Space: space}, Idem("job", owner), Idem("run", owner))
	_, err := agent.AnswerRequest(context.Background(), orv1.AnswerReq{
		Request: id, Channel: "mock", External: owner, Answer: "blue"})
	testkit.Deny(t, "sys.owner-request-self-answer",
		herrors.CodeOf(err) == herrors.CodeForbidden || herrors.CodeOf(err) == herrors.CodeUnauthorized)
}

// The run creates requests but never reports their IDs to task-engine.
func TestFinishedJobRetractsUnreportedRequests(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.seam-onesided")
	ctx := context.Background()
	owner, space := linkedOwner(t, "orphan")
	scope := OwnerSpace{Owner: owner, Space: space}
	job, err := TaskClient(t, scope).CreateJob(ctx, tv1.CreateJobReq{Space: space,
		Conversation: Idem("orphan"), Input: "slow:12000 finished", Idem: Idem("orphan")})
	if err != nil {
		t.Fatal(err)
	}
	WaitJob(t, scope, job.ID, "running", 2*time.Minute)
	var run string
	if err := AdminDB(t, "task_engine").QueryRowContext(ctx,
		"SELECT id FROM runs WHERE job=$1", job.ID).Scan(&run); err != nil {
		t.Fatal(err)
	}
	agent := runReqClient(t, scope, job.ID, run)
	for range 2 {
		r, err := agent.CreateRequest(ctx, orv1.CreateReq{Kind: "question", Prompt: "still needed?", TTLSec: 300})
		if err != nil || r.State != "pending" {
			t.Fatalf("create request: %+v %v", r, err)
		}
	}
	if j := WaitJob(t, scope, job.ID, "succeeded", time.Minute); j.Result != "answer: finished" {
		t.Fatalf("unexpected job result: %+v", j)
	}
	db := AdminDB(t, "tool_hub")
	var id string
	testkit.Poll(t, 15*time.Second, 250*time.Millisecond, func() bool {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(DISTINCT q.id), COALESCE(min(q.id),'')
			FROM owner_requests q JOIN fsm.machines m ON m.kind='owner_request' AND m.id=q.id
			JOIN fsm.outbox f ON f.machine_id=q.id AND f.topic='retract-request' AND f.done_at IS NOT NULL
			WHERE q.job=$1 AND m.state='expired'`, job.ID).Scan(&n, &id); err != nil {
			t.Fatal(err)
		}
		return n == 2
	})
	_, err = svcToolClient(t, "channel-hub", "orphan").AnswerRequest(ctx,
		orv1.AnswerReq{Request: id, Channel: "mock", External: owner, Answer: "too late"})
	testkit.Deny(t, "sys.seam-onesided", herrors.CodeOf(err) == herrors.CodeConflict)
}

func TestOwnerAnswerSingleUse(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.owner-request-single-use")
	mark := "once"
	owner, space := linkedOwner(t, mark)
	cl := svcToolClient(t, "task-engine", mark)
	id := createRequest(t, cl, owner, space, "question", "pick?", 300)
	ch := svcToolClient(t, "channel-hub", mark)
	if _, err := ch.AnswerRequest(context.Background(), orv1.AnswerReq{
		Request: id, Channel: "mock", External: owner, Answer: "blue"}); err != nil {
		t.Fatalf("first answer must be accepted: %v", err)
	}
	_, err := ch.AnswerRequest(context.Background(), orv1.AnswerReq{
		Request: id, Channel: "mock", External: owner, Answer: "red"})
	testkit.Deny(t, "sys.owner-request-single-use",
		herrors.CodeOf(err) == herrors.CodeConflict || herrors.CodeOf(err) == herrors.CodeForbidden)
}

func TestOwnerRequestExpiry(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.owner-request-timeout")
	mark := "ttl"
	owner, space := linkedOwner(t, mark)
	cl := svcToolClient(t, "task-engine", mark)
	id := createRequest(t, cl, owner, space, "question", "quick?", 5)
	testkit.Poll(t, 30*time.Second, 500*time.Millisecond, func() bool {
		res, err := cl.GetRequest(context.Background(), orv1.GetReq{Request: id})
		return err == nil && res.State == "expired"
	})
	ch := svcToolClient(t, "channel-hub", mark)
	_, err := ch.AnswerRequest(context.Background(), orv1.AnswerReq{
		Request: id, Channel: "mock", External: owner, Answer: "late"})
	testkit.Deny(t, "sys.owner-request-timeout",
		herrors.CodeOf(err) == herrors.CodeConflict || herrors.CodeOf(err) == herrors.CodeForbidden)
}
