package ownerreq

import (
	"context"
	"fmt"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	orv1 "github.com/letya999/h-hub/contracts/ownerreq/v1"
)

// ApproveAction is used by trusted tool implementations after admission.
// A human approval authorizes one identical action when its job resumes.
func ApproveAction(s *Server, ctx context.Context, claims idv1.RunClaims, action, prompt string) (orv1.CreateResp, error) {
	used, err := consumeAction(s, ctx, claims, action)
	if err != nil || used {
		return orv1.CreateResp{}, err
	}
	return s.create(ctx, caller{run: true, claims: claims, action: action}, orv1.CreateReq{
		Kind: orv1.KindApproval, Prompt: prompt, TTLSec: int64(defaultTTL.Seconds()),
	})
}

func consumeAction(s *Server, ctx context.Context, claims idv1.RunClaims, action string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE owner_requests q SET action_used_at=$1
		FROM fsm.machines m WHERE m.kind='owner_request' AND m.id=q.id AND m.state='answered'
		AND q.owner=$2 AND q.space=$3 AND q.job=$4 AND q.action_key=$5
		AND q.action_used_at IS NULL AND q.answer='yes' AND q.answered_by NOT LIKE 'mode:%'`,
		s.deps.Now(), claims.Owner, claims.Space, claims.Job, action)
	if err != nil {
		return false, fmt.Errorf("consume tool approval: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("approval rows: %w", err)
	}
	return n > 0, nil
}
