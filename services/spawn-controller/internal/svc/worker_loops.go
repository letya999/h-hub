package svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/letya999/h-hub/services/spawn-controller/internal/backend"
	"github.com/letya999/h-hub/services/spawn-controller/internal/worker"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/log"
)

func reapWorkerEffect(s *Server, ctx context.Context, f fsm.OutEffect) error {
	if err := s.workers.Reap(ctx, f, func(ctx context.Context, w worker.Record) (bool, error) {
		return s.deps.Docker.StopWorker(ctx, backend.WorkerRef{Key: w.ID, Generation: w.Generation})
	}); err != nil {
		return fmt.Errorf("reap generation: %w", err)
	}
	return nil
}

func reconcileWorkers(s *Server, ctx context.Context, l *log.L) {
	if err := s.workers.Sweep(ctx); err != nil {
		l.Error(ctx, "worker sweep", err)
	}
	containers, err := s.deps.Docker.Workers(ctx)
	if err != nil {
		l.Error(ctx, "worker inventory", err)
		return
	}
	for _, c := range containers {
		if err := reapUnregistered(s, ctx, l, c); err != nil {
			l.Error(ctx, "worker janitor", err, "worker", c.Key, "generation", c.Generation)
		}
	}
}

func reapUnregistered(s *Server, ctx context.Context, l *log.L, c backend.WorkerRef) error {
	w, err := s.workers.Lookup(ctx, c.Key)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read registered worker: %w", err)
	}
	if w.OwnsGeneration(c.Generation) {
		return nil
	}
	absent, err := s.deps.Docker.StopWorker(ctx, c)
	if absent {
		l.Info(ctx, "orphan worker removed", "worker", c.Key, "generation", c.Generation, "reason", "missing generation")
	}
	if err != nil {
		return fmt.Errorf("remove orphan worker: %w", err)
	}
	if !absent {
		return errors.New("orphan worker remains present")
	}
	return nil
}
