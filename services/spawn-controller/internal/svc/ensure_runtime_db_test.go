//go:build hhubdb

package svc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/edges"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

// A runtime reaped by the idle sweep lands in absent, the same state
// a fresh claim starts from. EnsureRuntime must re-claim it — the
// bug answered a live message with conflict and wedged the job.
func TestEnsureAbsentRuntimeReclaims(t *testing.T) {
	ctx := context.Background()
	fx, db, key := ensureSvc(t, ctx)
	space, runtime := newID("space"), newID("rt")
	seedRuntimeState(t, ctx, fx.s, space, runtime, "absent")
	client := ensureTestClient(t, fx.url, key)
	resp, err := client.EnsureRuntime(ctx, sv1.EnsureRuntimeReq{Space: space})
	if err != nil {
		t.Fatalf("absent runtime ensure refused: %v", err)
	}
	if resp.Ready || resp.Digest != "digest" {
		t.Fatalf("response: %+v", resp)
	}
	st, err := fx.s.runM.State(ctx, db, runtime)
	if err != nil || st != "starting" {
		t.Fatalf("machine state: %s %v", st, err)
	}
	var effects int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fsm.outbox
		WHERE machine_id=$1 AND topic='docker-create'`, runtime).Scan(&effects); err != nil {
		t.Fatal(err)
	}
	if effects != 1 {
		t.Fatalf("docker-create effects=%d", effects)
	}
}

// A draining runtime is transient: ensure answers not-ready so the
// caller polls until the machine lands somewhere claimable.
func TestEnsureDrainingRuntimeWaits(t *testing.T) {
	ctx := context.Background()
	fx, _, key := ensureSvc(t, ctx)
	space, runtime := newID("space"), newID("rt")
	seedRuntimeState(t, ctx, fx.s, space, runtime, "draining")
	client := ensureTestClient(t, fx.url, key)
	resp, err := client.EnsureRuntime(ctx, sv1.EnsureRuntimeReq{Space: space})
	if err != nil {
		t.Fatalf("draining runtime ensure refused: %v", err)
	}
	if resp.Ready {
		t.Fatalf("draining runtime reported ready: %+v", resp)
	}
}

type ensureFixture struct {
	s   *Server
	url string
}

// ensureSvc builds the service over a multi-connection pool:
// ensureRuntimeOp holds a FOR UPDATE tx and reads the machine state
// on a second conn, which a single-conn TempSchema pool deadlocks.
func ensureSvc(t *testing.T, ctx context.Context) (*ensureFixture, *sql.DB,
	ed25519.PrivateKey) {
	t.Helper()
	admin, err := sql.Open("pgx", testkit.DSN())
	if err != nil {
		t.Fatalf("admin open: %v", err)
	}
	schema := testkit.ScratchSchema(t, ctx, admin)
	db, err := sql.Open("pgx", testkit.DSN()+"&search_path="+schema)
	if err != nil {
		t.Fatalf("pool open: %v", err)
	}
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
	sv := New("spawn-controller", db, Deps{Issuer: pub, Now: time.Now,
		EdgeTab: edges.Table{"spawn-controller": {"task-engine": {"EnsureRuntime": true}}}})
	srv := httptest.NewServer(sv.Mux())
	t.Cleanup(func() {
		srv.Close()
		db.Close()    //nolint:errcheck // best-effort close
		admin.Close() //nolint:errcheck // best-effort close
	})
	return &ensureFixture{s: sv, url: srv.URL}, db, key
}

func ensureTestClient(t *testing.T, url string, key ed25519.PrivateKey) *sv1.Client {
	t.Helper()
	claims, err := json.Marshal(idv1.ServiceClaims{Sub: "task-engine",
		Aud: []string{"spawn-controller"}, Exp: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := auth.Sign(key, "service", claims)
	if err != nil {
		t.Fatal(err)
	}
	return &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(url, tok)}}
}
