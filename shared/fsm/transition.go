package fsm

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// ErrBadTransition reports an event with no edge from the current
// state. Forbidden edges write nothing.
var ErrBadTransition = herrors.New(herrors.CodeConflict, "err.machine-state")

// ErrStale reports a lost CAS race against the machine row.
var ErrStale = herrors.ErrStale

func badEdge(from, event string) error {
	return ErrBadTransition. //nolint:wrapcheck // shared error form
					With("from", from).With("event", event)
}

// Clock is the time source for deadlines and sweeps.
type Clock interface {
	Now() time.Time
}

// CreateReq describes a new machine row.
type CreateReq struct {
	ID      string
	Initial string
	Actor   string
}

// Create inserts a machine row in the caller's transaction and records
// creation in history. The initial state's deadline is armed when the
// spec declares one.
func (m *Machine) Create(ctx context.Context, tx *sql.Tx, clk Clock, r CreateReq) error {
	if !m.states[r.Initial] {
		return specErr("state", r.Initial, "unknown initial")
	}
	d := m.deadlineFor(r.Initial, clk.Now())
	_, err := tx.ExecContext(ctx, `INSERT INTO `+m.table("machines")+`
		(kind, id, state, deadline_at, deadline_event, deadline_actor, actor)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		m.kind, r.ID, r.Initial, d.at, d.event, d.actor, r.Actor)
	if err != nil {
		return fmt.Errorf("create machine %s/%s: %w", m.kind, r.ID, err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+m.table("history")+`
		(kind, id, from_state, event, to_state, actor)
		VALUES ($1,$2,'','create',$3,$4)`, m.kind, r.ID, r.Initial, r.Actor)
	if err != nil {
		return fmt.Errorf("create history: %w", err)
	}
	return nil
}

type armed struct {
	at    *time.Time
	event *string
	actor *string
}

func (m *Machine) deadlineFor(state string, now time.Time) armed {
	d, ok := m.deadlines[state]
	if !ok {
		return armed{}
	}
	at := now.Add(d.TTL)
	return armed{at: &at, event: &d.Event, actor: &d.Actor}
}

// Transition moves a machine through a declared edge inside the
// caller's transaction: CAS by version, history row, outbox effects,
// then the optional hook — all atomically.
func (m *Machine) Transition(ctx context.Context, tx *sql.Tx, r Request, hook Hook) error {
	state, version, err := m.readRow(ctx, tx, r.ID)
	if err != nil {
		return err
	}
	edge, ok := m.edges[edgeKey{state, r.Event}]
	if !ok {
		return badEdge(state, r.Event)
	}
	chg := Change{ID: r.ID, From: state, Event: r.Event, To: edge.To, Actor: r.Actor, Effects: r.Effects}
	now := r.At
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return m.commitTransition(ctx, tx, commit{version: version, chg: chg, hook: hook, at: now})
}

// readRow reads without a lock: the CAS update in commitTransition is
// the arbiter, so concurrent writers get a clean ErrStale rather than
// a serialized second write.
func (m *Machine) readRow(ctx context.Context, tx *sql.Tx, id string) (string, int64, error) {
	var state string
	var version int64
	err := tx.QueryRowContext(ctx,
		"SELECT state, version FROM "+m.table("machines")+" WHERE kind=$1 AND id=$2",
		m.kind, id).Scan(&state, &version)
	if err == sql.ErrNoRows {
		return "", 0, herrors.New(herrors.CodeNotFound, "err.not-found"). //nolint:wrapcheck // shared error form
											With("kind", m.kind).With("id", id)
	}
	if err != nil {
		return "", 0, fmt.Errorf("read machine: %w", err)
	}
	return state, version, nil
}

// commit bundles what one CAS write needs: the pre-read version, the
// change to commit, the optional same-tx hook and the deadline clock.
type commit struct {
	version int64
	chg     Change
	hook    Hook
	at      time.Time
}

func (m *Machine) commitTransition(ctx context.Context, tx *sql.Tx, c commit) error {
	d := m.deadlineFor(c.chg.To, c.at)
	res, err := tx.ExecContext(ctx, `UPDATE `+m.table("machines")+` SET
		state=$1, deadline_at=$2, deadline_event=$3, deadline_actor=$4,
		actor=$5, version=version+1, updated_at=now()
		WHERE kind=$6 AND id=$7 AND state=$8 AND version=$9`,
		c.chg.To, d.at, d.event, d.actor, c.chg.Actor, m.kind, c.chg.ID, c.chg.From, c.version)
	if err != nil {
		return fmt.Errorf("cas update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("cas rows: %w", err)
	}
	if n == 0 {
		return ErrStale.With("kind", m.kind).With("id", c.chg.ID) //nolint:wrapcheck // shared error form
	}
	if err := m.writeHistory(ctx, tx, c.chg); err != nil {
		return err
	}
	if err := m.writeEffects(ctx, tx, c.chg.ID, c.chg.Effects); err != nil {
		return err
	}
	if c.hook != nil {
		if err := c.hook(ctx, tx, c.chg); err != nil {
			return fmt.Errorf("hook: %w", err)
		}
	}
	return nil
}

func (m *Machine) writeHistory(ctx context.Context, tx *sql.Tx, chg Change) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO `+m.table("history")+`
		(kind, id, from_state, event, to_state, actor)
		VALUES ($1,$2,$3,$4,$5,$6)`, m.kind, chg.ID, chg.From, chg.Event, chg.To, chg.Actor)
	if err != nil {
		return fmt.Errorf("history: %w", err)
	}
	return nil
}

func (m *Machine) writeEffects(ctx context.Context, tx *sql.Tx, id string, fx []Effect) error {
	for _, f := range fx {
		_, err := tx.ExecContext(ctx, `INSERT INTO `+m.table("outbox")+`
			(kind, machine_id, topic, payload) VALUES ($1,$2,$3,$4)`,
			m.kind, id, f.Topic, f.Payload)
		if err != nil {
			return fmt.Errorf("outbox %q: %w", f.Topic, err)
		}
	}
	return nil
}
