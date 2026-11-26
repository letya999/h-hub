package svc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
)

const (
	maxWorkspaceBytes   = 64 << 10
	maxWorkspaceEntries = 256
	maxWorkspaceDepth   = 16
	maxWorkspacePath    = 1024
)

type workspaceAction struct {
	write bool
	run   func(*os.Root, string, sv1.WorkspaceReq) (sv1.WorkspaceResp, error)
}

var workspaceActions = map[string]workspaceAction{
	"list":   {run: listWorkspace},
	"read":   {run: readWorkspace},
	"search": {run: searchWorkspace},
	"write":  {write: true, run: replaceWorkspace},
	"edit":   {write: true, run: editWorkspace},
	"mkdir":  {write: true, run: mkdirWorkspace},
	"remove": {write: true, run: removeWorkspace},
}

func workspaceAccess(s *Server, ctx context.Context, req sv1.WorkspaceReq) (sv1.WorkspaceResp, error) {
	var out sv1.WorkspaceResp
	principal := httpapi.PrincipalFrom(ctx)
	if principal == nil || principal.Purpose != "service" || principal.Sub != "tool-hub" {
		return out, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	if req.Owner == "" || req.Space == "" || req.Digest == "" || req.Key == "" {
		return out, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	space, err := getSpace(ctx, s.db, req.Owner, req.Space)
	if err != nil {
		return out, err
	}
	if staleDigest(req.Digest, space.digest) {
		return out, herrors.ErrStale
	}
	p, err := specPassports(space)
	if err != nil {
		return out, err
	}
	root := filepath.Join(s.deps.RenderDir, workspaceStorage, space.id)
	return runWorkspaceAction(root, p.Workspace, req)
}

func runWorkspaceAction(dir string, w WorkspacePassport, req sv1.WorkspaceReq) (sv1.WorkspaceResp, error) {
	var out sv1.WorkspaceResp
	action, ok := workspaceActions[req.Operation]
	if !ok {
		return out, herrors.New(herrors.CodeInvalid, "err.invalid-arg")
	}
	if req.Operation == "list" && req.Path == "." {
		for _, node := range w.mountNodes() {
			out.Entries = append(out.Entries, strings.TrimPrefix(node.Path, "/")+"/")
		}
		out.OK = true
		return out, nil
	}
	node, rel, err := workspacePath(w, req.Path, action.write)
	if err != nil {
		return out, err
	}
	root, err := openWorkspaceNode(dir, strings.TrimPrefix(node, "/"))
	if err != nil {
		return out, err
	}
	defer root.Close() //nolint:errcheck // directory handle
	if err := refuseLinks(root, rel); err != nil {
		return out, err
	}
	out, err = action.run(root, rel, req)
	if err == nil {
		out.OK = true
	}
	return out, err
}

func openWorkspaceNode(dir, node string) (*os.Root, error) {
	workspace, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open workspace: %w", err)
	}
	defer workspace.Close() //nolint:errcheck // directory handle
	if err := refuseLinks(workspace, node); err != nil {
		return nil, err
	}
	root, err := workspace.OpenRoot(node)
	if err != nil {
		return nil, workspaceIO(err)
	}
	return root, nil
}

func workspacePath(w WorkspacePassport, input string, write bool) (node, rel string, err error) {
	if err := checkWorkspacePath(input); err != nil {
		return "", "", err
	}
	addr := "/" + input
	if write && workspaceReadOnly(addr) {
		return "", "", fileRefusal("err.workspace-read-only")
	}
	nodes := w.mountNodes()
	i := slices.IndexFunc(nodes, func(node sv1.WorkspaceNode) bool { return workspaceContains(node.Path, addr) })
	if i < 0 {
		return "", "", fileRefusal("err.workspace-outside")
	}
	chosen := nodes[i]
	if write && (chosen.Mode != "rw" || chosen.Path == addr) {
		return "", "", fileRefusal("err.workspace-read-only")
	}
	if chosen.Path == addr {
		return chosen.Path, ".", nil
	}
	return chosen.Path, strings.TrimPrefix(addr, chosen.Path+"/"), nil
}

func checkWorkspacePath(input string) error {
	if strings.HasPrefix(input, "/") {
		return fileRefusal("err.workspace-absolute")
	}
	if input == "." || !fs.ValidPath(input) || strings.ContainsAny(input, "\\\x00:") {
		return fileRefusal("err.workspace-path")
	}
	if len(input) > maxWorkspacePath || strings.Count(input, "/") >= maxWorkspaceDepth {
		return fileRefusal("err.workspace-depth")
	}
	return nil
}

func refuseLinks(root *os.Root, name string) error {
	var prefix string
	for _, part := range strings.Split(name, "/") {
		prefix = filepath.Join(prefix, part)
		info, err := root.Lstat(prefix)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return workspaceIO(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fileRefusal("err.workspace-symlink")
		}
	}
	return nil
}

func fileRefusal(key string) error {
	return herrors.New(herrors.CodeInvalid, key)
}
