package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"

	herrors "github.com/letya999/h-hub/shared/errors"
)

var migRe = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

func migErr(key, val, reason string) error {
	return herrors.New(herrors.CodeInvalid, "err.migration"). //nolint:wrapcheck // shared error form
									With(key, val).With("reason", reason)
}

type migration struct {
	num  int
	name string
	body string
	sum  string
}

// Migrate applies embedded NNNN_name.sql migrations under a Postgres
// advisory transaction lock. Numbering must be gap-free starting at
// 0001; applied files are verified by sha256 and a database newer than
// the binary's last migration is refused.
func Migrate(ctx context.Context, db *sql.DB, fsys fs.FS, dir string) error {
	migs, err := readMigrations(fsys, dir)
	if err != nil {
		return err
	}
	if len(migs) == 0 {
		return migErr("dir", dir, "empty")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(735714)"); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}
	if err := ensureTable(ctx, tx); err != nil {
		return err
	}
	applied, err := appliedSet(ctx, tx)
	if err != nil {
		return err
	}
	if err := checkApplied(migs, applied); err != nil {
		return err
	}
	for _, m := range migs {
		if _, done := applied[m.num]; done {
			continue
		}
		if err := applyOne(ctx, tx, m); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}

func readMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations %s: %w", dir, err)
	}
	var migs []migration
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migRe.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, migErr("file", e.Name(), "bad name")
		}
		num, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("migration num %s: %w", e.Name(), err)
		}
		body, err := fs.ReadFile(fsys, dir+"/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", e.Name(), err)
		}
		sum := sha256.Sum256(body)
		migs = append(migs, migration{
			num:  num,
			name: e.Name(),
			body: string(body),
			sum:  hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].num < migs[j].num })
	for i, m := range migs {
		if m.num != i+1 {
			return nil, migErr("file", m.name, "gap")
		}
	}
	return migs, nil
}

func ensureTable(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		num integer PRIMARY KEY,
		name text NOT NULL,
		sha256 text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now())`)
	if err != nil {
		return fmt.Errorf("schema_migrations: %w", err)
	}
	return nil
}

func appliedSet(ctx context.Context, tx *sql.Tx) (map[int]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT num, sha256 FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("read applied: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close
	out := map[int]string{}
	for rows.Next() {
		var num int
		var sum string
		if err := rows.Scan(&num, &sum); err != nil {
			return nil, fmt.Errorf("scan applied: %w", err)
		}
		out[num] = sum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("applied rows: %w", err)
	}
	return out, nil
}

func checkApplied(migs []migration, applied map[int]string) error {
	for num := range applied {
		if num > len(migs) {
			return migErr("num", strconv.Itoa(num), "database newer than binary")
		}
	}
	for _, m := range migs {
		if sum, ok := applied[m.num]; ok && sum != m.sum {
			return migErr("file", m.name, "applied body changed")
		}
	}
	return nil
}

func applyOne(ctx context.Context, tx *sql.Tx, m migration) error {
	if _, err := tx.ExecContext(ctx, m.body); err != nil {
		return fmt.Errorf("migration %s: %w", m.name, err)
	}
	_, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations (num, name, sha256) VALUES ($1,$2,$3)",
		m.num, m.name, m.sum)
	if err != nil {
		return fmt.Errorf("record %s: %w", m.name, err)
	}
	return nil
}

// Version reports the highest applied migration number (0 when none).
func Version(ctx context.Context, db *sql.DB) (int, error) {
	var v sql.NullInt64
	err := db.QueryRowContext(ctx,
		"SELECT max(num) FROM schema_migrations").Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("version: %w", err)
	}
	return int(v.Int64), nil
}
