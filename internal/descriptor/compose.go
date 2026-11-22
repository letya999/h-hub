package descriptor

import (
	"bytes"
	"fmt"
	"text/template"
)

var composeTpl = template.Must(template.New("compose").Parse(`# GENERATED from deploy/descriptor.yaml by tools/descriptor - do not edit.
name: hhub
services:
  postgres:
    image: {{ .Postgres.Image }}
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
volumes:
{{- range .Volumes }}
  {{ .Name }}:
{{- end }}
networks:
  default:
    name: hhub
    internal: true
`))

// Compose renders deploy/compose.yaml. Only postgres is emitted in
// phase 1; service containers appear with their images.
func (f *File) Compose() ([]byte, error) {
	var b bytes.Buffer
	if err := composeTpl.Execute(&b, f); err != nil {
		return nil, fmt.Errorf("compose render: %w", err)
	}
	return b.Bytes(), nil
}
