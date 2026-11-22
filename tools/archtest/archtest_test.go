package archtest

import (
	"path/filepath"
	"testing"
)

func TestRepo(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(root); err != nil {
		t.Error(err)
	}
}

// TestBroken proves the AST ceilings on minimal violations.
func TestBroken(t *testing.T) {
	t.Parallel()
	for _, dir := range []string{"archtest-methods", "archtest-fields"} {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()
			root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "broken", dir))
			if err != nil {
				t.Fatal(err)
			}
			if err := Check(root); err == nil {
				t.Errorf("Check passed on broken example %s", dir)
			}
		})
	}
}
