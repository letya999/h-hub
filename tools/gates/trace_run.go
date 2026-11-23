package gates

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type testEvent struct {
	Action  string
	Package string
	Test    string
	Output  string
}

type proofState struct {
	passed bool
	req    map[string]bool
	deny   map[string]bool
}

// TraceExecuted verifies that each proved scenario passed and exercised refusal.
func TraceExecuted(root string, r io.Reader) error {
	rows, err := invariantRows(root)
	if err != nil {
		return err
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return fmt.Errorf("trace: read module: %w", err)
	}
	module := strings.TrimPrefix(strings.SplitN(string(mod), "\n", 2)[0], "module ")
	states, err := readProofEvents(r)
	if err != nil {
		return err
	}
	var missing []string
	for id, row := range rows {
		if row.status != "proved" {
			continue
		}
		pkg := module + "/" + filepath.ToSlash(filepath.Dir(row.file))
		state := states[proofKey(pkg, row.test)]
		if !state.passed || !state.req[id] || !state.deny[id] {
			missing = append(missing, id+" ("+row.test+")")
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("trace: unproved scenarios: %s", strings.Join(missing, ", "))
	}
	return nil
}

func readProofEvents(r io.Reader) (map[string]proofState, error) {
	states := map[string]proofState{}
	dec := json.NewDecoder(r)
	for {
		var ev testEvent
		if err := dec.Decode(&ev); err != nil {
			if err == io.EOF {
				return states, nil
			}
			return nil, fmt.Errorf("trace: test JSON: %w", err)
		}
		applyProofEvent(states, ev)
	}
}

func applyProofEvent(states map[string]proofState, ev testEvent) {
	if ev.Test == "" {
		return
	}
	key := proofKey(ev.Package, ev.Test)
	state := states[key]
	if state.req == nil {
		state.req, state.deny = map[string]bool{}, map[string]bool{}
	}
	state.passed = state.passed || ev.Action == "pass"
	if ev.Action == "output" {
		for _, id := range idRe.FindAllString(ev.Output, -1) {
			state.req[id] = state.req[id] || strings.Contains(ev.Output, "TRACE_REQ "+id)
			state.deny[id] = state.deny[id] || strings.Contains(ev.Output, "TRACE_DENY "+id)
		}
	}
	states[key] = state
}

func proofKey(pkg, test string) string { return pkg + "#" + test }
