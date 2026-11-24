// Package proof runs the external gate tools on their broken examples
// and asserts they fail (phase brief: every gate is proven by a falling
// example, not just configured).
package proof

import (
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

// TestGitleaksBroken proves gitleaks finds a planted test secret.
// The content is fixed — a random draw lands below the detector's
// entropy threshold roughly one run in ten — and assembled in
// chunks so the literal never sits in git history.
func TestGitleaksBroken(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	secret := "x9K2mV8p" + "Q4rT7wZ1" + "bN5hJ3fG" + "6sD0aL9c" + "E2uY4iO"
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"),
		[]byte("aws_access_key_id = AKIAQ3W5E7R9T1Y6U3W5\n"+
			"aws_secret_access_key = "+secret+"\n"), 0600); err != nil {
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

// TestGoModulesOffline proves the dev image can build without runtime DNS.
func TestGoModulesOffline(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "list", "-deps", "./...")
	cmd.Dir = root(t)
	cmd.Env = append(cmd.Environ(), "GOPROXY=off", "GOSUMDB=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Go modules unavailable offline: %v\n%s", err, out)
	}
}
