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
	reqLineRe = regexp.MustCompile(`\b(req:|requires:|testkit\.Req\()`)
	idRe      = regexp.MustCompile(`\b(adr|cap|sys)\.[a-z0-9-]+(\.[a-z0-9-]+)?`)
	adrFileRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-([a-z0-9-]+)\.md$`)
	adrInvRe  = regexp.MustCompile(`\badr\.[a-z0-9-]+\.[a-z0-9-]+\b`)
)

// Trace is the traceability gate (rule 29): tests link to requirements
// via `req:` comments, `requires:` lines, or testkit.Req(...) calls.
// adr.<slug>.<invariant> resolves only when an ADR file declares the
// invariant; cap.* stays unresolved until a capability catalog exists.
func Trace(root string) error {
	adrs, err := adrIndex(root)
	if err != nil {
		return err
	}
	return findInFiles(root, []string{"_test.go", ".yaml", ".yml"}, func(rel, line string, n int) error {
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

func checkID(id string, adrs map[string]map[string]bool) error {
	parts := strings.SplitN(id, ".", 2)
	switch parts[0] {
	case "sys":
		if !sysIDs[parts[1]] {
			return fmt.Errorf("unknown system invariant %s", id)
		}
	case "adr":
		tail := strings.SplitN(parts[1], ".", 2)
		invs, ok := adrs[tail[0]]
		if !ok {
			return fmt.Errorf("unknown ADR %s", id)
		}
		if len(tail) != 2 || !invs[tail[1]] {
			return fmt.Errorf("ADR %s declares no invariant %s", tail[0], id)
		}
	default:
		return fmt.Errorf("capability catalog does not exist yet: %s", id)
	}
	return nil
}

// adrIndex maps each ADR slug to the invariant IDs it declares. An
// invariant exists only when the ADR text contains its full
// adr.<slug>.<invariant> token.
func adrIndex(root string) (map[string]map[string]bool, error) {
	dir := filepath.Join(root, "adr")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return map[string]map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("trace: read adr dir: %w", err)
	}
	out := map[string]map[string]bool{}
	for _, e := range entries {
		m := adrFileRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("trace: read %s: %w", e.Name(), err)
		}
		invs := map[string]bool{}
		for _, tok := range adrInvRe.FindAllString(string(body), -1) {
			invs[strings.SplitN(tok, ".", 3)[2]] = true
		}
		out[m[1]] = invs
	}
	return out, nil
}
