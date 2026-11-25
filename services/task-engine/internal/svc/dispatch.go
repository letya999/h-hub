package svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	hv1 "github.com/letya999/h-hub/contracts/harness/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	orv1 "github.com/letya999/h-hub/contracts/ownerreq/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/log"
)

// claim moves one due job queued -> admitted under the conversation
// lock and returns its id. The global running ceiling applies first.
func (d *dispatcher) claim(ctx context.Context) (string, error) {
	if n, err := globalRunning(ctx, d.s.db); err != nil {
		return "", err
	} else if n >= maxGlobalLive {
		return "", errNoClaim
	}
	tx, err := d.s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	j, err := claimable(ctx, tx)
	if err != nil {
		return "", err
	}
	if err := d.s.jobM.Transition(ctx, tx, fsm.Request{
		ID: j.id, Event: "admit", Actor: "dispatcher", At: d.s.deps.Now()}, nil); err != nil {
		return "", fmt.Errorf("admit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	return j.id, nil
}

// dispatch drives one admitted job through the runtime chain:
// spec -> runtime -> run token -> submit -> running. Any failure
// marks the job uncertain (the admitted deadline is the backstop).
func (d *dispatcher) dispatch(ctx context.Context, l *log.L, jobID string) {
	if err := d.dispatchInner(ctx, l, jobID); err != nil {
		l.Error(ctx, "dispatch "+jobID, err)
		d.lost(ctx, l, jobID)
	}
}

func (d *dispatcher) dispatchInner(ctx context.Context, l *log.L, jobID string) error {
	j, err := getJobByID(ctx, d.s.db, jobID)
	if err != nil {
		return err
	}
	sc, err := d.s.spawnClient(ctx)
	if err != nil {
		return err
	}
	spec, err := sc.ResolveSpec(ctx, sv1.ResolveSpecReq{Space: j.space})
	if err != nil {
		return fmt.Errorf("resolve spec: %w", err)
	}
	rt, err := d.awaitRuntime(ctx, sc, j.space, spec.Digest)
	if err != nil {
		return err
	}
	runID := newID("run")
	tok, err := d.mintRun(ctx, j, runID, spec.Digest)
	if err != nil {
		return err
	}
	if err := d.bindRun(ctx, j, runID, spec.Digest, rt); err != nil {
		return err
	}
	hc := d.s.harnessClient(rt.Endpoint, tok.Token)
	input, err := d.runInput(ctx, j)
	if err != nil {
		return err
	}
	req := hv1.SubmitRunReq{Run: runID, Job: j.id, Space: j.space, Input: input}
	if j.attach != "" {
		if err := json.Unmarshal([]byte(j.attach), &req.Attachments); err != nil {
			return fmt.Errorf("job attachments: %w", err)
		}
	}
	ack, err := hc.SubmitRun(ctx, req)
	if err != nil || !ack.Accepted {
		return fmt.Errorf("submit refused")
	}
	return nil
}

// runInput composes the input for the job's next run: the owner's
// answer joins the original message when the job resumes from
// waiting_owner. The answer is read from tool-hub at dispatch — the
// one copy lives there, never in the job row (rule 3).
func (d *dispatcher) runInput(ctx context.Context, j jobRow) (string, error) {
	if j.ownerRequest == "" {
		return j.input, nil
	}
	cl, err := d.s.toolClient(ctx)
	if err != nil {
		return "", err
	}
	res, err := cl.GetRequest(ctx, orv1.GetReq{Request: j.ownerRequest})
	if err != nil {
		return "", fmt.Errorf("resume request: %w", err)
	}
	return fmt.Sprintf("%s\n\n[owner request answered: %s]",
		j.input, res.Answer), nil
}

// awaitRuntime polls EnsureRuntime until the runtime is warm and
// returns the endpoint. Runtime starts are bounded by their own
// starting deadline; this pass stops polling well before it.
func (d *dispatcher) awaitRuntime(ctx context.Context, sc *sv1.Client,
	space, digest string) (sv1.EnsureRuntimeResp, error) {
	deadline := d.s.deps.Now().Add(80 * time.Second)
	for {
		rt, err := sc.EnsureRuntime(ctx,
			sv1.EnsureRuntimeReq{Space: space, Digest: digest})
		if err != nil {
			return rt, fmt.Errorf("ensure runtime: %w", err)
		}
		if rt.Ready {
			return rt, nil
		}
		if d.s.deps.Now().After(deadline) {
			return rt, fmt.Errorf("runtime never warmed for space %s", space)
		}
		select {
		case <-ctx.Done():
			return rt, fmt.Errorf("await runtime: %w", ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (d *dispatcher) mintRun(ctx context.Context, j jobRow, runID, digest string) (idv1.MintRunTokenResp, error) {
	tok, err := d.s.toks.Token(ctx, "identity-hub")
	if err != nil {
		return idv1.MintRunTokenResp{}, fmt.Errorf("service token: %w", err)
	}
	res, err := d.s.identityClient(tok).MintRunToken(ctx, idv1.MintRunTokenReq{
		Job: j.id, Run: runID, Space: j.space, Owner: j.owner,
		Digest: digest, Aud: []string{"harness-runtime", "task-engine",
			"model-gateway", "tool-hub"},
	})
	if err != nil {
		return res, fmt.Errorf("mint run token: %w", err)
	}
	return res, nil
}

// bindRun commits the run row, its machine and the conversation
// binding in one tx; a refused bind means a second run raced.
func (d *dispatcher) bindRun(ctx context.Context, j jobRow, runID, digest string,
	rt sv1.EnsureRuntimeResp) error {
	tx, err := d.s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	if err := insertRun(ctx, tx, runRow{
		id: runID, job: j.id, owner: j.owner, space: j.space,
		conv: j.conv, runtime: rt.Runtime, endpoint: rt.Endpoint, digest: digest}); err != nil {
		return err
	}
	if err := d.s.runM.Create(ctx, tx, clock{d.s.deps.Now},
		fsm.CreateReq{ID: runID, Initial: "dispatched", Actor: "dispatcher"}); err != nil {
		return fmt.Errorf("run machine: %w", err)
	}
	if err := d.s.jobM.Transition(ctx, tx, fsm.Request{
		ID: j.id, Event: "dispatch", Actor: "dispatcher", At: d.s.deps.Now()}, nil); err != nil {
		return fmt.Errorf("job running: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// lost moves a job to uncertain without retrying.
func (d *dispatcher) lost(ctx context.Context, l *log.L, jobID string) {
	if err := d.withJobTx(ctx, jobID, "lost", "dispatcher", nil); err != nil {
		l.Error(ctx, "lost "+jobID, err)
	}
}

// withJobTx applies one job event in its own transaction.
func (d *dispatcher) withJobTx(ctx context.Context, jobID, event, actor string,
	hook fsm.Hook) error {
	tx, err := d.s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	if err := d.s.jobM.Transition(ctx, tx, fsm.Request{
		ID: jobID, Event: event, Actor: actor, At: d.s.deps.Now()}, hook); err != nil {
		return fmt.Errorf("%s: %w", event, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// getJobByID is the owner-agnostic read for the dispatcher.
func getJobByID(ctx context.Context, db *sql.DB, id string) (jobRow, error) {
	var j jobRow
	err := db.QueryRowContext(ctx, `SELECT id, owner, space, conversation,
		input, idem, result, owner_request, COALESCE(attachments::text,'') FROM jobs WHERE id=$1`, id).Scan(
		&j.id, &j.owner, &j.space, &j.conv, &j.input, &j.idem, &j.result, &j.ownerRequest, &j.attach)
	if err != nil {
		return j, fmt.Errorf("get job %s: %w", id, err)
	}
	return j, nil
}
