// Package svc is spawn-controller's HTTP surface and lifecycle core:
// spaces from the built-in template, passports compiled to the
// effective spec, the runtime machine, and the reconcile/outbox
// passes that drive containers through the Docker backend only.
package svc

import (
	"crypto/ed25519"
	"database/sql"
	"embed"
	"io/fs"
	"net/http"
	"time"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	"github.com/letya999/h-hub/services/spawn-controller/internal/backend"
	"github.com/letya999/h-hub/shared/edges"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/log"
)

//go:embed migrations
var migrations embed.FS

// Migrations returns the domain migration set.
func Migrations() fs.FS { return migrations }

// Deps are the service's collaborators and launch parameters.
type Deps struct {
	Docker    *backend.Docker
	Hermes    string // official registry reference, verified during the build
	Image     string // deployment image containing the h-hub adapter
	ModelURL  string
	TaskURL   string
	RenderVol string // volume name mounted ro into runtimes
	RenderDir string // local path where configs are rendered
	Network   string // internal network for runtimes
	Issuer    ed25519.PublicKey
	SelfKey   ed25519.PrivateKey
	Identity  string
	EdgeTab   edges.Table // inbound edge table for this service
	Log       *log.L      // op errors are logged, never swallowed
	Now       func() time.Time
}

// Server owns the operation table.
type Server struct {
	sub  string
	db   *sql.DB
	deps Deps
	runM *fsm.Machine
}

// New builds the server.
func New(sub string, db *sql.DB, deps Deps) *Server {
	return &Server{sub: sub, db: db, deps: deps, runM: RuntimeMachine()}
}

// Mux mounts /v1 ops and /healthz. Space ops require owner tokens;
// runtime lifecycle ops require service tokens on declared edges.
func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	ops := sv1.Ops(s.handlers())
	ownerOps := map[string]bool{
		"CreateSpace": true, "GetSpace": true, "DestroySpace": true,
		"DrainRuntime": true, "SetModel": true,
		"SubmitLayers": true, "Preview": true, "Activate": true,
		"Rollback": true, "Explain": true,
	}
	serviceVer := httpapi.ServiceVerifier(s.deps.Issuer, s.sub, s.deps.EdgeTab, s.deps.Now)
	ownerVer := httpapi.EdgeVerifier(s.deps.Issuer, "owner", s.sub, s.deps.EdgeTab, s.deps.Now)
	for name, op := range ops {
		op := op
		run := func(w http.ResponseWriter, r *http.Request) error {
			err := op(w, r)
			if err != nil && s.deps.Log != nil {
				s.deps.Log.Error(r.Context(), "op "+name, err)
			}
			return err
		}
		if ownerOps[name] {
			mux.Handle("/v1/"+name, httpapi.HandleAuth(ownerVer,
				func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
					return run(w, r)
				}))
			continue
		}
		mux.Handle("/v1/"+name, httpapi.HandleAuth(serviceVer,
			func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
				return run(w, r)
			}))
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		healthz(s, w, r)
	})
	return mux
}
