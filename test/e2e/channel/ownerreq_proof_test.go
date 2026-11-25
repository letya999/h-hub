//go:build hhube2e

package channel

import (
	"context"
	"testing"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	orv1 "github.com/letya999/h-hub/contracts/ownerreq/v1"
	"github.com/letya999/h-hub/shared/auth"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
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
func runReqClient(t *testing.T, scope OwnerSpace, run string) *orv1.Client {
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
		Job: Idem("job", run), Run: run, Space: scope.Space, Owner: scope.Owner,
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
	if _, err := MockClient(t, owner, "Inject").Inject(context.Background(),
		cv1.InjectReq{External: owner, UpdateID: Idem("yes"), Text: "1"}); err != nil {
		t.Fatal(err)
	}
	testkit.Poll(t, 30*time.Second, 250*time.Millisecond, func() bool {
		res, err := cl.GetRequest(context.Background(), orv1.GetReq{Request: yesID})
		return err == nil && res.State == "answered" && res.Answer == "yes"
	})
	noID := createRequest(t, cl, owner, space, "approval", "wipe the archive?", 300)
	WaitMock(t, owner, cursor, "wipe the archive?", 30*time.Second)
	if _, err := MockClient(t, owner, "Inject").Inject(context.Background(),
		cv1.InjectReq{External: owner, UpdateID: Idem("no"), Text: "2"}); err != nil {
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
	agent := runReqClient(t, OwnerSpace{Owner: owner, Space: space}, Idem("run", owner))
	_, err := agent.AnswerRequest(context.Background(), orv1.AnswerReq{
		Request: id, Channel: "mock", External: owner, Answer: "blue"})
	testkit.Deny(t, "sys.owner-request-self-answer",
		herrors.CodeOf(err) == herrors.CodeForbidden || herrors.CodeOf(err) == herrors.CodeUnauthorized)
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
