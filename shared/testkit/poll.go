package testkit

import (
	"context"
	"testing"
	"time"
)

// Poll evaluates cond on a ticker until it holds or the deadline
// expires; tests wait on outcomes, never on fixed durations (rule 20).
func Poll(t *testing.T, timeout, interval time.Duration, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if cond() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("condition not met within %s", timeout)
		case <-tick.C:
		}
	}
}
