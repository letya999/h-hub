//go:build hhubdb

package svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestRunClaimUnique(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, schema := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	if err := store.Migrate(ctx, db, Migrations(), "migrations"); err != nil {
		t.Fatal(err)
	}
	_, err := db.ExecContext(ctx, `INSERT INTO jobs
		(id, owner, space, conversation, input, idem, conv_seq) VALUES
		('j1','o','s','c','one','i1',1), ('j2','o','s','c','two','i2',2)`)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(2)
	barrier := claimBarrier{ready: make(chan error, 2), start: make(chan struct{})}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []string{"1", "2"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			results <- insertClaim(ctx, db, schema, barrier, id)
		}(id)
	}
	for range 2 {
		if err := <-barrier.ready; err != nil {
			close(barrier.start)
			t.Fatal(err)
		}
	}
	close(barrier.start)
	wg.Wait()
	close(results)
	var success, conflicts int
	for err := range results {
		if err == nil {
			success++
			continue
		}
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "runs_one_claim_per_conversation" {
			conflicts++
			continue
		}
		t.Fatalf("unexpected insert error: %v", err)
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("parallel claims: success=%d unique-conflict=%d", success, conflicts)
	}
}

type claimBarrier struct {
	ready chan error
	start chan struct{}
}

func insertClaim(ctx context.Context, db *sql.DB, schema string, barrier claimBarrier, id string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		barrier.ready <- err
		return fmt.Errorf("begin claim: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	if _, err := tx.ExecContext(ctx, "SET LOCAL search_path TO "+schema); err != nil {
		barrier.ready <- err
		return fmt.Errorf("claim schema: %w", err)
	}
	barrier.ready <- nil
	<-barrier.start
	if err := insertRun(ctx, tx, runRow{
		id: "r" + id, job: "j" + id, owner: "o", space: "s", conv: "c"}); err != nil {
		return err
	}
	return tx.Commit()
}
