package svc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	"github.com/letya999/h-hub/services/tool-hub/internal/ownerreq"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/locale"
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
	if slices.Contains(c.receipt.Caps, "workspace.write") {
		pending, err := s.fileApproval(ctx, c, a)
		if err != nil || pending != nil {
			return pending, err
		}
	}
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

func (s *Server) fileApproval(ctx context.Context, c toolCall, a fileArguments) (json.RawMessage, error) {
	canonical, err := json.Marshal(a)
	if err != nil {
		return nil, fmt.Errorf("file arguments: %w", err)
	}
	id := sha256.Sum256(append([]byte(c.name+"\x00"), canonical...))
	prompt := locale.T(locale.EN, "tool.files.approve", "tool", c.name, "path", a.Path)
	pending, err := ownerreq.ApproveAction(s.oreq, ctx, c.claims, hex.EncodeToString(id[:]), prompt)
	if err != nil {
		return nil, fmt.Errorf("file approval: %w", err)
	}
	if pending.State == "pending" {
		return ownerRequestResult(pending)
	}
	return nil, nil
}
