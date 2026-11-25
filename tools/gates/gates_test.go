package gates

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

// TestRepo runs every gate on the repository: all must pass.
func TestRepo(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, g := range All {
		t.Run(g.Name, func(t *testing.T) {
			t.Parallel()
			if err := g.Run(root); err != nil {
				t.Error(err)
			}
		})
	}
}

// broken is the gate -> fixture-dir map. Every gate has a minimal
// violation under testdata/broken/<gate> that must fail it.
var broken = map[string][]string{
	"json-state":      {"json-state"},
	"banned-words":    {"banned-words"},
	"windows-paths":   {"windows-paths"},
	"msys-dirs":       {"msys-dirs"},
	"env-declared":    {"env-declared", "env-environ"},
	"env-no-secrets":  {"env-secrets-compose", "env-secrets-file"},
	"passport-schema": {"passport-schema"},
	"capabilities":    {"capabilities", "capabilities-grants", "toolset-capability"},
	"docs":            {"docs-agents", "docs-testlink"},
	"trace":           {"trace", "trace-invariant"},
	"miglint":         {"miglint-drop", "miglint-state"},
	"sql-eol":         {"sql-eol"},
	"module-ban":      {"module-ban"},
	"blob-size":       {"blob-size"},
	"fix-needs-test":  {"fix-needs-test", "fix-needs-test-blind"},
	"cron-alert":      {"cron-alert"},
}

// TestBroken proves each gate by running it on its broken example.
func TestBroken(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, g := range All {
		dirs, ok := broken[g.Name]
		if !ok || len(dirs) == 0 {
			t.Errorf("%s: no broken example declared", g.Name)
			continue
		}
		for _, d := range dirs {
			t.Run(g.Name+"/"+d, func(t *testing.T) {
				t.Parallel()
				fixture := filepath.Join(root, brokenPrefix, d)
				if err := g.Run(fixture); err == nil {
					t.Errorf("%s passed on broken example %s", g.Name, fixture)
				}
			})
		}
	}
}
