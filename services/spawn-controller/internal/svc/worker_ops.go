package svc

import (
	"context"
	"database/sql"
	"fmt"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	"github.com/letya999/h-hub/services/spawn-controller/internal/worker"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
)

func workerHandlers(s *Server) sv1.WorkerHandlers {
	return sv1.WorkerHandlers{
		Claim: func(ctx context.Context, r sv1.ClaimWorkerReq) (sv1.ClaimWorkerResp, error) {
			return claimWorker(s, ctx, r)
		},
		Lease: func(ctx context.Context, r sv1.WorkerLeaseReq) (sv1.WorkerLeaseResp, error) {
			return changeWorkerLease(s, ctx, r)
		},
	}
}

func workerCaller(ctx context.Context) error {
	principal := httpapi.PrincipalFrom(ctx)
	if principal == nil || principal.Purpose != "service" || principal.Sub != "tool-hub" {
		return herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	return nil
}

func claimWorker(s *Server, ctx context.Context, req sv1.ClaimWorkerReq) (sv1.ClaimWorkerResp, error) {
	var out sv1.ClaimWorkerResp
	if err := workerCaller(ctx); err != nil {
		return out, err
	}
	if req.Owner == "" || req.Space == "" || req.Digest == "" || req.Key == "" {
		return out, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	profile, ok := s.deps.WorkerPolicy.Profiles[req.Profile]
	if !ok {
		return out, herrors.New(herrors.CodeInvalid, "err.invalid-arg").With("profile", req.Profile) //nolint:wrapcheck // shared error form
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, fmt.Errorf("begin worker claim: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	if err := workerSpace(ctx, tx, req); err != nil {
		return out, err
	}
	w, err := s.workers.Reserve(ctx, tx, worker.Claim{
		Key:   worker.Key{Owner: req.Owner, Space: req.Space, Profile: req.Profile, Digest: req.Digest},
		Image: profile.Image, Lease: req.Key,
	})
	if err != nil {
		return out, fmt.Errorf("reserve worker: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return out, fmt.Errorf("commit worker claim: %w", err)
	}
	out = sv1.ClaimWorkerResp{Worker: w.ID, Generation: w.Generation, Lease: w.Lease, Ready: w.Ready()}
	if out.Ready {
		out.SocketSlot, out.Token = w.Slot, workerToken(s.deps.SelfKey, w.Generation)
	}
	return out, nil
}

func workerSpace(ctx context.Context, tx *sql.Tx, req sv1.ClaimWorkerReq) error {
	sp, err := ownedSpaceForUpdate(ctx, tx, req.Owner, req.Space)
	if err != nil {
		return err
	}
	if staleDigest(req.Digest, sp.digest) {
		return herrors.ErrStale
	}
	return nil
}

func changeWorkerLease(s *Server, ctx context.Context, req sv1.WorkerLeaseReq) (sv1.WorkerLeaseResp, error) {
	if err := workerCaller(ctx); err != nil {
		return sv1.WorkerLeaseResp{}, err
	}
	l := worker.Lease{Owner: req.Owner, Space: req.Space, Worker: req.Worker, Generation: req.Generation, ID: req.Lease}
	if err := s.workers.ChangeLease(ctx, l, req.Action); err != nil {
		return sv1.WorkerLeaseResp{}, fmt.Errorf("worker lease: %w", err)
	}
	return sv1.WorkerLeaseResp{OK: true}, nil
}
