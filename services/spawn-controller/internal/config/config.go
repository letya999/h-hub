// Package config is the single place this service reads its declared
// environment (rule 14).
package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// C is the service configuration: the declared env list.
type C struct {
	Sub         string // HHUB_SUB
	DSN         string // HHUB_DSN
	Keys        string // HHUB_KEYS: service.key, pubs/, edges
	DockerSock  string // HHUB_DOCKER_SOCK: engine socket path
	RuntimeImg  string // HHUB_RUNTIME_IMAGE: pinned harness image
	HermesImage string // official release packaged with the controller
	ModelURL    string // HHUB_MODEL_URL: models passport route
	TaskURL     string // HHUB_TASK_URL: report-back address for runtimes
	RenderVol   string // HHUB_RENDER_VOL: volume for rendered configs
	IdentityURL string // HHUB_IDENTITY_URL: service-token issuer
	NetworkName string // HHUB_NETWORK: internal network for runtimes
}

// Env loads C from the environment; every declared variable is
// required (missing input is a refusal).
func Env() (C, error) {
	c := C{
		Sub:         os.Getenv("HHUB_SUB"),
		DSN:         os.Getenv("HHUB_DSN"),
		Keys:        os.Getenv("HHUB_KEYS"),
		DockerSock:  os.Getenv("HHUB_DOCKER_SOCK"),
		RuntimeImg:  os.Getenv("HHUB_RUNTIME_IMAGE"),
		ModelURL:    os.Getenv("HHUB_MODEL_URL"),
		TaskURL:     os.Getenv("HHUB_TASK_URL"),
		RenderVol:   os.Getenv("HHUB_RENDER_VOL"),
		IdentityURL: os.Getenv("HHUB_IDENTITY_URL"),
		NetworkName: os.Getenv("HHUB_NETWORK"),
	}
	vals := []string{c.Sub, c.DSN, c.Keys, c.DockerSock, c.RuntimeImg,
		c.ModelURL, c.TaskURL, c.RenderVol, c.IdentityURL, c.NetworkName}
	for _, v := range vals {
		if v == "" {
			return c, fmt.Errorf("config: all declared HHUB_* values are required")
		}
	}
	raw, err := os.ReadFile("/usr/share/hhub/hermes-image.ref")
	if err != nil {
		return c, fmt.Errorf("hermes image pin: %w", err)
	}
	c.HermesImage = strings.TrimSpace(string(raw))
	if !hermesRelease.MatchString(c.HermesImage) {
		return c, fmt.Errorf("invalid Hermes image pin")
	}
	return c, nil
}

var hermesRelease = regexp.MustCompile(`^docker\.io/nousresearch/hermes-agent:v[0-9]+\.[0-9]+\.[0-9]+@sha256:[0-9a-f]{64}$`)
