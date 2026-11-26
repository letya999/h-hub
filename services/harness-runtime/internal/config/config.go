// Package config is the single place this service reads its declared
// environment (rule 14). The space zone carries no database env and
// exactly this allowlist crosses the container boundary.
package config

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	"github.com/letya999/h-hub/shared/auth"
)

// C is the service configuration: the declared env list.
type C struct {
	Sub    string // HHUB_SUB
	Config string // HHUB_CONFIG: host-rendered read-only file
}

// Tools reads the controller's projection from the same read-only render mount.
func (c C) Tools() (thv1.ProjectionResp, error) {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(c.Config), "tools.json"))
	if err != nil {
		return thv1.ProjectionResp{}, fmt.Errorf("read launch projection: %w", err)
	}
	var p thv1.ProjectionResp
	if err := json.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("decode launch projection: %w", err)
	}
	if p.Digest == "" {
		return p, fmt.Errorf("launch projection requires a passport digest")
	}
	return p, nil
}

// Rendered is the config file shape the controller renders and
// mounts read-only.
type Rendered struct {
	Space        string `json:"space"`
	Sub          string `json:"sub"`
	TaskURL      string `json:"task_url"`
	ModelURL     string `json:"model_url"`
	ToolURL      string `json:"tool_url"`
	TimeoutMS    int    `json:"timeout_ms"`
	Instructions string `json:"-"`
}

// Env loads C from the environment; every declared variable is
// required (missing input is a refusal).
func Env() (C, error) {
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "HHUB_") && key != "HHUB_CONFIG" && key != "HHUB_SUB" {
			return C{}, fmt.Errorf("config: unexpected environment key %s", key)
		}
	}
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
	// tool_url is not required: a spec activated before tool calling
	// existed renders no value for it, and that spec legitimately
	// runs without tools (the admission edge arrives with the first
	// recompiled spec).
	if r.Space == "" || r.TaskURL == "" || r.ModelURL == "" || r.TimeoutMS <= 0 {
		return Rendered{}, fmt.Errorf("config: space, task_url, model_url and timeout_ms are required")
	}
	var parts []string
	for _, name := range []string{"soul.md", "agents.md"} {
		content, err := os.ReadFile(filepath.Join(filepath.Dir(c.Config), "files", name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return r, fmt.Errorf("read space instructions %s: %w", name, err)
		}
		parts = append(parts, string(content))
	}
	r.Instructions = strings.Join(parts, "\n\n")
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
