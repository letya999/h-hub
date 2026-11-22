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
	{"create_get", caseCreateGet},
	{"tenant_scoping", caseTenantScoping},
	{"destroy", caseDestroy},
	{"unknown_space", caseUnknownSpace},
	{"invalid_input", caseInvalidInput},
}

func caseCreateGet(t *testing.T, ctx context.Context, c *Client) {
	res, err := c.CreateSpace(ctx, CreateSpaceReq{
		Owner: "o1", Name: "alpha", Template: "default",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.ID == "" || res.Digest == "" {
		t.Fatal("empty id or digest")
	}
	got, err := c.GetSpace(ctx, GetSpaceReq{Owner: "o1", ID: res.ID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Space.ID != res.ID || got.Space.Digest != res.Digest {
		t.Fatalf("mismatch: %+v", got.Space)
	}
}

func caseTenantScoping(t *testing.T, ctx context.Context, c *Client) {
	res, err := c.CreateSpace(ctx, CreateSpaceReq{Owner: "o1", Name: "a"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = c.GetSpace(ctx, GetSpaceReq{Owner: "o2", ID: res.ID})
	if herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("cross-owner read: got %v, want not-found", err)
	}
}

func caseDestroy(t *testing.T, ctx context.Context, c *Client) {
	res, err := c.CreateSpace(ctx, CreateSpaceReq{Owner: "o1", Name: "a"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := c.DestroySpace(ctx, DestroySpaceReq{Owner: "o1", ID: res.ID}); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	_, err = c.GetSpace(ctx, GetSpaceReq{Owner: "o1", ID: res.ID})
	if herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("post-destroy read: got %v, want not-found", err)
	}
}

func caseUnknownSpace(t *testing.T, ctx context.Context, c *Client) {
	_, err := c.GetSpace(ctx, GetSpaceReq{Owner: "o1", ID: "sp_none"})
	if herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("unknown space: got %v, want not-found", err)
	}
}

func caseInvalidInput(t *testing.T, ctx context.Context, c *Client) {
	_, err := c.CreateSpace(ctx, CreateSpaceReq{Owner: "o1"})
	if herrors.CodeOf(err) != herrors.CodeInvalid {
		t.Fatalf("missing name: got %v, want invalid", err)
	}
}
