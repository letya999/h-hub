package ownerreq

import (
	"context"
	"encoding/json"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	orv1 "github.com/letya999/h-hub/contracts/ownerreq/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
)

// askArgs are the argument object of the ask and choose tools.
type askArgs struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

// toolResult is everything the model may see: identity and state —
// no links, no nonces, no channel internals.
type toolResult struct {
	Request  string `json:"request_id"`
	State    string `json:"state"`
	Deadline int64  `json:"deadline"`
}

// InvokeTool runs the ownerreq toolset's two tools — ask and choose —
// for an admitted run caller. Approval is unreachable here: the
// toolset names no such tool and the claims gate refuses the kind.
func (s *Server) InvokeTool(ctx context.Context, claims idv1.RunClaims,
	name string, args json.RawMessage) (json.RawMessage, error) {
	var a askArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, herrors.Wrap(herrors.CodeInvalid, "err.invalid-arg", err)
	}
	req := orv1.CreateReq{Prompt: a.Question, Options: a.Options,
		TTLSec: int64(defaultTTL.Seconds())}
	switch name {
	case "ask":
		req.Kind = orv1.KindQuestion
	case "choose":
		req.Kind = orv1.KindChoice
	default:
		return nil, herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
											With("tool", name)
	}
	res, err := s.create(ctx, caller{run: true, claims: claims}, req)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(toolResult{Request: res.Request,
		State: res.State, Deadline: res.Deadline})
	if err != nil {
		return nil, herrors.Wrap(herrors.CodeInvalid, "err.invalid-arg", err)
	}
	return out, nil
}
