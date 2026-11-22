package svc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/edges"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
)

type memNoncer struct{ seen map[string]bool }

func (m *memNoncer) Claim(_ context.Context, sub, nonce string, _ time.Time) error {
	k := sub + "/" + nonce
	if m.seen[k] {
		return herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	m.seen[k] = true
	return nil
}

func TestIssueVerifies(t *testing.T) {
	t.Parallel()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	subPub, subKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer := func(purpose string, payload []byte) (string, error) {
		return auth.Sign(key, purpose, payload)
	}
	s := New("identity-hub", nil, Deps{
		Sign: idv1.Signer(signer), Pub: pub, Kid: idv1.Kid(pub),
		Pubs:  map[string]ed25519.PublicKey{"task-engine": subPub},
		Nonce: &memNoncer{seen: map[string]bool{}},
		Now:   func() int64 { return time.Now().Unix() },
	})
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	c := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(srv.URL, "")}}
	_, otherKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.IssueServiceToken(context.Background(), "task-engine", "spawn-controller", "n1", otherKey); herrors.CodeOf(err) != herrors.CodeForbidden {
		t.Fatalf("bad proof: %v", err)
	}
	res, err := c.IssueServiceToken(context.Background(), "task-engine", "spawn-controller", "n1", subKey)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	tok, err := auth.Parse(res.Token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := tok.Verify(pub); err != nil {
		t.Fatalf("issued token does not verify: %v", err)
	}
	_, err = c.IssueServiceToken(context.Background(), "task-engine", "spawn-controller", "n1", subKey)
	if herrors.CodeOf(err) != herrors.CodeForbidden {
		t.Fatalf("nonce replay: got %v, want forbidden", err)
	}
}

func TestMintNeedsEdge(t *testing.T) {
	t.Parallel()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer := func(purpose string, payload []byte) (string, error) {
		return auth.Sign(key, purpose, payload)
	}
	mint := idv1.MintRunTokenReq{
		Job: "j", Run: "r", Space: "s", Owner: "o", Aud: []string{"harness-runtime"},
	}
	build := func(tab edges.Table, token string) *idv1.Client {
		s := New("identity-hub", nil, Deps{
			Sign: idv1.Signer(signer), Pub: pub, Kid: idv1.Kid(pub),
			Tab: tab, Nonce: &memNoncer{seen: map[string]bool{}},
			Now: func() int64 { return time.Now().Unix() },
		})
		srv := httptest.NewServer(s.Mux())
		t.Cleanup(srv.Close)
		return &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(srv.URL, token)}}
	}
	// A caller without the task-engine edge is refused even with a
	// correctly signed token.
	c := build(edges.Table{}, serviceToken(t, key, "ghost", "identity-hub"))
	if _, err := c.MintRunToken(context.Background(), mint); herrors.CodeOf(err) != herrors.CodeForbidden {
		t.Fatalf("no-edge caller: got %v, want forbidden", err)
	}
	// The edged caller is accepted.
	c2 := build(
		edges.Table{"identity-hub": {"task-engine": {"MintRunToken": true}}},
		serviceToken(t, key, "task-engine", "identity-hub"))
	if _, err := c2.MintRunToken(context.Background(), mint); err != nil {
		t.Fatalf("edged caller refused: %v", err)
	}
}

func serviceToken(t *testing.T, key ed25519.PrivateKey, sub, aud string) string {
	t.Helper()
	tok, err := auth.Sign(key, "service", []byte(
		fmt.Sprintf(`{"sub":%q,"aud":[%q],"exp":%d,"kid":"k"}`,
			sub, aud, time.Now().Unix()+60)))
	if err != nil {
		t.Fatal(err)
	}
	return tok
}
