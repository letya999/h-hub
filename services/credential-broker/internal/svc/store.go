package svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// credRef builds the only reference form the builtin store owns
// (brief): cred://builtin/platform/<name>. Names are operator-typed,
// so the charset is deliberately narrow.
var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func credRef(name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", herrors.New(herrors.CodeInvalid, "err.invalid-arg")
	}
	return "cred://builtin/platform/" + name, nil
}

func refName(ref string) (string, error) {
	const pre = "cred://builtin/platform/"
	if !strings.HasPrefix(ref, pre) {
		return "", herrors.New(herrors.CodeInvalid, "err.invalid-arg")
	}
	return ref[len(pre):], nil
}

// The credential-type registry is code-owned from day one (brief).
// api-key stores one "value" field; oauth and file are registered so
// references resolve, their field sets land with their writers.
var credTypes = map[string][]string{
	"api-key": {"value"},
	"oauth":   {"access_token", "refresh_token", "expires_at"},
	"file":    {"path"},
}

func validType(t string) bool { _, ok := credTypes[t]; return ok }

// credVersion is the newest non-deleted sealed blob of a credential.
type credVersion struct {
	v    int
	blob []byte
	kid  string
}

func currentVersion(ctx context.Context, tx *sql.Tx, ref string) (credVersion, error) {
	var out credVersion
	err := tx.QueryRowContext(ctx,
		`SELECT version, sealed, kid FROM versions
		 WHERE ref=$1 AND deleted_at IS NULL
		 ORDER BY version DESC LIMIT 1`, ref).Scan(&out.v, &out.blob, &out.kid)
	if errors.Is(err, sql.ErrNoRows) {
		return out, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	if err != nil {
		return out, fmt.Errorf("version read: %w", err)
	}
	return out, nil
}

// putReq carries one new sealed version into store.
type putReq struct {
	ref  string
	name string
	typ  string
	blob []byte
	kid  string
}

// putVersion upserts the credential row and inserts a new sealed
// version in the same transaction.
func putVersion(ctx context.Context, tx *sql.Tx, p putReq) (int, error) {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO credentials(ref, name, type) VALUES ($1, $2, $3)
		 ON CONFLICT(ref) DO NOTHING`, p.ref, p.name, p.typ); err != nil {
		return 0, fmt.Errorf("credential row: %w", err)
	}
	var v int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) + 1 FROM versions WHERE ref=$1`,
		p.ref).Scan(&v); err != nil {
		return 0, fmt.Errorf("version next: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO versions(ref, version, sealed, kid) VALUES ($1, $2, $3, $4)`,
		p.ref, v, p.blob, p.kid); err != nil {
		return 0, fmt.Errorf("version write: %w", err)
	}
	return v, nil
}

type auditEvent struct {
	actor, event, ref, detail string
}

func audit(ctx context.Context, tx *sql.Tx, e auditEvent) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_events(actor, event, ref, detail) VALUES ($1, $2, $3, $4)`,
		e.actor, e.event, e.ref, e.detail); err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	return nil
}

func listConnections(ctx context.Context, db *sql.DB) ([]connRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT c.ref, c.name, c.type,
		        (SELECT COALESCE(MAX(v.version), 0) FROM versions v
		         WHERE v.ref = c.ref AND v.deleted_at IS NULL) AS ver
		 FROM credentials c ORDER BY c.ref`)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close
	var out []connRow
	for rows.Next() {
		var r connRow
		if err := rows.Scan(&r.ref, &r.name, &r.typ, &r.version); err != nil {
			return nil, fmt.Errorf("list scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list rows: %w", err)
	}
	return out, nil
}

type connRow struct {
	ref, name, typ string
	version        int
}
