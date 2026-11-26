package svc

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
)

func TestWorkspaceWriteRefusesUnsafePaths(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, path, reason string }{
		{"traversal", "files/../out", "err.workspace-path"},
		{"absolute", "/tmp/out", "err.workspace-absolute"},
		{"symlink", "files/link/out", "err.workspace-symlink"},
		{"reserved", "inbox/out", "err.workspace-read-only"},
		{"unlisted", "other/out", "err.workspace-outside"},
		{"size", "files/large", "err.workspace-size"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, w := fileWorkspace(t)
			if err := os.Symlink(t.TempDir(), filepath.Join(dir, "files/link")); err != nil {
				t.Fatal(err)
			}
			text := "attempt"
			if tc.name == "size" {
				text = strings.Repeat("a", maxWorkspaceBytes+1)
			}
			_, err := runWorkspaceAction(dir, w, sv1.WorkspaceReq{Path: tc.path, Operation: "write", Text: text})
			var rejected *herrors.Error
			if !errors.As(err, &rejected) || rejected.Key != tc.reason {
				t.Fatalf("write %q: got %v, want %s", tc.path, err, tc.reason)
			}
		})
	}
}

func TestWorkspaceWriteRollsBackFailedReplacement(t *testing.T) {
	t.Parallel()
	dir, w := fileWorkspace(t)
	p := filepath.Join(dir, "files/target")
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := runWorkspaceAction(dir, w, sv1.WorkspaceReq{Path: "files/target", Operation: "write", Text: "new"})
	if err == nil {
		t.Fatal("replacing a directory succeeded")
	}
	entries, err := os.ReadDir(filepath.Join(dir, "files"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "target" || !entries[0].IsDir() {
		t.Fatalf("failed write left unexpected files: %v %v", entries, err)
	}
}

func fileWorkspace(t *testing.T) (string, WorkspacePassport) {
	t.Helper()
	dir := t.TempDir()
	w := WorkspacePassport{Layout: WorkspaceLayout{Nodes: []sv1.WorkspaceNode{
		{Path: "/files", Mode: "rw"}, {Path: "/inbox"},
	}}, Writable: []string{"/files"}}
	for _, node := range w.Layout.Nodes {
		if err := os.Mkdir(filepath.Join(dir, node.Path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir, w
}
