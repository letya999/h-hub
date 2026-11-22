package v1

import (
	"context"
	"sync"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// Fake is an in-memory harness-runtime for tests: an admission table
// keyed by run id. SubmitRun admits; CancelRun revokes. The run-token
// check lives in the transport layer, so the fake only enforces the
// admission semantics the suite pins.
type Fake struct {
	mu       sync.Mutex
	admitted map[string]SubmitRunReq
	next     int
}

// NewFake builds an empty admission table.
func NewFake() *Fake {
	return &Fake{admitted: map[string]SubmitRunReq{}}
}

// Handlers exposes the fake as the contract operation table.
func (f *Fake) Handlers() Handlers {
	return Handlers{SubmitRun: f.submit, CancelRun: f.cancel}
}

func (f *Fake) submit(_ context.Context, r SubmitRunReq) (SubmitRunResp, error) {
	var out SubmitRunResp
	if err := herrors.Require(r.Run != "" && r.Job != "" && r.Space != "", "err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, dup := f.admitted[r.Run]; dup {
		return out, herrors.New(herrors.CodeConflict, "err.conflict").With("run", r.Run) //nolint:wrapcheck // shared error form
	}
	f.admitted[r.Run] = r
	return SubmitRunResp{Accepted: true}, nil
}

func (f *Fake) cancel(_ context.Context, r CancelRunReq) (CancelRunResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.admitted[r.Run]; !ok {
		return CancelRunResp{}, herrors.New(herrors.CodeNotFound, "err.not-found").With("run", r.Run) //nolint:wrapcheck // shared error form
	}
	delete(f.admitted, r.Run)
	return CancelRunResp{OK: true}, nil
}
