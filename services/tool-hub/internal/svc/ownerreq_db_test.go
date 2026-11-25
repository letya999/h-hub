//go:build hhubdb

package svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	orv1 "github.com/letya999/h-hub/contracts/ownerreq/v1"
	"github.com/letya999/h-hub/services/tool-hub/internal/ownerreq"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/log"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

// flipMode is a mutable mode source: the spec value the module
// resolves per create — flipping it proves the leaf applies live.
type flipMode struct{ mode string }

func (f *flipMode) ApprovalsMode(_ context.Context, _ string) (string, error) {
	return f.mode, nil
}

// oreqServer is the module wired to a scratch schema behind its
// contract ops; the verifier is a stub naming a service caller.
type oreqServer struct {
	srv  *httptest.Server
	mode *flipMode
}

func newOreqServer(t *testing.T, ctx context.Context) (*oreqServer, *orv1.Client) {
	t.Helper()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	if err := fsm.EnsureSchema(ctx, db, "fsm"); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db, Migrations(), "migrations"); err != nil {
		t.Fatal(err)
	}
	mode := &flipMode{mode: "ask"}
	mod := ownerreq.New(db, ownerreq.Deps{Now: time.Now, Mode: mode},
		log.New("tool-hub", io.Discard))
	ver := func(r *http.Request) (httpapi.Principal, error) {
		return httpapi.Principal{Purpose: "service", Sub: "task-engine"}, nil
	}
	mux := http.NewServeMux()
	for name, op := range orv1.Ops(mod.Handlers()) {
		op := op
		mux.Handle("/v1/"+name, httpapi.HandleAuth(ver,
			func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
				return op(w, r)
			}))
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &oreqServer{srv: srv, mode: mode},
		&orv1.Client{T: orv1.HTTP{C: httpapi.NewClient(srv.URL, "tk")}}
}

func serviceReq(kind string) orv1.CreateReq {
	return orv1.CreateReq{Kind: kind, Prompt: "proceed?",
		Owner: "alice", Space: "sp", Channel: "mock", External: "alice",
		TTLSec: 60}
}

// TestModeAnswersApprovalsAtOnce proves the approvals.mode leaf: ask
// arms a delivery and waits, auto and bypass resolve the approval at
// once with the canonical yes — the mode permits the action, it
// never picks an answer for the human.
func TestModeAnswersApprovalsAtOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	o, cl := newOreqServer(t, ctx)
	res, err := cl.CreateRequest(ctx, serviceReq(orv1.KindApproval))
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "pending" {
		t.Fatalf("ask mode state = %q, want pending", res.State)
	}
	for _, mode := range []string{"auto", "bypass"} {
		o.mode.mode = mode
		res, err := cl.CreateRequest(ctx, serviceReq(orv1.KindApproval))
		if err != nil {
			t.Fatal(err)
		}
		if res.State != "answered" {
			t.Fatalf("%s mode state = %q, want answered", mode, res.State)
		}
		got, err := cl.GetRequest(ctx, orv1.GetReq{Request: res.Request})
		if err != nil {
			t.Fatal(err)
		}
		if got.Answer != "yes" {
			t.Fatalf("%s mode answer = %q, want canonical yes", mode, got.Answer)
		}
	}
}

// TestModeLeavesQuestionsToTheHuman proves the mode governs
// approvals only: an information request is delivered and awaited
// under every mode — auto-answering it would feed the model a
// sentinel string instead of the owner's words.
func TestModeLeavesQuestionsToTheHuman(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	o, cl := newOreqServer(t, ctx)
	for _, mode := range []string{"ask", "auto", "bypass"} {
		o.mode.mode = mode
		res, err := cl.CreateRequest(ctx, serviceReq(orv1.KindQuestion))
		if err != nil {
			t.Fatal(err)
		}
		if res.State != "pending" {
			t.Fatalf("%s mode question state = %q, want pending", mode, res.State)
		}
	}
}

// runClaimsPayload fakes the claims the issuer would sign for one
// run — no conversation: the mint wire stays frozen for the
// rollback baseline.
func runClaimsPayload(t *testing.T) []byte {
	t.Helper()
	claims, err := json.Marshal(map[string]any{
		"sub": "run:rn_1", "job": "jb_1", "run": "rn_1",
		"space": "sp", "owner": "alice", "digest": "dg",
		"aud": []string{"tool-hub"}, "exp": time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return claims
}

// deliverEffects counts the delivery effects armed for one request.
func deliverEffects(t *testing.T, ctx context.Context, db *sql.DB, id string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fsm.outbox
		WHERE machine_id=$1 AND topic='deliver-request'`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestRunRequestBindsLazily proves the rollback-safe binding: a run
// caller's request carries no conversation, so no delivery arms at
// create; the service bind pins the conversation and arms the
// effect exactly once — the idempotent repeat never arms a second.
func TestRunRequestBindsLazily(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	if err := fsm.EnsureSchema(ctx, db, "fsm"); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db, Migrations(), "migrations"); err != nil {
		t.Fatal(err)
	}
	mod := ownerreq.New(db, ownerreq.Deps{Now: time.Now, Mode: &flipMode{mode: "ask"}},
		log.New("tool-hub", io.Discard))
	ver := func(r *http.Request) (httpapi.Principal, error) {
		if strings.Contains(r.Header.Get("Authorization"), "run-tk") {
			return httpapi.Principal{Purpose: "run", Sub: "run:rn_1",
				Aud: []string{"tool-hub"}, Payload: runClaimsPayload(t)}, nil
		}
		return httpapi.Principal{Purpose: "service", Sub: "task-engine"}, nil
	}
	mux := http.NewServeMux()
	for name, op := range orv1.Ops(mod.Handlers()) {
		op := op
		mux.Handle("/v1/"+name, httpapi.HandleAuth(ver,
			func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
				return op(w, r)
			}))
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	runCl := &orv1.Client{T: orv1.HTTP{C: httpapi.NewClient(srv.URL, "run-tk")}}
	svcCl := &orv1.Client{T: orv1.HTTP{C: httpapi.NewClient(srv.URL, "svc-tk")}}

	res, err := runCl.CreateRequest(ctx, orv1.CreateReq{
		Kind: orv1.KindQuestion, Prompt: "proceed?", TTLSec: 60})
	if err != nil {
		t.Fatal(err)
	}
	if n := deliverEffects(t, ctx, db, res.Request); n != 0 {
		t.Fatalf("unbound request armed %d deliveries, want 0", n)
	}
	for range 2 {
		if _, err := svcCl.BindRequest(ctx, orv1.BindReq{
			Request: res.Request, Conversation: "cv_1"}); err != nil {
			t.Fatalf("bind: %v", err)
		}
	}
	if n := deliverEffects(t, ctx, db, res.Request); n != 1 {
		t.Fatalf("bound request armed %d deliveries, want exactly 1", n)
	}
	_, err = svcCl.BindRequest(ctx, orv1.BindReq{Request: res.Request, Conversation: "cv_2"})
	if err == nil {
		t.Fatal("conflicting bind accepted")
	}
	var conv string
	if err := db.QueryRowContext(ctx,
		`SELECT conversation FROM owner_requests WHERE id=$1`, res.Request).Scan(&conv); err != nil {
		t.Fatal(err)
	}
	if conv != "cv_1" {
		t.Fatalf("conversation = %q, want cv_1", conv)
	}
}
