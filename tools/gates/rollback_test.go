package gates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRollbackImagePin(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "nminus1-image.ref"))
	if err != nil {
		t.Fatal(err)
	}
	ref := strings.TrimSpace(string(data))
	valid := regexp.MustCompile(`^ghcr\.io/letya999/h-hub/identity-hub@sha256:[a-f0-9]{64}$`)
	if !valid.MatchString(ref) {
		t.Fatalf("rollback image must be pinned by GHCR digest: %q", ref)
	}
	const broken = "5904994411d8b9429b33d7fd0cde5f008d9b41d3543e135b19a56bb5b0372b00"
	if strings.HasSuffix(ref, broken) {
		t.Fatal("v0.0.4 image rejects the 0.0.5 migration")
	}
}
