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

// Conformance runs every contract case as a subtest; the suite pins
// behavior shared by the real engine and the fake.
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
	{"create_get", caseCreateGet},
	{"idem_replay", caseIdemReplay},
	{"unknown_job", caseUnknownJob},
	{"cancel", caseCancel},
	{"hold_epoch", caseHoldEpoch},
	{"invalid_input", caseInvalidInput},
}

func caseCreateGet(t *testing.T, ctx context.Context, c *Client) {
	res, err := c.CreateJob(ctx, CreateJobReq{
		Space: "sp1", Conversation: "c1", Input: "hi", Idem: "k1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := c.GetJob(ctx, GetJobReq{ID: res.ID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Job.ID != res.ID {
		t.Fatalf("get returned %q, want %q", got.Job.ID, res.ID)
	}
}

func caseIdemReplay(t *testing.T, ctx context.Context, c *Client) {
	req := CreateJobReq{Space: "sp1", Input: "hi", Idem: "k-idem"}
	a, err := c.CreateJob(ctx, req)
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := c.CreateJob(ctx, req)
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	if a.ID != b.ID {
		t.Fatalf("idem replay returned different ids %q vs %q", a.ID, b.ID)
	}
}

func caseUnknownJob(t *testing.T, ctx context.Context, c *Client) {
	_, err := c.GetJob(ctx, GetJobReq{ID: "job_none"})
	if herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("unknown job: got %v, want not-found", err)
	}
}

func caseCancel(t *testing.T, ctx context.Context, c *Client) {
	res, err := c.CreateJob(ctx, CreateJobReq{
		Space: "sp1", Input: "hi", Idem: "k-cancel",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	j, err := c.CancelJob(ctx, JobReq{ID: res.ID})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if j.State != "cancelled" {
		t.Fatalf("cancel state %q", j.State)
	}
}

func caseHoldEpoch(t *testing.T, ctx context.Context, c *Client) {
	a, err := c.SetHold(ctx, SetHoldReq{Space: "sp1", Active: true})
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	b, err := c.SetHold(ctx, SetHoldReq{Space: "sp1", Active: false})
	if err != nil {
		t.Fatalf("unhold: %v", err)
	}
	if b.Epoch <= a.Epoch {
		t.Fatalf("epoch not increasing: %d then %d", a.Epoch, b.Epoch)
	}
}

func caseInvalidInput(t *testing.T, ctx context.Context, c *Client) {
	_, err := c.CreateJob(ctx, CreateJobReq{})
	if herrors.CodeOf(err) != herrors.CodeInvalid {
		t.Fatalf("missing input: got %v, want invalid", err)
	}
}
