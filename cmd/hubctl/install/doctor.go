package install

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/letya999/h-hub/internal/descriptor"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/store/admin"
)

// Doctor verifies an installation without writing (owner decision
// 2026-11-22): descriptor, files and permissions, databases and roles,
// install id. Every failure is reported; exit is non-zero on any.
func Doctor(ctx context.Context, cfg Config, out io.Writer) error {
	var fails int
	ok := func(name string, err error) {
		if err != nil {
			fails++
			fmt.Fprintf(out, "FAIL %s: %v\n", name, err) //nolint:errcheck // report is best effort
			return
		}
		fmt.Fprintf(out, "ok   %s\n", name) //nolint:errcheck // report is best effort
	}

	f, derr := descriptor.Load(cfg.Descriptor)
	ok("descriptor", derr)
	id, err := os.ReadFile(filepath.Join(cfg.ConfigDir, fileID))
	ok("install-id file", err)
	if err == nil && f != nil {
		ok("config files", checkFiles(cfg.ConfigDir, f))
	}
	if err != nil || f == nil {
		return fmt.Errorf("doctor: %d check(s) failed", fails)
	}
	db, err := store.Connect(ctx, cfg.AdminDSN)
	ok("postgres reachable", err)
	if err != nil {
		return fmt.Errorf("doctor: %d check(s) failed", fails)
	}
	defer db.Close() //nolint:errcheck // best-effort close
	checkDBs(ctx, db, f, scope{cfg: cfg, id: string(id), ok: ok})
	if fails > 0 {
		return fmt.Errorf("doctor: %d check(s) failed", fails)
	}
	return nil
}

// scope bundles the per-run values passed to check helpers.
type scope struct {
	cfg Config
	id  string
	ok  func(string, error)
}

// checkDBs verifies roles, databases and service schemas.
func checkDBs(ctx context.Context, db *sql.DB, f *descriptor.File, sc scope) {
	sc.ok("watchdog_ro role", exists(ctx, db, watchdogRole, admin.RoleExists))
	for _, s := range f.Services {
		role := dbName(s.Name)
		if s.Database != "postgres" {
			has, err := admin.DBExists(ctx, db, role)
			if err == nil && has {
				err = fmt.Errorf("unexpected database %s", role)
			}
			sc.ok(s.Name+" has no database", err)
			continue
		}
		sc.ok(role+" role", exists(ctx, db, role, admin.RoleExists))
		sc.ok(role+" database", exists(ctx, db, role, admin.DBExists))
		checkServiceDB(ctx, sc, s.Name)
	}
}

func exists(ctx context.Context, db *sql.DB, name string,
	f func(context.Context, *sql.DB, string) (bool, error)) error {
	ok, err := f(ctx, db, name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s is missing", name)
	}
	return nil
}

func statIn(dir, rel string) error {
	_, err := os.Stat(filepath.Join(dir, rel))
	if err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	return nil
}

func secretIn(dir, rel string) error {
	st, err := os.Stat(filepath.Join(dir, rel))
	if err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s: permissions %o, want owner-only", rel, st.Mode().Perm())
	}
	return nil
}

// checkFiles verifies key and secret files exist with owner-only perms.
func checkFiles(dir string, f *descriptor.File) error {
	secret := func(rel string) error { return secretIn(dir, rel) }
	plain := func(rel string) error { return statIn(dir, rel) }
	files := map[string]func(string) error{
		fileKEK:                              secret,
		filepath.Join(dirKeys, opKey+".key"): secret,
		filepath.Join(dirKeys, opKey+".pub"): plain,
	}
	for _, s := range f.Services {
		p := filepath.Join(dirSvc, s.Name, svcKey)
		files[p+".key"] = secret
		files[p+".pub"] = plain
		if s.Database == "postgres" {
			files[filepath.Join(dirDB, dbName(s.Name)+dbSuffix)] = secret
		}
	}
	files[filepath.Join(dirDB, watchdogRole+dbSuffix)] = secret
	for rel, chk := range files {
		if err := chk(rel); err != nil {
			return err
		}
	}
	return nil
}

// checkServiceDB verifies one service database: fsm schema, install id,
// and watchdog_ro privileges limited to SELECT on fsm.
func checkServiceDB(ctx context.Context, sc scope, svc string) {
	role := dbName(svc)
	dsn, err := dsnFor(sc.cfg.AdminDSN, role)
	if err != nil {
		sc.ok(role+" database", err)
		return
	}
	sdb, err := store.Connect(ctx, dsn)
	if err != nil {
		sc.ok(role+" database", err)
		return
	}
	defer sdb.Close() //nolint:errcheck // best-effort close
	var n int
	err = sdb.QueryRowContext(ctx,
		`SELECT count(*) FROM information_schema.schemata WHERE schema_name='fsm'`).Scan(&n)
	if err == nil && n == 0 {
		err = fmt.Errorf("fsm schema is missing")
	}
	sc.ok(role+" fsm schema", err)
	got, err := admin.InstallID(ctx, sdb)
	if err == nil && got != sc.id {
		err = fmt.Errorf("install id %q, want %q", got, sc.id)
	}
	sc.ok(role+" install id", err)
	sc.ok(role+" watchdog_ro grants", watchdogGrants(ctx, sdb))
}

func watchdogGrants(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx,
		`SELECT table_schema, privilege_type FROM information_schema.role_table_grants `+
			`WHERE grantee = $1`, watchdogRole)
	if err != nil {
		return fmt.Errorf("doctor: grants query: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close
	for rows.Next() {
		var sch, priv string
		if err := rows.Scan(&sch, &priv); err != nil {
			return fmt.Errorf("doctor: grant row: %w", err)
		}
		if sch != "fsm" || priv != "SELECT" {
			return fmt.Errorf("watchdog_ro has %s on %s (want SELECT on fsm only)", priv, sch)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("doctor: grants: %w", err)
	}
	var usage bool
	err = db.QueryRowContext(ctx,
		`SELECT has_schema_privilege($1, 'fsm', 'USAGE')`, watchdogRole).Scan(&usage)
	if err != nil {
		return fmt.Errorf("doctor: schema privilege: %w", err)
	}
	if !usage {
		return fmt.Errorf("watchdog_ro lacks USAGE on fsm")
	}
	return nil
}
