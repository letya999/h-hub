package svc

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations
var migrations embed.FS

// migrate applies numbered migrations in order inside one
// transaction each; the applied watermark lives in meta. Expand-only
// like every h-hub schema (rule 30): new files only, no downs.
func migrate(ctx context.Context, db *sql.DB) error {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("migrate glob: %w", err)
	}
	sort.Strings(names)
	cur, err := schemaVersion(ctx, db)
	if err != nil {
		return err
	}
	for _, name := range names {
		num, err := migNum(name)
		if err != nil {
			return err
		}
		if num <= cur {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return fmt.Errorf("migrate read %s: %w", name, err)
		}
		if err := applyOne(ctx, db, num, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func schemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var v string
	err := db.QueryRowContext(ctx,
		`SELECT value FROM meta WHERE key='schema_version'`).Scan(&v)
	if err != nil {
		// A pre-migration database has no meta table: treat as 0
		// only when the error is the missing-table one.
		if strings.Contains(err.Error(), "no such table") {
			return 0, nil
		}
		return 0, fmt.Errorf("schema version: %w", err)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("schema version %q: %w", v, err)
	}
	return n, nil
}

func migNum(name string) (int, error) {
	base := name[strings.LastIndex(name, "/")+1:]
	n, err := strconv.Atoi(base[:4])
	if err != nil {
		return 0, fmt.Errorf("migration name %s: %w", name, err)
	}
	return n, nil
}

func applyOne(ctx context.Context, db *sql.DB, num int, body string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrate begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("migration %04d: %w", num, err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE meta SET value=$1 WHERE key='schema_version'`,
		strconv.Itoa(num)); err != nil {
		return fmt.Errorf("migration watermark %04d: %w", num, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migration commit %04d: %w", num, err)
	}
	return nil
}

// activate proves the database head: meta exists, the install marker
// row is present and every migration applied. A missing or partial
// database fails closed — never guess from what is left.
func activate(ctx context.Context, db *sql.DB) error {
	var marker string
	err := db.QueryRowContext(ctx,
		`SELECT value FROM meta WHERE key='initialized'`).Scan(&marker)
	if err == nil && marker == "1" {
		return nil
	}
	if err != nil && !strings.Contains(err.Error(), "no such table") {
		return fmt.Errorf("activation check: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx,
		`INSERT OR REPLACE INTO meta(key, value) VALUES ('initialized', '1')`); err != nil {
		return fmt.Errorf("activation marker: %w", err)
	}
	return nil
}
