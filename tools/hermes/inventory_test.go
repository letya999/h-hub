package hermes

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeInventoryReview(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	outdated := strings.Repeat("b", 64)
	for _, tc := range []struct {
		name, reviewed, row string
		want                bool
	}{
		{"reviewed", digest, `{name: "native", effect: network}`, true},
		{"stale review", outdated, `{name: "native", effect: network}`, false},
		{"missing effect", digest, `"native"`, false},
		{"changed name", digest, `{name: "other", effect: network}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			observed := filepath.Join(dir, "observed")
			declared := filepath.Join(dir, "declared")
			header := "inventory_sha256: " + digest + "\n"
			writeInventory(t, observed, header+"tools:\n  - \"native\"\n")
			writeInventory(t, declared, header+"reviewed_inventory_sha256: "+tc.reviewed+"\ntools:\n  - "+tc.row+"\n")
			out, err := exec.Command("sh", "verify-inventory.sh", declared, observed).CombinedOutput()
			if (err == nil) != tc.want {
				t.Fatalf("gate success want %t, got %v: %s", tc.want, err, out)
			}
		})
	}
}

func writeInventory(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
