package svc

import (
	"os"
	"path/filepath"
	"testing"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
)

func TestWorkspaceLayoutRejectsReservedWrites(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"/config", "/inbox/archive", "/vault", "/skills"} {
		base, err := layerDocs("platform/layer")
		if err != nil {
			t.Fatal(err)
		}
		setLeaf(base, "workspace.layout.nodes", []sv1.WorkspaceNode{{Path: target, Mode: "rw"}})
		setLeaf(base, "workspace.writable", []string{target})
		if _, err := compileSet(layerSet{{role: "platform", docs: base}}); herrors.CodeOf(err) != herrors.CodeInvalid {
			t.Fatalf("reserved path became writable %s: %v", target, err)
		}
	}
}

func TestWorkspaceMountModesDriveExplain(t *testing.T) {
	t.Parallel()
	w := WorkspacePassport{Layout: WorkspaceLayout{Nodes: []sv1.WorkspaceNode{
		{Path: "/docs/inbox", Mode: "rw"},
		{Path: "/docs/out", Mode: "rw"},
	}}, Writable: []string{"/docs/out"}}
	dir := t.TempDir()
	mounts, err := prepareWorkspace(dir, "space-1", w)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 || !mounts[0].ReadOnly || mounts[1].ReadOnly {
		t.Fatalf("unexpected workspace mounts: %+v", mounts)
	}
	for _, mount := range mounts {
		info, err := os.Stat(filepath.Join(dir, mount.Subpath))
		if err != nil || !info.IsDir() {
			t.Fatalf("mount directory missing for %s: %v", mount.Path, err)
		}
	}
	checkWorkspaceTree(t, w.tree())
}

func checkWorkspaceTree(t *testing.T, branches []sv1.WorkspaceBranch) {
	t.Helper()
	if len(branches) != 1 || branches[0].Path != "/docs" || len(branches[0].Children) != 2 {
		t.Fatalf("unexpected explain tree: %+v", branches)
	}
	children := branches[0].Children
	if children[0].Mode != "ro" || children[1].Mode != "rw" {
		t.Fatalf("explain reported inconsistent modes: %+v", children)
	}
}
