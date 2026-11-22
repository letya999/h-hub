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

// Conformance runs every contract case as a subtest.
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
	{"submit", caseSubmit},
	{"duplicate_run", caseDuplicate},
	{"cancel", caseCancel},
	{"cancel_unknown", caseCancelUnknown},
	{"invalid_input", caseInvalidInput},
}

func caseSubmit(t *testing.T, ctx context.Context, c *Client) {
	res, err := c.SubmitRun(ctx, SubmitRunReq{
		Run: "run_1", Job: "job_1", Space: "sp_1", Input: "hi",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !res.Accepted {
		t.Fatal("submit not accepted")
	}
}

func caseDuplicate(t *testing.T, ctx context.Context, c *Client) {
	req := SubmitRunReq{Run: "run_dup", Job: "job_1", Space: "sp_1"}
	if _, err := c.SubmitRun(ctx, req); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	_, err := c.SubmitRun(ctx, req)
	if herrors.CodeOf(err) != herrors.CodeConflict {
		t.Fatalf("duplicate run: got %v, want conflict", err)
	}
}

func caseCancel(t *testing.T, ctx context.Context, c *Client) {
	req := SubmitRunReq{Run: "run_c", Job: "job_1", Space: "sp_1"}
	if _, err := c.SubmitRun(ctx, req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := c.CancelRun(ctx, CancelRunReq{Run: "run_c"}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
}

func caseCancelUnknown(t *testing.T, ctx context.Context, c *Client) {
	_, err := c.CancelRun(ctx, CancelRunReq{Run: "run_none"})
	if herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("unknown run: got %v, want not-found", err)
	}
}

func caseInvalidInput(t *testing.T, ctx context.Context, c *Client) {
	_, err := c.SubmitRun(ctx, SubmitRunReq{Run: "run_x"})
	if herrors.CodeOf(err) != herrors.CodeInvalid {
		t.Fatalf("missing fields: got %v, want invalid", err)
	}
}
