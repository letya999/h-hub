package gates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWorkflowYAML(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join(repoRoot(t), ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var workflow map[string]any
		if err := yaml.Unmarshal(body, &workflow); err != nil {
			t.Errorf("%s: %v", filepath.Base(file), err)
		}
	}
}

func TestRollbackSmokeFindsNestedJust(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, step := range workflow.Jobs["check"].Steps {
		if step.Name == "N-1 rollback smoke" {
			if !strings.Contains(step.Run, `export PATH="$RUNNER_TEMP:$PATH"`) {
				t.Fatal("nested just recipes cannot find the copied executable")
			}
			return
		}
	}
	t.Fatal("rollback smoke step is absent")
}
