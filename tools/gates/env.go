package gates

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/letya999/h-hub/internal/descriptor"
)

// envRe matches environment *reads* — os.Getenv/os.LookupEnv calls.
// Names a service writes for its children (spawn-controller's runtime
// env allowlist) are not reads and need no declaration of their own.
var envRe = regexp.MustCompile(`(?:Getenv|LookupEnv)\(\s*"(HHUB_[A-Z0-9_]+)"`)
var environRe = regexp.MustCompile(`\bos\.Environ\s*\(`)

// EnvDeclared checks that every HHUB_* variable read in a service's
// code is declared in its descriptor env list (enforcement.md 3).
func EnvDeclared(root string) error {
	used, err := usedEnv(root)
	if err != nil || len(used) == 0 {
		return err
	}
	f, err := descriptor.Load(filepath.Join(root, "deploy", "descriptor.yaml"))
	if err != nil {
		return fmt.Errorf("env-declared: %w", err)
	}
	declared := f.AllEnv()
	for svc, vars := range used {
		for v := range vars {
			if !slices.Contains(declared[svc], v) {
				return fmt.Errorf("env-declared: %s uses %s, not in its descriptor env", svc, v)
			}
		}
	}
	return nil
}

// usedEnv collects HHUB_* tokens per service dir.
func usedEnv(root string) (map[string]map[string]bool, error) {
	used := map[string]map[string]bool{}
	err := walk(root, func(rel string, d fs.DirEntry) error {
		parts := strings.SplitN(rel, "/", 3)
		if d.IsDir() || !hasExt(rel, ".go") {
			return nil
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return fmt.Errorf("env-declared: read %s: %w", rel, err)
		}
		if err := checkEnviron(rel, data); err != nil {
			return err
		}
		if len(parts) != 3 || parts[0] != "services" {
			return nil
		}
		if used[parts[1]] == nil {
			used[parts[1]] = map[string]bool{}
		}
		for _, m := range envRe.FindAllSubmatch(data, -1) {
			used[parts[1]][string(m[1])] = true
		}
		return nil
	})
	return used, err
}

func checkEnviron(rel string, data []byte) error {
	parts := strings.SplitN(rel, "/", 3)
	allowed := len(parts) == 3 && parts[0] == "services" &&
		strings.HasPrefix(parts[2], "internal/config/") ||
		strings.HasPrefix(rel, "cmd/hubctl/install/")
	if environRe.Match(data) && !allowed {
		return fmt.Errorf("env-declared: %s calls os.Environ outside config", rel)
	}
	return nil
}
