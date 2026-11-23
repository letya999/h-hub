package v1

import (
	"context"
	"testing"
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
	{"set_acquire_roundtrip", caseRoundtrip},
	{"acquire_unknown_ref", caseAcquireUnknown},
	{"revoke_lease", caseRevoke},
	{"revoke_unknown_lease", caseRevokeUnknown},
	{"list_connections", caseList},
	{"invalid_input", caseInvalid},
}

func caseRoundtrip(t *testing.T, ctx context.Context, c *Client) {
	set, err := c.SetSecret(ctx, SetSecretReq{Name: "round-trip", Value: "secret-value"})
	if err != nil {
		t.Fatalf("set secret: %v", err)
	}
	if set.Ref != "cred://builtin/platform/round-trip" || set.Version < 1 {
		t.Fatalf("unexpected set result: %+v", set)
	}
	acq, err := c.Acquire(ctx, AcquireReq{Ref: set.Ref})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if acq.Lease == "" || acq.Fields["value"] != "secret-value" {
		t.Fatalf("unexpected acquire: %+v", acq)
	}
	if acq.ExpiresAt == 0 {
		t.Fatal("expiry unset")
	}
}

func caseAcquireUnknown(t *testing.T, ctx context.Context, c *Client) {
	if _, err := c.Acquire(ctx, AcquireReq{
		Ref: "cred://builtin/platform/absent"}); err == nil {
		t.Fatal("expected error")
	}
}

func caseRevoke(t *testing.T, ctx context.Context, c *Client) {
	set, err := c.SetSecret(ctx, SetSecretReq{Name: "revoked", Value: "v"})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	acq, err := c.Acquire(ctx, AcquireReq{Ref: set.Ref})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if _, err := c.Revoke(ctx, RevokeReq{Lease: acq.Lease}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := c.Revoke(ctx, RevokeReq{Lease: acq.Lease}); err == nil {
		t.Fatal("second revoke should fail")
	}
}

func caseRevokeUnknown(t *testing.T, ctx context.Context, c *Client) {
	if _, err := c.Revoke(ctx, RevokeReq{Lease: "lease-absent"}); err == nil {
		t.Fatal("expected error")
	}
}

func caseList(t *testing.T, ctx context.Context, c *Client) {
	if _, err := c.SetSecret(ctx, SetSecretReq{Name: "listed", Value: "v"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	res, err := c.ListConnections(ctx, ListConnectionsReq{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, conn := range res.Connections {
		if conn.Name == "listed" && conn.Ref != "" && conn.Version >= 1 {
			return
		}
	}
	t.Fatalf("listed missing: %+v", res.Connections)
}

func caseInvalid(t *testing.T, ctx context.Context, c *Client) {
	if _, err := c.SetSecret(ctx, SetSecretReq{}); err == nil {
		t.Fatal("empty set should fail")
	}
	if _, err := c.Acquire(ctx, AcquireReq{}); err == nil {
		t.Fatal("empty acquire should fail")
	}
}
