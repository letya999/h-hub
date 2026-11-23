package svc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Passports are the phase-3 minimal set: stack, config, models. The
// effective spec compiles them once into one document with one
// digest (rule 4) — services hold no independent policy copies.
type Passports struct {
	SchemaVersion int            `json:"schema_version"`
	Stack         StackPassport  `json:"stack"`
	Config        ConfigPassport `json:"config"`
	Models        ModelsPassport `json:"models"`
}

// StackPassport pins the harness image and container limits.
type StackPassport struct {
	Image string `json:"image"` // pinned name@sha256
	UID   string `json:"uid"`
	Mem   string `json:"mem"`
	CPU   string `json:"cpu"`
	Pids  int64  `json:"pids"`
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

// Limits are the space-zone resource ceilings from the descriptor.
type Limits struct {
	Mem  string
	CPU  string
	Pids int64
}

// modelDefaults is the phase-6 route table: the scripted upstreams
// for checks and CI plus the real provider behind cliproxy. The
// scripted-alt route exists so /model has a second choice in tests.
func modelDefaults() ModelsPassport {
	return ModelsPassport{
		Default: "scripted",
		Routes: []ModelRoute{
			{Name: "scripted", URL: "http://scripted-model:9000", Model: "scripted", Vision: true},
			{Name: "scripted-alt", URL: "http://scripted-model:9000", Model: "scripted"},
			{Name: "devin", URL: "http://cliproxy:8317", Model: "devin/swe-2",
				KeyRef: "cred://builtin/platform/cliproxy-key", Vision: true},
		},
		DailyTokens: 1_000_000,
		RunTokens:   250_000,
		Reserve:     4096,
	}
}

// normalize fills the fields a phase-5 spec predates: the routes
// table and the token ceilings. The stored document is upgraded in
// place at resolve time so the digest keeps pinning real bytes.
func (p *Passports) normalize(modelURL string) bool {
	old := p.Models.Routes == nil || p.Models.DailyTokens == 0
	if !old {
		return false
	}
	p.Models = modelDefaults()
	p.Config.ModelURL = modelURL
	return true
}

// defaultPassports is the phase-3 built-in template. The image comes
// from service config (pinned digest), the model route from the
// declared env, limits from the descriptor space zone.
func defaultPassports(image, modelURL, taskURL string, lim Limits) Passports {
	return Passports{
		SchemaVersion: 1,
		Stack: StackPassport{
			Image: image, UID: "65534:65534",
			Mem: lim.Mem, CPU: lim.CPU, Pids: lim.Pids,
		},
		Config: ConfigPassport{
			Sub: "harness-runtime", TaskURL: taskURL,
			ModelURL: modelURL, TimeoutMS: 120000,
		},
		Models: modelDefaults(),
	}
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
