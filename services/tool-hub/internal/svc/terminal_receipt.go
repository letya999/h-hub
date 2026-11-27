package svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	wv1 "github.com/letya999/h-hub/contracts/worker/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/log"
)

var terminalMachine = newTerminalMachine()

func newTerminalMachine() *fsm.Machine {
	m, err := fsm.New(fsm.Spec{
		Kind: "terminal-call", Final: []string{"finished", "uncertain"},
		Edges: []fsm.Edge{
			{From: "prepared", Event: "dispatch", To: "executing"},
			{From: "prepared", Event: "expire", To: "uncertain"},
			{From: "executing", Event: "finish", To: "finished"},
			{From: "executing", Event: "expire", To: "uncertain"},
		},
		Deadlines: map[string]fsm.Deadline{
			"prepared":  {Event: "expire", Actor: "sweep", TTL: 5 * time.Second},
			"executing": {Event: "expire", Actor: "sweep", TTL: wv1.MaxDuration + 10*time.Second},
		},
	})
	if err != nil {
		panic("terminal call machine: " + err.Error())
	}
	return m
}

type terminalClock struct{ now func() time.Time }

func (c terminalClock) Now() time.Time { return c.now() }

func terminalResult(s *Server, ctx context.Context, call terminalCall) (json.RawMessage, bool, error) {
	var raw []byte
	var digest, state string
	err := s.db.QueryRowContext(ctx, `SELECT t.digest, t.result, m.state FROM terminal_calls t
		JOIN fsm.machines m ON m.id=t.id AND m.kind='terminal-call' WHERE t.id=$1`,
		call.id).Scan(&digest, &raw, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read terminal receipt: %w", err)
	}
	if digest != call.digest {
		return nil, true, herrors.New(herrors.CodeConflict, "err.stale")
	}
	if state != "finished" || len(raw) == 0 {
		return nil, true, terminalUncertain(nil)
	}
	return raw, true, nil
}

func terminalReplay(s *Server, ctx context.Context, call terminalCall) (json.RawMessage, error) {
	raw, found, err := terminalResult(s, ctx, call)
	if !found && err == nil {
		return nil, terminalUncertain(nil)
	}
	return raw, err
}

func beginTerminal(s *Server, ctx context.Context, call terminalCall) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin terminal dispatch: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO terminal_calls (id,digest) VALUES ($1,$2)
		ON CONFLICT (id) DO NOTHING RETURNING id`, call.id, call.digest).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("insert terminal dispatch: %w", err)
	}
	if err := recordTerminalAttempt(s, ctx, tx, id); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit terminal dispatch: %w", err)
	}
	return true, nil
}

func recordTerminalAttempt(s *Server, ctx context.Context, tx *sql.Tx, id string) error {
	if err := terminalMachine.Create(ctx, tx, terminalClock{s.deps.Now},
		fsm.CreateReq{ID: id, Initial: "prepared", Actor: "invoke"}); err != nil {
		return fmt.Errorf("create terminal call: %w", err)
	}
	req := fsm.Request{ID: id, Event: "dispatch", Actor: "invoke", At: s.deps.Now(),
		Effects: []fsm.Effect{{Topic: "execute-terminal-once", Payload: []byte("{}")}}}
	// Claim the delivery in this transaction. A lost response never requeues the command.
	if err := terminalMachine.Transition(ctx, tx, req, func(ctx context.Context, tx *sql.Tx, ch fsm.Change) error {
		if _, err := tx.ExecContext(ctx, `UPDATE fsm.outbox SET done_at=now()
			WHERE kind='terminal-call' AND machine_id=$1 AND topic='execute-terminal-once'`, ch.ID); err != nil {
			return fmt.Errorf("claim terminal delivery: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("arm terminal execution: %w", err)
	}
	return nil
}

func finishTerminal(s *Server, ctx context.Context, call terminalCall, result json.RawMessage, runErr error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin terminal settlement: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	event := "finish"
	if herrors.CodeOf(runErr) == herrors.CodeUncertain {
		event = "expire"
	}
	req := fsm.Request{ID: call.id, Event: event, Actor: "invoke", At: s.deps.Now()}
	if err := terminalMachine.Transition(ctx, tx, req, func(ctx context.Context, tx *sql.Tx, ch fsm.Change) error {
		if _, err := tx.ExecContext(ctx, "UPDATE terminal_calls SET result=$2 WHERE id=$1", ch.ID, result); err != nil {
			return fmt.Errorf("store terminal result: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("settle terminal state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit terminal settlement: %w", err)
	}
	return nil
}

func sweepTerminals(s *Server, ctx context.Context, l *log.L) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if _, err := terminalMachine.Sweep(ctx, s.db, s.deps.Now(), 32, nil); err != nil {
				l.Error(ctx, "terminal call sweep", err)
			}
		}
	}
}
