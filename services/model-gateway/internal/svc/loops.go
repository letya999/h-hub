package svc

import (
	"context"
	"time"

	"github.com/letya999/h-hub/shared/log"
)

const sweepEvery = time.Minute

// Loops converts abandoned reservations to charged spend until ctx
// ends; a crashed request can never wedge a counter open.
func (s *Server) Loops(ctx context.Context, l *log.L) {
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := sweepReservations(ctx, s.db,
				s.deps.Now().Add(-reserveTTL)); err != nil {
				l.Error(ctx, "reservation sweep", err)
			}
		}
	}
}
