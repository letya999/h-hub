package svc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
)

type fileArguments struct {
	Path   string `json:"path"`
	Text   string `json:"text"`
	Before string `json:"before"`
	Query  string `json:"query"`
}

func (s *Server) invokeFiles(ctx context.Context, c toolCall) (json.RawMessage, error) {
	a, err := decodeFileArguments(c.args)
	if err != nil {
		return nil, err
	}
	op, _ := strings.CutPrefix(c.name, "files_")
	cl, err := s.spawnClient(ctx)
	if err != nil {
		return nil, err
	}
	result, err := cl.Workspace(ctx, sv1.WorkspaceReq{
		Owner: c.claims.Owner, Space: c.claims.Space, Digest: c.specDigest, Key: c.receipt.ID,
		Operation: op, Path: a.Path, Text: a.Text, Before: a.Before, Query: a.Query,
	})
	if err != nil {
		return nil, fmt.Errorf("workspace %s: %w", op, err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("workspace result: %w", err)
	}
	return raw, nil
}

func decodeFileArguments(raw json.RawMessage) (fileArguments, error) {
	var a fileArguments
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return a, herrors.Wrap(herrors.CodeInvalid, "err.invalid-arg", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return a, herrors.New(herrors.CodeInvalid, "err.invalid-arg")
	}
	return a, nil
}
