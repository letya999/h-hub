// Package testkit's Docker client is the e2e's view of the daemon:
// raw Engine API over the mounted socket for stack lifecycle fixtures
// (restart, kill, orphan containers). Product containers are managed
// only by spawn-controller's backend; this is test infrastructure.
package testkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Docker is a minimal Engine API client over the unix socket.
type Docker struct {
	hc *http.Client
}

// NewDocker connects to the Engine socket (DOCKER_SOCK env override).
func NewDocker(t *testing.T) *Docker {
	t.Helper()
	sock := Env("DOCKER_SOCK", "/var/run/docker.sock")
	return &Docker{hc: &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sock)
			},
		},
	}}
}

func (d *Docker) req(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("docker marshal: %w", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, rd)
	if err != nil {
		return fmt.Errorf("docker request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := d.hc.Do(req)
	if err != nil {
		return fmt.Errorf("docker %s %s: %w", method, path, err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort close
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("docker read: %w", err)
	}
	if res.StatusCode >= 400 {
		return fmt.Errorf("docker %s %s: %d: %s", method, path, res.StatusCode,
			strings.TrimSpace(string(data)))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("docker decode: %w", err)
		}
	}
	return nil
}

// ContainerSummary is one row of /containers/json.
type ContainerSummary struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Labels map[string]string `json:"Labels"`
}

// List returns containers matching the label filter, including stopped
// ones.
func (d *Docker) List(ctx context.Context, label string) ([]ContainerSummary, error) {
	var out []ContainerSummary
	f := url.QueryEscape(fmt.Sprintf(`{"label":[%q]}`, label))
	err := d.req(ctx, http.MethodGet, "/containers/json?all=1&filters="+f, nil, &out)
	return out, err
}

// Inspect returns the full inspect document (loosely decoded).
type Inspect struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Image  string `json:"Image"`
	Config struct {
		Env    []string          `json:"Env"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	HostConfig struct {
		ReadonlyRootfs bool     `json:"ReadonlyRootfs"`
		CapDrop        []string `json:"CapDrop"`
	} `json:"HostConfig"`
	State struct {
		Status  string `json:"Status"`
		Running bool   `json:"Running"`
		Health  struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	Mounts []struct {
		Type        string `json:"Type"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
	NetworkSettings struct {
		Networks map[string]json.RawMessage `json:"Networks"`
	} `json:"NetworkSettings"`
}

// ContainerInspect inspects one container by id or name.
func (d *Docker) ContainerInspect(ctx context.Context, id string) (Inspect, error) {
	var out Inspect
	err := d.req(ctx, http.MethodGet, "/containers/"+id+"/json", nil, &out)
	return out, err
}

// Kill sends SIGKILL to a container.
func (d *Docker) Kill(ctx context.Context, id string) error {
	return d.req(ctx, http.MethodPost, "/containers/"+id+"/kill", nil, nil)
}

// Restart restarts a container with a generous timeout.
func (d *Docker) Restart(ctx context.Context, id string) error {
	return d.req(ctx, http.MethodPost, "/containers/"+id+"/restart?t=10", nil, nil)
}

// CreateSpec is the minimal fixture container spec the e2e needs.
type CreateSpec struct {
	Image    string
	Name     string
	Cmd      []string
	Labels   map[string]string
	Networks []string
}

// CreateRun creates and starts a fixture container, returning its id.
func (d *Docker) CreateRun(ctx context.Context, s CreateSpec) (string, error) {
	body := map[string]any{
		"Image":  s.Image,
		"Cmd":    s.Cmd,
		"Labels": s.Labels,
		"HostConfig": map[string]any{
			"NetworkMode": firstOr(s.Networks, "default"),
		},
	}
	var created struct {
		ID string `json:"Id"`
	}
	err := d.req(ctx, http.MethodPost, "/containers/create?name="+s.Name, body, &created)
	if err != nil {
		return "", err
	}
	for _, n := range s.Networks[1:] {
		err := d.req(ctx, http.MethodPost,
			"/networks/"+n+"/connect", map[string]any{"Container": created.ID}, nil)
		if err != nil {
			return "", fmt.Errorf("connect %s: %w", n, err)
		}
	}
	if err := d.req(ctx, http.MethodPost, "/containers/"+created.ID+"/start", nil, nil); err != nil {
		return "", err
	}
	return created.ID, nil
}

func firstOr(s []string, def string) string {
	if len(s) > 0 {
		return s[0]
	}
	return def
}

// Wait blocks until the container exits and returns its exit code.
func (d *Docker) Wait(ctx context.Context, id string) (int, error) {
	var out struct {
		StatusCode int `json:"StatusCode"`
	}
	err := d.req(ctx, http.MethodPost, "/containers/"+id+"/wait", nil, &out)
	return out.StatusCode, err
}

// Remove force-removes a container.
func (d *Docker) Remove(ctx context.Context, id string) error {
	return d.req(ctx, http.MethodDelete, "/containers/"+id+"?v=1&force=1", nil, nil)
}

// Logs returns combined stdout/stderr tail of a container.
func (d *Docker) Logs(ctx context.Context, id string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://docker/containers/"+id+"/logs?stdout=1&stderr=1&tail=200", nil)
	if err != nil {
		return "", fmt.Errorf("docker logs request: %w", err)
	}
	res, err := d.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("docker logs: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort close
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("docker logs read: %w", err)
	}
	return string(data), nil
}
