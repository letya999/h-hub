package gates

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

// NoStateJSON rejects .json/.jsonl files (enforcement.md section 3:
// state never lives in JSON files).
func NoStateJSON(root string) error {
	return walk(root, func(rel string, d fs.DirEntry) error {
		if !d.IsDir() && hasExt(rel, ".json", ".jsonl") {
			return fmt.Errorf("json-state: %s", rel)
		}
		return nil
	})
}

// The char classes keep the banned words out of this file itself.
var bannedWordsRe = regexp.MustCompile(`(?i)\b(legac[y]|compa[t]|fal{2}back)\b`)

// NoBannedWords rejects the rule-15 vocabulary in Go code.
func NoBannedWords(root string) error {
	return findInFiles(root, []string{".go"}, func(rel, line string, n int) error {
		if bannedWordsRe.MatchString(line) {
			return fmt.Errorf("banned-words: %s:%d: %s", rel, n, strings.TrimSpace(line))
		}
		return nil
	})
}

// winPathRe matches uppercase drive-letter paths but not scheme://,
// VAR:/mount in docker flags, or printf formats like %s:\n.
var winPathRe = regexp.MustCompile(`(?:^|[^A-Za-z0-9_])[A-Z]:(?:\\|/[^/])`)

// NoWindowsPaths rejects Windows path literals in code and configs
// (rule 26: nothing runs on Windows).
func NoWindowsPaths(root string) error {
	return findInFiles(root, []string{".go", ".yaml", ".yml"}, func(rel, line string, n int) error {
		if winPathRe.MatchString(line) {
			return fmt.Errorf("windows-paths: %s:%d: %s", rel, n, strings.TrimSpace(line))
		}
		return nil
	})
}

var msysRe = regexp.MustCompile(`^[A-Za-z]:|;`)

// NoMSYSDirs rejects MSYS artifact names like `x;C` or `C:Users`
// anywhere in the tree.
func NoMSYSDirs(root string) error {
	return walk(root, func(rel string, d fs.DirEntry) error {
		if msysRe.MatchString(d.Name()) {
			return fmt.Errorf("msys-dirs: %s", rel)
		}
		return nil
	})
}
