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
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	"github.com/letya999/h-hub/services/spawn-controller/internal/backend"
	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/edges"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestRuntimeDrainExitsEveryState(t *testing.T) {
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
	s := New("spawn-controller", db, Deps{Issuer: pub, Now: time.Now,
		EdgeTab: edges.Table{"spawn-controller": {"operator": {"DrainRuntime": true}}}})
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	for _, state := range []string{"starting", "warm", "busy", "draining", "failed", "quarantined", "missing", "absent"} {
		t.Run(state, func(t *testing.T) {
			space, runtime := newID("space"), newID("rt")
			seedRuntimeState(t, ctx, s, space, runtime, state)
			client := drainTestClient(t, srv.URL, key, "owner-a", space)
			if _, err := drainTestClient(t, srv.URL, key, "owner-b", space).DrainRuntime(ctx,
				sv1.DrainRuntimeReq{Space: space}); herrors.CodeOf(err) != herrors.CodeNotFound {
				t.Fatalf("foreign owner drained runtime: %v", err)
			}
			resp, err := client.DrainRuntime(ctx, sv1.DrainRuntimeReq{Space: space})
			if err != nil {
				t.Fatal(err)
			}
			want := "draining"
			if state == "absent" {
				want = "absent"
			}
			got, err := s.runM.State(ctx, db, runtime)
			if err != nil || got != want || resp.State != want {
				t.Fatalf("%s exit: state=%s resp=%s err=%v", state, got, resp.State, err)
			}
			var effects int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fsm.outbox
				WHERE machine_id=$1 AND topic='docker-rm'`, runtime).Scan(&effects); err != nil {
				t.Fatal(err)
			}
			if state != "absent" && effects != 1 || state == "absent" && effects != 0 {
				t.Fatalf("%s removal intents=%d", state, effects)
			}
		})
	}
}

func TestPendingRuntimeCreateIsNotMarkedMissing(t *testing.T) {
	ctx := context.Background()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	if err := fsm.EnsureSchema(ctx, db, "fsm"); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db, Migrations(), "migrations"); err != nil {
		t.Fatal(err)
	}
	s := New("spawn-controller", db, Deps{Docker: backend.New("/tmp/absent.sock", "hhub"), Now: time.Now})
	space, runtime := newID("space"), newID("rt")
	seedRuntimeState(t, ctx, s, space, runtime, "starting")
	if err := s.reconcileOne(ctx, runtimeRow{id: runtime, space: space}); err != nil {
		t.Fatal(err)
	}
	if state, err := s.runM.State(ctx, db, runtime); err != nil || state != "starting" {
		t.Fatalf("pending create lost before Docker effect: %s %v", state, err)
	}
}

func seedRuntimeState(t *testing.T, ctx context.Context, s *Server, space, runtime, state string) {
	t.Helper()
	seedRuntimeActor(t, ctx, s, space, runtime, state, "test")
}

func TestDrainDuringFinalJobRelease(t *testing.T) {
	t.Parallel()
	for _, actor := range []string{"api", "operator", "passport"} {
		t.Run(actor, func(t *testing.T) {
			t.Parallel()
			drainAfterFinalRelease(t, actor)
		})
	}
}

func drainAfterFinalRelease(t *testing.T, actor string) {
	t.Helper()
	ctx := context.Background()
	fixture, db, _ := ensureSvc(t, ctx)
	s := fixture.s
	space, rt := newID("space"), newID("rt")
	seedRuntimeState(t, ctx, s, space, rt, "warm")
	release, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release.Rollback() //nolint:errcheck // release the lock on test failure
	var holder int
	if err := release.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&holder); err != nil {
		t.Fatal(err)
	}
	if err := s.runM.Touch(ctx, release, rt, s.deps.Now()); err != nil {
		t.Fatal(err)
	}
	drain, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { drain.Rollback() }) //nolint:errcheck // after releasing the blocker
	done := make(chan error, 1)
	go func() {
		_, err := drainRuntimeTx(s, ctx, drain, space, actor)
		done <- err
	}()
	testkit.Poll(t, 5*time.Second, 20*time.Millisecond, func() bool {
		var waiting bool
		err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
			WHERE $1=ANY(pg_blocking_pids(pid)))`, holder).Scan(&waiting)
		return err == nil && waiting
	})
	if err := release.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("drain raced a committed final release: %v", err)
	}
}

// seedRuntimeActor lands the machine in state recording actor as the
// last transition's actor — the marker EnsureRuntime reads to tell a
// deliberate drain from a sweep or passport one.
func seedRuntimeActor(t *testing.T, ctx context.Context, s *Server, space, runtime, state, actor string) {
	t.Helper()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if err := insertSpace(ctx, tx, spaceRow{id: space, owner: "owner-a", name: space,
		template: "default", spec: mustJSON(Passports{Stack: StackPassport{Image: s.deps.Hermes}}), digest: "digest"}); err != nil {
		t.Fatal(err)
	}
	if err := insertRuntime(ctx, tx, runtimeRow{id: runtime, space: space, digest: "digest"}); err != nil {
		t.Fatal(err)
	}
	if err := s.runM.Create(ctx, tx, clock{time.Now}, fsm.CreateReq{ID: runtime, Initial: state,
		Actor: actor}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func drainTestClient(t *testing.T, url string, key ed25519.PrivateKey, owner, space string) *sv1.Client {
	t.Helper()
	claims, err := json.Marshal(idv1.OwnerClaims{Sub: "operator", Owner: owner, Space: space,
		Aud: []string{"spawn-controller"}, Exp: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := auth.Sign(key, "owner", claims)
	if err != nil {
		t.Fatal(err)
	}
	return &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(url, tok)}}
}

func TestReclaimDropsMissingContainer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fx, db, _ := ensureSvc(t, ctx)
	space, id := newID("space"), newID("rt")
	seedRuntimeState(t, ctx, fx.s, space, id, "missing")
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // the test owns this transaction
	if err := setContainer(ctx, tx, id, "dead", "http://dead"); err != nil {
		t.Fatal(err)
	}
	result, err := fx.s.runtimeForLaunch(ctx, tx, spaceRow{id: space, digest: "digest"})
	if err != nil || result.Ready {
		t.Fatalf("reclaim: %+v, %v", result, err)
	}
	row, ok, err := runtimeForSpace(ctx, tx, space)
	if err != nil || !ok || row.container != "" || row.endpoint != "" {
		t.Fatalf("reclaimed runtime still points at the missing process: %+v, %v", row, err)
	}
}
