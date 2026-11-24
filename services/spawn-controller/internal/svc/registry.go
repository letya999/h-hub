package svc

import (
	"embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed platform
var platformFS embed.FS

// Leaf is one schema-registry row: how a lower layer may touch the
// path (class), who enforces it (consumer) and what a change costs
// (effect). An unproven live is applied as respawn.
type Leaf struct {
	Path     string `yaml:"path"`
	Class    string `yaml:"class"`
	Consumer string `yaml:"consumer"`
	Effect   string `yaml:"effect"`
	Proven   bool   `yaml:"proven"`
}

// Registry is the passport schema: the ordered role chain and the
// leaf table, both data — adding a role is a yaml edit, not code.
type Registry struct {
	Chain  []string `yaml:"chain"`
	Leaves []Leaf   `yaml:"leaves"`
	byPath map[string]Leaf
}

var registry = mustRegistry()

func mustRegistry() *Registry {
	data, err := platformFS.ReadFile("platform/registry.yaml")
	if err != nil {
		panic(fmt.Sprintf("registry: %v", err))
	}
	var r Registry
	if err := yaml.Unmarshal(data, &r); err != nil {
		panic(fmt.Sprintf("registry parse: %v", err))
	}
	r.byPath = map[string]Leaf{}
	for i, l := range r.Leaves {
		if l.Path == "" || l.Consumer == "" {
			panic(fmt.Sprintf("registry leaf %d misses path or consumer", i))
		}
		switch l.Class {
		case "invariant", "narrow", "choice", "user_data", "keyset-narrow":
		default:
			panic(fmt.Sprintf("registry leaf %s: bad class %q", l.Path, l.Class))
		}
		switch l.Effect {
		case "live", "respawn", "recreate":
		default:
			panic(fmt.Sprintf("registry leaf %s: bad effect %q", l.Path, l.Effect))
		}
		r.byPath[l.Path] = l
	}
	if len(r.Chain) == 0 || r.Chain[0] != "platform" {
		panic("registry chain must start with platform")
	}
	return &r
}

// leaf returns the schema row for a path.
func (r *Registry) leaf(path string) (Leaf, bool) {
	l, ok := r.byPath[path]
	return l, ok
}

// appliedEffect downgrades an unproven live to respawn (the class a
// consumer has not demonstrated is not trusted to be live).
func (l Leaf) appliedEffect() string {
	if l.Effect == "live" && !l.Proven {
		return "respawn"
	}
	return l.Effect
}

// docSections is the closed set of passport document names.
var docSections = map[string]bool{
	"stack": true, "config": true, "models": true,
	"workspace": true, "tools": true, "grants": true,
}
