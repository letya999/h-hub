// Package config is the single place this service reads its declared
// environment (rule 14). The space zone carries no database env and
// exactly this allowlist crosses the container boundary.
package config

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/letya999/h-hub/shared/auth"
)

// C is the service configuration: the declared env list.
type C struct {
	Sub    string // HHUB_SUB
	Config string // HHUB_CONFIG: host-rendered read-only file
}

// Rendered is the config file shape the controller renders and
// mounts read-only.
type Rendered struct {
	Sub       string `json:"sub"`
	TaskURL   string `json:"task_url"`
	ModelURL  string `json:"model_url"`
	TimeoutMS int    `json:"timeout_ms"`
}

// Env loads C from the environment; every declared variable is
// required (missing input is a refusal).
func Env() (C, error) {
	c := C{
		Sub:    os.Getenv("HHUB_SUB"),
		Config: os.Getenv("HHUB_CONFIG"),
	}
	if c.Sub == "" || c.Config == "" {
		return c, fmt.Errorf("config: HHUB_SUB and HHUB_CONFIG are required")
	}
	return c, nil
}

// Load reads the rendered config file. A missing or malformed file is
// a refusal, never a default.
func (c C) Load() (Rendered, error) {
	data, err := os.ReadFile(c.Config)
	if err != nil {
		return Rendered{}, fmt.Errorf("config: %w", err)
	}
	var r Rendered
	if err := json.Unmarshal(data, &r); err != nil {
		return Rendered{}, fmt.Errorf("config: %w", err)
	}
	if r.TaskURL == "" || r.ModelURL == "" || r.TimeoutMS <= 0 {
		return Rendered{}, fmt.Errorf("config: task_url, model_url and timeout_ms are required")
	}
	return r, nil
}

// IssuerKey loads the run-token issuer's public key, mounted by the
// controller beside the rendered config.
func (c C) IssuerKey() (ed25519.PublicKey, error) {
	pub, err := auth.LoadPublicKey(filepath.Join(
		filepath.Dir(c.Config), "issuer.pub"))
	if err != nil {
		return nil, fmt.Errorf("issuer key: %w", err)
	}
	return pub, nil
}
