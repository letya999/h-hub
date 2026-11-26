package svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
)

const sweepLagLimit = 30 * time.Second

// alertSweepLag claims the oldest deadline and records the signal in
// the same transaction as the deadline transition.
func (lp *loopRunner) alertSweepLag(ctx context.Context, now time.Time) error {
	tx, err := lp.s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sweep lag begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	var kind string
	r := fsm.Request{At: now}
	err = tx.QueryRowContext(ctx, `SELECT kind,id,deadline_event,deadline_actor
		FROM fsm.machines WHERE kind IN ($1,$2) AND deadline_at < $3
		ORDER BY deadline_at,kind,id LIMIT 1 FOR UPDATE`,
		lp.s.jobM.Kind(), lp.s.runM.Kind(), now.Add(-sweepLagLimit)).
		Scan(&kind, &r.ID, &r.Event, &r.Actor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("sweep lag claim: %w", err)
	}
	machine, hook := lp.s.jobM, lp.jobSweepHook
	if kind == lp.s.runM.Kind() {
		machine, hook = lp.s.runM, lp.runSweepHook
	}
	if err := machine.Transition(ctx, tx, r, hook); err != nil {
		return fmt.Errorf("sweep lag transition: %w", err)
	}
	if err := queueOperatorAlert(ctx, tx, cv1.AlertReq{Key: "sweep-lag-" + r.ID, Code: "sweep-lag"}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sweep lag commit: %w", err)
	}
	return nil
}

func queueOperatorAlert(ctx context.Context, tx *sql.Tx, alert cv1.AlertReq) error {
	res, err := tx.ExecContext(ctx, `INSERT INTO operator_alerts(key,code)
		VALUES($1,$2) ON CONFLICT(key) DO NOTHING`, alert.Key, alert.Code)
	if err != nil {
		return fmt.Errorf("alert intent: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("alert rows: %w", err)
	}
	if n == 1 {
		payload, err := json.Marshal(alert)
		if err != nil {
			return fmt.Errorf("alert payload: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO fsm.outbox
			(kind,machine_id,topic,payload) VALUES('alert',$1,'operator-alert',$2)`, alert.Key, payload); err != nil {
			return fmt.Errorf("alert outbox: %w", err)
		}
	}
	return nil
}

func (lp *loopRunner) deliverOperatorAlert(ctx context.Context, payload []byte) error {
	var alert cv1.AlertReq
	if err := json.Unmarshal(payload, &alert); err != nil {
		return fmt.Errorf("alert decode: %w", err)
	}
	tok, err := lp.s.toks.Token(ctx, "channel-hub")
	if err != nil {
		return err
	}
	client := &cv1.Client{T: cv1.HTTP{C: httpapi.NewClient(lp.s.deps.ChannelURL, tok)}}
	if _, err := client.Alert(ctx, alert); err != nil {
		return fmt.Errorf("deliver alert: %w", err)
	}
	return nil
}
