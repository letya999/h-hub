package svc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/letya999/h-hub/services/spawn-controller/internal/backend"
	"github.com/letya999/h-hub/services/spawn-controller/internal/worker"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
)

func startWorkerEffect(s *Server, ctx context.Context, f fsm.OutEffect) error {
	if err := s.workers.Start(ctx, f, func(ctx context.Context, w worker.Record) error {
		return startWorker(s, ctx, w)
	}); err != nil {
		return fmt.Errorf("start worker intent: %w", err)
	}
	return nil
}

func startWorker(s *Server, ctx context.Context, w worker.Record) error {
	launch, err := workerLaunch(s, ctx, w)
	if err != nil {
		return err
	}
	if _, err := s.deps.Docker.CreateAndStartWorker(ctx, launch); err != nil {
		return fmt.Errorf("start worker container: %w", err)
	}
	token := workerToken(s.deps.SelfKey, w.Generation)
	if err := s.deps.Docker.InstallWorkerToken(ctx, launch.Ref, token); err != nil {
		return fmt.Errorf("install worker generation token: %w", err)
	}
	if err := s.deps.Docker.ProbeWorker(ctx, launch.Ref, token); err != nil {
		return fmt.Errorf("probe worker readiness: %w", err)
	}
	return nil
}

func workerLaunch(s *Server, ctx context.Context, w worker.Record) (backend.WorkerLaunch, error) {
	var out backend.WorkerLaunch
	p, ok := s.deps.WorkerPolicy.Profiles[w.Profile]
	if !ok || p.Image != w.Image {
		return out, herrors.ErrStale
	}
	sp, err := getSpace(ctx, s.db, w.Owner, w.Space)
	if err != nil {
		return out, err
	}
	if staleDigest(w.Digest, sp.digest) {
		return out, herrors.ErrStale
	}
	doc, err := specPassports(sp)
	if err != nil {
		return out, err
	}
	p.Memory = min(p.Memory, memBytes(doc.Stack.Mem))
	p.CPU = min(p.CPU, nanoCPUs(doc.Stack.CPU))
	p.PIDs = min(p.PIDs, doc.Stack.Pids)
	mounts, err := prepareWorkspace(s.deps.RenderDir, sp.id, doc.Workspace)
	if err != nil {
		return out, err
	}
	return backend.WorkerLaunch{Ref: backend.WorkerRef{Key: w.ID, Generation: w.Generation},
		Profile: p, RenderVol: s.deps.RenderVol, Workspace: mounts, Slot: w.Slot}, nil
}

func workerToken(key []byte, generation string) string {
	tag := hmac.New(sha256.New, key)
	tag.Write([]byte("hhub.worker.generation\x00" + generation))
	return hex.EncodeToString(tag.Sum(nil))
}
