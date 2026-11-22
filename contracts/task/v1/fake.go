package v1

import (
	"context"
	"sort"
	"sync"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// Fake is an in-memory task-engine for tests: it honors the contract
// surface (create/get/list/cancel/hold) without Postgres or machines.
// The conformance suite pins the shared behavior.
type Fake struct {
	mu    sync.Mutex
	jobs  map[string]*JobInfo
	byIdm map[string]string
	runs  map[string][]RunEvent
	epoch int64
	next  int
}

// NewFake builds an empty in-memory engine.
func NewFake() *Fake {
	return &Fake{
		jobs:  map[string]*JobInfo{},
		byIdm: map[string]string{},
		runs:  map[string][]RunEvent{},
	}
}

// Handlers exposes the fake as the contract operation table.
func (f *Fake) Handlers() Handlers {
	return Handlers{
		CreateJob:       f.create,
		GetJob:          f.get,
		ListJobs:        f.list,
		CancelJob:       f.cancel,
		RetryJob:        f.retry,
		UnquarantineJob: f.unquarantine,
		SetHold:         f.hold,
		ClaimNext:       f.claim,
		ReportRun:       f.report,
		GetRun:          f.runEvents,
	}
}

func (f *Fake) create(_ context.Context, r CreateJobReq) (CreateJobResp, error) {
	var out CreateJobResp
	if err := herrors.Require(r.Owner != "" && r.Space != "" && r.Input != "", "err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.byIdm[r.Owner+"/"+r.Idem]; ok && r.Idem != "" {
		return CreateJobResp{ID: id, State: f.jobs[id].State}, nil
	}
	f.next++
	id := jobID(f.next)
	f.jobs[id] = &JobInfo{ID: id, Space: r.Space, State: "queued", Created: "now"}
	f.byIdm[r.Owner+"/"+r.Idem] = id
	return CreateJobResp{ID: id, State: "queued"}, nil
}

func (f *Fake) get(_ context.Context, r GetJobReq) (GetJobResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[r.ID]
	if !ok {
		return GetJobResp{}, herrors.New(herrors.CodeNotFound, "err.not-found").With("id", r.ID) //nolint:wrapcheck // shared error form
	}
	return GetJobResp{Job: *j}, nil
}

func (f *Fake) list(_ context.Context, r ListJobsReq) (ListJobsResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, 0, len(f.jobs))
	for id := range f.jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := ListJobsResp{Jobs: make([]JobInfo, 0, len(ids))}
	for _, id := range ids {
		out.Jobs = append(out.Jobs, *f.jobs[id])
	}
	return out, nil
}

func (f *Fake) cancel(_ context.Context, r JobReq) (JobInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[r.ID]
	if !ok {
		return JobInfo{}, herrors.New(herrors.CodeNotFound, "err.not-found").With("id", r.ID) //nolint:wrapcheck // shared error form
	}
	j.State = "cancelled"
	return *j, nil
}

func (f *Fake) retry(_ context.Context, r JobReq) (JobInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[r.ID]
	if !ok {
		return JobInfo{}, herrors.New(herrors.CodeNotFound, "err.not-found").With("id", r.ID) //nolint:wrapcheck // shared error form
	}
	j.State = "queued"
	return *j, nil
}

func (f *Fake) unquarantine(ctx context.Context, r JobReq) (JobInfo, error) {
	return f.retry(ctx, r)
}

func (f *Fake) hold(_ context.Context, r SetHoldReq) (SetHoldResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.epoch++
	return SetHoldResp{Epoch: f.epoch}, nil
}

func (f *Fake) claim(_ context.Context, _ ClaimNextReq) (ClaimNextResp, error) {
	return ClaimNextResp{}, nil
}

func (f *Fake) report(_ context.Context, r ReportRunReq) (struct{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs[r.Run] = append(f.runs[r.Run], r.Events...)
	return struct{}{}, nil
}

func (f *Fake) runEvents(_ context.Context, r GetRunReq) (GetRunResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out GetRunResp
	for _, e := range f.runs[r.Run] {
		if e.Seq > r.From {
			out.Events = append(out.Events, e)
		}
	}
	return out, nil
}

func jobID(n int) string {
	digits := []byte{'0', '0', '0', '0', '0', '0'}
	for i := len(digits) - 1; i >= 0 && n > 0; i-- {
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return "job_" + string(digits)
}
