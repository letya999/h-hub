package svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	hv1 "github.com/letya999/h-hub/contracts/harness/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
)

const notifyRestart = "notify-restart"
const restartIdle = "restart-idle"

var errRestartBusy = errors.New("runtime has active admissions")

func stageRestart(ctx context.Context, tx *sql.Tx, sp spaceRow) error {
	r := sv1.RestartReq{Space: sp.id, Digest: sp.digest, Key: newID("restart")}
	return queueRestart(ctx, tx, notifyRestart, r)
}

func queueRestart(ctx context.Context, tx *sql.Tx, topic string, r sv1.RestartReq) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO fsm.outbox(kind,machine_id,topic,payload)
		VALUES('space',$1,$2,$3) ON CONFLICT DO NOTHING`, r.Space, topic, mustJSON(r)); err != nil {
		return fmt.Errorf("queue restart: %w", err)
	}
	return nil
}

func acceptRestart(s *Server, ctx context.Context, r sv1.RestartReq) (sv1.RestartResp, error) {
	var out sv1.RestartResp
	if r.Space == "" || r.Digest == "" || r.Key == "" {
		return out, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, fmt.Errorf("restart begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if err := knownRestartDigest(ctx, tx, r); err != nil {
		return out, err
	}
	if err := queueRestart(ctx, tx, restartIdle, r); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, fmt.Errorf("restart commit: %w", err)
	}
	return sv1.RestartResp{OK: true}, nil
}

func knownRestartDigest(ctx context.Context, tx *sql.Tx, r sv1.RestartReq) error {
	var known bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM effective_specs WHERE
		space=$1 AND digest=$2)`, r.Space, r.Digest).Scan(&known)
	if err != nil {
		return fmt.Errorf("verify restart digest: %w", err)
	}
	if !known {
		return herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	return nil
}

func restartPending(ctx context.Context, tx *sql.Tx, space string) (bool, error) {
	var pending bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fsm.outbox
		WHERE kind='space' AND machine_id=$1 AND topic IN ($2,$3) AND done_at IS NULL)`,
		space, notifyRestart, restartIdle).Scan(&pending)
	if err != nil {
		return false, fmt.Errorf("read restart request: %w", err)
	}
	return pending, nil
}

func runRestartEffect(s *Server, ctx context.Context, f fsm.OutEffect) error {
	var r sv1.RestartReq
	if err := json.Unmarshal(f.Payload, &r); err != nil {
		return fmt.Errorf("restart payload: %w", err)
	}
	switch f.Topic {
	case notifyRestart:
		return notifyToolHub(s, ctx, r)
	case restartIdle:
		return restartWhenIdle(s, ctx, r)
	}
	return fmt.Errorf("unexpected restart effect %q", f.Topic)
}

func notifyToolHub(s *Server, ctx context.Context, r sv1.RestartReq) error {
	sp, err := getSpaceAny(ctx, s.db, r.Space)
	if herrors.CodeOf(err) == herrors.CodeNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	p, err := specPassports(sp)
	if err != nil {
		return err
	}
	c, err := restartPeer(s, ctx, "tool-hub", p.Config.ToolURL)
	if err != nil {
		return err
	}
	cl := thv1.Client{T: thv1.HTTP{C: c}}
	if _, err := cl.NotifyProjection(ctx, r); err != nil {
		return fmt.Errorf("notify tool-hub: %w", err)
	}
	return nil
}

func restartPeer(s *Server, ctx context.Context, audience, url string) (*httpapi.Client, error) {
	cl := idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(s.deps.Identity, "")}}
	token, err := cl.IssueServiceToken(ctx, s.sub, audience, newID("restart"), s.deps.SelfKey)
	if err != nil {
		return nil, fmt.Errorf("authorize restart: %w", err)
	}
	return httpapi.NewClient(url, token.Token), nil
}

func restartWhenIdle(s *Server, ctx context.Context, r sv1.RestartReq) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("restart idle begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	active, err := spaceForUpdate(ctx, tx, r.Space)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	rt, ok, err := runtimeForSpace(ctx, tx, r.Space)
	if err != nil || !ok || rt.digest == r.Digest || rt.digest == active.digest {
		return err
	}
	if err := parkForRestart(s, ctx, tx, r, rt); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("restart idle commit: %w", err)
	}
	return nil
}

func parkForRestart(s *Server, ctx context.Context, tx *sql.Tx, r sv1.RestartReq, rt runtimeRow) error {
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM fsm.machines
		WHERE kind='runtime' AND id=$1 FOR UPDATE`, rt.id).Scan(&state); err != nil {
		return fmt.Errorf("restart runtime state: %w", err)
	}
	switch state {
	case "starting":
		return errRestartBusy
	case "warm", "busy":
		call, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		client, err := restartPeer(s, call, "harness-runtime", rt.endpoint)
		if err != nil {
			return err
		}
		harness := hv1.Client{T: hv1.HTTP{C: client}}
		ack, err := harness.PrepareRestart(call, hv1.PrepareRestartReq{Space: r.Space, Key: r.Key})
		if err != nil {
			return fmt.Errorf("park runtime: %w", err)
		}
		if !ack.Idle {
			return errRestartBusy
		}
	}
	_, err := drainRuntimeTx(s, ctx, tx, r.Space, "passport")
	return err
}
