//go:build linux

package cellinit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	wv1 "github.com/letya999/h-hub/contracts/worker/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
)

// Handler exposes the same authenticated API on the worker listener.
func Handler(token string, abort func(error)) (http.Handler, error) {
	return newHandler(token, abort, Run)
}

func newHandler(token string, abort func(error), execute func(context.Context, wv1.ExecReq) (wv1.ExecResp, error)) (http.Handler, error) {
	if len(token) < 32 || strings.ContainsAny(token, " \r\n\t") || abort == nil {
		return nil, errors.New("missing generation token")
	}
	c := cell{active: make(chan struct{}, 1), abort: abort, execute: execute, seen: make(map[string]struct{})}
	mux := http.NewServeMux()
	mux.Handle("POST "+wv1.ExecPath, httpapi.Handle(token, c.exec))
	mux.Handle("GET "+wv1.HealthPath, httpapi.Handle(token, health))
	return mux, nil
}

type cell struct {
	active  chan struct{}
	execute func(context.Context, wv1.ExecReq) (wv1.ExecResp, error)
	abort   func(error)
	failed  atomic.Bool
	seen    map[string]struct{}
}

func (c *cell) exec(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, wv1.MaxBody)
	req, err := wv1.Decode(r.Body)
	if err != nil {
		return fmt.Errorf("decode command: %w", err)
	}
	select {
	case c.active <- struct{}{}:
		defer func() { <-c.active }()
	default:
		return herrors.New(herrors.CodeConflict, "err.machine-state")
	}
	if c.failed.Load() {
		return herrors.New(herrors.CodeUnavailable, "err.unavailable")
	}
	if err := c.acceptRequest(r.Header.Get("Idempotency-Key")); err != nil {
		return err
	}
	res, err := c.execute(r.Context(), req)
	if errors.Is(err, ErrUnclean) {
		c.failed.Store(true)
		c.abort(err)
	}
	if err != nil {
		return fmt.Errorf("execute command: %w", err)
	}
	httpapi.WriteJSON(w, res)
	return nil
}

func (c *cell) acceptRequest(key string) error {
	if key == "" || len(key) > 128 {
		return herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	if _, found := c.seen[key]; found {
		err := herrors.New(herrors.CodeUncertain, "err.terminal-uncertain")
		err.Retryable = false
		return err
	}
	if len(c.seen) >= 4096 {
		return herrors.New(herrors.CodeCapacity, "err.worker-capacity")
	}
	c.seen[key] = struct{}{}
	return nil
}

func health(w http.ResponseWriter, _ *http.Request) error {
	httpapi.WriteJSON(w, map[string]string{"status": "ok"})
	return nil
}
