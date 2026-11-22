package gates

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

var (
	cronRe  = regexp.MustCompile(`(?m)^\s*(schedule|workflow_dispatch):`)
	alertRe = regexp.MustCompile(`failure\(\s*\)`)
)

// CronAlert rejects a scheduled or dispatch workflow without a failure
// alert (enforcement.md 7: silent cron failures are deleted).
func CronAlert(root string) error {
	dir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cron-alert: read workflows: %w", err)
	}
	for _, e := range entries {
		if err := checkWorkflow(dir, e); err != nil {
			return err
		}
	}
	return nil
}

func checkWorkflow(dir string, e fs.DirEntry) error {
	if e.IsDir() || !hasExt(e.Name(), ".yml", ".yaml") {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, e.Name()))
	if err != nil {
		return fmt.Errorf("cron-alert: read %s: %w", e.Name(), err)
	}
	if !cronRe.Match(data) {
		return nil
	}
	if !alertRe.Match(data) {
		return fmt.Errorf("cron-alert: %s is scheduled/dispatch but has no failure() alert", e.Name())
	}
	return nil
}
