//go:build hhubdb

package svc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/edges"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestJobResultAndStateShareSnapshot(t *testing.T) {
	t.Parallel()
	for _, op := range []string{"GetJob", "ListJobs"} {
		t.Run(op, func(t *testing.T) {
			t.Parallel()
			read, settled := settlingJob(t, op)
			first := read()
			select {
			case err := <-settled:
				if err != nil {
					t.Fatal(err)
				}
			default:
				t.Fatal("settlement did not overlap the job read")
			}
			if first.State == "running" {
				if first.Done || first.Result != "" {
					t.Fatalf("uncommitted result: %+v", first)
				}
			} else {
				assertSettledAnswer(t, first)
			}
			assertSettledAnswer(t, read())
		})
	}
}

func assertSettledAnswer(t *testing.T, j tv1.JobInfo) {
	t.Helper()
	if j.State != "succeeded" || !j.Done || j.Result != "finished answer" {
		t.Fatalf("inconsistent job reply: %+v", j)
	}
}

// The writer settles after the reader's first SQL statement, while the
// contract request is in flight. Both connections use the same scratch schema.
func settlingJob(t *testing.T, op string) (func() tv1.JobInfo, <-chan error) {
	t.Helper()
	ctx := t.Context()
	db, schema := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
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
	deps := Deps{Issuer: pub, Now: time.Now, EdgeTab: edges.Table{"task-engine": {"operator": {op: true}}}}
	writer := New("task-engine", db, deps)
	id := newID("job")
	seedWedgeJob(t, ctx, writer, id, "running")
	cfg, err := pgx.ParseConfig(testkit.DSN())
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeParams["search_path"] = schema
	settled := make(chan error, 1)
	cfg.Tracer = afterJobQuery(sync.OnceFunc(func() { settled <- finishSnapshotJob(ctx, writer, id) }))
	reader := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { reader.Close() }) //nolint:errcheck // test pool teardown
	srv := httptest.NewServer(New("task-engine", reader, deps).Mux())
	t.Cleanup(srv.Close)
	client := wedgeClient(t, srv.URL, key, "owner-a")
	return func() tv1.JobInfo {
		t.Helper()
		if op == "GetJob" {
			got, err := client.GetJob(ctx, tv1.GetJobReq{ID: id})
			if err != nil {
				t.Fatal(err)
			}
			return got.Job
		}
		got, err := client.ListJobs(ctx, tv1.ListJobsReq{})
		if err != nil || len(got.Jobs) != 1 {
			t.Fatalf("list jobs: %+v, %v", got, err)
		}
		return got.Jobs[0]
	}, settled
}

type afterJobQuery func()

func (afterJobQuery) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

func (f afterJobQuery) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) { f() }

func finishSnapshotJob(ctx context.Context, s *Server, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // test rollback after commit
	if err := setResult(ctx, tx, id, "finished answer"); err != nil {
		return err
	}
	if err := s.jobM.Transition(ctx, tx, fsm.Request{ID: id, Event: "settle-ok", Actor: "runtime"}, nil); err != nil {
		return err
	}
	return tx.Commit()
}
