package gates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTraceRejectsUnknownReferenceAndMissingID(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, omitted, ref string
	}{
		{"unknown test reference", "", "sys.unregistered"},
		{"missing declared ID", "store-missing", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			mustTraceDir(t, filepath.Join(root, "docs"))
			var register strings.Builder
			for slug := range sysIDs {
				if slug != tc.omitted {
					register.WriteString("| `sys." + slug + "` | deferred:6 | owner | — | Forbidden path must fail. |\n")
				}
			}
			mustTraceFile(t, filepath.Join(root, "docs", "invariants.md"), register.String())
			if tc.ref != "" {
				mustTraceFile(t, filepath.Join(root, "x_test.go"), "// req: "+tc.ref)
			}
			if err := Trace(root); err == nil {
				t.Fatal("trace accepted broken register or test reference")
			}
		})
	}
}

func mustTraceDir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustTraceFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
