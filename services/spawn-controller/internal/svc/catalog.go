package svc

import (
	"fmt"

	herrors "github.com/letya999/h-hub/shared/errors"
	"gopkg.in/yaml.v3"
)

// catalogFile is the capability catalog shipped as platform data.
type catalogFile struct {
	Capabilities []catalogEntry `yaml:"capabilities"`
}

type catalogEntry struct {
	Name    string `yaml:"name"`
	Summary string `yaml:"summary"`
	Why     string `yaml:"why"`
}

var catalog = mustCatalog()

// mustCatalog loads the capability names once; a grant may hold only
// names from this set (rule 5).
func mustCatalog() map[string]catalogEntry {
	data, err := platformFS.ReadFile("platform/capabilities.yaml")
	if err != nil {
		panic(fmt.Sprintf("catalog: %v", err))
	}
	var c catalogFile
	if err := yaml.Unmarshal(data, &c); err != nil {
		panic(fmt.Sprintf("catalog parse: %v", err))
	}
	names := map[string]catalogEntry{}
	for _, e := range c.Capabilities {
		if e.Name == "" || names[e.Name].Name != "" {
			panic(fmt.Sprintf("catalog: bad or duplicate entry %q", e.Name))
		}
		names[e.Name] = e
	}
	return names
}

// validateGrants refuses a compiled spec whose grants name a
// capability the catalog does not declare — activation rejects an
// invalid passport rather than letting the name sit inert.
func validateGrants(g GrantsPassport, space string) error {
	for _, name := range g.Allow {
		if _, ok := catalog[name]; !ok {
			err := herrors.New(herrors.CodeInvalid,
				"err.invalid-arg").With("grant", name).With("space", space)
			return fmt.Errorf("space %s: unknown grant %s: %w", space, name, err)
		}
	}
	return nil
}
