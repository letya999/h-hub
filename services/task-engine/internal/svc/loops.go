package svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	hv1 "github.com/letya999/h-hub/contracts/harness/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/log"
)

const (
	dispatchEvery = 250 * time.Millisecond
	sweepEvery    = time.Second
	outboxEvery   = 250 * time.Millisecond
	tickEvery     = 5 * time.Second
	leaseTTL      = 10 * time.Second
	tickName      = "task-engine"
)

// Loops runs the dispatcher, sweeps, outbox executor and watchdog
// ticks until ctx ends. Each iteration is one pass, never a sleep on
// work — timers pace polling only.
func (lp *loopRunner) Loops(ctx context.Context, l *log.L) {
	t := time.NewTicker(dispatchEvery)
	defer t.Stop()
	swp := time.NewTicker(sweepEvery)
	defer swp.Stop()
	out := time.NewTicker(outboxEvery)
	defer out.Stop()
	tick := time.NewTicker(tickEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			lp.dispatchPass(ctx, l)
		case <-swp.C:
			lp.sweepPass(ctx, l)
		case <-out.C:
			lp.outboxPass(ctx, l)
		case <-tick.C:
			if err := fsm.WriteTick(ctx, lp.s.db, "fsm", tickName, lp.s.deps.Now()); err != nil {
				l.Error(ctx, "watchdog tick", err)
			}
		}
	}
}

// dispatchPass claims and dispatches due jobs. Dispatch of each claim
// runs inline: the admitted deadline is the backstop against hangs.
func (lp *loopRunner) dispatchPass(ctx context.Context, l *log.L) {
	for {
		id, err := lp.s.disp.claim(ctx)
		if errors.Is(err, errNoClaim) {
			return
		}
		if err != nil {
			l.Error(ctx, "claim", err)
			return
		}
		lp.s.disp.dispatch(ctx, l, id)
	}
}

// sweepPass fires due deadlines on both machines under a lease, so a
// restarted stack has exactly one active sweeper.
func (lp *loopRunner) sweepPass(ctx context.Context, l *log.L) {
	ok, err := fsm.AcquireLease(ctx, lp.s.db, "fsm", fsm.LeaseReq{
		Name: "task-engine-sweep", Owner: lp.s.sub, TTL: leaseTTL, Now: lp.s.deps.Now()})
	if err != nil || !ok {
		if err != nil {
			l.Error(ctx, "sweep lease", err)
		}
		return
	}
	if err := lp.alertSweepLag(ctx, lp.s.deps.Now()); err != nil {
		l.Error(ctx, "sweep lag alert", err)
	}
	if _, err := lp.s.jobM.Sweep(ctx, lp.s.db, lp.s.deps.Now(), 32, lp.jobSweepHook); err != nil {
		l.Error(ctx, "job sweep", err)
	}
	if _, err := lp.s.runM.Sweep(ctx, lp.s.db, lp.s.deps.Now(), 32, lp.runSweepHook); err != nil {
		l.Error(ctx, "run sweep", err)
	}
}

// jobSweepHook reacts to deadline-driven job transitions: a run still
// bound to the conversation is released with the transition.
func (lp *loopRunner) jobSweepHook(ctx context.Context, tx *sql.Tx, ch fsm.Change) error {
	run, err := activeRunForJob(ctx, tx, ch.ID)
	if err != nil || run == "" {
		return err
	}
	var runtime string
	if err := tx.QueryRowContext(ctx,
		"SELECT runtime FROM runs WHERE id=$1", run).Scan(&runtime); err != nil {
		return fmt.Errorf("run runtime: %w", err)
	}
	release, err := json.Marshal(releaseEffect{Runtime: runtime})
	if err != nil {
		return fmt.Errorf("release effect: %w", err)
	}
	return appendOutbox(ctx, tx, "release-runtime", release)
}

// runSweepHook handles a run that timed out: the bound job becomes
// uncertain (stream never started or never finished).
func (lp *loopRunner) runSweepHook(ctx context.Context, tx *sql.Tx, ch fsm.Change) error {
	if ch.Event != "stream-timeout" {
		return nil
	}
	job, err := jobOfRun(ctx, tx, ch.ID)
	if err != nil {
		return err
	}
	if err := releaseRun(ctx, tx, ch.ID); err != nil {
		return err
	}
	err = lp.s.jobM.Transition(ctx, tx, fsm.Request{
		ID: job, Event: "lost", Actor: "sweep", At: lp.s.deps.Now()}, nil)
	if errors.Is(err, fsm.ErrBadTransition) {
		return nil // job already settled/cancelled
	}
	if err != nil {
		return fmt.Errorf("lost: %w", err)
	}
	return nil
}

// outboxPass executes due effects. cancel-run calls the runtime then
// settles the run; release-runtime frees the runtime claim.
func (lp *loopRunner) outboxPass(ctx context.Context, l *log.L) {
	fx, err := fsm.DueEffects(ctx, lp.s.db, "fsm", lp.s.deps.Now(), 32)
	if err != nil {
		l.Error(ctx, "outbox read", err)
		return
	}
	for _, f := range fx {
		if err := lp.execEffect(ctx, f); err != nil {
			l.Error(ctx, "effect "+f.Topic, err)
			if err := fsm.DeferEffect(ctx, lp.s.db, "fsm", f.Seq,
				lp.s.deps.Now().Add(5*time.Second)); err != nil {
				l.Error(ctx, "effect defer", err)
			}
			continue
		}
		if err := fsm.MarkDone(ctx, lp.s.db, "fsm", f.Seq); err != nil {
			l.Error(ctx, "effect done", err)
		}
	}
}

func (lp *loopRunner) execEffect(ctx context.Context, f fsm.OutEffect) error {
	switch f.Topic {
	case "cancel-run":
		return lp.execCancelRun(ctx, string(f.Payload))
	case "release-runtime":
		var e releaseEffect
		if err := json.Unmarshal(f.Payload, &e); err != nil {
			return fmt.Errorf("release payload: %w", err)
		}
		sc, err := lp.s.spawnClient(ctx)
		if err != nil {
			return err
		}
		_, err = sc.ReleaseRuntime(ctx, sv1.ReleaseRuntimeReq{Runtime: e.Runtime})
		if err != nil {
			return fmt.Errorf("release runtime: %w", err)
		}
		return nil
	case "operator-alert":
		return lp.deliverOperatorAlert(ctx, f.Payload)
	default:
		return fmt.Errorf("unknown topic %q", f.Topic)
	}
}

// execCancelRun asks the runtime to abort, then settles the run and
// frees the conversation.
func (lp *loopRunner) execCancelRun(ctx context.Context, runID string) error {
	run, err := getRun(ctx, lp.s.db, runID)
	if err != nil {
		return err
	}
	if run.endpoint == "" {
		return fmt.Errorf("run %s has no endpoint", runID)
	}
	j, err := getJobByID(ctx, lp.s.db, run.job)
	if err != nil {
		return err
	}
	tok, err := lp.s.disp.mintRun(ctx, j, runID, run.digest)
	if err != nil {
		return err
	}
	hc := lp.s.harnessClient(run.endpoint, tok.Token)
	if _, err := hc.CancelRun(ctx, hv1.CancelRunReq{Run: runID}); err != nil {
		return fmt.Errorf("cancel run: %w", err)
	}
	return lp.withRunTx(ctx, runID)
}

// withRunTx settles a cancelled run and frees the conversation.
func (lp *loopRunner) withRunTx(ctx context.Context, runID string) error {
	tx, err := lp.s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	hook := func(ctx context.Context, tx *sql.Tx, ch fsm.Change) error {
		return releaseRun(ctx, tx, runID)
	}
	err = lp.s.runM.Transition(ctx, tx, fsm.Request{
		ID: runID, Event: "settle", Actor: "outbox", At: lp.s.deps.Now()}, hook)
	if errors.Is(err, fsm.ErrBadTransition) {
		err = nil // already settled by a report
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
