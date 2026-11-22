package gates

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironRejected(t *testing.T) {
	t.Parallel()
	err := EnvDeclared(filepath.Join(repoRoot(t), brokenPrefix, "env-environ"))
	if err == nil || !strings.Contains(err.Error(), "calls os.Environ outside config") {
		t.Fatalf("unexpected gate result: %v", err)
	}
}

// TestEnvRe pins the env-declared gate to environment *reads*: a name
// a service writes into a child container's env list is not a read
// and must not require a descriptor declaration.
func TestEnvRe(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		src   string
		match bool
	}{
		{"getenv", `os.Getenv("HHUB_DSN")`, true},
		{"lookupenv", `os.LookupEnv("HHUB_ADDR")`, true},
		{"env write", `"HHUB_CONFIG=/render/config.json"`, false},
		{"env kv write", `[]string{"HHUB_ADDR=:9000"}`, false},
		{"non hhub", `os.Getenv("HOME")`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := envRe.MatchString(c.src); got != c.match {
				t.Errorf("envRe.MatchString(%q) = %v, want %v", c.src, got, c.match)
			}
		})
	}
}
