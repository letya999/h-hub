//go:build hhubdb

package svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	wv1 "github.com/letya999/h-hub/contracts/worker/v1"
	"github.com/letya999/h-hub/services/spawn-controller/internal/worker"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
)

func TestRuntimeWaitsForOlderWorkerMounts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture, db, key := ensureSvc(t, ctx)
	space, runtime := newID("space"), newID("rt")
	seedRuntimeState(t, ctx, fixture.s, space, runtime, "absent")
	r := worker.New(db, time.Now, wv1.Limits{MaxTotal: 1, MaxPerUser: 1})
	_, err := reserveWorker(t, db, r, worker.Claim{
		Key:   worker.Key{Owner: "owner", Space: space, Profile: "terminal", Digest: "earlier"},
		Image: "terminal-image", Lease: "mounts",
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := ensureTestClient(t, fixture.url, key).EnsureRuntime(ctx, sv1.EnsureRuntimeReq{Space: space})
	if err != nil || out.Ready {
		t.Fatalf("runtime should wait for the previous worker's mounts: ready=%t err=%v", out.Ready, err)
	}
	if state, err := fixture.s.runM.State(ctx, db, runtime); err != nil || state != "absent" {
		t.Fatalf("runtime started over a different revision's worker: %s %v", state, err)
	}
}

func TestWorkerDeadlineQueuesItsGenerationForStop(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, db, _ := ensureSvc(t, ctx)
	now := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	r := worker.New(db, func() time.Time { return now }, wv1.Limits{MaxTotal: 1, MaxPerUser: 1})
	w, err := reserveWorker(t, db, r, worker.Claim{
		Key:   worker.Key{Owner: "owner", Space: newID("space"), Profile: "terminal", Digest: "spec"},
		Image: "terminal-image", Lease: "starting",
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Second)
	if err := r.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	assertWorker(t, r, w.ID, "stopping", "start-deadline")
	e := workerEffect(t, db, w.ID, worker.StopTopic)
	var generation string
	if err := json.Unmarshal(e.Payload, &generation); err != nil || generation != w.Generation {
		t.Fatalf("stop effect generation=%q err=%v", generation, err)
	}
	if err := r.Reap(ctx, e, func(context.Context, worker.Record) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerIntentLeaseAndQuarantine(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, db, _ := ensureSvc(t, ctx)
	now := time.Now()
	readClock := func() time.Time { return now }
	limits := wv1.Limits{MaxTotal: 2, MaxPerUser: 1}
	reopen := func() *worker.Registry { return worker.New(db, readClock, limits) }
	r := reopen()
	claim := worker.Claim{
		Key:   worker.Key{Owner: "owner", Space: newID("space"), Profile: "terminal", Digest: "spec"},
		Image: "terminal-image", Lease: "first",
	}
	w, err := reserveWorker(t, db, r, claim)
	if err != nil || w.State != "starting" {
		t.Fatalf("claim = %#v, %v", w, err)
	}
	checkWorkerCapacity(t, db, r, claim)
	if err := r.Start(ctx, workerEffect(t, db, w.ID, worker.LaunchTopic), func(ctx context.Context, w worker.Record) error {
		persisted, err := reopen().Lookup(ctx, w.ID)
		if err != nil || persisted.State != "starting" || persisted.Generation != w.Generation {
			t.Fatalf("container launch lacks committed intent: %#v, %v", persisted, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r = reopen()
	assertWorker(t, r, w.ID, "leased", "")
	checkWorkerLease(t, r, w)
	claim.Lease = "next"
	next, err := reserveWorker(t, db, r, claim)
	if err != nil || next.Generation != w.Generation || next.State != "leased" || next.Lease != claim.Lease {
		t.Fatalf("reacquire = %#v, %v", next, err)
	}
	checkWorkerStop(t, db, r, next, claim)
}

func checkWorkerCapacity(t *testing.T, db *sql.DB, r *worker.Registry, claim worker.Claim) {
	t.Helper()
	claim.Space = newID("space")
	assertWorkerCapacity(t, db, r, claim)
	claim.Owner = "next-owner"
	if _, err := reserveWorker(t, db, r, claim); err != nil {
		t.Fatalf("another owner lacked available capacity: %v", err)
	}
	claim.Owner, claim.Space = "third-owner", newID("space")
	assertWorkerCapacity(t, db, r, claim)
}

func assertWorkerCapacity(t *testing.T, db *sql.DB, r *worker.Registry, claim worker.Claim) {
	t.Helper()
	_, err := reserveWorker(t, db, r, claim)
	var capacity *herrors.Error
	if !errors.As(err, &capacity) || capacity.Code != herrors.CodeCapacity || !capacity.Retryable {
		t.Fatalf("extra generation = %v, want retryable capacity", err)
	}
}

func checkWorkerLease(t *testing.T, r *worker.Registry, w worker.Record) {
	t.Helper()
	ctx := t.Context()
	lease := worker.Lease{Owner: "other", Space: w.Space, Worker: w.ID, Generation: w.Generation, ID: w.Lease}
	if err := r.ChangeLease(ctx, lease, "renew"); err == nil || herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("another owner renewed the lease: %v", err)
	}
	lease.Owner = w.Owner
	if err := r.ChangeLease(ctx, lease, "renew"); err != nil {
		t.Fatal(err)
	}
	if err := r.ChangeLease(ctx, lease, "release"); err != nil {
		t.Fatal(err)
	}
	assertWorker(t, r, w.ID, "idle", "")
}

func checkWorkerStop(t *testing.T, db *sql.DB, r *worker.Registry, w worker.Record, claim worker.Claim) {
	t.Helper()
	ctx := t.Context()
	if err := r.Stop(ctx, w, "lease-lost"); err != nil {
		t.Fatal(err)
	}
	effect := workerEffect(t, db, w.ID, worker.StopTopic)
	unobserved := errors.New("inspect unavailable")
	err := r.Reap(ctx, effect, func(context.Context, worker.Record) (bool, error) { return false, unobserved })
	if !errors.Is(err, unobserved) {
		t.Fatalf("unconfirmed stop = %v", err)
	}
	assertWorker(t, r, w.ID, "quarantined", "lease-lost")
	if _, err := reserveWorker(t, db, r, claim); err == nil || herrors.CodeOf(err) != herrors.CodeConflict {
		t.Fatalf("reserved a quarantined key: %v", err)
	}
	if err := r.Reap(ctx, effect, func(context.Context, worker.Record) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	assertWorker(t, r, w.ID, "absent", "lease-lost")
	claim.Lease = "replacement"
	next, err := reserveWorker(t, db, r, claim)
	if err != nil || next.ID != w.ID || next.Generation == w.Generation {
		t.Fatalf("replacement = %#v, %v", next, err)
	}
	if err := r.Reap(ctx, effect, func(context.Context, worker.Record) (bool, error) {
		t.Fatal("obsolete cleanup reached the new generation")
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func reserveWorker(t *testing.T, db *sql.DB, r *worker.Registry, claim worker.Claim) (worker.Record, error) {
	t.Helper()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	w, err := r.Reserve(t.Context(), tx, claim)
	if err != nil {
		return w, fmt.Errorf("reserve: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return w, fmt.Errorf("commit reservation: %w", err)
	}
	return w, nil
}

func workerEffect(t *testing.T, db *sql.DB, id, topic string) fsm.OutEffect {
	t.Helper()
	f := fsm.OutEffect{Kind: "worker", MachineID: id, Topic: topic}
	if err := db.QueryRowContext(t.Context(), `SELECT seq,payload FROM fsm.outbox
		WHERE kind='worker' AND machine_id=$1 AND topic=$2`, id, topic).Scan(&f.Seq, &f.Payload); err != nil {
		t.Fatal(err)
	}
	return f
}

func assertWorker(t *testing.T, r *worker.Registry, id, state, reason string) {
	t.Helper()
	w, err := r.Lookup(t.Context(), id)
	if err != nil || w.State != state || w.StopReason != reason {
		t.Fatalf("worker = %#v, %v, want state %q reason %q", w, err, state, reason)
	}
}
