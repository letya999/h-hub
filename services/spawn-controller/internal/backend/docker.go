// Package backend is the only place h-hub talks to the Docker Engine
// API (rule 11). It owns the hardened runtime container spec: pinned
// image digest, cap-drop ALL, read-only rootfs, non-root uid,
// resource limits, tmpfs, and exactly the networks the descriptor
// grants.
package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"time"
)

// Docker is a minimal Engine API client over the unix socket.
type Docker struct {
	hc   *http.Client
	sock string
}

// New connects to the engine socket.
func New(sock string) *Docker {
	return &Docker{sock: sock, hc: &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sock)
			},
		},
	}}
}

// Launch describes one hardened runtime container.
type Launch struct {
	Name      string   // container name == runtime endpoint host
	Image     string   // pinned name@sha256
	UID       string   // non-root uid:gid
	Env       []string // exact allowlist, nothing else
	MemBytes  int64
	NanoCPUs  int64
	Pids      int64
	Network   string // single internal network
	RenderVol string // volume mounted ro at /etc/hhub/render
	ConfigDir string // directory inside RenderVol with config.json
}

// createReq mirrors POST /containers/create.
type createReq struct {
	Image        string              `json:"Image"`
	User         string              `json:"User"`
	Env          []string            `json:"Env"`
	Labels       map[string]string   `json:"Labels"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`
	HostConfig   hostConfig          `json:"HostConfig"`
}

type hostConfig struct {
	NetworkMode    string            `json:"NetworkMode"`
	CapDrop        []string          `json:"CapDrop"`
	ReadonlyRootfs bool              `json:"ReadonlyRootfs"`
	Memory         int64             `json:"Memory"`
	NanoCPUs       int64             `json:"NanoCPUs"`
	PidsLimit      int64             `json:"PidsLimit"`
	Tmpfs          map[string]string `json:"Tmpfs"`
	Mounts         []mount           `json:"Mounts,omitempty"`
	SecurityOpt    []string          `json:"SecurityOpt"`
}

type mount struct {
	Type     string `json:"Type"`
	Source   string `json:"Source"`
	Target   string `json:"Target"`
	ReadOnly bool   `json:"ReadOnly"`
}

// CreateAndStart creates the container with the hardened spec and
// starts it. The caller must have written the intent row first.
func (d *Docker) CreateAndStart(ctx context.Context, l Launch) (string, error) {
	id, running, err := d.existingLaunch(ctx, l)
	if err != nil {
		return "", err
	}
	if id != "" {
		if !running {
			if err := d.req(ctx, http.MethodPost, "/containers/"+id+"/start", nil, nil); err != nil {
				return "", err
			}
		}
		return id, nil
	}
	spec := createReq{
		Image: l.Image,
		User:  l.UID,
		Env:   l.Env,
		Labels: map[string]string{
			"hhub.managed": "true",
			"hhub.runtime": l.Name,
		},
		HostConfig: hostConfig{
			NetworkMode:    l.Network,
			CapDrop:        []string{"ALL"},
			ReadonlyRootfs: true,
			Memory:         l.MemBytes,
			NanoCPUs:       l.NanoCPUs,
			PidsLimit:      l.Pids,
			Tmpfs:          map[string]string{"/tmp": "rw,noexec,nosuid,size=16m"},
			SecurityOpt:    []string{"no-new-privileges"},
		},
	}
	if l.RenderVol != "" {
		spec.HostConfig.Mounts = []mount{{
			Type: "volume", Source: l.RenderVol,
			Target: "/etc/hhub/render", ReadOnly: true,
		}}
	}
	var out struct{ ID string }
	if err := d.req(ctx, http.MethodPost,
		"/containers/create?name="+l.Name, spec, &out); err != nil {
		return "", err
	}
	if err := d.req(ctx, http.MethodPost,
		"/containers/"+out.ID+"/start", nil, nil); err != nil {
		return "", err
	}
	return out.ID, nil
}

// existingLaunch recovers a container created before the intent was acknowledged.
func (d *Docker) existingLaunch(ctx context.Context, l Launch) (string, bool, error) {
	var out struct {
		ID     string `json:"Id"`
		Config struct {
			Image  string
			User   string
			Env    []string
			Labels map[string]string
		}
		State struct{ Running bool }
	}
	err := d.req(ctx, http.MethodGet, "/containers/"+l.Name+"/json", nil, &out)
	if isNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if out.ID == "" || out.Config.Image != l.Image || out.Config.User != l.UID ||
		len(out.Config.Env) < len(l.Env) || !slices.Equal(out.Config.Env[:len(l.Env)], l.Env) ||
		out.Config.Labels["hhub.managed"] != "true" ||
		out.Config.Labels["hhub.runtime"] != l.Name {
		return "", false, fmt.Errorf("docker container %s does not match runtime intent", l.Name)
	}
	return out.ID, out.State.Running, nil
}

// ImageID resolves an image reference to its immutable id, used to
// pin the passport's image field at spec compile time.
func (d *Docker) ImageID(ctx context.Context, ref string) (string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	if err := d.req(ctx, http.MethodGet, "/images/"+ref+"/json", nil, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// Inspect returns (exists, running) for a container id or name.
func (d *Docker) Inspect(ctx context.Context, id string) (exists, running bool, err error) {
	var out struct {
		State struct{ Running bool }
	}
	err = d.req(ctx, http.MethodGet, "/containers/"+id+"/json", nil, &out)
	if err != nil {
		if isNotFound(err) {
			return false, false, nil
		}
		return false, false, err
	}
	return true, out.State.Running, nil
}

// Remove force-removes a container; a missing container is already done.
func (d *Docker) Remove(ctx context.Context, id string) error {
	err := d.req(ctx, http.MethodDelete,
		"/containers/"+id+"?force=true", nil, nil)
	if err != nil && !isNotFound(err) {
		return err
	}
	return nil
}

type notFoundError struct{ msg string }

func (e notFoundError) Error() string { return e.msg }

func isNotFound(err error) bool {
	var nf notFoundError
	return errors.As(err, &nf)
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
	if res.StatusCode == http.StatusNotFound {
		return notFoundError{msg: "docker: " + path + ": " + res.Status}
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("docker %s %s: %s: %s", method, path, res.Status, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("docker decode: %w", err)
		}
	}
	return nil
}
