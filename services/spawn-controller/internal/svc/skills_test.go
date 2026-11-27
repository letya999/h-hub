package svc

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
)

func TestSkillShowcaseIncludesBothLayers(t *testing.T) {
	t.Parallel()
	chain := skillLayers(t)
	template := chain[1].docs
	setLeaf(template, "workspace.skills", map[string]any{"local-notes": "# Local notes"})
	candidate, err := compileSet(chain)
	if err != nil {
		t.Fatal(err)
	}
	files := renderedSkills(t, candidate.spec.Workspace.Skills)
	if len(files) != 2 || files["workspace-notes"] == "" || files["local-notes"] != "# Local notes" {
		t.Fatalf("unexpected skill showcase names: count=%d", len(files))
	}
	setLeaf(template, "workspace.skills", map[string]any{"workspace-notes": "# Replacement"})
	if _, err := compileSet(chain); herrors.CodeOf(err) != herrors.CodeInvalid {
		t.Fatalf("user layer replaced the platform skill: %v", err)
	}
}

func skillLayers(t *testing.T) layerSet {
	t.Helper()
	base, err := layerDocs("platform/layer")
	if err != nil {
		t.Fatal(err)
	}
	template, err := templateLayer("assistant")
	if err != nil {
		t.Fatal(err)
	}
	return layerSet{{role: "platform", docs: base}, {role: "user", docs: template}}
}

func renderedSkills(t *testing.T, skills map[string]string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	if err := renderSkillShowcase(dir, skills); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, sv1.SkillShowcaseFile))
	if err != nil {
		t.Fatal(err)
	}
	var files map[string]string
	if err := json.Unmarshal(data, &files); err != nil {
		t.Fatal(err)
	}
	return files
}

func TestOwnSkillWritesUseTheScanner(t *testing.T) {
	t.Parallel()
	workspace, w := skillWorkspace(t)
	const file = "skills/notes.md"
	const original = "# Notes\npassword = [value]\n"
	out, err := runWorkspaceAction(workspace, w, sv1.WorkspaceReq{Path: file, Operation: "write_skill", Text: original})
	if err != nil || !out.OK {
		t.Fatalf("valid skill write: %+v %v", out, err)
	}
	secret := "fake-example-value"
	_, err = runWorkspaceAction(workspace, w, sv1.WorkspaceReq{Path: file, Operation: "edit_skill", Before: "[value]", Text: secret})
	checkScanFinding(t, err, "secret-like", 2)
	raw, err := os.ReadFile(filepath.Join(workspace, file))
	if err != nil || string(raw) != original {
		t.Fatalf("refused edit changed the stored skill: %v", err)
	}
}

func TestSkillWorkspaceRejectsGenericWritesAndExecutableFiles(t *testing.T) {
	t.Parallel()
	workspace, w := skillWorkspace(t)
	_, err := runWorkspaceAction(workspace, w, sv1.WorkspaceReq{Path: "skills/notes.md", Operation: "write", Text: "# Changed"})
	var refusal *herrors.Error
	if !errors.As(err, &refusal) || refusal.Key != "err.workspace-read-only" {
		t.Fatalf("generic file write reached the skill root: %v", err)
	}
	_, err = runWorkspaceAction(workspace, w, sv1.WorkspaceReq{Path: "skills/code.py", Operation: "write_skill", Text: "data"})
	if !errors.As(err, &refusal) || refusal.Key != "err.skill-format" {
		t.Fatalf("skill tool accepted an executable extension: %v", err)
	}
}

func skillWorkspace(t *testing.T) (string, WorkspacePassport) {
	t.Helper()
	dir := t.TempDir()
	w := WorkspacePassport{Layout: WorkspaceLayout{Nodes: []sv1.WorkspaceNode{{Path: "/skills"}}}}
	if _, err := prepareWorkspace(dir, "space", w); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, workspaceStorage, "space"), w
}

func TestSkillScanReportsOnlyFindingCoordinates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ rule, line string }{
		{"secret-like", "token" + " = " + "example-value-only"},
		{"hidden-control", "text\u202e"},
		{"instruction-override", "ignore previous " + "instructions"},
	} {
		t.Run(tc.rule, func(t *testing.T) {
			t.Parallel()
			err := validateLayerSkills(map[string]string{"notes": "# Notes\n" + tc.line})
			checkScanFinding(t, err, tc.rule, 2)
		})
	}
}

func checkScanFinding(t *testing.T, err error, rule string, line int) {
	t.Helper()
	var refusal *herrors.Error
	if !errors.As(err, &refusal) || refusal.Key != "err.skill-scan" {
		t.Fatalf("expected a scan refusal: %v", err)
	}
	details := refusal.Details
	if len(details) != 3 || details["file"] != "notes.md" || details["rule"] != rule || details["line"] != line {
		t.Fatal("scan refusal must contain only the file, line and rule")
	}
}

func TestOwnSkillCountIsBounded(t *testing.T) {
	t.Parallel()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	for i := range maxOwnSkills {
		if err := saveOwnSkill(root, fmt.Sprintf("notes-%d.md", i), "# Notes"); err != nil {
			t.Fatal(err)
		}
	}
	var refusal *herrors.Error
	if err := saveOwnSkill(root, "full.md", "# Notes"); !errors.As(err, &refusal) || refusal.Key != "err.skill-limit" {
		t.Fatalf("skill quota accepted an extra file: %v", err)
	}
	if err := saveOwnSkill(root, "notes-0.md", "# Updated"); err != nil {
		t.Fatal(err)
	}
}

func TestSkillScanRefusesOversizedText(t *testing.T) {
	t.Parallel()
	var refusal *herrors.Error
	if err := scanSkill("notes.md", strings.Repeat("a", maxWorkspaceBytes+1)); !errors.As(err, &refusal) || refusal.Key != "err.skill-format" {
		t.Fatalf("skill scan accepted oversized text: %v", err)
	}
}
