//go:build hhubdb

package svc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/edges"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestInjectDedupAndIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	if err := fsm.EnsureSchema(ctx, db, "fsm"); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db, Migrations(), "migrations"); err != nil {
		t.Fatal(err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := New("channel-hub", db, Deps{OperatorPub: pub, Edges: edges.Table{"channel-hub": {"operator": {"Inject": true, "ReadOutbox": true}}}, Now: time.Now})
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	alice := mockClient(t, srv.URL, key, "alice", "Inject")
	r := cv1.InjectReq{External: "alice", UpdateID: "one", Text: "hello"}
	a, err := alice.Inject(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	b, err := alice.Inject(ctx, r)
	if err != nil || a.ID != b.ID {
		t.Fatalf("dedup: %+v %+v %v", a, b, err)
	}
	r.Text = "changed"
	if _, err := alice.Inject(ctx, r); herrors.CodeOf(err) != herrors.CodeConflict {
		t.Fatalf("collision: %v", err)
	}
	r.Text = "hello"
	r.External = "boris"
	if _, err := alice.Inject(ctx, r); herrors.CodeOf(err) != herrors.CodeForbidden {
		t.Fatalf("cross-tenant: %v", err)
	}
	forged := mockClient(t, srv.URL, randomKey(t), "alice", "Inject")
	if _, err := forged.Inject(ctx, cv1.InjectReq{External: "alice", UpdateID: "two", Text: "x"}); herrors.CodeOf(err) != herrors.CodeUnauthorized {
		t.Fatalf("forged: %v", err)
	}
}

func randomKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mockClient(t *testing.T, url string, key ed25519.PrivateKey, external, target string) *cv1.Client {
	t.Helper()
	claims, err := json.Marshal(idv1.OwnerClaims{Sub: "operator", Owner: external, Target: target, Aud: []string{"channel-hub"}, Exp: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := auth.Sign(key, "operator", claims)
	if err != nil {
		t.Fatal(err)
	}
	return &cv1.Client{T: cv1.HTTP{C: httpapi.NewClient(url, tok)}}
}
