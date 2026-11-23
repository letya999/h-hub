package v1

import (
	"context"
	"encoding/json"
	"sync"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// Fake is an in-memory model-gateway for tests: a canned OpenAI
// answer, a charged counter and one fixed route table. The
// conformance suite pins the shared behavior.
type Fake struct {
	mu     sync.Mutex
	spent  int64
	daily  int64
	run    int64
	routes []RouteInfo
}

// NewFake builds a fake with the given daily and per-run limits.
func NewFake(daily, run int64) *Fake {
	return &Fake{daily: daily, run: run, routes: []RouteInfo{
		{Name: "scripted", Model: "scripted"},
		{Name: "scripted-alt", Model: "scripted"},
	}}
}

// Handlers exposes the fake as the contract operation table.
func (f *Fake) Handlers() Handlers {
	return Handlers{
		Complete: f.complete,
		Usage:    f.usage,
		Routes:   f.routes_,
	}
}

func (f *Fake) complete(_ context.Context, r CompleteReq) (CompleteResp, error) {
	var out CompleteResp
	if err := herrors.Require(r.Run != "" && len(r.Request) > 0, "err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spent += 100
	body, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"message": map[string]string{"role": "assistant", "content": "fake answer"},
		}},
		"usage": map[string]int64{"prompt_tokens": 10, "completion_tokens": 90, "total_tokens": 100},
	})
	if err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	return CompleteResp{Response: body}, nil
}

func (f *Fake) usage(_ context.Context, _ UsageReq) (UsageResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return UsageResp{Spent: f.spent, DailyLimit: f.daily, RunLimit: f.run}, nil
}

func (f *Fake) routes_(_ context.Context, _ RoutesReq) (RoutesResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return RoutesResp{Default: "scripted", Routes: f.routes}, nil
}
