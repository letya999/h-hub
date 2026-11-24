// Package svc is tool-hub's HTTP surface and admission core: the
// run-token-verified AdmitCall, Projection and Invoke ops, and the
// receipt ledger behind every decision. Grants and tool selection
// live in the caller's effective spec resolved from spawn-controller
// per request — the hub keeps no policy copy of its own (rules 3-4).
package svc

import (
	"crypto/ed25519"
	"database/sql"
	"embed"
	"io/fs"
	"net/http"
	"sync"
	"time"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/log"
)

//go:embed migrations
var migrations embed.FS

// Migrations returns the domain migration set.
func Migrations() fs.FS { return migrations }

// Deps are the hub's collaborators and key material.
type Deps struct {
	IdentityURL string
	SpawnURL    string
	Issuer      ed25519.PublicKey
	SelfKey     ed25519.PrivateKey
	Now         func() time.Time
}

// Server owns the operation table.
type Server struct {
	sub  string
	db   *sql.DB
	deps Deps
	toks *tokenSource
	l    *log.L
}

// New builds the server.
func New(sub string, db *sql.DB, deps Deps, l *log.L) *Server {
	return &Server{sub: sub, db: db, deps: deps, l: l,
		toks: &tokenSource{url: deps.IdentityURL, key: deps.SelfKey,
			sub: sub, now: deps.Now, cache: map[string]cached{}}}
}

// Mux mounts /v1 ops and /healthz; every op verifies a run token
// naming tool-hub in its audience.
func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	ops := thv1.Ops(thv1.Handlers{
		AdmitCall: s.admitCall, Projection: s.projection, Invoke: s.invoke})
	runVer := httpapi.RunVerifier(s.deps.Issuer, s.deps.Now)
	for name, op := range ops {
		op := op
		mux.Handle("/v1/"+name, httpapi.HandleAuth(runVer,
			func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
				return s.opErr(w, r, name, op)
			}))
	}
	mux.HandleFunc("/healthz", s.healthz)
	return mux
}

func (s *Server) opErr(w http.ResponseWriter, r *http.Request,
	name string, op httpapi.Handler) error {
	if err := op(w, r); err != nil {
		if s.l != nil {
			s.l.Error(r.Context(), "op "+name, err)
		}
		return err
	}
	return nil
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.db.PingContext(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		httpapi.WriteJSON(w, map[string]string{"status": "down"})
		return
	}
	httpapi.WriteJSON(w, map[string]string{"status": "ok"})
}

// tokenSource issues and caches service tokens per audience; the same
// pattern task-engine uses for its outbound calls.
type tokenSource struct {
	url   string
	key   ed25519.PrivateKey
	sub   string
	now   func() time.Time
	mu    sync.Mutex
	cache map[string]cached
	seq   int64
}

type cached struct {
	token string
	exp   int64
}

// clientNoAuth builds an identity client for the proof-signed
// issuance op, which carries no bearer token.
func clientNoAuth(url string) *idv1.Client {
	return &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(url, "")}}
}
