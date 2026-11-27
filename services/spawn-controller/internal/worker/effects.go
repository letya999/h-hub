package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/letya999/h-hub/shared/fsm"
)

// Start applies a committed launch intent for one generation.
func (r *Registry) Start(ctx context.Context, effect fsm.OutEffect, launch func(context.Context, Record) error) error {
	return r.apply(ctx, effect, func(tx *sql.Tx, w Record) error {
		if w.State != starting || !w.currentAt(r.now()) {
			return nil
		}
		start, cancel := context.WithTimeout(ctx, w.deadline.Time.Sub(r.now()))
		defer cancel()
		if err := launch(start, w); err != nil {
			return fmt.Errorf("launch worker: %w", err)
		}
		if !w.currentAt(r.now()) {
			return nil
		}
		return r.transition(ctx, tx, w, "ready", leaseTTL)
	})
}

// Reap keeps retrying until the backend confirms that the generation is absent.
func (r *Registry) Reap(ctx context.Context, effect fsm.OutEffect,
	stop func(context.Context, Record) (bool, error)) error {
	var observation error
	err := r.apply(ctx, effect, func(tx *sql.Tx, w Record) error {
		if w.State != stopping && w.State != quarantined {
			return nil
		}
		absent, err := stop(ctx, w)
		observation = err
		if absent {
			return r.observeAbsent(ctx, tx, w)
		}
		if observation == nil {
			observation = errors.New("worker generation still present")
		}
		if w.State == stopping {
			return r.transition(ctx, tx, w, "quarantine", stopTTL)
		}
		return nil
	})
	return errors.Join(err, observation)
}

func (r *Registry) observeAbsent(ctx context.Context, tx *sql.Tx, w Record) error {
	req := fsm.Request{ID: w.ID, Event: "observe", Actor: "controller", At: r.now()}
	err := r.machine.Transition(ctx, tx, req, func(ctx context.Context, tx *sql.Tx, ch fsm.Change) error {
		if _, err := tx.ExecContext(ctx, "UPDATE intents SET worker_slot=NULL WHERE id=$1 AND kind='worker'", ch.ID); err != nil {
			return fmt.Errorf("release worker socket slot: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("worker absence: %w", err)
	}
	return nil
}

// Stop records the reason and schedules deletion after the transaction commits.
func (r *Registry) Stop(ctx context.Context, w Record, reason string) error {
	return r.withGeneration(ctx, w.ID, w.Generation,
		func(tx *sql.Tx, current Record) error {
			return r.stop(ctx, tx, current, reason)
		})
}

func (r *Registry) stop(ctx context.Context, tx *sql.Tx, w Record, reason string) error {
	switch w.State {
	case starting, leased, idle:
		req, err := effectRequest(w, "stop", reason, StopTopic)
		if err != nil {
			return err
		}
		req.At = r.now()
		if err := r.machine.Transition(ctx, tx, req, saveReason); err != nil {
			return fmt.Errorf("stop worker: %w", err)
		}
	}
	return nil
}

func saveReason(ctx context.Context, tx *sql.Tx, ch fsm.Change) error {
	if _, err := tx.ExecContext(ctx, "UPDATE intents SET stop_reason=$2 WHERE id=$1 AND kind='worker'",
		ch.ID, ch.Actor); err != nil {
		return fmt.Errorf("worker stop reason: %w", err)
	}
	return nil
}

func (r *Registry) apply(ctx context.Context, effect fsm.OutEffect, exec func(*sql.Tx, Record) error) error {
	var generation string
	if err := json.Unmarshal(effect.Payload, &generation); err != nil {
		return fmt.Errorf("read worker effect: %w", err)
	}
	return r.withGeneration(ctx, effect.MachineID, generation, exec)
}

func (r *Registry) withGeneration(ctx context.Context, key, generation string, exec func(*sql.Tx, Record) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin worker effect: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	w, err := lock(ctx, tx, key)
	if err != nil {
		return err
	}
	if w.Generation != generation {
		return nil
	}
	if err := exec(tx, w); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit worker effect: %w", err)
	}
	return nil
}
