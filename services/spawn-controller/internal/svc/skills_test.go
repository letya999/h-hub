package svc

import (
	"encoding/json"
	"os"
	"path/filepath"
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
