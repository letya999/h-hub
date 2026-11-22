// Package lifecycle owns background loops and shutdown: every loop has
// an owner that joins it on Stop, and HTTP servers drain in-flight
// requests under a deadline.
package lifecycle

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Group owns background loops. Stop cancels them and waits for all to
// return; there is no fire-and-forget.
type Group struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewGroup builds a group cancelled with ctx or by Stop.
func NewGroup(ctx context.Context) *Group {
	c, cancel := context.WithCancel(ctx)
	return &Group{ctx: c, cancel: cancel}
}

// Ctx returns the group's context for loops to select on.
func (g *Group) Ctx() context.Context { return g.ctx }

// Go runs fn under group ownership; Stop joins it.
func (g *Group) Go(fn func(ctx context.Context)) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		fn(g.ctx)
	}()
}

// Stop cancels and waits for every loop to exit.
func (g *Group) Stop() {
	g.cancel()
	g.wg.Wait()
}

// Serve runs an HTTP server and drains it when ctx ends, bounded by
// grace. Returns the server error or the drain error.
func Serve(ctx context.Context, srv *http.Server, grace time.Duration) error {
	errc := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errc <- err
			return
		}
		errc <- nil
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	dctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := srv.Shutdown(dctx); err != nil {
		return fmt.Errorf("drain: %w", err)
	}
	return <-errc
}
