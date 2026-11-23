package svc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
)

const sweepLagLimit = 30 * time.Second

// alertSweepLag records one durable signal for the oldest overdue
// machine before applying its transition, so delivery can retry.
func (lp *loopRunner) alertSweepLag(ctx context.Context, now time.Time) error {
	var oldest fsm.DueRow
	for _, machine := range []*fsm.Machine{lp.s.jobM, lp.s.runM} {
		rows, err := machine.DueRows(ctx, lp.s.db, now, 1)
		if err != nil {
			return fmt.Errorf("sweep lag read: %w", err)
		}
		if len(rows) > 0 && (oldest.ID == "" || rows[0].DeadlineAt.Before(oldest.DeadlineAt)) {
			oldest = rows[0]
		}
	}
	if oldest.ID == "" || now.Sub(oldest.DeadlineAt) <= sweepLagLimit {
		return nil
	}
	return lp.queueOperatorAlert(ctx, cv1.AlertReq{
		Key: "sweep-lag-" + oldest.ID, Code: "sweep-lag"})
}

func (lp *loopRunner) queueOperatorAlert(ctx context.Context, alert cv1.AlertReq) error {
	tx, err := lp.s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("alert begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
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
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("alert commit: %w", err)
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
