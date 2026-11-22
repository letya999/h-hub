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
	Name   string
	Mem    string
	CPU    float64
	Pids   int
	Env    []envKV
	Mounts []string
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
    networks: [hhub]
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
	for _, s := range f.Services {
		if !s.Deploy {
			continue
		}
		sv, err := s.view()
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

func (s Service) view() (svcView, error) {
	v := svcView{Name: s.Name, Mem: s.Mem, CPU: s.CPU, Pids: s.Pids}
	for _, e := range s.Env {
		val, err := s.envValue(e)
		if err != nil {
			return v, err
		}
		v.Env = append(v.Env, envKV{K: e, V: val})
	}
	for _, m := range s.Volumes {
		mount := m.Name + ":" + m.Path + ":ro"
		if m.RW {
			mount = m.Name + ":" + m.Path
		}
		v.Mounts = append(v.Mounts, mount)
	}
	if s.Name == "spawn-controller" {
		v.Mounts = append(v.Mounts, "/var/run/docker.sock:/var/run/docker.sock")
	}
	return v, nil
}

// envFixed is the closed set of compose values independent of the
// service identity. A name outside it (and the per-service cases
// below) fails generation.
var envFixed = map[string]string{
	"HHUB_KEYS":          keysPath,
	"HHUB_CONFIG":        keysPath + "/config.json",
	"HHUB_RUNTIME_IMAGE": "hhub-harness-runtime:local",
	"HHUB_DOCKER_SOCK":   "/var/run/docker.sock",
	"HHUB_IDENTITY_URL":  "http://identity-hub:9000",
	"HHUB_SPAWN_URL":     "http://spawn-controller:9000",
	"HHUB_TASK_URL":      "http://task-engine:9000",
	"HHUB_MODEL_URL":     "http://scripted-model:9000",
	// Compose prefixes volume names with the project name "hhub".
	"HHUB_RENDER_VOL": "hhub_hhub-render",
	"HHUB_RENDER_DIR": "/render",
	"HHUB_NETWORK":    "hhub",
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
