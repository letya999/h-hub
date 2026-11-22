package edges

import "testing"

func TestParseAllows(t *testing.T) {
	t.Parallel()
	tab, err := Parse("task-engine:operator:CreateJob+GetJob;spawn-controller:task-engine:ClaimRuntime")
	if err != nil {
		t.Fatal(err)
	}
	if !tab.Allows("task-engine", "operator", "CreateJob") {
		t.Error("operator CreateJob should be allowed")
	}
	if tab.Allows("task-engine", "operator", "ClaimRuntime") {
		t.Error("operator ClaimRuntime must be refused")
	}
	if tab.Allows("task-engine", "spawn-controller", "CreateJob") {
		t.Error("undeclared caller must be refused")
	}
	if tab.Allows("spawn-controller", "task-engine", "GetJob") {
		t.Error("undeclared op must be refused")
	}
}

func TestParseEmpty(t *testing.T) {
	t.Parallel()
	tab, err := Parse("  ")
	if err != nil {
		t.Fatal(err)
	}
	if tab.Allows("a", "b", "c") {
		t.Error("empty table must allow nothing")
	}
}

func TestParseBadEntries(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"a:b", "a::op", ":b:op", "a:b:", "a:b:op;;"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("entry %q must fail", s)
		}
	}
}

func TestRenderRoundTrip(t *testing.T) {
	t.Parallel()
	src := "b-svc:a-svc:Op2+Op1;a-svc:b-svc:Op3"
	tab, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	got := tab.Render()
	want := "a-svc:b-svc:Op3;b-svc:a-svc:Op1+Op2"
	if got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}
