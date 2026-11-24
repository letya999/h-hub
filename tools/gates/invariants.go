package gates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type invariantRow struct {
	status string
	owner  string
	file   string
	test   string
	deny   string
}

var scenarioRe = regexp.MustCompile(`\]\(\.\./([^)#]+_test\.go)#(Test[A-Za-z0-9_]+)\)`)
var deferredRe = regexp.MustCompile(`^deferred(:[0-9]+(,[0-9]+)*|, consumer not planned yet)$`)

func invariantRows(root string) (map[string]invariantRow, error) {
	data, err := os.ReadFile(filepath.Join(root, "docs", "invariants.md"))
	if err != nil {
		return nil, fmt.Errorf("trace: read register: %w", err)
	}
	rows := map[string]invariantRow{}
	for n, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		id, row, err := parseInvariantRow(line)
		if err != nil {
			return nil, fmt.Errorf("trace: register line %d: %w", n+1, err)
		}
		if _, exists := rows[id]; exists {
			return nil, fmt.Errorf("trace: duplicate register ID %s", id)
		}
		rows[id] = row
	}
	return rows, nil
}

func parseInvariantRow(line string) (string, invariantRow, error) {
	cells := strings.Split(line, "|")
	if len(cells) != 7 {
		return "", invariantRow{}, fmt.Errorf("expected five columns")
	}
	id := strings.Trim(strings.TrimSpace(cells[1]), "`")
	row := invariantRow{status: strings.TrimSpace(cells[2]), owner: strings.TrimSpace(cells[3]),
		deny: strings.TrimSpace(cells[5])}
	if m := scenarioRe.FindStringSubmatch(cells[4]); m != nil {
		row.file, row.test = m[1], m[2]
	}
	if row.owner == "" || row.deny == "" || (row.status != "proved" && !deferredRe.MatchString(row.status)) {
		return "", invariantRow{}, fmt.Errorf("incomplete row %s", id)
	}
	if row.status == "proved" && row.test == "" {
		return "", invariantRow{}, fmt.Errorf("proved %s has no scenario", id)
	}
	return id, row, nil
}

func checkRegister(root string, rows map[string]invariantRow,
	adrs map[string]map[string]bool, caps map[string]bool) error {
	if err := checkDeclared(rows, adrs, caps); err != nil {
		return err
	}
	for id, row := range rows {
		if err := checkID(id, adrs, caps); err != nil {
			return err
		}
		if row.test != "" {
			if err := checkScenario(root, id, row); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkDeclared(rows map[string]invariantRow, adrs map[string]map[string]bool,
	caps map[string]bool) error {
	if err := checkDeclaredSys(rows); err != nil {
		return err
	}
	for slug, invs := range adrs {
		for inv := range invs {
			if _, ok := rows["adr."+slug+"."+inv]; !ok {
				return fmt.Errorf("trace: unregistered adr.%s.%s", slug, inv)
			}
		}
	}
	for name := range caps {
		if _, ok := rows["cap."+name]; !ok {
			return fmt.Errorf("trace: unregistered cap.%s", name)
		}
	}
	return nil
}

func checkDeclaredSys(rows map[string]invariantRow) error {
	for slug := range sysIDs {
		if _, ok := rows["sys."+slug]; !ok {
			return fmt.Errorf("trace: unregistered sys.%s", slug)
		}
	}
	return nil
}

func checkScenario(root, id string, row invariantRow) error {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(row.file)))
	if err != nil {
		return fmt.Errorf("trace: %s scenario: %w", id, err)
	}
	if !strings.Contains(string(data), "func "+row.test+"(") {
		return fmt.Errorf("trace: %s scenario %s missing", id, row.test)
	}
	return nil
}
