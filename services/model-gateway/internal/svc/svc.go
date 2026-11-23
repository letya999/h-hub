// Package svc is model-gateway's HTTP surface and accounting core:
// the run-token-verified OpenAI relay, the owner-facing Usage and
// Routes reads, and the reserve/settle counters behind the ceilings.
// Routes and ceilings live in the caller's effective passport
// resolved from spawn-controller per request — the gateway keeps no
// policy copy of its own (rules 3-4).
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
	mv1 "github.com/letya999/h-hub/contracts/model/v1"
	"github.com/letya999/h-hub/shared/edges"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/log"
)

//go:embed migrations
var migrations embed.FS

// Migrations returns the domain migration set.
func Migrations() fs.FS { return migrations }

// Deps are the gateway's collaborators and key material.
type Deps struct {
	IdentityURL string
	SpawnURL    string
	KeysDir     string // upstream key files live here, outside env
	Issuer      ed25519.PublicKey
	SelfKey     ed25519.PrivateKey
	EdgeTab     edges.Table
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

// Mux mounts /v1 ops and /healthz. Complete verifies a run token;
// Usage and Routes verify an owner token on a declared edge.
func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	ops := mv1.Ops(mv1.Handlers{
		Complete: s.complete, Usage: s.usage, Routes: s.routes})
	runVer := httpapi.RunVerifier(s.deps.Issuer, s.deps.Now)
	ownerVer := httpapi.EdgeVerifier(s.deps.Issuer, "owner", s.sub,
		s.deps.EdgeTab, s.deps.Now)
	mux.Handle("/v1/Complete", httpapi.HandleAuth(runVer,
		func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
			return s.opErr(w, r, "Complete", ops["Complete"])
		}))
	for _, name := range []string{"Usage", "Routes"} {
		op := ops[name]
		mux.Handle("/v1/"+name, httpapi.HandleAuth(ownerVer,
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
