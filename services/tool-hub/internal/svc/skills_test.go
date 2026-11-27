package svc

import (
	"fmt"
	"strings"
	"testing"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/locale"
)

func TestSkillCatalogOverflowKeepsEveryName(t *testing.T) {
	t.Parallel()
	entries := map[string]string{"layer/hello": "# A short description"}
	pin := skillPin{inventory: sv1.SkillInventoryResp{Entries: entries}, revision: "fixture"}
	prompt, err := renderSkillPrompt(pin)
	if err != nil || !strings.Contains(prompt, "A short description") {
		t.Fatalf("short catalog missing descriptions: %v", err)
	}
	const count = 62
	for n := range count {
		entries[fmt.Sprintf("own/skill-%02d", n)] = strings.Repeat("description ", 10)
	}
	prompt, err = renderSkillPrompt(pin)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompt) > skillPromptBytes || strings.Contains(prompt, "A short description") ||
		!strings.Contains(prompt, locale.T(locale.EN, "tool.skills.search-hint")) {
		t.Fatal("overflow catalog must return names and a search hint within budget")
	}
	for name := range entries {
		if !strings.Contains(prompt, `"`+name+`"`) {
			t.Fatalf("overflow catalog omitted %q", name)
		}
	}
	if matches := skillSummaries(entries, "short description"); len(matches) != 1 || matches[0].Name != "layer/hello" {
		t.Fatalf("search did not find the description omitted from the prompt: %+v", matches)
	}
}

func TestSkillSearchRequiresOneQuery(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`{}`, `{"query":null}`, `{"query":"x","name":"y"}`, `{"query":false}`} {
		if _, err := skillArgument([]byte(raw), "query", 256); herrors.CodeOf(err) != herrors.CodeInvalid {
			t.Fatalf("malformed query accepted: %v", err)
		}
	}
	if result, err := skillArgument([]byte(`{"query":""}`), "query", 256); err != nil || result != "" {
		t.Fatalf("explicit empty query refused: %q %v", result, err)
	}
}
