// Package proof runs the external gate tools on their broken examples
// and asserts they fail (phase brief: every gate is proven by a falling
// example, not just configured).
package proof

import (
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func root(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// run executes cmd in dir and reports whether it failed.
func run(t *testing.T, dir string, name string, args ...string) (string, bool) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err != nil
}

// TestGolangciBroken proves funlen/revive/forbidigo thresholds fire on
// the fixture module using the repo's .golangci.yml.
func TestGolangciBroken(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(root(t), "testdata", "broken", "golangci")
	out, failed := run(t, dir, "golangci-lint", "run", "--config",
		filepath.Join(root(t), ".golangci.yml"), "./...")
	if !failed {
		t.Fatalf("golangci-lint passed on broken example:\n%s", out)
	}
	for _, want := range []string{"funlen", "forbidigo", "revive"} {
		if !strings.Contains(out, want) {
			t.Errorf("golangci output missing %s:\n%s", want, out)
		}
	}
}

// TestDepguardBroken proves depguard denies a banned import.
func TestDepguardBroken(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(root(t), "testdata", "broken", "depguard")
	out, failed := run(t, dir, "golangci-lint", "run", "./...")
	if !failed || !strings.Contains(out, "depguard") {
		t.Fatalf("depguard did not fail on banned import:\n%s", out)
	}
}

// TestGitleaksBroken proves gitleaks finds a generated test secret.
func TestGitleaksBroken(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for i := range key {
		key[i] = alphabet[int(key[i])%len(alphabet)]
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"),
		[]byte("AWS_ACCESS_KEY_ID=AKIA"+string(key)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, failed := run(t, dir, "gitleaks", "dir", "--redact", ".")
	if !failed {
		t.Fatalf("gitleaks passed on generated secret:\n%s", out)
	}
}

// TestGitleaksRepo asserts the real repo history is clean.
func TestGitleaksRepo(t *testing.T) {
	t.Parallel()
	out, failed := run(t, root(t), "gitleaks", "git", ".")
	if failed {
		t.Fatalf("gitleaks found secrets in repo history:\n%s", out)
	}
}
