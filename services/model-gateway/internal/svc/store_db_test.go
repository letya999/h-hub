//go:build hhubdb

package svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

// usageDB is a migrated scratch schema for accounting tests.
func usageDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	if err := fsm.EnsureSchema(ctx, db, "fsm"); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db, Migrations(), "migrations"); err != nil {
		t.Fatal(err)
	}
	return ctx, db
}

// counters reads one owner's daily spent/reserved pair.
func counters(t *testing.T, ctx context.Context, db *sql.DB, owner string) (int64, int64) {
	t.Helper()
	var spent, reserved int64
	err := db.QueryRowContext(ctx, `SELECT spent, reserved FROM usage_daily
		WHERE owner=$1`, owner).Scan(&spent, &reserved)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return spent, reserved
}

// TestReserveSettleChargesMeasured covers the happy path: reserve the
// estimate, settle the measured spend, and the reservation returns.
func TestReserveSettleChargesMeasured(t *testing.T) {
	testkit.Req(t, "sys.model-usage")
	ctx, db := usageDB(t)
	if err := reserve(ctx, db, reservation{owner: "o1", run: "run-1", estimate: 4096, daily: 1_000_000, runLimit: 250_000}); err != nil {
		t.Fatal(err)
	}
	spent, reserved := counters(t, ctx, db, "o1")
	if spent != 0 || reserved != 4096 {
		t.Fatalf("after reserve: spent=%d reserved=%d", spent, reserved)
	}
	if err := settle(ctx, db, settlement{owner: "o1", run: "run-1", route: "scripted", reserved: 4096, charged: 512, measured: true}); err != nil {
		t.Fatal(err)
	}
	spent, reserved = counters(t, ctx, db, "o1")
	if spent != 512 || reserved != 0 {
		t.Fatalf("after settle: spent=%d reserved=%d", spent, reserved)
	}
	var events int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM spend_events WHERE run='run-1'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("spend events=%d", events)
	}
}

// TestDailyCeilingRefuses reserves under a small daily ceiling until
// the conditional update matches nothing — the refusal is a distinct
// error, not a silent clamp.
func TestDailyCeilingRefuses(t *testing.T) {
	testkit.Req(t, "sys.model-quota", "adr.model-gateway.reserve-conditional")
	ctx, db := usageDB(t)
	for i, run := range []string{"r1", "r2"} {
		if err := reserve(ctx, db, reservation{owner: "o2", run: run, estimate: 50, daily: 100, runLimit: 1000}); err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
	}
	if err := reserve(ctx, db, reservation{owner: "o2", run: "r3", estimate: 50, daily: 100, runLimit: 1000}); !errors.Is(err, errOverBudget) {
		t.Fatalf("over-ceiling reserve: %v", err)
	}
}

// TestRunCeilingRefuses proves the per-run ceiling independently.
func TestRunCeilingRefuses(t *testing.T) {
	testkit.Req(t, "sys.model-quota", "adr.model-gateway.reserve-conditional")
	ctx, db := usageDB(t)
	if err := reserve(ctx, db, reservation{owner: "o3", run: "run-x", estimate: 50, daily: 1_000_000, runLimit: 100}); err != nil {
		t.Fatal(err)
	}
	if err := reserve(ctx, db, reservation{owner: "o3", run: "run-x", estimate: 60, daily: 1_000_000, runLimit: 100}); !errors.Is(err, errOverBudget) {
		t.Fatalf("run ceiling reserve: %v", err)
	}
}

// TestParallelReservesCannotOverflow is the phase-6 race proof: N
// goroutines race the same owner's counter and the committed sum of
// reservations never exceeds the ceiling.
func TestParallelReservesCannotOverflow(t *testing.T) {
	testkit.Req(t, "sys.model-quota", "adr.model-gateway.reserve-conditional")
	ctx, db := usageDB(t)
	const daily, estimate, workers = 1000, 100, 32
	var wg sync.WaitGroup
	var wins atomic.Int64
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			run := fmt.Sprintf("run-p%d", i)
			for try := 0; try < 8; try++ {
				err := reserve(ctx, db, reservation{owner: "o4", run: run, estimate: estimate, daily: daily, runLimit: 100_000})
				if errors.Is(err, errOverBudget) {
					return
				}
				if err != nil {
					t.Errorf("reserve: %v", err)
					return
				}
				wins.Add(estimate)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() > daily {
		t.Fatalf("reservations overflowed: %d > %d", wins.Load(), daily)
	}
	spent, reserved := counters(t, ctx, db, "o4")
	if reserved > daily || spent != 0 {
		t.Fatalf("counter: spent=%d reserved=%d", spent, reserved)
	}
}

// TestEstimateChargedWhenUnmeasured: a response without usage data
// settles at the estimate (measured=false).
func TestEstimateChargedWhenUnmeasured(t *testing.T) {
	testkit.Req(t, "adr.model-gateway.measured-or-estimate")
	ctx, db := usageDB(t)
	if err := reserve(ctx, db, reservation{owner: "o5", run: "run-e", estimate: 4096, daily: 1_000_000, runLimit: 250_000}); err != nil {
		t.Fatal(err)
	}
	if err := settle(ctx, db, settlement{owner: "o5", run: "run-e", route: "scripted", reserved: 4096, charged: 4096, measured: false}); err != nil {
		t.Fatal(err)
	}
	var measured bool
	if err := db.QueryRowContext(ctx,
		`SELECT measured FROM spend_events WHERE run='run-e'`).Scan(&measured); err != nil {
		t.Fatal(err)
	}
	if measured {
		t.Fatal("unmeasured settle recorded as measured")
	}
	spent, _ := counters(t, ctx, db, "o5")
	if spent != 4096 {
		t.Fatalf("estimate settle spent=%d", spent)
	}
}

// TestSweepConvertsStaleReservations: an abandoned reservation becomes
// charged spend after the TTL so a crashed call cannot wedge a counter.
func TestSweepConvertsStaleReservations(t *testing.T) {
	testkit.Req(t, "adr.model-gateway.measured-or-estimate")
	ctx, db := usageDB(t)
	if err := reserve(ctx, db, reservation{owner: "o6", run: "run-s", estimate: 4096, daily: 1_000_000, runLimit: 250_000}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE usage_runs SET reserved_at = now() - interval '1 hour' WHERE run='run-s'`); err != nil {
		t.Fatal(err)
	}
	if err := sweepReservations(ctx, db, time.Now().Add(-reserveTTL)); err != nil {
		t.Fatal(err)
	}
	spent, reserved := counters(t, ctx, db, "o6")
	if spent != 4096 || reserved != 0 {
		t.Fatalf("swept: spent=%d reserved=%d", spent, reserved)
	}
}
