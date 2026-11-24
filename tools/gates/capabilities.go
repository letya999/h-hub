package gates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// capabilityFile mirrors the platform capabilities.yaml entries.
type capabilityFile struct {
	Capabilities []capabilityEntry `yaml:"capabilities"`
}

type capabilityEntry struct {
	Name    string `yaml:"name"`
	Summary string `yaml:"summary"`
	Why     string `yaml:"why"`
}

// capName is the closed identifier form: lowercase words joined by
// dots (tools.use, observability.read_own_logs).
var capName = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// Capabilities enforces the single-vocabulary rule for grants: the
// catalog is well-formed (name/summary/why, unique, word.word names)
// and every grant a shipped passport declares resolves to it.
func Capabilities(root string) error {
	names, err := loadCapabilities(root)
	if err != nil {
		return err
	}
	return walkDocs(root, platformDir, checkGrantNames(names))
}

// checkGrantNames verifies every allow entry in a grants document
// resolves to a catalog name.
func checkGrantNames(names map[string]bool) func(string, map[string]any) error {
	return func(rel string, doc map[string]any) error {
		// walkDocs wraps each file under its section name; only the
		// grants section carries allow lists.
		g, ok := doc["grants"].(map[string]any)
		if !ok {
			return nil
		}
		allow, ok := g["allow"].([]any)
		if !ok {
			return nil
		}
		for _, v := range allow {
			if name, ok := v.(string); ok && !names[name] {
				return fmt.Errorf("capabilities: %s grants unknown %q", rel, name)
			}
		}
		return nil
	}
}

// loadCapabilities reads and validates the catalog itself.
func loadCapabilities(root string) (map[string]bool, error) {
	data, err := os.ReadFile(filepath.Join(root, platformDir, "capabilities.yaml"))
	if err != nil {
		return nil, fmt.Errorf("capabilities: %w", err)
	}
	var c capabilityFile
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("capabilities: parse: %w", err)
	}
	names := map[string]bool{}
	for _, e := range c.Capabilities {
		if err := checkCapability(e, names); err != nil {
			return nil, err
		}
		names[e.Name] = true
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("capabilities: empty catalog")
	}
	return names, nil
}

// checkCapability validates one catalog entry.
func checkCapability(e capabilityEntry, names map[string]bool) error {
	switch {
	case e.Name == "":
		return fmt.Errorf("capabilities: entry without name")
	case !capName.MatchString(e.Name):
		return fmt.Errorf("capabilities: %q not word.word", e.Name)
	case names[e.Name]:
		return fmt.Errorf("capabilities: duplicate %q", e.Name)
	case e.Summary == "" || e.Why == "":
		return fmt.Errorf("capabilities: %q missing summary or why", e.Name)
	}
	return nil
}
