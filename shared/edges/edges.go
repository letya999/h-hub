// Package edges is the service-call authorization table (rules 9, 31):
// the descriptor's edge list is rendered into one HHUB_EDGES value per
// service and parsed here, so "the pair (sub, operation) is declared"
// is checked against a single source.
package edges

import (
	"fmt"
	"strings"
)

// Table maps callee -> caller -> allowed operations.
type Table map[string]map[string]map[string]bool

// Parse reads the rendered edge form: entries separated by ";", each
// "to:from:op1+op2". An empty string is a valid, empty table.
func Parse(s string) (Table, error) {
	t := Table{}
	s = strings.TrimSpace(s)
	if s == "" {
		return t, nil
	}
	for _, e := range strings.Split(s, ";") {
		ent, err := parseEntry(e)
		if err != nil {
			return nil, err
		}
		if t[ent.to] == nil {
			t[ent.to] = map[string]map[string]bool{}
		}
		if t[ent.to][ent.from] == nil {
			t[ent.to][ent.from] = map[string]bool{}
		}
		for _, op := range ent.ops {
			t[ent.to][ent.from][op] = true
		}
	}
	return t, nil
}

type entry struct {
	to   string
	from string
	ops  []string
}

func parseEntry(e string) (entry, error) {
	var ent entry
	parts := strings.SplitN(strings.TrimSpace(e), ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return ent, fmt.Errorf("edges: bad entry %q", e)
	}
	for _, op := range strings.Split(parts[2], "+") {
		if op == "" {
			return ent, fmt.Errorf("edges: empty op in %q", e)
		}
		ent.ops = append(ent.ops, op)
	}
	ent.to, ent.from = parts[0], parts[1]
	return ent, nil
}

// Allows reports whether caller may invoke op on the service to.
func (t Table) Allows(to, from, op string) bool {
	return t[to][from][op]
}

// For returns the sub-table of edges targeting one callee — what a
// service needs to authorize its inbound calls.
func (t Table) For(to string) Table {
	out := Table{}
	if c, ok := t[to]; ok {
		out[to] = c
	}
	return out
}

// Render serializes a table back to the canonical env form: entries
// sorted by "to" then "from", ops sorted, so generated config is
// deterministic.
func (t Table) Render() string {
	var entries []string
	for to, callers := range t {
		for from, ops := range callers {
			var list []string
			for op := range ops {
				list = append(list, op)
			}
			sortStrings(list)
			entries = append(entries, to+":"+from+":"+strings.Join(list, "+"))
		}
	}
	sortStrings(entries)
	return strings.Join(entries, ";")
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
