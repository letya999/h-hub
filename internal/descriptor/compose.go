package descriptor

import (
	"bytes"
	"fmt"
	"text/template"
)

// composeView is the flattened data the template renders.
type composeView struct {
	Postgres Postgres
	Volumes  []Volume
	Services []svcView
	Keys     []string // keys-* volumes keydist populates
}

type svcView struct {
	Name    string
	Mem     string
	CPU     float64
	Pids    int
	Env     []envKV
	Mounts  []string
	NetList string
}

type envKV struct{ K, V string }

const (
	keysPath = "/etc/hhub"
	keysPref = "keys-"
)

var composeTpl = template.Must(template.New("compose").Parse(`# GENERATED from deploy/descriptor.yaml by tools/descriptor - do not edit.
name: hhub
services:
  postgres:
    image: {{ .Postgres.Image }}
    container_name: postgres
    restart: unless-stopped
    mem_limit: {{ .Postgres.Mem }}
    cpus: {{ .Postgres.CPU }}
    pids_limit: {{ .Postgres.Pids }}
    environment:
      POSTGRES_HOST_AUTH_METHOD: trust
    volumes:
      - {{ .Postgres.Volume }}:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 5s
      timeout: 3s
      retries: 12
    networks: [hhub]
  keydist:
    image: hhub-hubctl:local
    container_name: keydist
    entrypoint: ["keydist"]
    command: ["--conf", "/conf", "--keys", "/keys", "--descriptor", "/descriptor.yaml"]
    volumes:
      - hhub-conf:/conf:ro
      - ./descriptor.yaml:/descriptor.yaml:ro
{{- range .Keys }}
      - {{ . }}:/keys/{{ . }}
{{- end }}
    networks: [hhub]
{{- range .Services }}
  {{ .Name }}:
    image: hhub-{{ .Name }}:local
    container_name: {{ .Name }}
    restart: unless-stopped
    mem_limit: {{ .Mem }}
    cpus: {{ .CPU }}
    pids_limit: {{ .Pids }}
    environment:
{{- range .Env }}
      {{ .K }}: {{ .V }}
{{- end }}
{{- if .Mounts }}
    volumes:
{{- range .Mounts }}
      - {{ . }}
{{- end }}
{{- end }}
    depends_on:
      postgres: {condition: service_healthy}
      keydist: {condition: service_completed_successfully}
    networks: [{{ .NetList }}]
{{- end }}
volumes:
  # hhub-conf is shared with install-time hubctl runs outside compose;
  # pin the name so the project prefix does not split it in two.
  hhub-conf:
    name: hhub-conf
{{- range .Volumes }}
  {{ .Name }}:
{{- end }}
networks:
  hhub:
    name: hhub
    internal: true
  # hhub-space is the space zone: runtimes join only this net and can
  # reach just the services their descriptor edges name.
  hhub-space:
    name: hhub-space
    internal: true
  # hhub-egress is the only net with outbound; egress-zone services
  # and the cliproxy upstream live here.
  hhub-egress:
    name: hhub-egress
`))

// Compose renders deploy/compose.yaml: postgres, the key materializer
// and every service marked deploy:true. Space-zone runtimes are
// spawned dynamically and never appear here.
func (f *File) Compose() ([]byte, error) {
	v, err := f.composeView()
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := composeTpl.Execute(&b, v); err != nil {
		return nil, fmt.Errorf("compose render: %w", err)
	}
	return b.Bytes(), nil
}

func (f *File) composeView() (composeView, error) {
	v := composeView{Postgres: f.Postgres}
	v.Volumes = append(v.Volumes, f.Volumes...)
	targets := f.spaceTargets()
	for _, s := range f.Services {
		if !s.Deploy {
			continue
		}
		sv, err := s.view(targets[s.Name])
		if err != nil {
			return v, err
		}
		v.Services = append(v.Services, sv)
	}
	for _, vol := range f.Volumes {
		if len(vol.Name) > len(keysPref) && vol.Name[:len(keysPref)] == keysPref {
			v.Keys = append(v.Keys, vol.Name)
		}
	}
	return v, nil
}

func (s Service) view(spaceTarget bool) (svcView, error) {
	v := svcView{Name: s.Name, Mem: s.Mem, CPU: s.CPU, Pids: s.Pids}
	for _, e := range s.Env {
		val, err := s.envValue(e)
		if err != nil {
			return v, err
		}
		v.Env = append(v.Env, envKV{K: e, V: val})
	}
	v.Mounts = s.mounts()
	v.NetList = s.netList(spaceTarget)
	return v, nil
}

// spaceTargets names the services a space-zone edge may reach: they
// join the space network so a runtime can call them.
func (f *File) spaceTargets() map[string]bool {
	out := map[string]bool{}
	for _, s := range f.Services {
		if s.Zone != "space" {
			continue
		}
		for _, e := range s.Edges {
			out[e.To] = true
		}
	}
	return out
}

// netList is the service's compose networks: the control net always,
// the space net when a runtime edge names the service, and the egress
// net for the egress zone.
func (s Service) netList(spaceTarget bool) string {
	list := "hhub"
	if spaceTarget {
		list += ", hhub-space"
	}
	// The egress and edge zones both reach out: egress faces model
	// providers, edge polls channel providers (Telegram).
	if s.Zone == "egress" || s.Zone == "edge" {
		list += ", hhub-egress"
	}
	return list
}

func (s Service) mounts() []string {
	var mounts []string
	for _, m := range s.Volumes {
		mount := m.Name + ":" + m.Path + ":ro"
		if m.RW {
			mount = m.Name + ":" + m.Path
		}
		mounts = append(mounts, mount)
	}
	if s.Name == "spawn-controller" {
		mounts = append(mounts, "/var/run/docker.sock:/var/run/docker.sock")
	}
	if s.Name == "identity-hub" || s.Name == "credential-broker" {
		mounts = append(mounts, "./../../h-hub-data/secrets:/run/hhub-secrets")
	}
	return mounts
}

// envFixed is the closed set of compose values independent of the
// service identity. A name outside it (and the per-service cases
// below) fails generation.
var envFixed = map[string]string{
	"HHUB_KEYS":          keysPath,
	"HHUB_KEK_PATH":      "/run/hhub-secrets/seal-kek",
	"HHUB_CONFIG":        keysPath + "/config.json",
	"HHUB_RUNTIME_IMAGE": "hhub-harness-runtime:local",
	"HHUB_DOCKER_SOCK":   "/var/run/docker.sock",
	"HHUB_IDENTITY_URL":  "http://identity-hub:9000",
	"HHUB_SPAWN_URL":     "http://spawn-controller:9000",
	"HHUB_CHANNEL_URL":   "http://channel-hub:9000",
	"HHUB_TASK_URL":      "http://task-engine:9000",
	"HHUB_MODEL_URL":     "http://model-gateway:9000",
	"HHUB_BROKER_URL":    "http://credential-broker:9000",
	"HHUB_TOOL_URL":      "http://tool-hub:9000",
	"HHUB_DB_PATH":       "/data/broker.db",
	// Compose prefixes volume names with the project name "hhub".
	"HHUB_RENDER_VOL": "hhub_hhub-render",
	"HHUB_RENDER_DIR": "/render",
	"HHUB_NETWORK":    "hhub-space",
}

// envValue resolves each declared variable to its compose value.
func (s Service) envValue(e string) (string, error) {
	if e == "HHUB_SUB" {
		return s.Name, nil
	}
	if e == "HHUB_DSN" {
		db := dbIdent(s.Name)
		return "postgres://" + db + "@postgres:5432/" + db + "?sslmode=disable", nil
	}
	if e == "HHUB_DB_KEY" {
		return "/run/hhub-secrets/" + dbIdent(s.Name) + "-db-key", nil
	}
	if v, ok := envFixed[e]; ok {
		return v, nil
	}
	return "", fmt.Errorf("descriptor: %s: no compose value for env %q", s.Name, e)
}

func dbIdent(name string) string {
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		if name[i] == '-' {
			out = append(out, '_')
		} else {
			out = append(out, name[i])
		}
	}
	return string(out)
}
