// Package svc is tool-hub's HTTP surface and admission core: the
// run-token-verified AdmitCall, Projection and Invoke ops, and the
// receipt ledger behind every decision. Grants and tool selection
// live in the caller's effective spec resolved from spawn-controller
// per request — the hub keeps no policy copy of its own (rules 3-4).
package svc

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"embed"
	"io/fs"
	"net/http"
	"sync"
	"time"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	orv1 "github.com/letya999/h-hub/contracts/ownerreq/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	"github.com/letya999/h-hub/services/tool-hub/internal/ownerreq"
	"github.com/letya999/h-hub/shared/edges"
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
	ChannelURL  string
	TaskURL     string
	Issuer      ed25519.PublicKey
	SelfKey     ed25519.PrivateKey
	Edges       edges.Table
	Now         func() time.Time
}

// Server owns the operation table.
type Server struct {
	sub  string
	db   *sql.DB
	deps Deps
	toks *tokenSource
	oreq *ownerreq.Server
	l    *log.L
}

// New builds the server and its owner-request module.
func New(sub string, db *sql.DB, deps Deps, l *log.L) *Server {
	s := &Server{sub: sub, db: db, deps: deps, l: l,
		toks: &tokenSource{url: deps.IdentityURL, key: deps.SelfKey,
			sub: sub, now: deps.Now, cache: map[string]cached{}}}
	s.oreq = ownerreq.New(db, ownerreq.Deps{
		IdentityURL: deps.IdentityURL, ChannelURL: deps.ChannelURL,
		Now:    deps.Now,
		Tokens: s.toks, Mode: specMode{s}, RequestTurn: s.requestTurn}, l)
	return s
}

func (s *Server) requestTurn(ctx context.Context, r tv1.RequestTurnReq) (tv1.RequestTurnResp, error) {
	cl, err := taskClient(s, ctx)
	if err != nil {
		return tv1.RequestTurnResp{}, err
	}
	return cl.GetRequestTurn(ctx, r) //nolint:wrapcheck // preserve the task contract error
}

func taskClient(s *Server, ctx context.Context) (*tv1.Client, error) {
	tok, err := s.toks.Token(ctx, "task-engine")
	if err != nil {
		return nil, err
	}
	return &tv1.Client{T: tv1.HTTP{C: httpapi.NewClient(s.deps.TaskURL, tok)}}, nil
}

// specMode resolves config.approvals.mode through the same spec the
// admission path resolves — one policy source.
type specMode struct{ s *Server }

// ApprovalsMode returns the space's configured mode; a spec without
// the leaf behaves as ask.
func (m specMode) ApprovalsMode(ctx context.Context, space string) (string, error) {
	spec, _, err := m.s.spec(ctx, space)
	if err != nil {
		return "", err
	}
	if spec.Config.Approvals.Mode == "" {
		return "ask", nil
	}
	return spec.Config.Approvals.Mode, nil
}

// Mux mounts /v1 ops and /healthz. Admission ops verify run tokens;
// owner-request ops take run or service tokens per op (the answer op
// is service-only — an agent never answers).
func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	ops := thv1.Ops(thv1.Handlers{
		AdmitCall: s.admitCall, Projection: s.projection, Invoke: s.invoke,
		SkillPrompt: func(ctx context.Context, r thv1.SkillPromptReq) (thv1.SkillPromptResp, error) {
			return skillPrompt(s, ctx, r)
		},
		NotifyProjection: s.notifyProjection})
	runVer := httpapi.RunVerifier(s.deps.Issuer, s.deps.Now)
	svcVer := httpapi.ServiceVerifier(s.deps.Issuer, s.sub, s.deps.Edges, s.deps.Now)
	dual := func(r *http.Request) (httpapi.Principal, error) {
		if p, err := runVer(r); err == nil {
			return p, nil
		}
		return svcVer(r)
	}
	for name, op := range ops {
		v := runVer
		switch name {
		case "Projection":
			v = dual
		case "NotifyProjection":
			v = svcVer
		}
		mux.Handle("/v1/"+name, httpapi.HandleAuth(v,
			func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
				return s.opErr(w, r, name, op)
			}))
	}
	verOf := map[string]httpapi.Verifier{"AnswerRequest": svcVer, "BindRequest": svcVer}
	for name, op := range orv1.Ops(s.oreq.Handlers()) {
		op := op
		v := verOf[name]
		if v == nil {
			v = dual
		}
		mux.Handle("/v1/"+name, httpapi.HandleAuth(v,
			func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
				return s.opErr(w, r, name, op)
			}))
	}
	mux.HandleFunc("/healthz", s.healthz)
	return mux
}

// Loops runs the owner-request module's outbox executor and sweeper.
func (s *Server) Loops(ctx context.Context, l *log.L) {
	go sweepTerminals(s, ctx, l)
	s.oreq.Loops(ctx, l)
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
