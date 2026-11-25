//go:build hhubdb

package svc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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
