//go:build hhubdb

package store

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/letya999/h-hub/shared/testkit"
)

func TestMigrateApplyAndIdempotent(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.store.advisory-lock", "sys.schema-version")
	ctx := context.Background()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	fsys := fstest.MapFS{
		"migrations/0001_a.sql": &fstest.MapFile{Data: []byte(
			"CREATE TABLE a (id int PRIMARY KEY)")},
		"migrations/0002_b.sql": &fstest.MapFile{Data: []byte(
			"CREATE TABLE b (id int PRIMARY KEY)")},
	}
	if err := Migrate(ctx, db, fsys, "migrations"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db, fsys, "migrations"); err != nil {
		t.Fatalf("second run must be a no-op: %v", err)
	}
	if v, err := Version(ctx, db); err != nil || v != 2 {
		t.Fatalf("version=%d err=%v", v, err)
	}
	var n int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM schema_migrations").Scan(&n); err != nil || n != 2 {
		t.Fatalf("applied=%d err=%v", n, err)
	}
}

func TestMigrateRejectsTamperedBody(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.store.numbered-sql")
	ctx := context.Background()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	orig := fstest.MapFS{
		"migrations/0001_a.sql": &fstest.MapFile{Data: []byte(
			"CREATE TABLE a (id int PRIMARY KEY)")},
	}
	if err := Migrate(ctx, db, orig, "migrations"); err != nil {
		t.Fatal(err)
	}
	edited := fstest.MapFS{
		"migrations/0001_a.sql": &fstest.MapFile{Data: []byte(
			"CREATE TABLE a (id int PRIMARY KEY); -- edited")},
	}
	if err := Migrate(ctx, db, edited, "migrations"); err == nil {
		t.Fatal("edited applied migration must be refused")
	}
}

func TestMigrateRejectsNewerDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	two := fstest.MapFS{
		"migrations/0001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1")},
		"migrations/0002_b.sql": &fstest.MapFile{Data: []byte("SELECT 2")},
	}
	if err := Migrate(ctx, db, two, "migrations"); err != nil {
		t.Fatal(err)
	}
	one := fstest.MapFS{
		"migrations/0001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1")},
	}
	if err := Migrate(ctx, db, one, "migrations"); err == nil {
		t.Fatal("binary older than database must be refused")
	}
}
