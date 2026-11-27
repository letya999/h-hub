package svc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"time"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	wv1 "github.com/letya999/h-hub/contracts/worker/v1"
	"github.com/letya999/h-hub/services/tool-hub/internal/ownerreq"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/locale"
)

type terminalCall struct {
	id, digest string
	req        wv1.ExecReq
}

func terminalArguments(c toolCall) (terminalCall, error) {
	var out terminalCall
	if c.key == "" || len(c.key) > 256 {
		return out, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	req, err := wv1.Decode(bytes.NewReader(c.args))
	if err != nil {
		return out, fmt.Errorf("terminal arguments: %w", err)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return out, fmt.Errorf("encode terminal arguments: %w", err)
	}
	if len(raw) > wv1.MaxBody {
		return out, herrors.New(herrors.CodeInvalid, "err.invalid-arg")
	}
	return terminalCall{id: fmt.Sprintf("terminal-%x", sha256.Sum256([]byte(c.claims.Run+"\x00"+c.key))),
		digest: fmt.Sprintf("%x", sha256.Sum256(raw)), req: req}, nil
}

func invokeTerminal(s *Server, ctx context.Context, c toolCall) (json.RawMessage, error) {
	call, err := terminalArguments(c)
	if err != nil {
		return nil, err
	}
	if result, found, err := terminalResult(s, ctx, call); found || err != nil {
		return result, err
	}
	if pending, err := terminalApproval(s, ctx, c, call); pending != nil || err != nil {
		return pending, err
	}
	controller, err := s.spawnClient(ctx)
	if err != nil {
		return nil, err
	}
	worker, err := claimTerminal(ctx, controller, sv1.ClaimWorkerReq{
		Owner: c.claims.Owner, Space: c.claims.Space, Digest: c.specDigest, Profile: "terminal", Key: call.id,
	})
	if err != nil {
		return nil, err
	}
	if accepted, err := beginTerminal(s, ctx, call); err != nil || !accepted {
		if err != nil {
			return nil, err
		}
		return terminalReplay(s, ctx, call)
	}
	lease := sv1.WorkerLeaseReq{Owner: c.claims.Owner, Space: c.claims.Space, Worker: worker.Worker,
		Generation: worker.Generation, Lease: worker.Lease}
	return runTerminal(s, ctx, call, terminalWorker{controller: controller, lease: lease, worker: worker})
}

func terminalApproval(s *Server, ctx context.Context, c toolCall, call terminalCall) (json.RawMessage, error) {
	prompt := locale.T(locale.EN, "tool.terminal.approve", "program", call.req.Argv[0], "path", path.Clean(call.req.Workdir))
	pending, err := ownerreq.ApproveAction(s.oreq, ctx, c.claims, call.digest, prompt)
	if err != nil {
		return nil, fmt.Errorf("terminal approval: %w", err)
	}
	if pending.State == "pending" {
		return ownerRequestResult(pending)
	}
	return nil, nil
}

func claimTerminal(ctx context.Context, controller *sv1.Client, req sv1.ClaimWorkerReq) (sv1.ClaimWorkerResp, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	wait := time.NewTicker(50 * time.Millisecond)
	defer wait.Stop()
	for {
		res, err := controller.ClaimWorker(ctx, req)
		if err != nil {
			return res, fmt.Errorf("claim terminal: %w", err)
		}
		if res.Ready {
			if res.SocketSlot <= 0 || res.Token == "" {
				return res, herrors.New(herrors.CodeUnavailable, "err.unavailable")
			}
			return res, nil
		}
		select {
		case <-ctx.Done():
			return res, fmt.Errorf("await terminal readiness: %w", ctx.Err())
		case <-wait.C:
		}
	}
}

type terminalWorker struct {
	controller *sv1.Client
	lease      sv1.WorkerLeaseReq
	worker     sv1.ClaimWorkerResp
}

func runTerminal(s *Server, ctx context.Context, call terminalCall, target terminalWorker) (json.RawMessage, error) {
	raw, runErr := callTerminal(ctx, call, target.worker)
	action := sv1.ReleaseWorkerLease
	if herrors.CodeOf(runErr) == herrors.CodeUnavailable && runErr != nil {
		runErr = terminalUncertain(runErr)
		action = sv1.RetireWorkerLease
	}
	record, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	resultErr := finishTerminal(s, record, call, raw, runErr)
	target.lease.Action = action
	if _, err := target.controller.WorkerLease(record, target.lease); err != nil && s.l != nil {
		s.l.Error(record, "settle terminal worker lease", err)
	}
	if resultErr != nil {
		return nil, terminalUncertain(resultErr)
	}
	return raw, runErr
}

func callTerminal(ctx context.Context, call terminalCall, worker sv1.ClaimWorkerResp) (json.RawMessage, error) {
	peer := wv1.NewHTTPClient(wv1.SocketPath(worker.SocketSlot), worker.Token,
		time.Duration(call.req.TimeoutMS)*time.Millisecond+3*time.Second)
	var res wv1.ExecResp
	if err := peer.DoLimit(ctx, httpapi.Call{Method: http.MethodPost, Path: wv1.ExecPath,
		Req: call.req, Resp: &res, Key: call.id, Limit: 256 << 10}); err != nil {
		return nil, fmt.Errorf("execute terminal command: %w", err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return nil, fmt.Errorf("encode terminal result: %w", err)
	}
	return raw, nil
}

func terminalUncertain(cause error) error {
	err := herrors.Wrap(herrors.CodeUncertain, "err.terminal-uncertain", cause)
	err.Retryable = false
	return err
}
