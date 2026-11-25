package svc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
)

// handlers implements the contract's operation table.
func (s *Server) handlers() tv1.Handlers {
	return tv1.Handlers{
		CreateJob:       s.ops.createJob,
		GetJob:          s.ops.getJob,
		ListJobs:        s.ops.listJobs,
		CancelJob:       s.ops.cancelJob,
		RetryJob:        s.ops.retryJob,
		UnquarantineJob: s.ops.unquarantineJob,
		SetHold:         s.ops.setHold,
		ReportRun:       s.rep.reportRun,
		GetRun:          s.ops.getRun,
		GetRequestTurn:  s.ops.getRequestTurn,
	}
}

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("rand: " + err.Error())
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

func taskScope(ctx context.Context, requested string) (idv1.OwnerClaims, error) {
	scope, err := idv1.ScopeFrom(ctx)
	if err != nil {
		return scope, fmt.Errorf("scope: %w", err)
	}
	if scope.Space == "" {
		return scope, herrors.New(herrors.CodeUnauthorized, "err.unauthorized")
	}
	if requested != "" && requested != scope.Space {
		return scope, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	return scope, nil
}

func (o *jobOps) createJob(ctx context.Context, r tv1.CreateJobReq) (tv1.CreateJobResp, error) {
	scope, err := taskScope(ctx, r.Space)
	if err != nil {
		return tv1.CreateJobResp{}, fmt.Errorf("scope: %w", err)
	}
	for _, v := range []string{r.Space, r.Conversation, r.Input, r.Idem} {
		if v == "" {
			return tv1.CreateJobResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
		}
	}
	if err := o.spaceOwner(ctx, scope.Space, scope.Owner); err != nil {
		return tv1.CreateJobResp{}, err
	}
	if res, ok, err := o.idemReplay(ctx, scope, r); err != nil || ok {
		return res, err
	}
	id := newID("job")
	tx, err := o.s.db.BeginTx(ctx, nil)
	if err != nil {
		return tv1.CreateJobResp{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	j := jobRow{id: id, owner: scope.Owner, space: scope.Space,
		conv: r.Conversation, input: r.Input, idem: r.Idem}
	attach, err := attachField(r.Attachments)
	if err != nil {
		return tv1.CreateJobResp{}, err
	}
	j.attach = attach
	if err := createJob(ctx, tx, j); err != nil {
		return tv1.CreateJobResp{}, err
	}
	if err := o.s.jobM.Create(ctx, tx, clock{o.s.deps.Now},
		fsm.CreateReq{ID: id, Initial: "queued", Actor: "api"}); err != nil {
		return tv1.CreateJobResp{}, fmt.Errorf("job machine: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return tv1.CreateJobResp{}, fmt.Errorf("commit: %w", err)
	}
	return tv1.CreateJobResp{ID: id, State: "queued"}, nil
}

// attachField serializes job attachments into the stored JSON
// column; malformed input is a refusal, not a silent drop.
func attachField(a []tv1.Attachment) (string, error) {
	if len(a) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return "", herrors.New(herrors.CodeInvalid, "err.invalid-arg").With("field", "attachments") //nolint:wrapcheck // shared error form
	}
	return string(raw), nil
}

func (o *jobOps) spaceOwner(ctx context.Context, space, owner string) error {
	c, err := o.s.spawnClient(ctx)
	if err != nil {
		return err
	}
	res, err := c.ResolveSpec(ctx, sv1.ResolveSpecReq{Space: space})
	if err != nil {
		return fmt.Errorf("resolve space: %w", err)
	}
	if res.Owner != owner {
		return herrors.New(herrors.CodeNotFound, "err.not-found").With("space", space) //nolint:wrapcheck // shared error form
	}
	return nil
}

// idemReplay returns the existing job when the idem key was seen, and
// enforces the per-owner live ceiling on a genuinely new job.
func (o *jobOps) idemReplay(ctx context.Context, scope idv1.OwnerClaims, r tv1.CreateJobReq) (tv1.CreateJobResp, bool, error) {
	id, err := jobByIdem(ctx, o.s.db, scope.Owner, r.Idem)
	if err != nil {
		return tv1.CreateJobResp{}, false, err
	}
	if id != "" {
		if _, err := getJob(ctx, o.s.db, scope.Owner, scope.Space, id); err != nil {
			return tv1.CreateJobResp{}, false, err
		}
		st, err := o.s.jobM.State(ctx, o.s.db, id)
		if err != nil {
			return tv1.CreateJobResp{}, false, fmt.Errorf("job state: %w", err)
		}
		return tv1.CreateJobResp{ID: id, State: st}, true, nil
	}
	if n, err := ownerLiveJobs(ctx, o.s.db, scope.Owner); err != nil {
		return tv1.CreateJobResp{}, false, err
	} else if n >= maxOwnerLive {
		exhausted := herrors.New(herrors.CodeUnavailable, "err.exhausted").
			With("ceiling", "owner-live")
		return tv1.CreateJobResp{}, false, exhausted //nolint:wrapcheck // shared error form
	}
	return tv1.CreateJobResp{}, false, nil
}

// clock adapts the injected Now to fsm.Clock.
type clock struct{ f func() time.Time }

func (c clock) Now() time.Time { return c.f() }

func (o *jobOps) getJob(ctx context.Context, r tv1.GetJobReq) (tv1.GetJobResp, error) {
	scope, err := taskScope(ctx, "")
	if err != nil {
		return tv1.GetJobResp{}, fmt.Errorf("scope: %w", err)
	}
	j, err := getJob(ctx, o.s.db, scope.Owner, scope.Space, r.ID)
	if err != nil {
		return tv1.GetJobResp{}, err
	}
	st, err := o.s.jobM.State(ctx, o.s.db, j.id)
	if err != nil {
		return tv1.GetJobResp{}, fmt.Errorf("job state: %w", err)
	}
	return tv1.GetJobResp{Job: info(j, st)}, nil
}

func (o *jobOps) listJobs(ctx context.Context, r tv1.ListJobsReq) (tv1.ListJobsResp, error) {
	scope, err := taskScope(ctx, "")
	if err != nil {
		return tv1.ListJobsResp{}, fmt.Errorf("scope: %w", err)
	}
	rows, err := listJobs(ctx, o.s.db, scope.Owner, scope.Space)
	if err != nil {
		return tv1.ListJobsResp{}, err
	}
	out := tv1.ListJobsResp{Jobs: []tv1.JobInfo{}}
	for _, j := range rows {
		st, err := o.s.jobM.State(ctx, o.s.db, j.id)
		if err != nil {
			return tv1.ListJobsResp{}, fmt.Errorf("job state: %w", err)
		}
		out.Jobs = append(out.Jobs, info(j, st))
	}
	return out, nil
}

func info(j jobRow, state string) tv1.JobInfo {
	return tv1.JobInfo{ID: j.id, Space: j.space, State: state, Result: j.result, Done: JobMachine().Terminal(state)}
}

// jobEvent applies an operator event to a job in one tx; the state
// guard is the machine's edge table.
func (o *jobOps) jobEvent(ctx context.Context, scope idv1.OwnerClaims, id, event string,
	hook fsm.Hook) (tv1.JobInfo, error) {
	j, err := getJob(ctx, o.s.db, scope.Owner, scope.Space, id)
	if err != nil {
		return tv1.JobInfo{}, err
	}
	tx, err := o.s.db.BeginTx(ctx, nil)
	if err != nil {
		return tv1.JobInfo{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	err = o.s.jobM.Transition(ctx, tx, fsm.Request{
		ID: id, Event: event, Actor: "api", At: o.s.deps.Now()}, hook)
	if err != nil {
		return tv1.JobInfo{}, fmt.Errorf("%s: %w", event, err)
	}
	if err := tx.Commit(); err != nil {
		return tv1.JobInfo{}, fmt.Errorf("commit: %w", err)
	}
	st, err := o.s.jobM.State(ctx, o.s.db, id)
	if err != nil {
		return tv1.JobInfo{}, fmt.Errorf("job state: %w", err)
	}
	return info(j, st), nil
}

func (o *jobOps) cancelJob(ctx context.Context, r tv1.JobReq) (tv1.JobInfo, error) {
	scope, err := taskScope(ctx, "")
	if err != nil {
		return tv1.JobInfo{}, fmt.Errorf("scope: %w", err)
	}
	j, err := getJob(ctx, o.s.db, scope.Owner, scope.Space, r.ID)
	if err != nil {
		return tv1.JobInfo{}, err
	}
	tx, err := o.s.db.BeginTx(ctx, nil)
	if err != nil {
		return tv1.JobInfo{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	// A running job has an active run; the cancel reaches the runtime
	// through an outbox effect committed with the transition.
	var effects []fsm.Effect
	run, err := activeRunForJob(ctx, tx, j.id)
	if err != nil {
		return tv1.JobInfo{}, err
	}
	if run != "" {
		effects = []fsm.Effect{{Topic: "cancel-run", Payload: []byte(run)}}
	}
	err = o.s.jobM.Transition(ctx, tx, fsm.Request{
		ID: j.id, Event: "cancel", Actor: "api", At: o.s.deps.Now(),
		Effects: effects}, nil)
	if err != nil {
		return tv1.JobInfo{}, fmt.Errorf("cancel: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return tv1.JobInfo{}, fmt.Errorf("commit: %w", err)
	}
	st, err := o.s.jobM.State(ctx, o.s.db, j.id)
	if err != nil {
		return tv1.JobInfo{}, fmt.Errorf("job state: %w", err)
	}
	return info(j, st), nil
}

func (o *jobOps) retryJob(ctx context.Context, r tv1.JobReq) (tv1.JobInfo, error) {
	scope, err := taskScope(ctx, "")
	if err != nil {
		return tv1.JobInfo{}, fmt.Errorf("scope: %w", err)
	}
	return o.jobEvent(ctx, scope, r.ID, "retry", nil)
}

func (o *jobOps) unquarantineJob(ctx context.Context, r tv1.JobReq) (tv1.JobInfo, error) {
	scope, err := taskScope(ctx, "")
	if err != nil {
		return tv1.JobInfo{}, fmt.Errorf("scope: %w", err)
	}
	return o.jobEvent(ctx, scope, r.ID, "unquarantine", nil)
}

func (o *jobOps) setHold(ctx context.Context, r tv1.SetHoldReq) (tv1.SetHoldResp, error) {
	scope, err := taskScope(ctx, r.Space)
	if err != nil {
		return tv1.SetHoldResp{}, fmt.Errorf("scope: %w", err)
	}
	if r.Space == "" {
		return tv1.SetHoldResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	if err := o.spaceOwner(ctx, scope.Space, scope.Owner); err != nil {
		return tv1.SetHoldResp{}, err
	}
	epoch, err := setHold(ctx, o.s.db, scope.Space, r.Active, r.Note)
	if err != nil {
		return tv1.SetHoldResp{}, err
	}
	return tv1.SetHoldResp{Epoch: epoch}, nil
}

func (o *jobOps) getRun(ctx context.Context, r tv1.GetRunReq) (tv1.GetRunResp, error) {
	scope, err := taskScope(ctx, "")
	if err != nil {
		return tv1.GetRunResp{}, fmt.Errorf("scope: %w", err)
	}
	run, err := getRun(ctx, o.s.db, r.Run)
	if err != nil {
		return tv1.GetRunResp{}, err
	}
	if run.owner != scope.Owner || run.space != scope.Space {
		return tv1.GetRunResp{}, herrors.New(herrors.CodeNotFound, "err.not-found").With("run", r.Run) //nolint:wrapcheck // shared error form
	}
	st, err := o.s.runM.State(ctx, o.s.db, r.Run)
	if err != nil {
		return tv1.GetRunResp{}, fmt.Errorf("run state: %w", err)
	}
	rows, err := runEvents(ctx, o.s.db, r.Run, r.From)
	if err != nil {
		return tv1.GetRunResp{}, err
	}
	out := tv1.GetRunResp{State: st, Events: []tv1.RunEvent{}}
	for _, e := range rows {
		out.Events = append(out.Events, tv1.RunEvent{Seq: e.seq, Kind: e.kind, Data: e.data})
	}
	return out, nil
}
