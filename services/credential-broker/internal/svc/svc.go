// Package svc is credential-broker's HTTP surface and storage core:
// the SQLCipher database, the envelope seal over credential values,
// the short-lease disclosure path and the operator write path
// (blueprint 2.9).
package svc

import (
	"crypto/ed25519"
	"database/sql"
	"net/http"
	"time"

	credv1 "github.com/letya999/h-hub/contracts/cred/v1"
	"github.com/letya999/h-hub/shared/edges"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/log"
	"github.com/letya999/h-hub/shared/seal"
)

// Deps are the broker's collaborators and key material. The SEAL KEK
// is loaded outside the volume; the database key is applied by
// openDB, not kept here.
type Deps struct {
	Issuer      ed25519.PublicKey // verifies service tokens
	OperatorPub ed25519.PublicKey // verifies operator tokens
	EdgeTab     edges.Table
	Sealer      *seal.Seal
	DBPath      string // for the VACUUM INTO snapshot destination
	Now         func() time.Time
}

// Server owns the operation table.
type Server struct {
	sub    string
	db     *sql.DB
	deps   Deps
	leases *leaseBook
	l      *log.L
}

// New builds the server over an already-activated database.
func New(sub string, db *sql.DB, deps Deps, l *log.L) *Server {
	return &Server{sub: sub, db: db, deps: deps,
		leases: newLeaseBook(), l: l}
}

// Mux mounts /v1 ops and /healthz. Acquire and Revoke take a service
// token on a declared edge; the operator ops take an operator token —
// only host-side tooling holds the operator key, which is what makes
// the write path host-only.
func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	ops := credv1.Ops(s.handlers())
	svcVer := httpapi.ServiceVerifier(s.deps.Issuer, s.sub,
		s.deps.EdgeTab, s.deps.Now)
	opVer := httpapi.EdgeVerifier(s.deps.OperatorPub, "operator", s.sub,
		s.deps.EdgeTab, s.deps.Now)
	for _, name := range []string{"Acquire", "Revoke"} {
		op := ops[name]
		mux.Handle("/v1/"+name, httpapi.HandleAuth(svcVer,
			func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
				return op(w, r)
			}))
	}
	for _, name := range []string{"SetSecret", "ListConnections", "Snapshot"} {
		op := ops[name]
		mux.Handle("/v1/"+name, httpapi.HandleAuth(opVer,
			func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
				return op(w, r)
			}))
	}
	mux.HandleFunc("/healthz", s.healthz)
	return mux
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.db.PingContext(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		httpapi.WriteJSON(w, map[string]string{"status": "down"})
		return
	}
	httpapi.WriteJSON(w, map[string]string{"status": "ok"})
}
