package svc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
)

const maxLayerSkills = 32
const maxOwnSkills = 32
const maxEncodedSkillBytes = 12 << 10
const ownSkillRoot = "/skills"

var skillName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
var skillWrites sync.Mutex

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
	for _, name := range slices.Sorted(maps.Keys(skills)) {
		if !skillName.MatchString(name) {
			return herrors.New(herrors.CodeInvalid, "err.skill-format")
		}
		if err := scanSkill(name+".md", skills[name]); err != nil {
			return err
		}
	}
	return nil
}

func ownSkillName(file string) (string, bool) {
	rel, inSkills := strings.CutPrefix(file, strings.TrimPrefix(ownSkillRoot, "/")+"/")
	name, markdown := strings.CutSuffix(rel, ".md")
	return name, inSkills && markdown && skillName.MatchString(name)
}

var skillRules = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"secret-like", regexp.MustCompile(`(?i)-----BEGIN [A-Z ]*PRIVATE KEY-----|(?:api[_-]?key|access[_-]?token|client[_-]?secret|password|token)["']?[[:space:]]*[:=][[:space:]]*["']?[a-z0-9_./+=-]{8,}|bearer[[:space:]]+[a-z0-9_./+=-]{12,}|\b(?:gh[pousr]_|AKIA|sk-(?:proj-)?)[a-z0-9_-]{12,}`)},
	{"hidden-control", regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f\x{200b}\x{202a}-\x{202e}\x{2066}-\x{2069}\x{feff}]`)},
	{"instruction-override", regexp.MustCompile(`(?i)\b(?:ignore|override|disregard)[[:space:]]+(?:all[[:space:]]+)?(?:previous|system|developer)[[:space:]]+instructions\b`)},
}

func scanSkill(file, body string) error {
	if body == "" || len(body) > maxWorkspaceBytes || !utf8.ValidString(body) {
		return herrors.New(herrors.CodeInvalid, "err.skill-format")
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode skill body: %w", err)
	}
	if len(encoded) > maxEncodedSkillBytes {
		return herrors.New(herrors.CodeInvalid, "err.skill-format")
	}
	for line, text := range strings.Split(body, "\n") {
		for _, rule := range skillRules {
			if rule.pattern.MatchString(text) {
				finding := herrors.New(herrors.CodeInvalid, "err.skill-scan").
					With("file", file).With("line", line+1).With("rule", rule.name)
				return fmt.Errorf("scan %s: %w", file, finding)
			}
		}
	}
	return nil
}

func saveOwnSkill(root *os.Root, file, body string) error {
	if err := scanSkill(file, body); err != nil {
		return err
	}
	_, err := root.Lstat(file)
	if errors.Is(err, fs.ErrNotExist) {
		entries, err := boundedDirectory(root, ".")
		if err != nil {
			return err
		}
		if len(entries) >= maxOwnSkills {
			return fileRefusal("err.skill-limit")
		}
	} else if err != nil {
		return workspaceIO(err)
	}
	return atomicWorkspaceWrite(root, file, body)
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
