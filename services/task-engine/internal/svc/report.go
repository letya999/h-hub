package svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
)

// reportRun appends journal events and settles the run/job when the
// runtime reports a final event. The run token's claims bind the
// report to one run.
func (rep *runReporter) reportRun(ctx context.Context, r tv1.ReportRunReq) (struct{}, error) {
	p := RunPrincipal(ctx)
	if p == nil {
		return struct{}{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	claims, err := idv1.ParseRunClaims(p.Payload)
	if err != nil || claims.Run != r.Run {
		return struct{}{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	// The report must name the audience this token was minted for.
	if !slices.Contains(p.Aud, rep.s.sub) {
		return struct{}{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	if _, err := getRun(ctx, rep.s.db, r.Run); err != nil {
		return struct{}{}, err
	}
	for _, e := range r.Events {
		if err := rep.applyEvent(ctx, r.Run, e); err != nil {
			return struct{}{}, err
		}
	}
	return struct{}{}, nil
}

// applyEvent writes one journal row and fires the machine events it
// implies, all inside one transaction.
func (rep *runReporter) applyEvent(ctx context.Context, runID string, e tv1.RunEvent) error {
	tx, err := rep.s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	inserted, err := appendEvent(ctx, tx, runID, e)
	if err != nil {
		return err
	}
	if err := rep.applyIfOpen(ctx, tx, runID, e, inserted); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (rep *runReporter) applyIfOpen(ctx context.Context, tx *sql.Tx, runID string, e tv1.RunEvent, inserted bool) error {
	if !inserted {
		return nil
	}
	state, err := lockedRunState(ctx, tx, runID)
	if err != nil {
		return err
	}
	if state == "settled" {
		return nil
	}
	return rep.eventMachines(ctx, tx, runID, e)
}

func lockedRunState(ctx context.Context, tx *sql.Tx, run string) (string, error) {
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM fsm.machines WHERE kind='run' AND id=$1 FOR UPDATE`, run).Scan(&state); err != nil {
		return "", fmt.Errorf("run state: %w", err)
	}
	return state, nil
}

// eventMachines maps journal kinds to machine events. A final event
// settles the run, frees the conversation and settles the job; the
// release effect rides the same transaction.
func (rep *runReporter) eventMachines(ctx context.Context, tx *sql.Tx, runID string,
	e tv1.RunEvent) error {
	switch e.Kind {
	case "start":
		err := rep.s.runM.Transition(ctx, tx, fsm.Request{
			ID: runID, Event: "stream-start", Actor: "runtime", At: rep.s.deps.Now()}, nil)
		if err != nil {
			return fmt.Errorf("stream-start: %w", err)
		}
		return rep.touchJob(ctx, tx, runID)
	case "owner-request":
		return rep.waitOwner(ctx, tx, runID, e)
	case "final-ok", "final-fail":
		return rep.settle(ctx, tx, runID, e)
	case "progress":
		// A heartbeat re-arms the run's stall deadline and the job's
		// running deadline; silence is what sweeps them.
		if err := rep.s.runM.Touch(ctx, tx, runID, rep.s.deps.Now()); err != nil {
			return fmt.Errorf("run touch: %w", err)
		}
		return rep.touchJob(ctx, tx, runID)
	default:
		return nil
	}
}

// touchJob re-arms the running deadline when the stream starts.
func (rep *runReporter) touchJob(ctx context.Context, tx *sql.Tx, runID string) error {
	job, err := jobOfRun(ctx, tx, runID)
	if err != nil {
		return err
	}
	if waiting, err := jobWaiting(ctx, tx, job); err != nil || waiting {
		return err // run heartbeats cannot extend a human's deadline
	}
	if err := rep.s.jobM.Touch(ctx, tx, job, rep.s.deps.Now()); err != nil {
		return fmt.Errorf("job touch: %w", err)
	}
	return nil
}

// waitEvent is the owner-request journal payload the runtime
// reports: the pending request's id and its deadline, both read from
// the tool result — the only place they cross a service boundary.
type waitEvent struct {
	Request  string `json:"request"`
	Deadline int64  `json:"deadline"`
}

// waitOwner binds the newest request and keeps the earlier job or request deadline.
// A job that already left running or waiting ignores a late report.
func (rep *runReporter) waitOwner(ctx context.Context, tx *sql.Tx, runID string, e tv1.RunEvent) error {
	var w waitEvent
	if err := json.Unmarshal([]byte(e.Data), &w); err != nil || w.Request == "" || w.Deadline <= 0 {
		return fmt.Errorf("owner-request event: bad payload")
	}
	job, err := jobOfRun(ctx, tx, runID)
	if err != nil {
		return err
	}
	var at time.Time
	if err := tx.QueryRowContext(ctx, `SELECT LEAST(deadline_at,$2) FROM fsm.machines
		WHERE kind='job' AND id=$1 FOR UPDATE`, job, time.Unix(w.Deadline, 0)).Scan(&at); err != nil {
		return fmt.Errorf("waiting deadline: %w", err)
	}
	hook := func(ctx context.Context, tx *sql.Tx, _ fsm.Change) error {
		if err := bindRequest(ctx, tx, job, w.Request); err != nil {
			return err
		}
		return armJobDeadline(ctx, tx, job, at)
	}
	err = rep.s.jobM.Transition(ctx, tx, fsm.Request{
		ID: job, Event: "wait-owner", Actor: "runtime", At: rep.s.deps.Now()}, hook)
	if errors.Is(err, fsm.ErrBadTransition) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("wait-owner: %w", err)
	}
	return nil
}

// settle commits run -> settled, releases its conversation claim,
// stores the result and settles the job; the runtime release rides
// outbox topic release-runtime. A job waiting on the owner stays
// waiting: the run's turn is over, the answer has not landed.
func (rep *runReporter) settle(ctx context.Context, tx *sql.Tx, runID string, e tv1.RunEvent) error {
	run, err := runForTx(ctx, tx, runID)
	if err != nil {
		return err
	}
	jobEvent := "settle-ok"
	if e.Kind == "final-fail" {
		jobEvent = "settle-fail"
	}
	release, err := json.Marshal(releaseEffect{Runtime: run.runtime})
	if err != nil {
		return fmt.Errorf("release effect: %w", err)
	}
	hook := func(ctx context.Context, tx *sql.Tx, ch fsm.Change) error {
		if err := releaseRun(ctx, tx, runID); err != nil {
			return err
		}
		if waiting, err := jobWaiting(ctx, tx, run.job); err != nil {
			return err
		} else if waiting {
			return nil
		}
		if err := setResult(ctx, tx, run.job, e.Data); err != nil {
			return err
		}
		if err := rep.s.jobM.Transition(ctx, tx, fsm.Request{
			ID: run.job, Event: jobEvent, Actor: "runtime", At: rep.s.deps.Now()}, nil); err != nil {
			return fmt.Errorf("job %s: %w", jobEvent, err)
		}
		return nil
	}
	if err := rep.s.runM.Transition(ctx, tx, fsm.Request{
		ID: runID, Event: "settle", Actor: "runtime", At: rep.s.deps.Now(),
		Effects: []fsm.Effect{{Topic: "release-runtime", Payload: release}}}, hook); err != nil {
		return fmt.Errorf("settle: %w", err)
	}
	return nil
}

// jobWaiting reports whether the job machine is in waiting_owner.
func jobWaiting(ctx context.Context, tx *sql.Tx, job string) (bool, error) {
	var state string
	if err := tx.QueryRowContext(ctx,
		"SELECT state FROM fsm.machines WHERE kind='job' AND id=$1 FOR UPDATE", job).Scan(&state); err != nil {
		return false, fmt.Errorf("job state: %w", err)
	}
	return state == "waiting_owner", nil
}

type releaseEffect struct {
	Runtime string `json:"runtime"`
}

// jobOfRun resolves a run to its job id.
func jobOfRun(ctx context.Context, tx *sql.Tx, run string) (string, error) {
	var job string
	err := tx.QueryRowContext(ctx,
		"SELECT job FROM runs WHERE id=$1", run).Scan(&job)
	if err != nil {
		return "", fmt.Errorf("job of run: %w", err)
	}
	return job, nil
}

// runForTx reads the run row inside a transaction.
func runForTx(ctx context.Context, tx *sql.Tx, id string) (runRow, error) {
	var r runRow
	err := tx.QueryRowContext(ctx, `SELECT id, job, owner, space, conversation,
		runtime, endpoint, digest FROM runs WHERE id=$1`, id).Scan(
		&r.id, &r.job, &r.owner, &r.space, &r.conv, &r.runtime, &r.endpoint, &r.digest)
	if err != nil {
		return r, fmt.Errorf("run: %w", err)
	}
	return r, nil
}
