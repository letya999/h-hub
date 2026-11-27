package svc

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/locale"
)

const skillPromptBytes = 4096

type skillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func skillPrompt(s *Server, ctx context.Context, r thv1.SkillPromptReq) (thv1.SkillPromptResp, error) {
	var out thv1.SkillPromptResp
	claims, err := s.runClaims(ctx, r.Run, r.Digest)
	if err != nil {
		return out, err
	}
	p, err := s.projection(ctx, thv1.ProjectionReq{Run: r.Run, Digest: r.Digest})
	if err != nil {
		return out, err
	}
	visible := func(t thv1.ToolInfo) bool { return t.Toolset == "skills" && t.Name == "skills_search" }
	if !slices.ContainsFunc(p.Core, visible) && !slices.ContainsFunc(p.Extended, visible) {
		return out, herrors.New(herrors.CodeForbidden, "err.tool-denied")
	}
	pin, err := pinnedSkills(s, ctx, claims, p.Digest)
	if err != nil {
		return out, err
	}
	out.Text, err = renderSkillPrompt(pin)
	return out, err
}

func renderSkillPrompt(pin skillPin) (string, error) {
	summaries := skillSummaries(pin.inventory.Entries, "")
	list, err := json.Marshal(summaries)
	if err != nil {
		return "", fmt.Errorf("encode skill descriptions: %w", err)
	}
	heading := locale.T(locale.EN, "tool.skills.catalog", "revision", pin.revision)
	if len(heading)+1+len(list) <= skillPromptBytes {
		return heading + "\n" + string(list), nil
	}
	names := slices.Sorted(maps.Keys(pin.inventory.Entries))
	list, err = json.Marshal(names)
	if err != nil {
		return "", fmt.Errorf("encode skill names: %w", err)
	}
	text := heading + "\n" + string(list) + "\n" + locale.T(locale.EN, "tool.skills.search-hint")
	if len(text) > skillPromptBytes {
		return "", herrors.New(herrors.CodeInvalid, "err.skill-limit")
	}
	return text, nil
}

func skillSummaries(entries map[string]string, query string) []skillSummary {
	matches := []skillSummary{}
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		description := skillDescription(entries[name])
		if strings.Contains(strings.ToLower(name), query) || strings.Contains(strings.ToLower(description), query) {
			matches = append(matches, skillSummary{Name: name, Description: description})
		}
	}
	return matches
}

func skillDescription(body string) string {
	for line := range strings.Lines(body) {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
		if line != "" {
			runes := []rune(line)
			return string(runes[:min(len(runes), 120)])
		}
	}
	return ""
}

func invokeSkills(s *Server, ctx context.Context, c toolCall) (json.RawMessage, error) {
	key, limit := "query", 256
	if c.name == "skills_read" {
		key, limit = "name", 64
	}
	value, err := skillArgument(c.args, key, limit)
	if err != nil {
		return nil, err
	}
	pin, err := pinnedSkills(s, ctx, c.claims, c.specDigest)
	if err != nil {
		return nil, err
	}
	var result any
	switch c.name {
	case "skills_search":
		result = skillSummaries(pin.inventory.Entries, strings.ToLower(value))
	case "skills_read":
		body, found := pin.inventory.Entries[value]
		if !found {
			return nil, herrors.New(herrors.CodeNotFound, "err.not-found")
		}
		result = map[string]string{"name": value, "markdown": body}
	default:
		return nil, herrors.New(herrors.CodeInvalid, "err.invalid-arg")
	}
	raw, err := json.Marshal(map[string]any{"revision": pin.revision, "result": result})
	if err != nil {
		return nil, fmt.Errorf("encode skill tool result: %w", err)
	}
	return raw, nil
}

func skillArgument(raw json.RawMessage, key string, limit int) (string, error) {
	var args map[string]*string
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", herrors.New(herrors.CodeInvalid, "err.invalid-arg")
	}
	value := args[key]
	if len(args) != 1 || value == nil || len(*value) > limit {
		return "", herrors.New(herrors.CodeInvalid, "err.invalid-arg")
	}
	return *value, nil
}
