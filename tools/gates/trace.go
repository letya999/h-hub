package gates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// sysIDs is the fixed list of system invariants (enforcement.md 6).
var sysIDs = map[string]bool{
	"tenant-isolation": true, "forged-token": true, "secret-leak": true,
	"kill-uncertain": true, "store-missing": true, "stack-restart": true,
	"late-receipt": true, "seam-onesided": true, "sweep-lag": true,
	"wedge-unblock": true, "schema-version": true, "worker-intent": true,
	"space-no-db": true, "seal-at-rest": true, "watchdog-stale": true,
	"service-edge": true, "worker-tenant": true,
	"worker-stop-confirmed": true, "package-build": true, "recipe-hosts": true,
}

var (
	reqLineRe = regexp.MustCompile(`\breq:`)
	idRe      = regexp.MustCompile(`\b(adr|cap|sys)\.[a-z0-9-]+(\.[a-z0-9-]+)?`)
	adrFileRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-([a-z0-9-]+)\.md$`)
)

// Trace is the traceability gate (rule 29): a test links to an ID via
// a `req:` comment. Phase 1 runs an empty matrix: the only failure is
// a link to an ID that cannot exist (unknown sys, missing ADR slug, or
// cap.*, whose catalog is not built yet).
func Trace(root string) error {
	adrs, err := adrSlugs(root)
	if err != nil {
		return err
	}
	return findInFiles(root, []string{"_test.go"}, func(rel, line string, n int) error {
		if !reqLineRe.MatchString(line) {
			return nil
		}
		for _, id := range idRe.FindAllString(line, -1) {
			if err := checkID(id, adrs); err != nil {
				return fmt.Errorf("trace: %s:%d: %w", rel, n, err)
			}
		}
		return nil
	})
}

func checkID(id string, adrs map[string]bool) error {
	parts := strings.SplitN(id, ".", 2)
	switch parts[0] {
	case "sys":
		if !sysIDs[parts[1]] {
			return fmt.Errorf("unknown system invariant %s", id)
		}
	case "adr":
		if !adrs[strings.SplitN(parts[1], ".", 2)[0]] {
			return fmt.Errorf("unknown ADR %s", id)
		}
	default: // cap.*: no capability catalog exists yet
		return fmt.Errorf("capability catalog does not exist yet: %s", id)
	}
	return nil
}

func adrSlugs(root string) (map[string]bool, error) {
	entries, err := os.ReadDir(filepath.Join(root, "adr"))
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("trace: read adr dir: %w", err)
	}
	out := map[string]bool{}
	for _, e := range entries {
		if m := adrFileRe.FindStringSubmatch(e.Name()); m != nil {
			out[m[1]] = true
		}
	}
	return out, nil
}
