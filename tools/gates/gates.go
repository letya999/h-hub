// Package gates holds the repo gates that are not golangci rules
// (enforcement.md): file scans, docs, trace, migrations, git hygiene.
// Every gate is a function over a root dir; the same function runs on
// the repo and on its broken example under testdata/broken/<gate>.
package gates

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Gate is one named check over a directory tree.
type Gate struct {
	Name string
	Run  func(root string) error
}

// All is the gate registry; `just check` runs every entry on the repo.
var All = []Gate{
	{"json-state", NoStateJSON},
	{"banned-words", NoBannedWords},
	{"windows-paths", NoWindowsPaths},
	{"msys-dirs", NoMSYSDirs},
	{"env-declared", EnvDeclared},
	{"env-no-secrets", NoEnvSecrets},
	{"passport-schema", PassportSchema},
	{"capabilities", Capabilities},
	{"docs", Docs},
	{"trace", Trace},
	{"miglint", MigLint},
	{"sql-eol", SQLLineEndings},
	{"module-ban", ModuleBan},
	{"blob-size", BlobSize},
	{"fix-needs-test", FixNeedsTest},
	{"cron-alert", CronAlert},
}

const brokenPrefix = "testdata/broken"

// walk visits files under root, skipping .git and testdata/broken.
// A gate run on a fixture dir scans everything inside it.
func walk(root string, fn func(rel string, d fs.DirEntry) error) error {
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return fmt.Errorf("walk rel: %w", err)
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.IsDir() && rel == ".git":
			return filepath.SkipDir
		case rel == brokenPrefix:
			return filepath.SkipDir
		case strings.HasPrefix(rel, brokenPrefix+"/"):
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		return fn(rel, d)
	})
	if err != nil {
		return fmt.Errorf("walk %s: %w", root, err)
	}
	return nil
}

// hasExt reports whether rel has one of exts.
func hasExt(rel string, exts ...string) bool {
	for _, e := range exts {
		if strings.HasSuffix(rel, e) {
			return true
		}
	}
	return false
}

// findInFiles calls check for every matching file's content.
func findInFiles(root string, exts []string,
	check func(rel, line string, n int) error) error {
	return walk(root, func(rel string, d fs.DirEntry) error {
		if d.IsDir() || !hasExt(rel, exts...) {
			return nil
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if err := check(rel, line, i+1); err != nil {
				return err
			}
		}
		return nil
	})
}
