// Package admin is the only database access of the install package
// (rule 10): connect under the install role, create databases, roles
// and the fsm/hub schemas declared by the descriptor. It is idempotent:
// Ensure* functions create only what is missing and never overwrite.
package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // one shared Postgres driver
)

var identRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Connect opens a verified install-role connection (pgx DSN).
func Connect(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("admin: open: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("admin: ping: %w", err), db.Close())
	}
	return db, nil
}

func ident(name string) (string, error) {
	if !identRe.MatchString(name) {
		return "", fmt.Errorf("admin: unsafe identifier %q", name)
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`, nil
}

func literal(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

func exists(ctx context.Context, db *sql.DB, q, arg string) (bool, error) {
	var n int
	if err := db.QueryRowContext(ctx, q, arg).Scan(&n); err != nil {
		return false, fmt.Errorf("admin: exists %s: %w", arg, err)
	}
	return n > 0, nil
}

// RoleExists reports whether the role exists (roles are cluster-global).
func RoleExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	return exists(ctx, db, `SELECT count(*) FROM pg_roles WHERE rolname = $1`, name)
}

// EnsureRole creates the role with LOGIN and the given password if
// absent; an existing role is left untouched, password included.
func EnsureRole(ctx context.Context, db *sql.DB, name, password string) error {
	qname, err := ident(name)
	if err != nil {
		return err
	}
	ok, err := RoleExists(ctx, db, name)
	if err != nil || ok {
		return err
	}
	stmt := fmt.Sprintf(`CREATE ROLE %s LOGIN PASSWORD %s`, qname, literal(password))
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("admin: create role %s: %w", name, err)
	}
	return nil
}

// DBExists reports whether the database exists.
func DBExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	return exists(ctx, db, `SELECT count(*) FROM pg_database WHERE datname = $1`, name)
}

// EnsureDatabase creates the database owned by owner if absent.
func EnsureDatabase(ctx context.Context, db *sql.DB, name, owner string) error {
	qname, err := ident(name)
	if err != nil {
		return err
	}
	qowner, err := ident(owner)
	if err != nil {
		return err
	}
	ok, err := DBExists(ctx, db, name)
	if err != nil || ok {
		return err
	}
	_, err = db.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE %s OWNER %s`, qname, qowner))
	if err != nil {
		return fmt.Errorf("admin: create database %s: %w", name, err)
	}
	return nil
}

// EnsureSchema creates the schema owned by owner if absent.
func EnsureSchema(ctx context.Context, db *sql.DB, name, owner string) error {
	qname, err := ident(name)
	if err != nil {
		return err
	}
	qowner, err := ident(owner)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx,
		fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s AUTHORIZATION %s`, qname, qowner))
	if err != nil {
		return fmt.Errorf("admin: create schema %s: %w", name, err)
	}
	return nil
}

// GrantWatchdog gives watchdog_ro SELECT-only access to the fsm schema
// of the connected database, including tables the owner role creates
// later (owner decision 2026-11-22).
func GrantWatchdog(ctx context.Context, db *sql.DB, owner string) error {
	qowner, err := ident(owner)
	if err != nil {
		return err
	}
	stmts := []string{
		`GRANT USAGE ON SCHEMA fsm TO watchdog_ro`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA fsm TO watchdog_ro`,
		fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA fsm `+
			`GRANT SELECT ON TABLES TO watchdog_ro`, qowner),
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("admin: grant watchdog: %w", err)
		}
	}
	return nil
}

// EnsureInstallID stores the install id in hub.install_id on first run.
// A differing stored id is a refusal, not an overwrite (init must never
// rewrite install state).
func EnsureInstallID(ctx context.Context, db *sql.DB, id string) error {
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS hub`); err != nil {
		return fmt.Errorf("admin: hub schema: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS hub.install_id (id text PRIMARY KEY)`); err != nil {
		return fmt.Errorf("admin: install_id table: %w", err)
	}
	var stored string
	err := db.QueryRowContext(ctx, `SELECT id FROM hub.install_id LIMIT 1`).Scan(&stored)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := db.ExecContext(ctx,
			`INSERT INTO hub.install_id (id) VALUES ($1)`, id); err != nil {
			return fmt.Errorf("admin: store install id: %w", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("admin: read install id: %w", err)
	case stored != id:
		return fmt.Errorf("admin: install id mismatch: db has %q", stored)
	default:
		return nil
	}
}

// InstallID returns the stored id or "" when absent.
func InstallID(ctx context.Context, db *sql.DB) (string, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM information_schema.tables `+
			`WHERE table_schema='hub' AND table_name='install_id'`).Scan(&n)
	if err != nil {
		return "", fmt.Errorf("admin: install_id table check: %w", err)
	}
	if n == 0 {
		return "", nil
	}
	var id string
	err = db.QueryRowContext(ctx, `SELECT id FROM hub.install_id LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("admin: read install id: %w", err)
	}
	return id, nil
}
