package gates

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// SQLLineEndings pins migration bytes because store.Migrate hashes them.
func SQLLineEndings(root string) error {
	attrs, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		return fmt.Errorf("sql-eol: attributes: %w", err)
	}
	if !bytes.Contains(attrs, []byte("*.sql text eol=lf\n")) {
		return fmt.Errorf("sql-eol: missing SQL LF attribute")
	}
	return walk(root, func(rel string, d fs.DirEntry) error {
		if d.IsDir() || !hasExt(rel, ".sql") {
			return nil
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return fmt.Errorf("sql-eol: read %s: %w", rel, err)
		}
		if bytes.Contains(data, []byte{'\r'}) {
			return fmt.Errorf("sql-eol: %s contains CR bytes", rel)
		}
		return nil
	})
}
