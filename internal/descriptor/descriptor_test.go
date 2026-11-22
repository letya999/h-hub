package descriptor

import (
	"path/filepath"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// TestRepoDescriptor proves the committed descriptor is valid.
func TestRepoDescriptor(t *testing.T) {
	t.Parallel()
	if _, err := Load(filepath.Join(repoRoot(t), "deploy", "descriptor.yaml")); err != nil {
		t.Error(err)
	}
}

// TestBroken proves the schema ceilings by minimal violations:
// a service without resource ceilings and one with 11 env entries.
func TestBroken(t *testing.T) {
	t.Parallel()
	for _, dir := range []string{"descriptor-noceil", "descriptor-env11"} {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()
			p := filepath.Join(repoRoot(t), "testdata", "broken", dir, "deploy", "descriptor.yaml")
			if _, err := Load(p); err == nil {
				t.Errorf("Load passed on broken descriptor %s", dir)
			}
		})
	}
}
