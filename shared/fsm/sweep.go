package fsm

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"
	"time"
)

// DueRow is the narrow projection a sweeper reads. It carries only
// what re-verification needs.
type DueRow struct {
	ID            string
	State         string
	DeadlineAt    time.Time
	DeadlineEvent string
	DeadlineActor string
}

// DueRows lists machines past their deadline. Long-lived and final
// states never appear: they have no deadline armed.
func (m *Machine) DueRows(ctx context.Context, db *sql.DB, now time.Time, limit int) ([]DueRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, state, deadline_at, deadline_event, deadline_actor
		FROM `+m.table("machines")+`
		WHERE kind=$1 AND deadline_at IS NOT NULL AND deadline_at <= $2
		ORDER BY deadline_at LIMIT $3`, m.kind, now, limit)
	if err != nil {
		return nil, fmt.Errorf("due rows: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close
	var out []DueRow
	for rows.Next() {
		var r DueRow
		var ev, ac sql.NullString
		if err := rows.Scan(&r.ID, &r.State, &r.DeadlineAt, &ev, &ac); err != nil {
			return nil, fmt.Errorf("scan due: %w", err)
		}
		r.DeadlineEvent, r.DeadlineActor = ev.String, ac.String
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("due rows: %w", err)
	}
	return out, nil
}

// SweepRow re-reads one row inside its own write transaction and fires
// the deadline event only when the row is still due. A moved-on row is
// skipped cleanly so real actors win over stale sweepers.
func (m *Machine) SweepRow(ctx context.Context, db *sql.DB, id string, now time.Time, hook Hook) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("sweep begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	var state, event, actor string
	var at sql.NullTime
	var ev, ac sql.NullString
	var version int64
	err = tx.QueryRowContext(ctx, `SELECT state, deadline_at, deadline_event,
		deadline_actor, version FROM `+m.table("machines")+`
		WHERE kind=$1 AND id=$2`, m.kind, id).
		Scan(&state, &at, &ev, &ac, &version)
	if err != nil {
		return false, fmt.Errorf("sweep reread: %w", err)
	}
	if !at.Valid || at.Time.After(now) {
		return false, nil
	}
	event, actor = ev.String, ac.String
	edge, ok := m.edges[edgeKey{state, event}]
	if !ok {
		return false, badEdge(state, event)
	}
	chg := Change{ID: id, From: state, Event: event, To: edge.To, Actor: actor}
	err = m.commitTransition(ctx, tx, commit{version: version, chg: chg, hook: hook, at: now})
	if stderrors.Is(err, ErrStale) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("sweep commit: %w", err)
	}
	return true, nil
}

// Sweep processes up to limit due rows, each in its own transaction.
func (m *Machine) Sweep(ctx context.Context, db *sql.DB, now time.Time, limit int, hook Hook) (int, error) {
	rows, err := m.DueRows(ctx, db, now, limit)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		done, err := m.SweepRow(ctx, db, r.ID, now, hook)
		if err != nil {
			return n, err
		}
		if done {
			n++
		}
	}
	return n, nil
}

// LeaseReq is a lease acquisition attempt.
type LeaseReq struct {
	Name  string
	Owner string
	TTL   time.Duration
	Now   time.Time
}

// AcquireLease takes or renews a named sweeper lease in the given
// schema. The write is one atomic upsert so exactly one owner holds an
// unexpired lease — the single-sweeper model of v1.0 (one sweeper per
// service, lease keyed by sweeper name, no sharding).
func AcquireLease(ctx context.Context, db *sql.DB, schema string, r LeaseReq) (bool, error) {
	if err := checkSchema(schema); err != nil {
		return false, err
	}
	var got string
	tbl := schema + ".sweeper_leases"
	err := db.QueryRowContext(ctx, `INSERT INTO `+tbl+` (name, owner, expires_at)
		VALUES ($1,$2,$3)
		ON CONFLICT (name) DO UPDATE
		SET owner=excluded.owner, expires_at=excluded.expires_at
		WHERE `+tbl+`.expires_at <= $4
		   OR `+tbl+`.owner = excluded.owner
		RETURNING owner`, r.Name, r.Owner, r.Now.Add(r.TTL), r.Now).Scan(&got)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lease %s: %w", r.Name, err)
	}
	return got == r.Owner, nil
}

// ReleaseLease drops a lease only when the caller still owns it.
func ReleaseLease(ctx context.Context, db *sql.DB, schema, name, owner string) error {
	if err := checkSchema(schema); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx,
		"DELETE FROM "+schema+".sweeper_leases WHERE name=$1 AND owner=$2", name, owner)
	if err != nil {
		return fmt.Errorf("release lease %s: %w", name, err)
	}
	return nil
}
