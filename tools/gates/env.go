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

// envAssignRe matches `NAME: value` / `NAME=value` / `- NAME=value`
// assignments in compose, descriptor and dotenv-style files.
var envAssignRe = regexp.MustCompile(
	`^\s*-?\s*([A-Za-z_][A-Za-z0-9_]*)\s*[:=]\s*"?([^"'\s#]+)"?\s*$`)

// secretNameRe names variables that carry credentials. Their value
// must be a file path or a cred:// reference, never an inline value.
var secretNameRe = regexp.MustCompile(
	`(?i)(token|secret|passwd|password|api[_-]?key|private[_-]?key|` +
		`credential|signing[_-]?key|access[_-]?key|db[_-]?key|session[_-]?key)`)

// secretValueRes matches values that are themselves a key or token,
// whatever the variable name.
var secretValueRes = []*regexp.Regexp{
	regexp.MustCompile(`^\d{6,}:[A-Za-z0-9_-]{25,}$`),    // telegram bot token
	regexp.MustCompile(`^sk-[A-Za-z0-9_-]{16,}$`),        // openai-style key
	regexp.MustCompile(`^xox[baprs]-[A-Za-z0-9-]{8,}$`),  // slack
	regexp.MustCompile(`^gh[pousr]_[A-Za-z0-9]{20,}$`),   // github
	regexp.MustCompile(`^github_pat_[A-Za-z0-9_]{20,}$`), // github fine-grained
	regexp.MustCompile(`^glpat-[A-Za-z0-9_-]{15,}$`),     // gitlab
	regexp.MustCompile(`^AIza[0-9A-Za-z_-]{20,}$`),       // google api
	regexp.MustCompile(`^AKIA[0-9A-Z]{16}$`),             // aws access key
	regexp.MustCompile(`^-----BEGIN [A-Z ]*PRIVATE KEY`), // pem private key
	regexp.MustCompile(`^[A-Za-z]+://[^/\s:]+:[^@\s]+@`), // dsn with password
}

// envFileRe matches tracked dotenv-style files anywhere in the tree.
var envFileRe = regexp.MustCompile(`(?:^|/)[^/]+\.env(?:\.[^/]+)?$`)

// NoEnvSecrets rejects secrets in the environment: a variable whose
// value looks like a key or token, a secret-named variable with an
// inline value instead of a path or cred:// reference, or a tracked
// env file carrying secrets (enforcement.md 3).
func NoEnvSecrets(root string) error {
	files := map[string]bool{
		"deploy/compose.yaml":    true,
		"deploy/descriptor.yaml": true,
	}
	err := walk(root, func(rel string, d fs.DirEntry) error {
		if d.IsDir() {
			return nil
		}
		if envFileRe.MatchString(rel) {
			files[rel] = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	for rel := range files {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return fmt.Errorf("env-no-secrets: read %s: %w", rel, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if err := checkEnvLine(rel, line, i+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkEnvLine(rel, line string, n int) error {
	m := envAssignRe.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	name, value := m[1], m[2]
	for _, re := range secretValueRes {
		if re.MatchString(value) {
			return fmt.Errorf("env-no-secrets: %s:%d: %s carries a secret-looking value; pass it via cred:// or a mounted file", rel, n, name)
		}
	}
	if !secretNameRe.MatchString(name) {
		return nil
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "cred://") ||
		strings.HasPrefix(value, "${") {
		return nil
	}
	return fmt.Errorf("env-no-secrets: %s:%d: %s takes an inline value; secrets reach services only via cred:// or a mounted file", rel, n, name)
}
