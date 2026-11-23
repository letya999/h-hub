package svc

import (
	"context"
	"time"

	"github.com/letya999/h-hub/shared/log"
)

// SweepLinks expires one-time codes even when no new link request arrives.
func (s *Server) SweepLinks(ctx context.Context, l *log.L) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if _, err := linkMachine().Sweep(ctx, s.db, time.Unix(s.now(), 0), 32, nil); err != nil {
				l.Error(ctx, "link code sweep", err)
			}
		}
	}
}
