package svc

import (
	"encoding/json"
	"testing"
)

// TestRegistryLoads proves the shipped registry parses: every tool
// names a capability and carries an argument schema.
func TestRegistryLoads(t *testing.T) {
	t.Parallel()
	if len(registry) != 2 {
		t.Fatalf("toolsets: got %d, want probe+cap", len(registry))
	}
	for _, ts := range registry {
		for _, td := range ts.Tools {
			if td.Capability == "" || len(td.Parameters) == 0 {
				t.Fatalf("tool %s lacks capability or schema", td.Name)
			}
		}
	}
}

// TestDecideMatrix pins admission: unknown tools, unselected
// toolsets and ungranted capabilities all deny; only the fully
// granted path allows.
func TestDecideMatrix(t *testing.T) {
	t.Parallel()
	selected := map[string]bool{"probe": true}
	granted := map[string]bool{"tools.use": true}
	for _, tc := range []struct {
		name    string
		ts, tl  string
		sel     map[string]bool
		gr      map[string]bool
		allowed bool
	}{
		{"granted", "probe", "probe_echo", selected, granted, true},
		{"ungranted", "probe", "probe_echo", selected, nil, false},
		{"unselected", "probe", "probe_echo", nil, granted, false},
		{"unknown_tool", "probe", "nope", selected, granted, false},
		{"unknown_toolset", "nope", "probe_echo", selected, granted, false},
	} {
		got, caps, reason := decide(tc.sel, tc.gr, tc.ts, tc.tl)
		if got != tc.allowed {
			t.Fatalf("%s: decide=%v want %v", tc.name, got, tc.allowed)
		}
		if tc.name == "granted" && (len(caps) != 1 || caps[0] != "tools.use" || reason != "") {
			t.Fatalf("granted path: caps %v reason %q", caps, reason)
		}
	}
}

// TestProjectFiltersUngranted pins the projection rule: a tool whose
// capability is not granted is absent from the output, not refused
// in place.
func TestProjectFiltersUngranted(t *testing.T) {
	t.Parallel()
	spec := specView{
		Grants: grantsView{Allow: []string{"tools.use"}},
		Tools:  toolsView{Core: []string{"probe"}, Extended: []string{"cap"}},
	}
	p, err := project(spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Core) != 1 || p.Core[0].Name != "probe_echo" {
		t.Fatalf("core: %+v", p.Core)
	}
	for _, ti := range p.Extended {
		if ti.Name != "cap_tools_use" {
			t.Fatalf("ungranted cap tool projected: %s", ti.Name)
		}
	}
	var params struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(p.Core[0].Parameters, &params); err != nil {
		t.Fatalf("schema decode: %v", err)
	}
}

// TestProjectUnknownToolset proves a spec naming a toolset the
// registry lacks is refused, not silently projected.
func TestProjectUnknownToolset(t *testing.T) {
	t.Parallel()
	spec := specView{Tools: toolsView{Core: []string{"ghost"}}}
	if _, err := project(spec); err == nil {
		t.Fatal("unknown toolset projected")
	}
}
