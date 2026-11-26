package svc

import (
	"context"
	"encoding/json"
	"fmt"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
)

func (s *Server) invokeConfigEdit(ctx context.Context, c toolCall) (json.RawMessage, error) {
	var args struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(c.args, &args); err != nil {
		return nil, herrors.Wrap(herrors.CodeInvalid, "err.invalid-arg", err)
	}
	cl, err := s.spawnClient(ctx)
	if err != nil {
		return nil, err
	}
	r, err := cl.EditConfig(ctx, sv1.EditConfigReq{Owner: c.claims.Owner, Space: c.claims.Space,
		Digest: c.specDigest, Key: c.receipt.ID, Setting: args.Key, Value: args.Value})
	if err != nil {
		return nil, fmt.Errorf("edit config: %w", err)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("config edit result: %w", err)
	}
	return raw, nil
}
