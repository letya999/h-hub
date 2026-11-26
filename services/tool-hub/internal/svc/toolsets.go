package svc

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
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

// toolset defines the model surface and the implementation available
// when the tools passport selects the tool-hub executor.
type toolset struct {
	Name     string    `yaml:"name"`
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

// specView decodes the grants, tools and config blocks of the
// effective spec owned by spawn-controller. The hub resolves it per
// request — no local policy copy is kept (rules 3-4).
type specView struct {
	Grants grantsView `json:"grants"`
	Tools  toolsView  `json:"tools"`
	Config configView `json:"config"`
}

type grantsView struct {
	Allow []string `json:"allow"`
}

type toolsView = thv1.Selection

// configView decodes config.approvals — the ask/auto/bypass leaf the
// owner-request module consumes.
type configView struct {
	Approvals approvalsView `json:"approvals"`
}

type approvalsView struct {
	Mode string `json:"mode"`
}

// project computes the projection for one resolved spec — a pure
// function, so the cache row keyed by digest is always safe. A tool
// whose capability is not granted is absent, not refused in place.
func project(spec specView) (thv1.ProjectionResp, error) {
	selected, err := selectedToolsets(spec.Tools)
	if err != nil {
		return thv1.ProjectionResp{}, err
	}
	grants := map[string]bool{}
	for _, g := range spec.Grants.Allow {
		grants[g] = true
	}
	out := thv1.ProjectionResp{
		Core:     projectSection(spec.Tools.Core, selected, grants),
		Extended: projectSection(spec.Tools.Extended, selected, grants),
	}
	if grants["tools.use"] {
		for _, name := range spec.Tools.Names() {
			if spec.Tools.Executor(name) == "harness" {
				out.Native = append(out.Native, name)
			}
		}
	}
	return out, nil
}

func projectSection(names []string, selected, grants map[string]bool) []thv1.ToolInfo {
	var out []thv1.ToolInfo
	for _, tsName := range names {
		if !selected[tsName] {
			continue
		}
		for _, t := range registry[tsName].Tools {
			if grants[t.Capability] {
				out = append(out, toolInfo(tsName, t))
			}
		}
	}
	return out
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

// toolCall bundles what an executor needs for one admitted call.
type toolCall struct {
	ts         toolset
	name       string
	claims     idv1.RunClaims
	args       json.RawMessage
	receipt    thv1.Receipt
	specDigest string
}

// toolhubExec is the toolhub executor's provider dispatch: builtin
// echoes, ownerreq asks the human through the request module.
func toolhubExec(ctx context.Context, s *Server, c toolCall) (json.RawMessage, error) {
	switch c.ts.Provider {
	case "builtin":
		return builtinEcho(ctx, c.args)
	case "files":
		return s.invokeFiles(ctx, c)
	case "config":
		return s.invokeConfigEdit(ctx, c)
	case "ownerreq":
		return invokeOwnerRequest(ctx, s, c)
	}
	return nil, herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
										With("provider", c.ts.Provider)
}

// builtinEcho returns the received argument object unchanged — no
// network, no files, no processes.
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

// selectedToolsets returns the selection executed by this service.
func selectedToolsets(tools toolsView) (map[string]bool, error) {
	out := map[string]bool{}
	for _, name := range tools.Names() {
		executor := tools.Executor(name)
		if executor == "harness" {
			continue
		}
		if _, ok := registry[name]; !ok || executor != "toolhub" {
			return nil, herrors.New(herrors.CodeInvalid, //nolint:wrapcheck // shared error form
				"err.invalid-arg").With("toolset", name)
		}
		out[name] = true
	}
	return out, nil
}
