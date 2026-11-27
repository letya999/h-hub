package svc

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	"github.com/letya999/h-hub/shared/locale"
)

const guideSkillName = "hub/guide"

func currentSkillGuide(s *Server, ctx context.Context, c toolCall, capabilities map[string]string) (string, error) {
	p, err := s.projection(ctx, thv1.ProjectionReq{Run: c.claims.Run, Digest: c.claims.Digest})
	if err != nil {
		return "", err
	}
	return renderSkillGuide(p, capabilities), nil
}

func renderSkillGuide(p thv1.ProjectionResp, capabilities map[string]string) string {
	implemented := map[string]bool{}
	for _, set := range registry {
		for _, tool := range set.Tools {
			implemented[tool.Capability] = true
		}
	}
	var guide strings.Builder
	guide.WriteString("# " + locale.T(locale.EN, "tool.skills.guide-title") + "\n\n")
	guide.WriteString("## " + locale.T(locale.EN, "tool.skills.guide-capabilities") + "\n\n")
	for _, name := range slices.Sorted(maps.Keys(capabilities)) {
		if implemented[name] {
			guide.WriteString("- " + capabilities[name] + "\n")
		}
	}
	guide.WriteString("\n## " + locale.T(locale.EN, "tool.skills.guide-available") + "\n\n")
	available := slices.Concat(p.Core, p.Extended)
	for _, tool := range available {
		fmt.Fprintf(&guide, "- `%s` %s\n", tool.Name, tool.Description)
	}
	visible := func(t thv1.ToolInfo) bool { return t.Name == "ask" }
	if slices.ContainsFunc(available, visible) {
		guide.WriteString("\n" + locale.T(locale.EN, "tool.skills.guide-ask") + "\n")
	}
	return guide.String()
}
