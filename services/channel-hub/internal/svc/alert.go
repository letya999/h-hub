package svc

import (
	"context"
	"database/sql"
	"fmt"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/locale"
)

type alertOps struct{ s *Server }

// alert uses the normal delivery outbox so a duplicate signal sends once.
func (a *alertOps) alert(ctx context.Context, r cv1.AlertReq) (cv1.AlertResp, error) {
	body, err := alertBody(r)
	if err != nil {
		return cv1.AlertResp{}, err
	}
	id := "alert-" + r.Key
	tx, err := a.s.db.BeginTx(ctx, nil)
	if err != nil {
		return cv1.AlertResp{}, fmt.Errorf("alert begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	inserted, err := insertAlert(ctx, tx, id, body)
	if err != nil {
		return cv1.AlertResp{}, err
	}
	if inserted {
		if err := a.s.delivery.Create(ctx, tx, clock{a.s.deps.Now}, fsm.CreateReq{ID: id, Initial: "queued", Actor: "channel-hub"}); err != nil {
			return cv1.AlertResp{}, fmt.Errorf("alert machine: %w", err)
		}
		if err := a.s.delivery.Transition(ctx, tx, fsm.Request{ID: id, Event: "send", Actor: "channel-hub",
			Effects: []fsm.Effect{{Topic: "send", Payload: []byte(id)}}}, nil); err != nil {
			return cv1.AlertResp{}, fmt.Errorf("alert send: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return cv1.AlertResp{}, fmt.Errorf("alert commit: %w", err)
	}
	return cv1.AlertResp{OK: true}, nil
}

func alertBody(r cv1.AlertReq) (string, error) {
	if len(r.Key) == 0 || len(r.Key) > 80 {
		return "", herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	if r.Code != "sweep-lag" && r.Code != "watchdog-stale" {
		return "", herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	return locale.T(locale.EN, "operator."+r.Code), nil
}

func insertAlert(ctx context.Context, tx *sql.Tx, id, body string) (bool, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO deliveries(id,external_id,kind,body)
		VALUES($1,'operator','text',$2) ON CONFLICT(id) DO NOTHING`, id, body)
	if err != nil {
		return false, fmt.Errorf("alert insert: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("alert rows: %w", err)
	}
	if n == 1 {
		return true, nil
	}
	var existing string
	if err := tx.QueryRowContext(ctx, "SELECT body FROM deliveries WHERE id=$1", id).Scan(&existing); err != nil {
		return false, fmt.Errorf("alert existing: %w", err)
	}
	if existing != body {
		return false, herrors.New(herrors.CodeConflict, "err.conflict")
	}
	return false, nil
}
