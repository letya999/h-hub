package gates

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// platformDir is where spawn-controller ships its layer data: the
// registry, the platform layer and the role templates.
const platformDir = "services/spawn-controller/internal/svc/platform"

type registryFile struct {
	Chain  []string       `yaml:"chain"`
	Leaves []registryLeaf `yaml:"leaves"`
}

type registryLeaf struct {
	Path     string `yaml:"path"`
	Class    string `yaml:"class"`
	Consumer string `yaml:"consumer"`
	Effect   string `yaml:"effect"`
}

// PassportSchema enforces the schema-registry rule: every leaf a
// platform document or template writes must be registered with a
// consumer — a leaf nobody enforces fails the gate.
func PassportSchema(root string) error {
	data, err := os.ReadFile(filepath.Join(root, platformDir, "registry.yaml"))
	if err != nil {
		return fmt.Errorf("passport-schema: registry: %w", err)
	}
	var reg registryFile
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return fmt.Errorf("passport-schema: registry parse: %w", err)
	}
	known := map[string]bool{}
	for _, l := range reg.Leaves {
		if l.Consumer == "" {
			return fmt.Errorf("passport-schema: leaf %s has no consumer", l.Path)
		}
		switch l.Class {
		case "invariant", "narrow", "choice", "user_data", "keyset-narrow":
		default:
			return fmt.Errorf("passport-schema: leaf %s bad class %q", l.Path, l.Class)
		}
		switch l.Effect {
		case "live", "respawn", "recreate":
		default:
			return fmt.Errorf("passport-schema: leaf %s bad effect %q", l.Path, l.Effect)
		}
		known[l.Path] = true
	}
	return walkDocs(root, platformDir, func(rel string, doc map[string]any) error {
		return checkDocLeaves(rel, doc, "", known)
	})
}

// walkDocs decodes every passport document under dir — the platform
// layer and every template folder — as a JSON object.
func walkDocs(root, dir string, check func(rel string, doc map[string]any) error) error {
	return walk(root, func(rel string, d fs.DirEntry) error {
		if !isPassportDoc(rel, dir, d) {
			return nil
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return fmt.Errorf("passport-schema: %s: %w", rel, err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("passport-schema: %s: %w", rel, err)
		}
		section := strings.TrimSuffix(filepath.Base(rel), ".yaml")
		return check(rel, map[string]any{section: doc})
	})
}

// isPassportDoc selects the layer documents under dir — yaml files
// other than the registry itself and the capability catalog, which
// is platform data, not a passport document.
func isPassportDoc(rel, dir string, d fs.DirEntry) bool {
	base := filepath.Base(rel)
	return !d.IsDir() && strings.HasPrefix(rel, dir+"/") &&
		strings.HasSuffix(rel, ".yaml") &&
		base != "registry.yaml" && base != "capabilities.yaml"
}

// checkDocLeaves flattens the document into dotted paths; each must
// be a registry leaf or a strict prefix of one.
func checkDocLeaves(rel string, doc map[string]any, prefix string, known map[string]bool) error {
	for k, v := range doc {
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		if known[p] {
			continue
		}
		if !hasLeafBelow(p, known) {
			return fmt.Errorf("passport-schema: %s: leaf %s has no consumer in the registry", rel, p)
		}
		if m, ok := v.(map[string]any); ok {
			if err := checkDocLeaves(rel, m, p, known); err != nil {
				return err
			}
		}
	}
	return nil
}

// hasLeafBelow reports whether path is a strict prefix of a leaf.
func hasLeafBelow(path string, known map[string]bool) bool {
	for p := range known {
		if strings.HasPrefix(p, path+".") {
			return true
		}
	}
	return false
}
