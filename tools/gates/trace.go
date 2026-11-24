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
	"model-usage": true, "model-quota": true, "model-route": true,
	"space-egress":    true,
	"passport-narrow": true, "passport-activate": true,
	"passport-live": true, "passport-respawn": true,
	"passport-recreate": true, "passport-rollback": true,
	"passport-explain": true, "grant-revocation": true,
}

var (
	reqLineRe = regexp.MustCompile(`\b(req:|requires:|testkit\.(Req|Deny)\()`)
	idRe      = regexp.MustCompile(`\b(adr|cap|sys)\.[a-z0-9_-]+(\.[a-z0-9_-]+)*`)
	adrFileRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-([a-z0-9-]+)\.md$`)
	adrInvRe  = regexp.MustCompile(`\badr\.[a-z0-9-]+\.[a-z0-9-]+\b`)
)

// Trace checks every declared ID against the register and test references.
func Trace(root string) error {
	adrs, err := adrIndex(root)
	if err != nil {
		return err
	}
	rows, err := invariantRows(root)
	if err != nil {
		return err
	}
	caps, err := capabilityNames(root)
	if err != nil {
		return err
	}
	if err := checkRegister(root, rows, adrs, caps); err != nil {
		return err
	}
	return findInFiles(root, []string{"_test.go", ".yaml", ".yml"}, func(rel, line string, n int) error {
		if !reqLineRe.MatchString(line) {
			return nil
		}
		for _, id := range idRe.FindAllString(line, -1) {
			if _, ok := rows[id]; !ok {
				return fmt.Errorf("trace: %s:%d: unregistered %s", rel, n, id)
			}
		}
		return nil
	})
}

func checkID(id string, adrs map[string]map[string]bool,
	caps map[string]bool) error {
	parts := strings.SplitN(id, ".", 2)
	switch parts[0] {
	case "sys":
		return checkSysID(parts[1], id)
	case "adr":
		return checkADRID(id, parts[1], adrs)
	case "cap":
		if !caps[parts[1]] {
			return fmt.Errorf("unknown capability %s", id)
		}
		return nil
	default:
		return fmt.Errorf("unknown trace class %s", id)
	}
}

func checkSysID(slug, id string) error {
	if !sysIDs[slug] {
		return fmt.Errorf("unknown system invariant %s", id)
	}
	return nil
}

func checkADRID(id, tail string, adrs map[string]map[string]bool) error {
	parts := strings.SplitN(tail, ".", 2)
	invs, ok := adrs[parts[0]]
	if !ok {
		return fmt.Errorf("unknown ADR %s", id)
	}
	if len(parts) != 2 || !invs[parts[1]] {
		return fmt.Errorf("ADR %s declares no invariant %s", parts[0], id)
	}
	return nil
}

// capabilityNames resolves the catalog for cap.* ids; roots without
// a catalog (gate unit tests) carry no capabilities.
func capabilityNames(root string) (map[string]bool, error) {
	names, err := loadCapabilities(root)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	return names, nil
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
