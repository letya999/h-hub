package v1

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// Case is one conformance scenario. mk supplies a Client and the
// caller's private key.
type Case struct {
	Name string
	Run  func(t *testing.T, ctx context.Context, c *Client, key ed25519.PrivateKey)
}

// Conformance runs every contract case as a subtest.
func Conformance(t *testing.T, mk func(t *testing.T) (*Client, ed25519.PrivateKey)) {
	t.Helper()
	ctx := context.Background()
	for _, cs := range cases {
		t.Run(cs.Name, func(t *testing.T) {
			t.Parallel()
			c, key := mk(t)
			cs.Run(t, ctx, c, key)
		})
	}
}

var cases = []Case{
	{"issue_service_token", caseIssue},
	{"issue_unknown_sub", caseUnknownSub},
	{"issue_bad_proof", caseBadProof},
	{"mint_run_token", caseMintRun},
	{"mint_invalid", caseMintInvalid},
}

func caseIssue(t *testing.T, ctx context.Context, c *Client, key ed25519.PrivateKey) {
	res, err := c.IssueServiceToken(ctx, "task-engine", "spawn-controller", "n-1", key)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if res.Token == "" || res.ExpiresAt == 0 {
		t.Fatal("empty token or expiry")
	}
}

func caseUnknownSub(t *testing.T, ctx context.Context, c *Client, key ed25519.PrivateKey) {
	_, err := c.IssueServiceToken(ctx, "ghost", "spawn-controller", "n-2", key)
	if herrors.CodeOf(err) != herrors.CodeForbidden {
		t.Fatalf("unknown sub: got %v, want forbidden", err)
	}
}

func caseBadProof(t *testing.T, ctx context.Context, c *Client, _ ed25519.PrivateKey) {
	_, other, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.IssueServiceToken(ctx, "task-engine", "spawn-controller", "n-3", other)
	if herrors.CodeOf(err) != herrors.CodeForbidden {
		t.Fatalf("bad proof: got %v, want forbidden", err)
	}
}

func caseMintRun(t *testing.T, ctx context.Context, c *Client, _ ed25519.PrivateKey) {
	res, err := c.MintRunToken(ctx, MintRunTokenReq{
		Job: "job_1", Run: "run_1", Space: "sp_1", Owner: "o_1",
		Digest: "sha256:x", Aud: []string{"harness-runtime"},
	})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if res.Token == "" {
		t.Fatal("empty run token")
	}
}

func caseMintInvalid(t *testing.T, ctx context.Context, c *Client, _ ed25519.PrivateKey) {
	_, err := c.MintRunToken(ctx, MintRunTokenReq{Run: "run_1"})
	if herrors.CodeOf(err) != herrors.CodeInvalid {
		t.Fatalf("missing fields: got %v, want invalid", err)
	}
}
