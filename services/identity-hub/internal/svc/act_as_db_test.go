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
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/edges"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestActAsBindsAndAudits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	if err := store.Migrate(ctx, db, Migrations(), "migrations"); err != nil {
		t.Fatal(err)
	}
	issuerPub, issuerKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	opPub, opKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := New("identity-hub", db, Deps{
		Sign: func(purpose string, payload []byte) (string, error) {
			return auth.Sign(issuerKey, purpose, payload)
		},
		Pub: issuerPub, OperatorPub: opPub, Kid: idv1.Kid(issuerPub),
		Tab: edges.Table{"identity-hub": {"operator": {"ActAs": true}}},
		Now: func() int64 { return time.Now().Unix() },
	})
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	claims, err := json.Marshal(idv1.OwnerClaims{
		Sub: "operator", Owner: "alice", Target: "task-engine",
		Aud: []string{"identity-hub"}, Exp: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	opToken, err := auth.Sign(opKey, "operator", claims)
	if err != nil {
		t.Fatal(err)
	}
	c := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(srv.URL, opToken)}}
	if _, err := c.ActAs(ctx, idv1.ActAsReq{Owner: "boris", Aud: "task-engine"}); herrors.CodeOf(err) != herrors.CodeForbidden {
		t.Fatalf("cross-owner delegation: %v", err)
	}
	if _, err := c.ActAs(ctx, idv1.ActAsReq{Owner: "alice", Aud: "spawn-controller"}); herrors.CodeOf(err) != herrors.CodeForbidden {
		t.Fatalf("cross-audience delegation: %v", err)
	}
	res, err := c.ActAs(ctx, idv1.ActAsReq{Owner: "alice", Aud: "task-engine"})
	if err != nil {
		t.Fatalf("act as: %v", err)
	}
	tok, err := auth.Parse(res.Token)
	if err != nil || tok.Purpose != "owner" || tok.Verify(issuerPub) != nil {
		t.Fatalf("bad issued token: %v", err)
	}
	owner, err := idv1.ParseOwnerClaims(tok.Payload)
	if err != nil || owner.Owner != "alice" || len(owner.Aud) != 1 || owner.Aud[0] != "task-engine" {
		t.Fatalf("bad issued scope: %+v, %v", owner, err)
	}
	var n int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM operator_audit WHERE owner='alice' AND audience='task-engine'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit rows=%d: %v", n, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM operator_audit WHERE owner='boris'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("forbidden request audited as success: %d, %v", n, err)
	}
}
