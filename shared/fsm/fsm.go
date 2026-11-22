// Package fsm is the one state-machine mechanism: CAS by version on
// fsm.machines, history and outbox effects in the same transaction,
// deadlines on every non-final state, and a due-row sweeper that
// re-verifies rows inside its write transaction.
package fsm

import (
	"context"
	"database/sql"
	"regexp"
	"time"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// identRe bounds schema names to safe SQL identifiers: they interpolate
// into qualified table names and DDL templates.
var identRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Edge is one allowed transition.
type Edge struct {
	From  string
	Event string
	To    string
}

// Deadline describes how a non-final state expires: the sweep event to
// fire and the actor recorded in history.
type Deadline struct {
	Event string
	Actor string
	TTL   time.Duration
}

// Spec is the complete machine declaration.
type Spec struct {
	Kind      string
	Schema    string // table schema; empty means "fsm"
	Final     []string
	LongLived []string
	Edges     []Edge
	Deadlines map[string]Deadline
}

type edgeKey struct {
	from  string
	event string
}

// Machine is a validated machine definition.
type Machine struct {
	kind      string
	schema    string
	final     map[string]bool
	longLived map[string]bool
	edges     map[edgeKey]Edge
	deadlines map[string]Deadline
	states    map[string]bool
}

// New validates a spec: unique edges, known targets, no edges out of
// final states, and a valid deadline edge for every non-final,
// non-long-lived state.
func New(s Spec) (*Machine, error) {
	m := &Machine{
		kind:      s.Kind,
		schema:    s.Schema,
		final:     set(s.Final),
		longLived: set(s.LongLived),
		edges:     map[edgeKey]Edge{},
		deadlines: s.Deadlines,
		states:    map[string]bool{},
	}
	if m.kind == "" {
		return nil, specErr("field", "kind", "empty")
	}
	if m.schema == "" {
		m.schema = "fsm"
	}
	if err := checkSchema(m.schema); err != nil {
		return nil, err
	}
	collectStates(m, s)
	for _, e := range s.Edges {
		if err := m.checkEdge(e); err != nil {
			return nil, err
		}
		m.edges[edgeKey{e.From, e.Event}] = e
	}
	for st, d := range m.deadlines {
		if err := m.checkDeadline(st, d); err != nil {
			return nil, err
		}
	}
	for st := range m.states {
		if m.final[st] || m.longLived[st] {
			continue
		}
		if _, ok := m.deadlines[st]; !ok {
			return nil, specErr("state", st, "no deadline")
		}
	}
	return m, nil
}

func set(ss []string) map[string]bool {
	out := make(map[string]bool, len(ss))
	for _, s := range ss {
		out[s] = true
	}
	return out
}

func collectStates(m *Machine, s Spec) {
	for st := range s.Deadlines {
		m.states[st] = true
	}
	for _, e := range s.Edges {
		m.states[e.From] = true
		m.states[e.To] = true
	}
	for st := range m.final {
		m.states[st] = true
	}
	for st := range m.longLived {
		m.states[st] = true
	}
}

func (m *Machine) checkEdge(e Edge) error {
	err := herrors.Require(e.Event != "" && e.From != "" && e.To != "", "err.invalid-arg")
	if err != nil {
		return err //nolint:wrapcheck // shared error form
	}
	if m.final[e.From] {
		return specErr("from", e.From, "edge out of final")
	}
	k := edgeKey{e.From, e.Event}
	if _, dup := m.edges[k]; dup {
		return specErr("event", e.Event, "duplicate edge")
	}
	return nil
}

func (m *Machine) checkDeadline(st string, d Deadline) error {
	if m.final[st] || m.longLived[st] {
		return specErr("state", st, "deadline on final/long-lived")
	}
	if d.TTL <= 0 || d.Event == "" || d.Actor == "" {
		return specErr("state", st, "bad deadline spec")
	}
	if _, ok := m.edges[edgeKey{st, d.Event}]; !ok {
		return specErr("event", d.Event, "deadline has no edge")
	}
	return nil
}

// specErr builds a spec-validation error in the shared form. Details
// carry the offending field and the reason.
func specErr(key, val, reason string) error {
	return herrors.New(herrors.CodeInvalid, "err.machine-state"). //nolint:wrapcheck // shared error form
									With(key, val).With("reason", reason)
}

// Kind returns the machine kind.
func (m *Machine) Kind() string { return m.kind }

// Schema returns the schema holding this machine's tables.
func (m *Machine) Schema() string { return m.schema }

// table qualifies a table name with the machine's schema.
func (m *Machine) table(name string) string { return m.schema + "." + name }

// checkSchema validates a schema name for SQL interpolation.
func checkSchema(name string) error {
	if !identRe.MatchString(name) {
		return specErr("field", "schema", "unsafe identifier")
	}
	return nil
}

// Request is one transition attempt: the edge event, the acting
// identity, and the outbox effects to commit with it. At overrides the
// clock used to arm the next deadline (zero means the wall clock).
type Request struct {
	ID      string
	Event   string
	Actor   string
	At      time.Time
	Effects []Effect
}

// Change describes a committed transition for hooks.
type Change struct {
	ID      string
	From    string
	Event   string
	To      string
	Actor   string
	Effects []Effect
}

// Effect is an outbox intent committed with the transition.
type Effect struct {
	Topic   string
	Payload []byte
}

// Hook runs inside the transition transaction for same-tx side effects.
type Hook func(ctx context.Context, tx *sql.Tx, ch Change) error
