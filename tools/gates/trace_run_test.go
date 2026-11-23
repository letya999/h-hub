package gates

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestTraceExecutedRequiresPassedRefusal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustTraceDir(t, filepath.Join(root, "docs"))
	mustTraceFile(t, filepath.Join(root, "go.mod"), "module example.test\n")
	row := "| `sys.store-missing` | proved | task-engine | [TestMissing](../test/e2e/missing_test.go#TestMissing) | Missing database refuses startup. |\n"
	mustTraceFile(t, filepath.Join(root, "docs", "invariants.md"), row)
	for _, tc := range []struct {
		name, action string
		deny         bool
		wantError    bool
	}{
		{"passed refusal", "pass", true, false},
		{"label only", "pass", false, true},
		{"failed test", "fail", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			for _, ev := range []testEvent{
				{Action: "output", Package: "example.test/test/e2e", Test: "TestMissing", Output: "TRACE_REQ sys.store-missing\n"},
				{Action: "output", Package: "example.test/test/e2e", Test: "TestMissing", Output: denyOutput(tc.deny)},
				{Action: tc.action, Package: "example.test/test/e2e", Test: "TestMissing"},
			} {
				if err := json.NewEncoder(&out).Encode(ev); err != nil {
					t.Fatal(err)
				}
			}
			err := TraceExecuted(root, &out)
			if (err != nil) != tc.wantError {
				t.Fatalf("trace result: %v", err)
			}
		})
	}
}

func denyOutput(deny bool) string {
	if deny {
		return "TRACE_DENY sys.store-missing\n"
	}
	return "no refusal\n"
}
