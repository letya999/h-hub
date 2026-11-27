// Package config is the single place this service reads its declared
// environment (rule 14).
package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	wv1 "github.com/letya999/h-hub/contracts/worker/v1"
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
	Workers     wv1.Policy
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
	hermes, err := hermesImage()
	if err != nil {
		return c, err
	}
	c.HermesImage = hermes
	c.Workers, err = wv1.ReadPolicy()
	if err != nil {
		return c, fmt.Errorf("load worker policy: %w", err)
	}
	return c, nil
}

func hermesImage() (string, error) {
	raw, err := os.ReadFile("/usr/share/hhub/hermes-image.ref")
	if err != nil {
		return "", fmt.Errorf("hermes image pin: %w", err)
	}
	ref := strings.TrimSpace(string(raw))
	if !hermesRelease.MatchString(ref) {
		return "", fmt.Errorf("invalid Hermes image pin")
	}
	return ref, nil
}

var hermesRelease = regexp.MustCompile(`^docker\.io/nousresearch/hermes-agent:v[0-9]+\.[0-9]+\.[0-9]+@sha256:[0-9a-f]{64}$`)
