package svc

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"unicode/utf8"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
)

const maxLayerSkills = 32

var skillName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func mergeLayerSkills(parent, child any) (map[string]any, error) {
	upper, valid := parent.(map[string]any)
	if !valid && parent != nil {
		return nil, herrors.New(herrors.CodeInvalid, "err.skill-format")
	}
	lower, ok := child.(map[string]any)
	if !ok {
		return nil, herrors.New(herrors.CodeInvalid, "err.skill-format")
	}
	merged := make(map[string]any, len(upper)+len(lower))
	maps.Copy(merged, upper)
	for _, name := range slices.Sorted(maps.Keys(lower)) {
		if !skillName.MatchString(name) {
			return nil, herrors.New(herrors.CodeInvalid, "err.skill-format")
		}
		if _, exists := merged[name]; exists {
			return nil, herrors.New(herrors.CodeInvalid, "err.skill-duplicate").With("file", name) //nolint:wrapcheck // safe field
		}
		merged[name] = lower[name]
	}
	return merged, nil
}

func validateLayerSkills(skills map[string]string) error {
	if len(skills) > maxLayerSkills {
		return herrors.New(herrors.CodeInvalid, "err.skill-limit")
	}
	for name, body := range skills {
		if !skillName.MatchString(name) || body == "" || len(body) > maxWorkspaceBytes || !utf8.ValidString(body) {
			return herrors.New(herrors.CodeInvalid, "err.skill-format")
		}
	}
	return nil
}

func renderSkillShowcase(dir string, skills map[string]string) error {
	if skills == nil {
		skills = map[string]string{}
	}
	raw, err := json.Marshal(skills)
	if err != nil {
		return fmt.Errorf("encode layer skills: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, sv1.SkillShowcaseFile), raw, 0o644); err != nil {
		return fmt.Errorf("render layer skills: %w", err)
	}
	return nil
}
