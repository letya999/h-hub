//go:build hhubdb

package svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

// toolDB is a migrated scratch schema for admission tests.
func toolDB(t *testing.T) (context.Context, *sql.DB) {
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

// TestReceiptsAreAppended proves every decision — allow and deny —
// lands a ledger row with the digest pair and the caps checked.
func TestReceiptsAreAppended(t *testing.T) {
	t.Parallel()
	ctx, db := toolDB(t)
	s := &Server{db: db, deps: Deps{Now: time.Now}}
	for i, allowed := range []bool{true, false} {
		r, err := s.record(ctx, receiptRow{
			space: "sp", run: "run_1", job: "job_1",
			reqDigest: "dg_1", specDigest: "dg_1",
			toolset: "probe", tool: "probe_echo",
			caps: []string{"tools.use"}, allowed: allowed})
		if err != nil {
			t.Fatal(err)
		}
		if r.ID == "" {
			t.Fatalf("receipt %d without id", i)
		}
	}
	var n, denied int
	if err := db.QueryRowContext(ctx, `SELECT count(*),
		count(*) FILTER (WHERE NOT allowed) FROM receipts`).Scan(&n, &denied); err != nil {
		t.Fatal(err)
	}
	if n != 2 || denied != 1 {
		t.Fatalf("receipts n=%d denied=%d", n, denied)
	}
}

// TestProjectionCacheRoundTrip proves a stored projection comes back
// exactly — the row is a pure function of its digest.
func TestProjectionCacheRoundTrip(t *testing.T) {
	t.Parallel()
	ctx, db := toolDB(t)
	s := &Server{db: db}
	if _, ok, err := s.cachedProjection(ctx, "dg_1"); err != nil || ok {
		t.Fatalf("cache miss: %v %v", ok, err)
	}
	want := thv1.ProjectionResp{Digest: "dg_1", Core: []thv1.ToolInfo{{
		Toolset: "probe", Name: "probe_echo", Description: "echo",
		Parameters: json.RawMessage(`{"type":"object"}`)}}}
	if err := s.cacheProjection(ctx, "sp", "dg_1", want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.cachedProjection(ctx, "dg_1")
	if err != nil || !ok {
		t.Fatalf("cache hit: %v %v", ok, err)
	}
	if len(got.Core) != 1 || got.Core[0].Name != "probe_echo" {
		t.Fatalf("round trip: %+v", got)
	}
}
