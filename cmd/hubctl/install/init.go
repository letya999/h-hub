package install

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"path/filepath"

	"github.com/letya999/h-hub/internal/descriptor"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/store/admin"
)

const watchdogRole = "watchdog_ro"

// Init provisions an installation idempotently: config files are never
// overwritten, missing pieces are filled in (owner decision 2026-11-22).
func Init(ctx context.Context, cfg Config, out io.Writer) error {
	f, err := descriptor.Load(cfg.Descriptor)
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}
	if err := mkdir(cfg.ConfigDir); err != nil {
		return err
	}
	id, err := ensureID(cfg.ConfigDir)
	if err != nil {
		return err
	}
	if err := mkdir(cfg.KEKDir); err != nil {
		return err
	}
	if err := ensureHostKEK(cfg.ConfigDir, cfg.KEKDir); err != nil {
		return err
	}
	if err := provisionFiles(cfg.ConfigDir, cfg.KEKDir, f); err != nil {
		return err
	}
	if err := provisionDB(ctx, cfg, f, id); err != nil {
		return err
	}
	fmt.Fprintf(out, "install %s: %d services provisioned\n", id, len(f.Services)) //nolint:errcheck // report is best effort
	return nil
}

func provisionFiles(dir, kekDir string, f *descriptor.File) error {
	if err := mkdir(filepath.Join(dir, dirKeys)); err != nil {
		return err
	}
	if err := ensureKey(filepath.Join(dir, dirKeys), opKey); err != nil {
		return err
	}
	if err := mkdir(filepath.Join(dir, dirDB)); err != nil {
		return err
	}
	for _, s := range f.Services {
		if err := provisionServiceFiles(dir, kekDir, s); err != nil {
			return err
		}
	}
	return nil
}

func provisionServiceFiles(dir, kekDir string, s descriptor.Service) error {
	sd := filepath.Join(dir, dirSvc, s.Name)
	if err := mkdir(sd); err != nil {
		return err
	}
	if s.Name == "identity-hub" {
		if err := ensureIssuerKey(sd, kekDir); err != nil {
			return err
		}
	} else if err := ensureKey(sd, svcKey); err != nil {
		return err
	}
	if s.Database == "postgres" {
		if _, err := ensurePassword(filepath.Join(dir, dirDB), dbName(s.Name)); err != nil {
			return err
		}
	}
	if s.Database == "sqlcipher" {
		if err := ensureHexKey(filepath.Join(kekDir, dbKeyName(s.Name))); err != nil {
			return err
		}
	}
	return nil
}

func dsnFor(base, db string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("init: bad admin DSN: %w", err)
	}
	u.Path = "/" + db
	return u.String(), nil
}

func provisionDB(ctx context.Context, cfg Config, f *descriptor.File, id string) error {
	db, err := store.Connect(ctx, cfg.AdminDSN)
	if err != nil {
		return fmt.Errorf("init: connect: %w", err)
	}
	defer db.Close() //nolint:errcheck // best-effort close
	if _, err := ensurePassword(filepath.Join(cfg.ConfigDir, dirDB), watchdogRole); err != nil {
		return err
	}
	if err := ensureRoles(ctx, db, f, cfg.ConfigDir); err != nil {
		return err
	}
	for _, s := range f.DBServices() {
		if err := provisionServiceDB(ctx, cfg, db, s.Name, id); err != nil {
			return err
		}
	}
	return nil
}

func ensureRoles(ctx context.Context, db *sql.DB, f *descriptor.File, dir string) error {
	roles := []string{watchdogRole}
	for _, s := range f.DBServices() {
		roles = append(roles, dbName(s.Name))
	}
	for _, r := range roles {
		pw, err := ensurePassword(filepath.Join(dir, dirDB), r)
		if err != nil {
			return err
		}
		if err := admin.EnsureRole(ctx, db, r, pw); err != nil {
			return fmt.Errorf("init: role %s: %w", r, err)
		}
	}
	return nil
}

func provisionServiceDB(ctx context.Context, cfg Config, db *sql.DB, svc, id string) error {
	role := dbName(svc)
	if err := admin.EnsureDatabase(ctx, db, role, role); err != nil {
		return fmt.Errorf("init: database %s: %w", role, err)
	}
	dsn, err := dsnFor(cfg.AdminDSN, role)
	if err != nil {
		return err
	}
	sdb, err := store.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("init: connect %s: %w", role, err)
	}
	defer sdb.Close() //nolint:errcheck // best-effort close
	if err := admin.EnsureSchema(ctx, sdb, "fsm", role); err != nil {
		return fmt.Errorf("init: fsm schema %s: %w", role, err)
	}
	if err := admin.GrantWatchdog(ctx, sdb, role); err != nil {
		return fmt.Errorf("init: watchdog grants %s: %w", role, err)
	}
	if err := admin.EnsureInstallID(ctx, sdb, id); err != nil {
		return fmt.Errorf("init: install id %s: %w", role, err)
	}
	return nil
}
