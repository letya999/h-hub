package svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	tv1 "github.com/letya999/h-hub/contracts/task/v1"
)

// Read the job, its originating run and that turn's final text in one snapshot.
// The journal can also contain late receipts recorded after the claim was released.
func (o *jobOps) getRequestTurn(ctx context.Context, r tv1.RequestTurnReq) (tv1.RequestTurnResp, error) {
	var out tv1.RequestTurnResp
	if err := r.Validate(); err != nil {
		return out, fmt.Errorf("request turn: %w", err)
	}
	var state, request, conversation, intro string
	var released bool
	var deadline sql.NullTime
	err := o.s.db.QueryRowContext(ctx, `SELECT m.state, m.deadline_at, j.owner_request,
		j.conversation, r.claim_released_at IS NOT NULL, COALESCE((
			SELECT data FROM run_events WHERE run=r.id AND kind='final-ok'
			AND at <= r.claim_released_at ORDER BY seq LIMIT 1), '')
		FROM runs r JOIN jobs j ON j.id=r.job JOIN fsm.machines m ON m.kind='job' AND m.id=j.id
		WHERE r.id=$1 AND j.id=$2`, r.Run, r.Job).
		Scan(&state, &deadline, &request, &conversation, &released, &intro)
	if errors.Is(err, sql.ErrNoRows) {
		return tv1.RequestTurnResp{Done: true}, nil
	}
	if err != nil {
		return out, fmt.Errorf("request turn: %w", err)
	}
	waiting := state == "waiting_owner" && request == r.Request
	if o.s.jobM.Terminal(state) || (released && !waiting) ||
		(waiting && !deadline.Time.After(o.s.deps.Now())) {
		return tv1.RequestTurnResp{Done: true}, nil
	}
	if waiting && released {
		out = tv1.RequestTurnResp{Conversation: conversation, Introduction: intro, Deadline: deadline.Time.Unix()}
	}
	return out, nil
}
