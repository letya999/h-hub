package svc

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	"github.com/letya999/h-hub/services/spawn-controller/internal/backend"
	herrors "github.com/letya999/h-hub/shared/errors"
)

const workspaceStorage = "workspaces"

var reservedWorkspaceRoots = []string{"/config", "/inbox", "/vault", ownSkillRoot}

func workspaceContains(root, file string) bool {
	return file == root || strings.HasPrefix(file, root+"/")
}

func workspaceReadOnly(file string) bool {
	return slices.ContainsFunc(reservedWorkspaceRoots, func(root string) bool {
		return workspaceContains(root, file)
	})
}

func (w WorkspacePassport) validateLayout() error {
	for i, node := range w.Layout.Nodes {
		if !validWorkspaceNode(node) || slices.ContainsFunc(w.Layout.Nodes[:i], func(other sv1.WorkspaceNode) bool {
			return workspaceContains(other.Path, node.Path) || workspaceContains(node.Path, other.Path)
		}) {
			return invalidWorkspaceNode(node.Path)
		}
	}
	for _, writable := range w.Writable {
		if !slices.ContainsFunc(w.Layout.Nodes, func(node sv1.WorkspaceNode) bool {
			return node.Path == writable && node.Mode == "rw"
		}) {
			return invalidWorkspaceNode(writable)
		}
	}
	return nil
}

func validWorkspaceNode(node sv1.WorkspaceNode) bool {
	rel, absolute := strings.CutPrefix(node.Path, "/")
	if !absolute || rel == "." || !fs.ValidPath(rel) || strings.ContainsAny(rel, "\\\x00:") {
		return false
	}
	switch node.Mode {
	case "", "ro":
		return true
	case "rw":
		return !workspaceReadOnly(node.Path)
	}
	return false
}

func invalidWorkspaceNode(node string) error {
	return herrors.New(herrors.CodeInvalid, "err.workspace-layout").With("path", node) //nolint:wrapcheck // shared error form
}

// mountNodes resolves modes once for the container, explain and files.
func (w WorkspacePassport) mountNodes() []sv1.WorkspaceNode {
	nodes := slices.Clone(w.Layout.Nodes)
	for i, node := range nodes {
		if node.Mode != "rw" || !slices.Contains(w.Writable, node.Path) || workspaceReadOnly(node.Path) {
			nodes[i].Mode = "ro"
		}
	}
	slices.SortFunc(nodes, func(a, b sv1.WorkspaceNode) int { return strings.Compare(a.Path, b.Path) })
	return nodes
}

func (w WorkspacePassport) tree() []sv1.WorkspaceBranch {
	var branches []sv1.WorkspaceBranch
	for _, node := range w.mountNodes() {
		branches = addWorkspaceNode(branches, node, "")
	}
	return branches
}

func addWorkspaceNode(branches []sv1.WorkspaceBranch, node sv1.WorkspaceNode, prefix string) []sv1.WorkspaceBranch {
	next, _, _ := strings.Cut(strings.TrimPrefix(node.Path, prefix+"/"), "/")
	current := prefix + "/" + next
	index := slices.IndexFunc(branches, func(branch sv1.WorkspaceBranch) bool { return branch.Path == current })
	if index < 0 {
		branches = append(branches, sv1.WorkspaceBranch{WorkspaceNode: sv1.WorkspaceNode{Path: current}})
		index = len(branches) - 1
	}
	if current == node.Path {
		branches[index].WorkspaceNode = node
	} else {
		branches[index].Children = addWorkspaceNode(branches[index].Children, node, current)
	}
	return branches
}

func prepareWorkspace(dir, space string, w WorkspacePassport) ([]backend.WorkspaceMount, error) {
	if err := w.validateLayout(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("workspace root: %w", err)
	}
	defer root.Close() //nolint:errcheck // directory handle
	var mounts []backend.WorkspaceMount
	for _, node := range w.mountNodes() {
		source := path.Join(workspaceStorage, space, node.Path)
		if err := root.MkdirAll(source, 0o755); err != nil {
			return nil, fmt.Errorf("workspace mount %s: %w", node.Path, err)
		}
		if node.Mode == "rw" {
			if err := root.Chmod(source, 0o777); err != nil {
				return nil, fmt.Errorf("workspace write mode %s: %w", node.Path, err)
			}
		}
		mounts = append(mounts, backend.WorkspaceMount{Subpath: source, Path: node.Path, ReadOnly: node.Mode != "rw"})
	}
	return mounts, nil
}
