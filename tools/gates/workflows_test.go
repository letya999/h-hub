package gates

import (
	"os"
	"path/filepath"
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
