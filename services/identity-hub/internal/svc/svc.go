// Package svc is identity-hub's HTTP surface: the phase-3 fake
// contract implementation plus the single-use nonce store, wired to
// the shared auth plumbing.
package svc

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/shared/edges"
	"github.com/letya999/h-hub/shared/httpapi"
)

//go:embed migrations
var migrations embed.FS

// Migrations returns the domain migration set.
func Migrations() fs.FS { return migrations }

// Server owns the operation table and its authorization.
type Server struct {
	sub   string
	db    *sql.DB
	pub   ed25519.PublicKey
	tab   edges.Table
	nonce Noncer
	now   func() int64
	ops   idv1.Handlers
}

// Deps are the issuer collaborators: the signer bound to the issuer
// key, the registered subject pubs, this service's inbound edge
// table, the single-use nonce store and the unix-second clock.
type Deps struct {
	Sign  idv1.Signer
	Pub   ed25519.PublicKey
	Kid   string
	Pubs  map[string]ed25519.PublicKey
	Tab   edges.Table
	Nonce Noncer
	Now   func() int64
}

// New builds a Server.
func New(sub string, db *sql.DB, deps Deps) *Server {
	fake := idv1.NewFake(deps.Sign, deps.Kid, deps.Pubs, deps.Now)
	s := &Server{sub: sub, db: db, pub: deps.Pub, tab: deps.Tab,
		nonce: deps.Nonce, now: deps.Now}
	h := fake.Handlers()
	base := h.IssueServiceToken
	h.IssueServiceToken = func(ctx context.Context, r idv1.IssueServiceTokenReq) (idv1.IssueServiceTokenResp, error) {
		if err := deps.Nonce.Claim(ctx, r.Sub, r.Nonce, time.Unix(deps.Now()+idv1.ServiceTTL, 0)); err != nil {
			return idv1.IssueServiceTokenResp{}, fmt.Errorf("nonce: %w", err)
		}
		return base(ctx, r)
	}
	s.ops = h
	return s
}

// Mux mounts /v1 ops, /healthz. IssueServiceToken is proof-based
// (HandleOpen); MintRunToken requires a service token from an edged
// caller.
func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	ops := idv1.Ops(s.ops)
	verifier := httpapi.ServiceVerifier(s.pub, s.sub, s.tab,
		func() time.Time { return time.Unix(s.now(), 0) })
	mux.Handle("/v1/IssueServiceToken", httpapi.HandleOpen(
		func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
			return ops["IssueServiceToken"](w, r)
		}))
	mux.Handle("/v1/MintRunToken", httpapi.HandleAuth(verifier,
		func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
			return ops["MintRunToken"](w, r)
		}))
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
