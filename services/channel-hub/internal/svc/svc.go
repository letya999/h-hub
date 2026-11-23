// Package svc owns the mock-channel intake and delivery machines.
package svc

import (
	"crypto/ed25519"
	"database/sql"
	"embed"
	"io/fs"
	"net/http"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	"github.com/letya999/h-hub/shared/edges"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
)

//go:embed migrations
var migrations embed.FS

func Migrations() fs.FS { return migrations }

type Deps struct {
	IdentityURL, TaskURL, SpawnURL string
	Issuer, OperatorPub            ed25519.PublicKey
	SelfKey                        ed25519.PrivateKey
	Edges                          edges.Table
	Now                            func() time.Time
}
type Server struct {
	sub      string
	db       *sql.DB
	deps     Deps
	intake   *fsm.Machine
	delivery *fsm.Machine
	thread   *fsm.Machine
}

func New(sub string, db *sql.DB, deps Deps) *Server {
	return &Server{sub: sub, db: db, deps: deps, intake: intakeMachine(), delivery: deliveryMachine(), thread: threadMachine()}
}

func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	ops := cv1.Ops(cv1.Handlers{Inject: s.inject, ReadOutbox: s.readOutbox, Alert: (&alertOps{s}).alert})
	ver := httpapi.EdgeVerifier(s.deps.OperatorPub, "operator", s.sub, s.deps.Edges, s.deps.Now)
	serviceVer := httpapi.ServiceVerifier(s.deps.Issuer, s.sub, s.deps.Edges, s.deps.Now)
	for name, op := range ops {
		if name == "Alert" {
			mux.Handle("/v1/"+name, httpapi.HandleAuth(serviceVer, func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error { return op(w, r) }))
			continue
		}
		mux.Handle("/v1/"+name, httpapi.HandleAuth(ver, func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error { return op(w, r) }))
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.db.PingContext(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			httpapi.WriteJSON(w, map[string]string{"status": "down"})
			return
		}
		httpapi.WriteJSON(w, map[string]string{"status": "ok"})
	})
	return mux
}

type clock struct{ now func() time.Time }

func (c clock) Now() time.Time { return c.now() }

func (s *Server) now() time.Time { return s.deps.Now() }
