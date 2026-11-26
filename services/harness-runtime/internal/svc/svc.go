// Package svc admits runs, drives the native Hermes gateway and reports its events to task-engine.
// The hub credentials for admitted runs live in memory.
package svc

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"slices"
	"sync"
	"time"

	hv1 "github.com/letya999/h-hub/contracts/harness/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/services/harness-runtime/internal/config"
	"github.com/letya999/h-hub/services/harness-runtime/internal/hermes"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/log"
)

// Deps are the runtime's collaborators.
type Deps struct {
	Issuer   ed25519.PublicKey // run-token issuer
	Now      func() time.Time
	Gateway  *hermes.Gateway
	Log      *log.L
	Rendered config.Rendered
}

// Admission is the in-memory admit row: created only by SubmitRun,
// keyed by run id, never persisted (space zone has no database).
type Admission struct {
	Run      string
	Job      string
	Space    string
	Digest   string // the spec digest the run was admitted under
	Token    string // the run token, reused for report-back
	ctx      context.Context
	stop     context.CancelFunc
	native   hermes.Run
	allowed  map[string]string
	fault    error
	done     bool
	expires  int64
	eventMu  sync.Mutex
	sequence int64
	probe    chan error // only the startup inventory check has no user credential
}

// Server owns the operation table and the admission registry.
type Server struct {
	sub            string
	deps           Deps
	mu             sync.Mutex
	admit          map[string]*Admission
	probeAdmission *Admission
	changed        chan struct{}
	parked         bool
}

// New builds the server.
func New(sub string, deps Deps) *Server {
	return &Server{sub: sub, deps: deps, admit: map[string]*Admission{}, changed: make(chan struct{})}
}

// Mux mounts /v1 ops and /healthz. Every op is run-token verified.
func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	ops := hv1.Ops(s.handlers())
	for name, op := range ops {
		op := op
		verifier := httpapi.RunVerifier(s.deps.Issuer, s.deps.Now)
		if name == "PrepareRestart" {
			verifier = httpapi.ServiceVerifier(s.deps.Issuer, s.sub, s.deps.Rendered.Edges, s.deps.Now)
		}
		mux.Handle("/v1/"+name, httpapi.HandleAuth(verifier,
			func(w http.ResponseWriter, r *http.Request, p httpapi.Principal) error {
				ctx := context.WithValue(r.Context(), principalKey{}, p)
				ctx = context.WithValue(ctx, bearerKey{}, bearer(r))
				return op(w, r.WithContext(ctx))
			}))
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpapi.WriteJSON(w, map[string]string{"status": "ok"})
	})
	return mux
}

type principalKey struct{}

func (s *Server) handlers() hv1.Handlers {
	return hv1.Handlers{SubmitRun: s.submitRun, CancelRun: s.cancelRun, PrepareRestart: s.prepareRestart}
}

// claims binds the caller's run token to the request: purpose, aud,
// and the claims must name this run.
func (s *Server) claims(ctx context.Context, run string) (idv1.RunClaims, error) {
	p, ok := ctx.Value(principalKey{}).(httpapi.Principal)
	if !ok {
		return idv1.RunClaims{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	if !slices.Contains(p.Aud, s.sub) {
		return idv1.RunClaims{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	c, err := idv1.ParseRunClaims(p.Payload)
	if err != nil || c.Run != run || c.Space == "" || c.Space != s.deps.Rendered.Space {
		return idv1.RunClaims{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	return c, nil
}

// submitRun creates the admission row — the only writer. The caller's
// headers never propagate; the runner starts detached.
func (s *Server) submitRun(ctx context.Context, r hv1.SubmitRunReq) (hv1.SubmitRunResp, error) {
	c, err := s.claims(ctx, r.Run)
	if err != nil {
		return hv1.SubmitRunResp{}, err
	}
	if c.Job != r.Job || c.Space != r.Space || c.Owner == "" {
		return hv1.SubmitRunResp{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	tok := bearerOf(ctx)
	s.mu.Lock()
	if _, ok := s.admit[r.Run]; ok {
		s.mu.Unlock()
		return hv1.SubmitRunResp{Accepted: true}, nil // idempotent
	}
	if s.parked {
		s.mu.Unlock()
		return hv1.SubmitRunResp{}, herrors.New(herrors.CodeUnavailable, "err.runtime-restarting")
	}
	rctx, stop := context.WithCancel(context.Background())
	for id, previous := range s.admit {
		if previous.done && previous.expires <= s.deps.Now().Unix() {
			delete(s.admit, id)
		}
	}
	a := &Admission{
		Run: r.Run, Job: r.Job, Space: r.Space, Digest: c.Digest,
		Token: tok, ctx: rctx, stop: stop, expires: c.Exp}
	s.admit[r.Run] = a
	s.mu.Unlock()
	go s.runAdmission(a, r)
	return hv1.SubmitRunResp{Accepted: true}, nil
}

func (s *Server) prepareRestart(_ context.Context, r hv1.PrepareRestartReq) (hv1.PrepareRestartResp, error) {
	if r.Space != s.deps.Rendered.Space || r.Key == "" {
		return hv1.PrepareRestartResp{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.admit {
		if !a.done {
			return hv1.PrepareRestartResp{}, nil
		}
	}
	s.parked = true
	return hv1.PrepareRestartResp{Idle: true}, nil
}

// cancelRun aborts an admitted run. A run id with no admission row is
// a refusal — nothing else may invent work.
func (s *Server) cancelRun(ctx context.Context, r hv1.CancelRunReq) (hv1.CancelRunResp, error) {
	if _, err := s.claims(ctx, r.Run); err != nil {
		return hv1.CancelRunResp{}, err
	}
	s.mu.Lock()
	a, ok := s.admit[r.Run]
	s.mu.Unlock()
	if !ok {
		nf := herrors.New(herrors.CodeNotFound, "err.not-found").
			With("run", r.Run)
		return hv1.CancelRunResp{}, nf //nolint:wrapcheck // shared error form
	}
	a.stop()
	return hv1.CancelRunResp{OK: true}, nil
}

// bearerKey carries the raw run token for report-back reuse.
type bearerKey struct{}

func bearer(r *http.Request) string {
	const pre = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) > len(pre) && h[:len(pre)] == pre {
		return h[len(pre):]
	}
	return ""
}

func bearerOf(ctx context.Context) string {
	v, ok := ctx.Value(bearerKey{}).(string)
	if !ok {
		return ""
	}
	return v
}
