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
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"
)

// Docker is a minimal Engine API client over the unix socket.
type Docker struct {
	hc      *http.Client
	sock    string
	network string
}

// New connects to the engine socket.
func New(sock, network string) *Docker {
	return &Docker{sock: sock, network: network, hc: &http.Client{
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
	Name      string // container name == runtime endpoint host
	Image     string // pinned name@sha256
	Adapter   string // immutable h-hub image containing /svc
	Space     string
	Digest    string   // effective passport at launch
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
	Entrypoint   []string            `json:"Entrypoint"`
	Labels       map[string]string   `json:"Labels"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`
	HostConfig   hostConfig          `json:"HostConfig"`
	Healthcheck  healthCheck         `json:"Healthcheck"`
}

// healthCheck runs inside the runtime's own net namespace: the
// control zone has no network path into the space zone, so the
// readiness probe rides the engine instead of HTTP.
type healthCheck struct {
	Test        []string `json:"Test"`
	Interval    int64    `json:"Interval"`
	Timeout     int64    `json:"Timeout"`
	Retries     int64    `json:"Retries"`
	StartPeriod int64    `json:"StartPeriod"`
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
	Init           bool              `json:"Init"`
}

type mount struct {
	Type          string     `json:"Type"`
	Source        string     `json:"Source"`
	Target        string     `json:"Target"`
	ReadOnly      bool       `json:"ReadOnly"`
	VolumeOptions *mountPath `json:"VolumeOptions,omitempty"`
	ImageOptions  *mountPath `json:"ImageOptions,omitempty"`
}

type mountPath struct {
	Subpath string `json:"Subpath"`
}

// CreateAndStart creates the container with the hardened spec and
// starts it. The caller must have written the intent row first.
func (d *Docker) CreateAndStart(ctx context.Context, l Launch) (string, error) {
	if l.Network == "" || l.Network != d.network {
		return "", fmt.Errorf("docker launch outside network %q", d.network)
	}
	spec := runtimeSpec(l)
	var upstream struct{ Config struct{ Env []string } }
	if err := d.req(ctx, http.MethodGet, "/images/"+l.Image+"/json", nil, &upstream); err != nil {
		return "", fmt.Errorf("inspect pinned Hermes: %w", err)
	}
	spec.Env = append(slices.Clone(l.Env), upstream.Config.Env...)
	id, running, err := d.existingLaunch(ctx, l.Name, spec)
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

func runtimeSpec(l Launch) createReq {
	return createReq{
		Image:      l.Image,
		User:       l.UID,
		Env:        l.Env,
		Entrypoint: []string{"/hhub-adapter"},
		Labels: map[string]string{
			"hhub.managed": "true",
			"hhub.runtime": l.Name,
			"hhub.agent":   "main",
			"hhub.space":   l.Space,
			"hhub.digest":  l.Digest,
		},
		HostConfig: hostConfig{
			NetworkMode:    l.Network,
			CapDrop:        []string{"ALL"},
			ReadonlyRootfs: true,
			Memory:         l.MemBytes,
			NanoCPUs:       l.NanoCPUs,
			PidsLimit:      l.Pids,
			Tmpfs: map[string]string{
				"/tmp":      "rw,noexec,nosuid,size=16m",
				"/opt/data": "rw,nodev,noexec,nosuid,uid=65534,gid=65534,mode=0700,size=64m",
			},
			Init: true,
			Mounts: []mount{
				{Type: "image", Source: l.Adapter, Target: "/hhub-adapter", ReadOnly: true, ImageOptions: &mountPath{"svc"}},
				{Type: "image", Source: l.Adapter, Target: "/opt/data/plugins", ReadOnly: true, ImageOptions: &mountPath{"hhub-plugins"}},
				{Type: "volume", Source: l.RenderVol, Target: "/etc/hhub/render", ReadOnly: true, VolumeOptions: &mountPath{l.ConfigDir}},
				{Type: "volume", Source: l.RenderVol, Target: "/opt/data/config.yaml", ReadOnly: true, VolumeOptions: &mountPath{l.ConfigDir + "/config.yaml"}},
			},
			SecurityOpt: []string{"no-new-privileges"},
		},
		Healthcheck: healthCheck{
			Test: []string{"CMD", "curl", "-fsS", "-o", "/dev/null",
				"http://127.0.0.1:9000/healthz"},
			Interval:    1e9,
			Timeout:     2e9,
			Retries:     30,
			StartPeriod: 5e9,
		},
	}
}

// existingLaunch recovers a container created before the intent was acknowledged.
type containerInspect struct {
	ID     string `json:"Id"`
	Config struct {
		Image      string
		User       string
		Env        []string
		Entrypoint []string
		Labels     map[string]string
	}
	HostConfig hostConfig
	State      struct {
		Running bool
		Health  struct {
			Status string
		}
	}
	NetworkSettings struct{ Networks map[string]json.RawMessage }
}

func (d *Docker) existingLaunch(ctx context.Context, name string, spec createReq) (string, bool, error) {
	var out containerInspect
	err := d.req(ctx, http.MethodGet, "/containers/"+name+"/json", nil, &out)
	if isNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if out.ID == "" || !out.matches(spec) || !d.onNetwork(out.NetworkSettings.Networks) {
		return "", false, fmt.Errorf("docker container %s does not match runtime intent", name)
	}
	return out.ID, out.State.Running, nil
}

func (out containerInspect) matches(spec createReq) bool {
	for label, value := range spec.Labels {
		if stored, ok := out.Config.Labels[label]; !ok || stored != value {
			return false
		}
	}
	return out.Config.Image == spec.Image && out.Config.User == spec.User &&
		slices.Equal(out.Config.Env, spec.Env) && slices.Equal(out.Config.Entrypoint, spec.Entrypoint) &&
		reflect.DeepEqual(out.HostConfig, spec.HostConfig) && len(out.NetworkSettings.Networks) == 1
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
	var out containerInspect
	err = d.req(ctx, http.MethodGet, "/containers/"+id+"/json", nil, &out)
	if err != nil {
		if isNotFound(err) {
			return false, false, nil
		}
		return false, false, err
	}
	if !d.onNetwork(out.NetworkSettings.Networks) {
		return false, false, fmt.Errorf("docker container %s outside network %s", id, d.network)
	}
	return true, out.State.Running, nil
}

func (d *Docker) onNetwork(networks map[string]json.RawMessage) bool {
	_, ok := networks[d.network]
	return d.network != "" && ok
}

// Healthy reports the engine-side health of a container: the runtime
// probes itself, so readiness needs no network path from the control
// zone into the space zone.
func (d *Docker) Healthy(ctx context.Context, id string) (bool, error) {
	var out containerInspect
	if err := d.req(ctx, http.MethodGet, "/containers/"+id+"/json", nil, &out); err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return out.State.Health.Status == "healthy", nil
}

// ManagedContainer identifies a container owned by one runtime.
type ManagedContainer struct{ ID, Runtime string }

// ManagedContainers lists runtime containers, including stopped ones.
func (d *Docker) ManagedContainers(ctx context.Context) ([]ManagedContainer, error) {
	var rows []struct {
		ID              string            `json:"Id"`
		Names           []string          `json:"Names"`
		Labels          map[string]string `json:"Labels"`
		NetworkSettings struct{ Networks map[string]json.RawMessage }
	}
	filter := url.QueryEscape(`{"label":["hhub.managed=true"]}`)
	if err := d.req(ctx, http.MethodGet, "/containers/json?all=1&filters="+filter, nil, &rows); err != nil {
		return nil, fmt.Errorf("list managed containers: %w", err)
	}
	var out []ManagedContainer
	for _, row := range rows {
		name := row.Labels["hhub.runtime"]
		if strings.HasPrefix(name, "hhub-rt-") && slices.Contains(row.Names, "/"+name) &&
			d.onNetwork(row.NetworkSettings.Networks) {
			out = append(out, ManagedContainer{row.ID, name})
		}
	}
	return out, nil
}

// Remove force-removes a container; a missing container is already done.
func (d *Docker) Remove(ctx context.Context, id string) error {
	exists, _, err := d.Inspect(ctx, id)
	if err != nil || !exists {
		return err
	}
	err = d.req(ctx, http.MethodDelete,
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
