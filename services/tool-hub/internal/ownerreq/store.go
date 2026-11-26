package ownerreq

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	tv1 "github.com/letya999/h-hub/contracts/task/v1"
)

// request is one owner_requests row joined with its machine row.
type request struct {
	id, owner, space, channel, external string
	chat, topic, conv, job, run         string
	kind, prompt, answer                string
	options                             []string
	state                               string
	deadline                            time.Time
	turn                                tv1.RequestTurnResp
	action                              string
}

// insert writes the request row inside the create transaction.
func insert(ctx context.Context, tx *sql.Tx, r request) error {
	opts, err := json.Marshal(r.options)
	if err != nil {
		return fmt.Errorf("options marshal: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO owner_requests
		(id,owner,space,channel,external_id,chat_id,topic,conversation,
		 job,run,kind,prompt,options,action_key) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		r.id, r.owner, r.space, r.channel, r.external, r.chat, r.topic,
		r.conv, r.job, r.run, r.kind, r.prompt, opts, r.action)
	if err != nil {
		return fmt.Errorf("request insert: %w", err)
	}
	return nil
}

// load joins the row with its machine state; missing is not-found.
func (s *Server) load(ctx context.Context, id string) (request, error) {
	var r request
	var opts []byte
	var deadline sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT q.id,q.owner,q.space,q.channel,
		q.external_id,q.chat_id,q.topic,q.conversation,q.job,q.run,q.kind,
		q.prompt,q.options,q.answer,m.state,m.deadline_at FROM owner_requests q
		JOIN fsm.machines m ON m.kind='owner_request' AND m.id=q.id
		WHERE q.id=$1`, id).Scan(&r.id, &r.owner, &r.space, &r.channel,
		&r.external, &r.chat, &r.topic, &r.conv, &r.job, &r.run, &r.kind,
		&r.prompt, &opts, &r.answer, &r.state, &deadline)
	if err == sql.ErrNoRows {
		return r, errRequestNotFound()
	}
	if err != nil {
		return r, fmt.Errorf("request read: %w", err)
	}
	if err := json.Unmarshal(opts, &r.options); err != nil {
		return r, fmt.Errorf("request options: %w", err)
	}
	r.deadline = deadline.Time
	return r, nil
}

// recordAnswer stores the canonical answer inside the transition tx.
func recordAnswer(ctx context.Context, tx *sql.Tx, id, answer, by string) error {
	_, err := tx.ExecContext(ctx, `UPDATE owner_requests
		SET answer=$2, answered_by=$3 WHERE id=$1`, id, answer, by)
	if err != nil {
		return fmt.Errorf("request answer: %w", err)
	}
	return nil
}

// setDelivery records the channel delivery id once the outbox
// effect lands it.
func (s *Server) setDelivery(ctx context.Context, id, delivery string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE owner_requests SET delivery_id=$2 WHERE id=$1`, id, delivery)
	if err != nil {
		return fmt.Errorf("request delivery: %w", err)
	}
	return nil
}

// armDeadline re-arms the machine deadline to the request's own TTL
// inside the create transaction.
func armDeadline(ctx context.Context, tx *sql.Tx, id string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE fsm.machines SET deadline_at=$2
		WHERE kind='owner_request' AND id=$1 AND state='pending'`, id, at)
	if err != nil {
		return fmt.Errorf("request deadline: %w", err)
	}
	return nil
}
