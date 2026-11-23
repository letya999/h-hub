package svc

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
)

func operatorScope(ctx context.Context, external, op string) error {
	c, err := idv1.ScopeFrom(ctx)
	if err != nil {
		return fmt.Errorf("operator scope: %w", err)
	}
	if c.Sub != "operator" || c.Owner != external || c.Space != "" || c.Target != op {
		return herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	return nil
}

func randomID(prefix string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("id random: %w", err)
	}
	return prefix + hex.EncodeToString(raw[:]), nil
}

func (s *Server) inject(ctx context.Context, r cv1.InjectReq) (cv1.InjectResp, error) {
	if r.External == "" || r.UpdateID == "" || r.Text == "" {
		return cv1.InjectResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	if err := operatorScope(ctx, r.External, "Inject"); err != nil {
		return cv1.InjectResp{}, err
	}
	id, err := randomID("in-")
	if err != nil {
		return cv1.InjectResp{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return cv1.InjectResp{}, fmt.Errorf("intake begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	inserted, err := insertUpdate(ctx, tx, id, r)
	if err != nil {
		return cv1.InjectResp{}, err
	}
	if !inserted {
		return s.existingUpdate(ctx, tx, r)
	}
	if err := s.intake.Create(ctx, tx, clock{s.deps.Now}, fsm.CreateReq{ID: id, Initial: "queued", Actor: "mock"}); err != nil {
		return cv1.InjectResp{}, fmt.Errorf("intake create: %w", err)
	}
	if err := s.intake.Transition(ctx, tx, fsm.Request{ID: id, Event: "accept", Actor: "mock",
		Effects: []fsm.Effect{{Topic: "dispatch", Payload: []byte(id)}}}, nil); err != nil {
		return cv1.InjectResp{}, fmt.Errorf("accept intake: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return cv1.InjectResp{}, fmt.Errorf("intake commit: %w", err)
	}
	return cv1.InjectResp{ID: id}, nil
}

func insertUpdate(ctx context.Context, tx *sql.Tx, id string, r cv1.InjectReq) (bool, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO inbound_updates(id,channel,update_id,external_id,body)
		VALUES($1,'mock',$2,$3,$4) ON CONFLICT(channel,update_id) DO NOTHING`, id, r.UpdateID, r.External, r.Text)
	if err != nil {
		return false, fmt.Errorf("intake insert: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("intake rows: %w", err)
	}
	return n > 0, nil
}

func (s *Server) existingUpdate(ctx context.Context, tx *sql.Tx, r cv1.InjectReq) (cv1.InjectResp, error) {
	var id, external, body string
	if err := tx.QueryRowContext(ctx, `SELECT id,external_id,body FROM inbound_updates WHERE channel='mock' AND update_id=$1`, r.UpdateID).Scan(&id, &external, &body); err != nil {
		return cv1.InjectResp{}, fmt.Errorf("existing update: %w", err)
	}
	if external != r.External || body != r.Text {
		return cv1.InjectResp{}, herrors.New(herrors.CodeConflict, "err.conflict")
	}
	return cv1.InjectResp{ID: id}, nil
}

func (s *Server) readOutbox(ctx context.Context, r cv1.ReadOutboxReq) (cv1.ReadOutboxResp, error) {
	if r.External == "" || r.After < 0 {
		return cv1.ReadOutboxResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	if err := operatorScope(ctx, r.External, "ReadOutbox"); err != nil {
		return cv1.ReadOutboxResp{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seq,kind,body FROM mock_outbox WHERE external_id=$1 AND seq>$2 ORDER BY seq LIMIT 100`, r.External, r.After)
	if err != nil {
		return cv1.ReadOutboxResp{}, fmt.Errorf("outbox read: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close
	out := cv1.ReadOutboxResp{Messages: []cv1.OutMessage{}}
	for rows.Next() {
		var m cv1.OutMessage
		if err := rows.Scan(&m.Seq, &m.Kind, &m.Body); err != nil {
			return out, fmt.Errorf("outbox scan: %w", err)
		}
		out.Messages = append(out.Messages, m)
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("outbox rows: %w", err)
	}
	return out, nil
}
