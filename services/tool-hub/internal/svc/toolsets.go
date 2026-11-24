package svc

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"gopkg.in/yaml.v3"
)

//go:embed platform
var platformFS embed.FS

// toolsetFile is the embedded platform/toolsets.yaml: the registry
// of what tools exist, grouped by the toolset a model can see.
type toolsetFile struct {
	Toolsets []toolset `yaml:"toolsets"`
}

// toolset is the first level of the tool model: the set the model
// sees. Executor names the admission path (rule: toolset -> executor
// -> provider); provider names the backing implementation inside it.
type toolset struct {
	Name     string    `yaml:"name"`
	Executor string    `yaml:"executor"`
	Provider string    `yaml:"provider"`
	Tools    []toolDef `yaml:"tools"`
}

// toolDef is one tool: its model-facing text and the single
// capability from the catalog it requires. Params carries the yaml
// argument schema; Parameters is its JSON form for the contract.
type toolDef struct {
	Name        string          `yaml:"name"`
	Description string          `yaml:"description"`
	Capability  string          `yaml:"capability"`
	Params      map[string]any  `yaml:"parameters"`
	Parameters  json.RawMessage `yaml:"-"`
}

var registry = mustToolsets()

// mustToolsets loads the registry once; a malformed file is a build
// defect, not a runtime state.
func mustToolsets() map[string]toolset {
	data, err := platformFS.ReadFile("platform/toolsets.yaml")
	if err != nil {
		panic(fmt.Sprintf("toolsets: %v", err))
	}
	var f toolsetFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		panic(fmt.Sprintf("toolsets parse: %v", err))
	}
	out := map[string]toolset{}
	toolNames := map[string]bool{}
	for _, ts := range f.Toolsets {
		if ts.Name == "" || out[ts.Name].Name != "" {
			panic(fmt.Sprintf("toolsets: bad or duplicate %q", ts.Name))
		}
		if _, ok := executors[ts.Executor]; !ok {
			panic(fmt.Sprintf("toolsets: %s unknown executor %q", ts.Name, ts.Executor))
		}
		for i := range ts.Tools {
			checkTool(&ts.Tools[i], toolNames)
		}
		out[ts.Name] = ts
	}
	return out
}

// checkTool validates one registry tool and bakes its schema.
func checkTool(t *toolDef, names map[string]bool) {
	if t.Name == "" || names[t.Name] {
		panic(fmt.Sprintf("toolsets: bad or duplicate tool %q", t.Name))
	}
	names[t.Name] = true
	raw, err := json.Marshal(t.Params)
	if err != nil {
		panic(fmt.Sprintf("toolsets: %s params: %v", t.Name, err))
	}
	t.Parameters = raw
}

// specView decodes the grants and tools blocks of the effective spec
// owned by spawn-controller. The hub resolves it per request — no
// local policy copy is kept (rules 3-4).
type specView struct {
	Grants grantsView `json:"grants"`
	Tools  toolsView  `json:"tools"`
}

type grantsView struct {
	Allow []string `json:"allow"`
}

type toolsView struct {
	Core     []string `json:"core"`
	Extended []string `json:"extended"`
}

// project computes the projection for one resolved spec — a pure
// function, so the cache row keyed by digest is always safe. A tool
// whose capability is not granted is absent, not refused in place.
func project(spec specView) (thv1.ProjectionResp, error) {
	if _, err := selectedToolsets(spec.Tools.Core, spec.Tools.Extended); err != nil {
		return thv1.ProjectionResp{}, err
	}
	grants := map[string]bool{}
	for _, g := range spec.Grants.Allow {
		grants[g] = true
	}
	var out thv1.ProjectionResp
	for _, tsName := range spec.Tools.Core {
		for _, t := range registry[tsName].Tools {
			if grants[t.Capability] {
				out.Core = append(out.Core, toolInfo(tsName, t))
			}
		}
	}
	for _, tsName := range spec.Tools.Extended {
		for _, t := range registry[tsName].Tools {
			if grants[t.Capability] {
				out.Extended = append(out.Extended, toolInfo(tsName, t))
			}
		}
	}
	return out, nil
}

func toolInfo(tsName string, t toolDef) thv1.ToolInfo {
	return thv1.ToolInfo{Toolset: tsName, Name: t.Name,
		Description: t.Description, Parameters: t.Parameters}
}

// tool looks up one tool by toolset and name.
func tool(tsName, name string) (toolDef, bool) {
	ts, ok := registry[tsName]
	if !ok {
		return toolDef{}, false
	}
	for _, t := range ts.Tools {
		if t.Name == name {
			return t, true
		}
	}
	return toolDef{}, false
}

// executors is the executor table: the second level of the tool
// model. Only toolhub exists in this phase; other names in the
// closed set (harness, mcp, cli, remote) land with their phases.
var executors = map[string]executor{
	"toolhub": builtinEcho,
}

type executor func(ctx context.Context, args json.RawMessage) (json.RawMessage, error)

// builtinEcho is the toolhub executor's only provider this phase: it
// returns the received argument object unchanged — no network, no
// files, no processes.
func builtinEcho(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		args = json.RawMessage("null")
	}
	out, err := json.Marshal(map[string]json.RawMessage{"echo": args})
	if err != nil {
		return nil, fmt.Errorf("echo marshal: %w", err)
	}
	return out, nil
}

// decide is the admission decision core: the tool must exist in the
// registry, its toolset must be selected by the spec, and the tool's
// capability must be granted. The caller reads the spec first and
// decides after — never under a lock (step-4 rule).
func decide(selected map[string]bool, grants map[string]bool,
	tsName, name string) (allowed bool, caps []string, reason string) {
	t, ok := tool(tsName, name)
	if !ok {
		return false, nil, "err.tool-denied"
	}
	caps = []string{t.Capability}
	if !selected[tsName] || !grants[t.Capability] {
		return false, caps, "err.tool-denied"
	}
	return true, caps, ""
}

// selectedToolsets maps the spec's two tool lists to the set of
// selected toolset names; an unknown name is a spec defect and is
// refused, not skipped.
func selectedToolsets(core, extended []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, name := range append(append([]string{}, core...), extended...) {
		if _, ok := registry[name]; !ok {
			return nil, herrors.New(herrors.CodeInvalid, //nolint:wrapcheck // shared error form
				"err.invalid-arg").With("toolset", name)
		}
		out[name] = true
	}
	return out, nil
}
