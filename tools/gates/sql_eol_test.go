package gates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLLineEndingsRejectsCR(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"),
		[]byte("*.sql text eol=lf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "migration.sql"),
		[]byte("SELECT 1;\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := SQLLineEndings(root)
	if err == nil || !strings.Contains(err.Error(), "contains CR bytes") {
		t.Fatalf("unexpected gate result: %v", err)
	}
}
