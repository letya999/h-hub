package v1

import (
	"context"
	"testing"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// Case is one conformance scenario.
type Case struct {
	Name string
	Run  func(t *testing.T, ctx context.Context, c *Client)
}

// Conformance runs every contract case as a subtest. The client must
// treat "mock/alice" as bound to owner "alice" — the suite's
// addressee.
func Conformance(t *testing.T, mk func(t *testing.T) *Client) {
	t.Helper()
	ctx := context.Background()
	for _, cs := range cases {
		t.Run(cs.Name, func(t *testing.T) {
			t.Parallel()
			cs.Run(t, ctx, mk(t))
		})
	}
}

var cases = []Case{
	{"create_pending", caseCreatePending},
	{"question_answered", caseQuestionAnswered},
	{"choice_numbered", caseChoiceNumbered},
	{"choice_bad_index", caseChoiceBadIndex},
	{"approval_yes_no", caseApprovalYesNo},
	{"answer_once", caseAnswerOnce},
	{"bind_idempotent", caseBindIdempotent},
	{"bind_conflict", caseBindConflict},
	{"bind_closed", caseBindClosed},
	{"answer_wrong_addressee", caseAnswerWrongAddressee},
	{"invalid_input", caseInvalidInput},
}

// get reads one request or fails the case.
func get(t *testing.T, ctx context.Context, c *Client, id string) GetResp {
	t.Helper()
	got, err := c.GetRequest(ctx, GetReq{Request: id})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return got
}

// create builds one pending request addressed to alice on the mock
// channel.
func create(t *testing.T, ctx context.Context, c *Client, kind string, opts []string) CreateResp {
	t.Helper()
	res, err := c.CreateRequest(ctx, CreateReq{
		Owner: "alice", Space: "sp_1", Channel: "mock", External: "alice",
		Kind: kind, Prompt: "pick one", Options: opts, TTLSec: 300})
	if err != nil {
		t.Fatalf("create %s: %v", kind, err)
	}
	if res.Request == "" || res.State != StatePending || res.Deadline == 0 {
		t.Fatalf("create: %+v", res)
	}
	return res
}

func caseCreatePending(t *testing.T, ctx context.Context, c *Client) {
	res := create(t, ctx, c, KindQuestion, nil)
	got, err := c.GetRequest(ctx, GetReq{Request: res.Request})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != StatePending || got.Kind != KindQuestion || got.Owner != "alice" {
		t.Fatalf("get: %+v", got)
	}
}

func caseQuestionAnswered(t *testing.T, ctx context.Context, c *Client) {
	res := create(t, ctx, c, KindQuestion, nil)
	ans, err := c.AnswerRequest(ctx, AnswerReq{
		Request: res.Request, Channel: "mock", External: "alice", Answer: "blue"})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if ans.State != StateAnswered {
		t.Fatalf("answer state: %+v", ans)
	}
	got := get(t, ctx, c, res.Request)
	if got.State != StateAnswered || got.Answer != "blue" {
		t.Fatalf("stored answer: %+v", got)
	}
}

func caseChoiceNumbered(t *testing.T, ctx context.Context, c *Client) {
	res := create(t, ctx, c, KindChoice, []string{"red", "blue"})
	ans, err := c.AnswerRequest(ctx, AnswerReq{
		Request: res.Request, Channel: "mock", External: "alice", Answer: "2"})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	got := get(t, ctx, c, res.Request)
	if ans.State != StateAnswered || got.Answer != "blue" {
		t.Fatalf("numbered answer: %+v %+v", ans, got)
	}
}

func caseChoiceBadIndex(t *testing.T, ctx context.Context, c *Client) {
	res := create(t, ctx, c, KindChoice, []string{"red", "blue"})
	_, err := c.AnswerRequest(ctx, AnswerReq{
		Request: res.Request, Channel: "mock", External: "alice", Answer: "9"})
	if herrors.CodeOf(err) != herrors.CodeInvalid {
		t.Fatalf("bad option: got %v, want invalid", err)
	}
	got := get(t, ctx, c, res.Request)
	if got.State != StatePending {
		t.Fatalf("refused answer still closed: %+v", got)
	}
}

func caseApprovalYesNo(t *testing.T, ctx context.Context, c *Client) {
	yes := create(t, ctx, c, KindApproval, nil)
	ans, err := c.AnswerRequest(ctx, AnswerReq{
		Request: yes.Request, Channel: "mock", External: "alice", Answer: "yes"})
	if err != nil || ans.State != StateAnswered {
		t.Fatalf("approve: %+v err %v", ans, err)
	}
	no := create(t, ctx, c, KindApproval, nil)
	ans, err = c.AnswerRequest(ctx, AnswerReq{
		Request: no.Request, Channel: "mock", External: "alice", Answer: "no"})
	if err != nil || ans.State != StateDenied {
		t.Fatalf("deny: %+v err %v", ans, err)
	}
}

func caseAnswerOnce(t *testing.T, ctx context.Context, c *Client) {
	res := create(t, ctx, c, KindQuestion, nil)
	_, err := c.AnswerRequest(ctx, AnswerReq{
		Request: res.Request, Channel: "mock", External: "alice", Answer: "one"})
	if err != nil {
		t.Fatalf("first answer: %v", err)
	}
	_, err = c.AnswerRequest(ctx, AnswerReq{
		Request: res.Request, Channel: "mock", External: "alice", Answer: "two"})
	if herrors.CodeOf(err) != herrors.CodeConflict {
		t.Fatalf("second answer: got %v, want conflict", err)
	}
	got := get(t, ctx, c, res.Request)
	if got.Answer != "one" {
		t.Fatalf("first answer lost: %+v", got)
	}
}

func caseAnswerWrongAddressee(t *testing.T, ctx context.Context, c *Client) {
	res := create(t, ctx, c, KindQuestion, nil)
	_, err := c.AnswerRequest(ctx, AnswerReq{
		Request: res.Request, Channel: "mock", External: "mallory", Answer: "hi"})
	if herrors.CodeOf(err) != herrors.CodeForbidden {
		t.Fatalf("wrong addressee: got %v, want forbidden", err)
	}
}

func caseInvalidInput(t *testing.T, ctx context.Context, c *Client) {
	_, err := c.CreateRequest(ctx, CreateReq{})
	if herrors.CodeOf(err) != herrors.CodeInvalid {
		t.Fatalf("empty create: got %v, want invalid", err)
	}
	_, err = c.CreateRequest(ctx, CreateReq{
		Owner: "alice", Space: "sp_1", Channel: "mock", External: "alice",
		Kind: KindChoice, Prompt: "no options", TTLSec: 60})
	if herrors.CodeOf(err) != herrors.CodeInvalid {
		t.Fatalf("choice without options: got %v, want invalid", err)
	}
}

// caseBindIdempotent pins the conversation once; a repeat with the
// same value is the idempotent success, not a second effect.
func caseBindIdempotent(t *testing.T, ctx context.Context, c *Client) {
	res := create(t, ctx, c, KindQuestion, nil)
	for range 2 {
		b, err := c.BindRequest(ctx, BindReq{Request: res.Request, Conversation: "cv_1"})
		if err != nil {
			t.Fatalf("bind: %v", err)
		}
		if b.State != StatePending {
			t.Fatalf("bind state: %+v", b)
		}
	}
}

// caseBindConflict refuses a second, different conversation — the
// first binding owns the request.
func caseBindConflict(t *testing.T, ctx context.Context, c *Client) {
	res := create(t, ctx, c, KindQuestion, nil)
	if _, err := c.BindRequest(ctx, BindReq{Request: res.Request, Conversation: "cv_1"}); err != nil {
		t.Fatalf("first bind: %v", err)
	}
	_, err := c.BindRequest(ctx, BindReq{Request: res.Request, Conversation: "cv_2"})
	if herrors.CodeOf(err) != herrors.CodeConflict {
		t.Fatalf("rebind: got %v, want conflict", err)
	}
}

// caseBindClosed refuses binding a request that already answered —
// a late journal event must not rearm delivery.
func caseBindClosed(t *testing.T, ctx context.Context, c *Client) {
	res := create(t, ctx, c, KindQuestion, nil)
	if _, err := c.AnswerRequest(ctx, AnswerReq{
		Request: res.Request, Channel: "mock", External: "alice", Answer: "done"}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	_, err := c.BindRequest(ctx, BindReq{Request: res.Request, Conversation: "cv_1"})
	if herrors.CodeOf(err) != herrors.CodeConflict {
		t.Fatalf("bind closed: got %v, want conflict", err)
	}
}
