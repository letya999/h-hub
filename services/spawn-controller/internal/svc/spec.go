package svc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Passports are the six full-form documents — stack, config, models,
// workspace, tools, grants — compiled from the layer chain into one
// effective spec with one digest (rule 4); services hold no copies.
type Passports struct {
	SchemaVersion int               `json:"schema_version"`
	Stack         StackPassport     `json:"stack"`
	Config        ConfigPassport    `json:"config"`
	Models        ModelsPassport    `json:"models"`
	Workspace     WorkspacePassport `json:"workspace"`
	Tools         ToolsPassport     `json:"tools"`
	Grants        GrantsPassport    `json:"grants"`
}

// StackPassport pins the harness image and container limits.
type StackPassport struct {
	Identity StackIdentity `json:"identity"` // per-space binding
	Image    string        `json:"image"`    // pinned name@sha256
	UID      string        `json:"uid"`
	Mem      string        `json:"mem"`
	CPU      string        `json:"cpu"`
	Pids     int64         `json:"pids"`
}

// ConfigPassport is the harness runtime config shape rendered
// host-side and mounted read-only.
type ConfigPassport struct {
	Sub       string `json:"sub"`
	TaskURL   string `json:"task_url"`
	ModelURL  string `json:"model_url"`
	TimeoutMS int    `json:"timeout_ms"`
}

// ModelsPassport routes the model through the gateway: the named
// default route, the route table, the daily and per-run token
// ceilings and the reservation estimate — all policy, one digest.
type ModelsPassport struct {
	Default     string       `json:"default"`
	Routes      []ModelRoute `json:"routes"`
	DailyTokens int64        `json:"daily_tokens"`
	RunTokens   int64        `json:"run_tokens"`
	Reserve     int64        `json:"reserve"`
}

// ModelRoute is one named upstream the gateway may relay to. KeyRef
// names a credential-broker object the gateway leases per call —
// never a value in the spec itself. Vision declares the upstream
// accepts image content parts; channels gate photo intake on it.
type ModelRoute struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Model  string `json:"model"`
	KeyRef string `json:"key_ref,omitempty"`
	Vision bool   `json:"vision,omitempty"`
}

// StackIdentity carries the per-space binding written by the
// platform layer at create: who the space belongs to.
type StackIdentity struct {
	Owner string `json:"owner"`
}

// WorkspacePassport declares the space's file world: mount layout
// and the workspace files (soul.md, agents.md) rendered into the
// read-only mount.
type WorkspacePassport struct {
	Layout WorkspaceLayout   `json:"layout"`
	Files  map[string]string `json:"files"`
}

// WorkspaceLayout is the ordered node list for the space's mounts.
type WorkspaceLayout struct {
	Nodes []WorkspaceNode `json:"nodes"`
}

// WorkspaceNode is one declared mount point.
type WorkspaceNode struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Note string `json:"note,omitempty"`
}

// ToolsPassport selects visible toolsets; capabilities and the
// executor land with tool-hub in phase 9.
type ToolsPassport struct {
	Core     []string `json:"core"`
	Extended []string `json:"extended"`
}

// GrantsPassport is the unconditional capability ceiling for the
// space; rights come only from here (rule 5).
type GrantsPassport struct {
	Allow []string `json:"allow"`
}

// modelDefaults decodes the shipped platform layer's models doc so
// the normalize upgrade and new spaces share the one route table —
// a second table in Go is what let the vision flag drift.
func modelDefaults() (ModelsPassport, error) {
	docs, err := layerDocs("platform/layer")
	if err != nil {
		return ModelsPassport{}, err
	}
	raw, err := json.Marshal(docs["models"])
	var m ModelsPassport
	if err == nil {
		err = json.Unmarshal(raw, &m)
	}
	if err != nil {
		return ModelsPassport{}, fmt.Errorf("platform layer models: %w", err)
	}
	if len(m.Routes) == 0 {
		return ModelsPassport{}, fmt.Errorf("platform layer: empty models doc")
	}
	return m, nil
}

// normalize fills the fields a phase-5 spec predates: the routes
// table and the token ceilings. The stored document is upgraded in
// place at resolve time so the digest keeps pinning real bytes.
func (p *Passports) normalize(modelURL string) (bool, error) {
	old := p.Models.Routes == nil || p.Models.DailyTokens == 0
	if !old {
		return false, nil
	}
	m, err := modelDefaults()
	if err != nil {
		return false, err
	}
	p.Models = m
	p.Config.ModelURL = modelURL
	return true, nil
}

// compile renders the effective spec as canonical JSON plus its
// digest. Canonical = compact JSON, object keys sorted (encoding/json
// sorts map keys; struct order is fixed by tags).
func compile(p Passports) (spec []byte, digest string, err error) {
	spec, err = json.Marshal(p)
	if err != nil {
		return nil, "", fmt.Errorf("spec marshal: %w", err)
	}
	sum := sha256.Sum256(spec)
	return spec, "sha256:" + hex.EncodeToString(sum[:]), nil
}
