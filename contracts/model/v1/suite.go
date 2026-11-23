package v1

import (
	"context"
	"encoding/json"
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
	{"complete", caseComplete},
	{"usage_counts_spend", caseUsage},
	{"routes", caseRoutes},
	{"invalid_input", caseInvalidInput},
}

func caseComplete(t *testing.T, ctx context.Context, c *Client) {
	res, err := c.Complete(ctx, CompleteReq{
		Run: "run_1", Request: json.RawMessage(`{"model":"x","messages":[]}`)})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	var body struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(res.Response, &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Choices) == 0 || body.Choices[0].Message.Content == "" {
		t.Fatal("empty answer")
	}
}

func caseUsage(t *testing.T, ctx context.Context, c *Client) {
	if _, err := c.Complete(ctx, CompleteReq{
		Run: "run_1", Request: json.RawMessage(`{"model":"x"}`)}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	res, err := c.Usage(ctx, UsageReq{})
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if res.Spent <= 0 || res.DailyLimit <= 0 || res.RunLimit <= 0 {
		t.Fatalf("usage counters: %+v", res)
	}
}

func caseRoutes(t *testing.T, ctx context.Context, c *Client) {
	res, err := c.Routes(ctx, RoutesReq{})
	if err != nil {
		t.Fatalf("routes: %v", err)
	}
	if res.Default == "" || len(res.Routes) == 0 {
		t.Fatalf("empty routes: %+v", res)
	}
	for _, r := range res.Routes {
		if r.Name == "" {
			t.Fatalf("unnamed route: %+v", r)
		}
	}
}

func caseInvalidInput(t *testing.T, ctx context.Context, c *Client) {
	_, err := c.Complete(ctx, CompleteReq{})
	if herrors.CodeOf(err) != herrors.CodeInvalid {
		t.Fatalf("missing run/body: got %v, want invalid", err)
	}
}
