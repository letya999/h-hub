package svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// spaceRow is the space domain record; state lives in fsm.machines.
type spaceRow struct {
	id       string
	owner    string
	name     string
	template string
	spec     []byte
	digest   string
}

func insertSpace(ctx context.Context, tx *sql.Tx, s spaceRow) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO spaces
		(id, owner, name, template, spec, digest) VALUES ($1,$2,$3,$4,$5,$6)`,
		s.id, s.owner, s.name, s.template, s.spec, s.digest)
	if err != nil {
		return fmt.Errorf("insert space: %w", err)
	}
	return nil
}

func getSpace(ctx context.Context, db *sql.DB, owner, id string) (spaceRow, error) {
	var s spaceRow
	err := db.QueryRowContext(ctx, `SELECT id, owner, name, template,
		spec, digest FROM spaces WHERE owner=$1 AND id=$2`,
		owner, id).Scan(&s.id, &s.owner, &s.name, &s.template, &s.spec, &s.digest)
	if errors.Is(err, sql.ErrNoRows) {
		return s, herrors.New(herrors.CodeNotFound, "err.not-found"). //nolint:wrapcheck // shared error form
										With("space", id)
	}
	if err != nil {
		return s, fmt.Errorf("get space: %w", err)
	}
	return s, nil
}

// spaceByName reads a space by its natural key; the zero row with an
// empty id means "no such space".
func spaceByName(ctx context.Context, db *sql.DB, owner, name string) (spaceRow, error) {
	var s spaceRow
	err := db.QueryRowContext(ctx, `SELECT id, owner, name, template,
		spec, digest FROM spaces WHERE owner=$1 AND name=$2`,
		owner, name).Scan(&s.id, &s.owner, &s.name, &s.template, &s.spec, &s.digest)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("space by name: %w", err)
	}
	return s, nil
}

// getSpaceAny reads a space without owner scoping (dispatch-internal).
func getSpaceAny(ctx context.Context, db *sql.DB, id string) (spaceRow, error) {
	var s spaceRow
	err := db.QueryRowContext(ctx, `SELECT id, owner, name, template,
		spec, digest FROM spaces WHERE id=$1`, id).Scan(
		&s.id, &s.owner, &s.name, &s.template, &s.spec, &s.digest)
	if errors.Is(err, sql.ErrNoRows) {
		return s, herrors.New(herrors.CodeNotFound, "err.not-found"). //nolint:wrapcheck // shared error form
										With("space", id)
	}
	if err != nil {
		return s, fmt.Errorf("get space: %w", err)
	}
	return s, nil
}

func specPassports(s spaceRow) (Passports, error) {
	var p Passports
	if err := json.Unmarshal(s.spec, &p); err != nil {
		return p, fmt.Errorf("spec decode: %w", err)
	}
	if p.SchemaVersion > 2 || p.SchemaVersion < 0 {
		return p, fmt.Errorf("spec schema version %d is unsupported", p.SchemaVersion)
	}
	return p, nil
}

// runtimeRow is the runtime domain record.
type runtimeRow struct {
	id        string
	space     string
	digest    string
	container string
	endpoint  string
}

func insertRuntime(ctx context.Context, tx *sql.Tx, r runtimeRow) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO runtimes
		(id, space, digest, container, endpoint) VALUES ($1,$2,$3,$4,$5)`,
		r.id, r.space, r.digest, r.container, r.endpoint)
	if err != nil {
		return fmt.Errorf("insert runtime: %w", err)
	}
	return nil
}

func getRuntime(ctx context.Context, db *sql.DB, id string) (runtimeRow, error) {
	var r runtimeRow
	err := db.QueryRowContext(ctx, `SELECT id, space, digest, container,
		endpoint FROM runtimes WHERE id=$1`, id).Scan(
		&r.id, &r.space, &r.digest, &r.container, &r.endpoint)
	if errors.Is(err, sql.ErrNoRows) {
		return r, herrors.New(herrors.CodeNotFound, "err.not-found"). //nolint:wrapcheck // shared error form
										With("runtime", id)
	}
	if err != nil {
		return r, fmt.Errorf("get runtime: %w", err)
	}
	return r, nil
}

// runtimeForSpace returns the space's latest runtime, "" when none.
func runtimeForSpace(ctx context.Context, tx *sql.Tx, space string) (runtimeRow, bool, error) {
	var r runtimeRow
	err := tx.QueryRowContext(ctx, `SELECT id, space, digest, container,
		endpoint FROM runtimes WHERE space=$1 ORDER BY created DESC
		LIMIT 1 FOR UPDATE`, space).Scan(
		&r.id, &r.space, &r.digest, &r.container, &r.endpoint)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, fmt.Errorf("runtime for space: %w", err)
	}
	return r, true, nil
}

func setContainer(ctx context.Context, tx *sql.Tx, runtime, container, endpoint string) error {
	_, err := tx.ExecContext(ctx, `UPDATE runtimes
		SET container=$2, endpoint=$3 WHERE id=$1`, runtime, container, endpoint)
	if err != nil {
		return fmt.Errorf("set container: %w", err)
	}
	return nil
}

// writeIntent records the launch intent inside the caller's
// transaction — the row exists before docker create runs (sys.worker-intent).
func writeIntent(ctx context.Context, tx *sql.Tx, i intentRow) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO intents
		(id, runtime, space, digest, image) VALUES ($1,$2,$3,$4,$5)`,
		i.id, i.runtime, i.space, i.digest, i.image)
	if err != nil {
		return fmt.Errorf("intent: %w", err)
	}
	return nil
}

type intentRow struct {
	id      string
	runtime string
	space   string
	digest  string
	image   string
}

// liveIntents lists intents whose runtime machine has not settled —
// used by tests and the reconcile pass.
func deleteSpace(ctx context.Context, tx *sql.Tx, id string) error {
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM intents WHERE space=$1", id); err != nil {
		return fmt.Errorf("delete intents: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM runtimes WHERE space=$1", id); err != nil {
		return fmt.Errorf("delete runtimes: %w", err)
	}
	for _, t := range []string{"layer_revisions", "effective_specs", "activations"} {
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM "+t+" WHERE space=$1", id); err != nil {
			return fmt.Errorf("delete %s: %w", t, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM spaces WHERE id=$1", id); err != nil {
		return fmt.Errorf("delete space: %w", err)
	}
	return nil
}
