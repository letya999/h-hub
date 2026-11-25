package svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	hv1 "github.com/letya999/h-hub/contracts/harness/v1"
	orv1 "github.com/letya999/h-hub/contracts/ownerreq/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
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

// Loops runs the dispatcher and the maintenance side — sweeps,
// outbox and watchdog ticks — until ctx ends. A dispatch can wait on
// a runtime for tens of seconds, so maintenance runs on its own
// goroutine: a busy dispatcher must never starve deadlines. The
// lease still guarantees one active sweeper.
func (lp *loopRunner) Loops(ctx context.Context, l *log.L) {
	t := time.NewTicker(dispatchEvery)
	defer t.Stop()
	go lp.maintenance(ctx, l)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			lp.dispatchPass(ctx, l)
		}
	}
}

// maintenance paces sweep, outbox and tick passes on their own
// tickers. Each iteration is one pass, never a sleep on work.
func (lp *loopRunner) maintenance(ctx context.Context, l *log.L) {
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
	lp.pollWaiting(ctx, l)
}

// pollWaiting resolves every waiting_owner job against its request:
// an answer resumes the job into the lane, a denial or expiry fails
// it with a clear reason — never a hang. The machine deadline at the
// request's own TTL is the backstop for a request that never
// resolves in tool-hub.
func (lp *loopRunner) pollWaiting(ctx context.Context, l *log.L) {
	waiting, err := waitingJobs(ctx, lp.s.db)
	if err != nil {
		l.Error(ctx, "waiting poll", err)
		return
	}
	for _, w := range waiting {
		if err := lp.resolveWaiting(ctx, w); err != nil {
			l.Error(ctx, "waiting "+w.id, err)
		}
	}
}

// resolveWaiting applies one request's outcome to its job. The poll
// also carries the conversation binding: the runtime cannot mint it
// into the run token — the mint wire stays frozen for the rollback
// baseline — so the binding rides this poll until it lands; the
// delivery effect arms inside tool-hub at that moment.
func (lp *loopRunner) resolveWaiting(ctx context.Context, w waitingJob) error {
	cl, err := lp.s.toolClient(ctx)
	if err != nil {
		return err
	}
	if _, err := cl.BindRequest(ctx, orv1.BindReq{
		Request: w.request, Conversation: w.conv}); err != nil &&
		herrors.CodeOf(err) != herrors.CodeConflict &&
		herrors.CodeOf(err) != herrors.CodeNotFound {
		return fmt.Errorf("bind request: %w", err)
	}
	res, err := cl.GetRequest(ctx, orv1.GetReq{Request: w.request})
	if herrors.CodeOf(err) == herrors.CodeNotFound {
		return lp.finishWaiting(ctx, w.id, "expire", "owner request expired")
	}
	if err != nil {
		return fmt.Errorf("get request: %w", err)
	}
	switch res.State {
	case "answered":
		return lp.finishWaiting(ctx, w.id, "resume", "")
	case "denied":
		return lp.finishWaiting(ctx, w.id, "settle-fail", "owner denied the request")
	case "expired":
		return lp.finishWaiting(ctx, w.id, "expire", "owner request expired")
	}
	return nil
}

// finishWaiting fires one exit event on a waiting job, storing the
// refusal reason as its result when it fails. A job that already
// left waiting_owner (cancelled alongside) is untouched.
func (lp *loopRunner) finishWaiting(ctx context.Context, job, event, result string) error {
	tx, err := lp.s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	hook := func(ctx context.Context, tx *sql.Tx, _ fsm.Change) error {
		if result == "" {
			return nil
		}
		return setResult(ctx, tx, job, result)
	}
	err = lp.s.jobM.Transition(ctx, tx, fsm.Request{
		ID: job, Event: event, Actor: "sweep", At: lp.s.deps.Now()}, hook)
	if errors.Is(err, fsm.ErrBadTransition) {
		err = nil
	}
	if err != nil {
		return fmt.Errorf("%s: %w", event, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
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
