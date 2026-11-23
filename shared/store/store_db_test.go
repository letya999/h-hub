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
	_, err := db.ExecContext(ctx,
		`INSERT INTO schema_migrations (num, name, sha256) VALUES (1,'dup','x')`)
	testkit.Deny(t, "adr.store.advisory-lock", err != nil)
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

func TestMigrateReadsNewerDatabase(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.store.n-minus-one")
	ctx := context.Background()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	two := fstest.MapFS{
		"migrations/0001_a.sql": &fstest.MapFile{Data: []byte("CREATE TABLE a (id int PRIMARY KEY)")},
		"migrations/0002_b.sql": &fstest.MapFile{Data: []byte("ALTER TABLE a ADD COLUMN note text DEFAULT ''")},
	}
	if err := Migrate(ctx, db, two, "migrations"); err != nil {
		t.Fatal(err)
	}
	one := fstest.MapFS{
		"migrations/0001_a.sql": &fstest.MapFile{Data: []byte("CREATE TABLE a (id int PRIMARY KEY)")},
	}
	if err := Migrate(ctx, db, one, "migrations"); err != nil {
		t.Fatalf("N-1 binary cannot read expanded database: %v", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM a`).Scan(&n); err != nil {
		t.Fatalf("N-1 read: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE num=1`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db, one, "migrations"); err == nil {
		t.Fatal("applied gap accepted")
	}
}
