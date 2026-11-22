package gates

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const agentsMaxLines = 150

var (
	testRefRe = regexp.MustCompile("`Test[A-Za-z0-9_]+`")
	testDefRe = regexp.MustCompile(`func\s+(Test[A-Za-z0-9_]+)\s*\(`)
	headingRe = regexp.MustCompile(`^#{1,6}\s+\S`)
)

// Docs is the docs gate (rule 25): AGENTS.md <= 150 lines, service
// READMEs follow the template headings, and every `TestX` link in a
// document resolves to an existing test.
func Docs(root string) error {
	if err := agentsLen(root); err != nil {
		return err
	}
	defs, err := testDefs(root)
	if err != nil {
		return err
	}
	if err := testLinks(root, defs); err != nil {
		return err
	}
	return readmeTemplate(root)
}

func agentsLen(root string) error {
	data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		return fmt.Errorf("docs: %w", err)
	}
	if n := strings.Count(string(data), "\n") + 1; n > agentsMaxLines {
		return fmt.Errorf("docs: AGENTS.md has %d lines, max %d", n, agentsMaxLines)
	}
	return nil
}

func testDefs(root string) (map[string]bool, error) {
	defs := map[string]bool{}
	err := findInFiles(root, []string{"_test.go"}, func(_, line string, _ int) error {
		for _, m := range testDefRe.FindAllStringSubmatch(line, -1) {
			defs[m[1]] = true
		}
		return nil
	})
	return defs, err
}

func testLinks(root string, defs map[string]bool) error {
	return findInFiles(root, []string{".md"}, func(rel, line string, n int) error {
		for _, m := range testRefRe.FindAllString(line, -1) {
			name := strings.Trim(m, "`")
			if !defs[name] {
				return fmt.Errorf("docs: %s:%d links to %s which does not exist", rel, n, name)
			}
		}
		return nil
	})
}

// readmeTemplate enforces the template headings on service READMEs.
// The template itself ships in the docs phase; until it exists a
// missing template makes the check vacuous.
func readmeTemplate(root string) error {
	headings, err := templateHeadings(root)
	if err != nil || headings == nil {
		return err
	}
	return walk(root, func(rel string, d fs.DirEntry) error {
		if d.IsDir() || !strings.HasPrefix(rel, "services/") || !strings.HasSuffix(rel, "/README.md") {
			return nil
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return fmt.Errorf("docs: read %s: %w", rel, err)
		}
		got := headingsOf(string(data))
		if strings.Join(got, "\n") != strings.Join(headings, "\n") {
			return fmt.Errorf("docs: %s headings do not match the service template", rel)
		}
		return nil
	})
}

func templateHeadings(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, "docs", "templates", "service-README.md"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("docs: read template: %w", err)
	}
	return headingsOf(string(data)), nil
}

func headingsOf(doc string) []string {
	var out []string
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "# ") {
			continue
		}
		if headingRe.MatchString(line) {
			out = append(out, line)
		}
	}
	return out
}
