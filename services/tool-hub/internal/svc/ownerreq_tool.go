package svc

import (
	"context"
	"encoding/json"
	"fmt"

	orv1 "github.com/letya999/h-hub/contracts/ownerreq/v1"
	"github.com/letya999/h-hub/services/tool-hub/internal/ownerreq"
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

func invokeOwnerRequest(ctx context.Context, s *Server, call toolCall) (json.RawMessage, error) {
	var a askArgs
	if err := json.Unmarshal(call.args, &a); err != nil {
		return nil, herrors.Wrap(herrors.CodeInvalid, "err.invalid-arg", err)
	}
	req := orv1.CreateReq{Prompt: a.Question, Options: a.Options}
	switch call.name {
	case "ask":
		req.Kind = orv1.KindQuestion
	case "choose":
		req.Kind = orv1.KindChoice
	default:
		return nil, herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
											With("tool", call.name)
	}
	res, err := ownerreq.CreateRunRequest(s.oreq, ctx, call.claims, req)
	if err != nil {
		return nil, fmt.Errorf("owner request: %w", err)
	}
	return ownerRequestResult(res)
}

func ownerRequestResult(res orv1.CreateResp) (json.RawMessage, error) {
	out, err := json.Marshal(toolResult{Request: res.Request,
		State: res.State, Deadline: res.Deadline})
	if err != nil {
		return nil, herrors.Wrap(herrors.CodeInvalid, "err.invalid-arg", err)
	}
	return out, nil
}
