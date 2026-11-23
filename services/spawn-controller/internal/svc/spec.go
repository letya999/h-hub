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

// ModelsPassport routes the one scripted model endpoint.
type ModelsPassport struct {
	Default string `json:"default"`
}

// Limits are the space-zone resource ceilings from the descriptor.
type Limits struct {
	Mem  string
	CPU  string
	Pids int64
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
		Models: ModelsPassport{Default: modelURL},
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
