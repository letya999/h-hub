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
	{"destroy", caseDestroy},
	{"unknown_space", caseUnknownSpace},
	{"invalid_input", caseInvalidInput},
	{"set_model", caseSetModel},
	{"set_model_unknown_route", caseSetModelUnknown},
}

func caseCreateGet(t *testing.T, ctx context.Context, c *Client) {
	res, err := c.CreateSpace(ctx, CreateSpaceReq{
		Name: "alpha", Template: "default",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.ID == "" || res.Digest == "" {
		t.Fatal("empty id or digest")
	}
	got, err := c.GetSpace(ctx, GetSpaceReq{ID: res.ID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Space.ID != res.ID || got.Space.Digest != res.Digest {
		t.Fatalf("mismatch: %+v", got.Space)
	}
}

func caseDestroy(t *testing.T, ctx context.Context, c *Client) {
	res, err := c.CreateSpace(ctx, CreateSpaceReq{Name: "a"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := c.DestroySpace(ctx, DestroySpaceReq{ID: res.ID}); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	_, err = c.GetSpace(ctx, GetSpaceReq{ID: res.ID})
	if herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("post-destroy read: got %v, want not-found", err)
	}
}

func caseUnknownSpace(t *testing.T, ctx context.Context, c *Client) {
	_, err := c.GetSpace(ctx, GetSpaceReq{ID: "sp_none"})
	if herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("unknown space: got %v, want not-found", err)
	}
}

func caseInvalidInput(t *testing.T, ctx context.Context, c *Client) {
	_, err := c.CreateSpace(ctx, CreateSpaceReq{})
	if herrors.CodeOf(err) != herrors.CodeInvalid {
		t.Fatalf("missing name: got %v, want invalid", err)
	}
}

func caseSetModel(t *testing.T, ctx context.Context, c *Client) {
	res, err := c.CreateSpace(ctx, CreateSpaceReq{Name: "m"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	set, err := c.SetModel(ctx, SetModelReq{Space: res.ID, Default: "scripted-alt"})
	if err != nil {
		t.Fatalf("set model: %v", err)
	}
	if set.Digest == "" || set.Digest == res.Digest {
		t.Fatalf("digest unchanged after model switch: %q", set.Digest)
	}
	got, err := c.GetSpace(ctx, GetSpaceReq{ID: res.ID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Space.Digest != set.Digest {
		t.Fatalf("digest mismatch: got %q, want %q", got.Space.Digest, set.Digest)
	}
}

func caseSetModelUnknown(t *testing.T, ctx context.Context, c *Client) {
	res, err := c.CreateSpace(ctx, CreateSpaceReq{Name: "m2"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = c.SetModel(ctx, SetModelReq{Space: res.ID, Default: "no-such-route"})
	if herrors.CodeOf(err) != herrors.CodeInvalid {
		t.Fatalf("unknown route: got %v, want invalid", err)
	}
}
