package gates

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	destructiveRe = regexp.MustCompile(`(?i)\bDROP\b|\bRENAME\b|\bSET\s+NOT\s+NULL\b|\bALTER\s+(COLUMN\s+\w+\s+)?TYPE\b`)
	contractRe    = regexp.MustCompile(`contract-of:\s*(\S+)`)
	stateColRe    = regexp.MustCompile(`(?i)\b(state|status)\s+(text|varchar|int|bigint|smallint|boolean|jsonb|timestamptz|timestamp)\b`)
)

// MigLint is the expand/contract migration linter (rule 30): no down
// migrations; DROP/RENAME/type change/SET NOT NULL needs a
// `contract-of: <tag>` marker naming an already published tag; domain
// tables carry no state/status columns (state lives in fsm.machines).
func MigLint(root string) error {
	tags, err := publishedTags(root)
	if err != nil {
		return err
	}
	return walk(root, func(rel string, d fs.DirEntry) error {
		if d.IsDir() || !hasExt(rel, ".sql") {
			return nil
		}
		return lintSQL(root, rel, tags)
	})
}

func lintSQL(root, rel string, tags map[string]bool) error {
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return fmt.Errorf("miglint: read %s: %w", rel, err)
	}
	base := strings.ToLower(filepath.Base(rel))
	if strings.Contains(base, "down") {
		return fmt.Errorf("miglint: %s: down migrations do not exist", rel)
	}
	body := string(data)
	if destructiveRe.MatchString(body) {
		if err := contractOf(body, tags); err != nil {
			return fmt.Errorf("miglint: %s: %w", rel, err)
		}
	}
	if stateColRe.MatchString(body) && !strings.Contains(rel, "fsm") {
		return fmt.Errorf("miglint: %s: state/status column outside fsm schema", rel)
	}
	return nil
}

func contractOf(body string, tags map[string]bool) error {
	m := contractRe.FindStringSubmatch(body)
	if m == nil {
		return fmt.Errorf("destructive operation without contract-of marker")
	}
	if !tags[m[1]] {
		return fmt.Errorf("contract-of %s is not a published tag", m[1])
	}
	return nil
}

// publishedTags lists git tags; a tags.txt file overrides (fixtures).
func publishedTags(root string) (map[string]bool, error) {
	out := map[string]bool{}
	if data, err := os.ReadFile(filepath.Join(root, "tags.txt")); err == nil {
		for _, t := range strings.Fields(string(data)) {
			out[t] = true
		}
		return out, nil
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		return out, nil
	}
	b, err := exec.Command("git", "-C", root, "tag", "-l").Output()
	if err != nil {
		return nil, fmt.Errorf("published tags: %w", err)
	}
	for _, t := range strings.Fields(string(b)) {
		out[t] = true
	}
	return out, nil
}
