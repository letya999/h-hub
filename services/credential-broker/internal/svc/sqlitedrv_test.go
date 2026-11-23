//go:build hhubdb && libsqlite3

package svc

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	credv1 "github.com/letya999/h-hub/contracts/cred/v1"
)

// The driver exists because mattn/go-sqlite3 cannot reopen an
// encrypted file: its Open runs schema-reading pragmas before the
// key hook. This test is the boundary contract — create, reopen,
// read, write — and it failed before the driver was in place.
func TestCipherReopenReadWrite(t *testing.T) {
	ctx := context.Background()
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	path := filepath.Join(t.TempDir(), "probe.db")
	db, err := Open(ctx, path, key)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO credentials(ref, name, type) VALUES ($1, $2, $3)`,
		"cred://builtin/platform/probe", "probe", "api-key"); err != nil {
		t.Fatalf("credential row: %v", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO versions(ref, version, sealed, kid) VALUES ($1, $2, $3, $4)`,
		"cred://builtin/platform/probe", 1, []byte{1, 2, 3}, "kid"); err != nil {
		t.Fatalf("version write: %v", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_events(actor, event, ref, detail) VALUES ($1, $2, $3, $4)`,
		"operator", "set", "ref", ""); err != nil {
		t.Fatalf("audit: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	db, err = Open(ctx, path, key)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close() //nolint:errcheck // best-effort close
	var name, sealed string
	var blob []byte
	if err := db.QueryRowContext(ctx,
		`SELECT name FROM credentials WHERE ref=$1`,
		"cred://builtin/platform/probe").Scan(&name); err != nil {
		t.Fatalf("read after reopen: %v", err)
	}
	if name != "probe" {
		t.Fatalf("name = %q", name)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT sealed FROM versions WHERE ref=$1 AND version=$2`,
		"cred://builtin/platform/probe", 1).Scan(&blob); err != nil {
		t.Fatalf("blob after reopen: %v", err)
	}
	if string(blob) != string([]byte{1, 2, 3}) {
		t.Fatalf("blob mismatch: %v", blob)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT ref || ':' || name FROM credentials WHERE ref=$1`,
		"cred://builtin/platform/probe").Scan(&sealed); err != nil {
		t.Fatalf("concat read: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 16 || string(raw[:15]) == "SQLite format 3" {
		t.Fatal("database file is not encrypted at rest")
	}
}

// Backup runs snapshot repeatedly; VACUUM INTO refuses an existing
// target, so the handler must clear the previous sealed copy. This
// failed before the stale file was removed.
func TestSnapshotOverwritesExisting(t *testing.T) {
	ctx := context.Background()
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	path := filepath.Join(t.TempDir(), "probe.db")
	db, err := Open(ctx, path, key)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer db.Close() //nolint:errcheck // best-effort close
	s := &Server{db: db, deps: Deps{DBPath: path}}
	if _, err := s.snapshot(ctx, credv1.SnapshotReq{}); err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	if _, err := s.snapshot(ctx, credv1.SnapshotReq{}); err != nil {
		t.Fatalf("snapshot over existing file: %v", err)
	}
}

// A wrong key must refuse the file — the same fail-closed property
// the cipher proof carries for a non-SQLCipher engine.
func TestCipherWrongKeyRefuses(t *testing.T) {
	ctx := context.Background()
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	other := "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	path := filepath.Join(t.TempDir(), "probe.db")
	db, err := Open(ctx, path, key)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO credentials(ref, name, type) VALUES ($1, $2, $3)`,
		"r", "n", "api-key"); err != nil {
		t.Fatalf("write: %v", err)
	}
	db.Close() //nolint:errcheck // best-effort close
	db, err = Open(ctx, path, other)
	if err == nil {
		var one int
		err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM credentials`).Scan(&one)
		_ = one
	}
	if err == nil {
		db.Close() //nolint:errcheck // best-effort close
		t.Fatal("wrong key opened the encrypted database")
	}
}
