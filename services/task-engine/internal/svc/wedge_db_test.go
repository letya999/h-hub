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

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/edges"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestJobExitCommandsCoverEveryNonfinalState(t *testing.T) {
	testkit.Req(t, "sys.wedge-unblock")
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
	s := New("task-engine", db, Deps{Issuer: pub, Now: time.Now,
		EdgeTab: edges.Table{"task-engine": {"operator": {"CancelJob": true,
			"RetryJob": true, "UnquarantineJob": true}}}})
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	for _, tc := range []struct{ state, op, want string }{
		{"queued", "cancel", "cancelled"}, {"admitted", "cancel", "cancelled"},
		{"running", "cancel", "cancelled"}, {"uncertain", "cancel", "cancelled"},
		{"failed", "retry", "queued"}, {"quarantined", "unquarantine", "queued"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			id := newID("job")
			seedWedgeJob(t, ctx, s, id, tc.state)
			owner := wedgeClient(t, srv.URL, key, "owner-a")
			foreign := wedgeClient(t, srv.URL, key, "owner-b")
			if _, err := exitJob(ctx, foreign, tc.op, id); herrors.CodeOf(err) != herrors.CodeNotFound {
				t.Fatalf("foreign owner exited job: %v", err)
			}
			got, err := exitJob(ctx, owner, tc.op, id)
			if err != nil || got.State != tc.want {
				t.Fatalf("%s from %s: %+v %v", tc.op, tc.state, got, err)
			}
		})
	}
}

func seedWedgeJob(t *testing.T, ctx context.Context, s *Server, id, state string) {
	t.Helper()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if _, err := tx.ExecContext(ctx, `INSERT INTO jobs(id,owner,space,conversation,input,idem,conv_seq)
		VALUES($1,'owner-a','space-a',$1,'input',$1,1)`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.jobM.Create(ctx, tx, clock{time.Now}, fsm.CreateReq{ID: id, Initial: state,
		Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func wedgeClient(t *testing.T, url string, key ed25519.PrivateKey, owner string) *tv1.Client {
	t.Helper()
	claims, err := json.Marshal(idv1.OwnerClaims{Sub: "operator", Owner: owner, Space: "space-a",
		Aud: []string{"task-engine"}, Exp: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := auth.Sign(key, "owner", claims)
	if err != nil {
		t.Fatal(err)
	}
	return &tv1.Client{T: tv1.HTTP{C: httpapi.NewClient(url, tok)}}
}

func exitJob(ctx context.Context, c *tv1.Client, op, id string) (tv1.JobInfo, error) {
	switch op {
	case "cancel":
		return c.CancelJob(ctx, tv1.JobReq{ID: id})
	case "retry":
		return c.RetryJob(ctx, tv1.JobReq{ID: id})
	default:
		return c.UnquarantineJob(ctx, tv1.JobReq{ID: id})
	}
}
