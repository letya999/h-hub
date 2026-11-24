// Package svc is the harness runtime: strict run admission in memory
// only, the Hermes turn against the scripted model, and report-back
// to task-engine. There is no database in the space zone.
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
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
)

// Deps are the runtime's collaborators.
type Deps struct {
	Issuer   ed25519.PublicKey // run-token issuer
	Now      func() time.Time
	Runner   Runner
	Rendered config.Rendered
}

// Runner executes one admitted turn; the production runner calls the
// scripted model and reports through the task contract.
type Runner interface {
	Run(ctx context.Context, a Admission)
}

// Admission is the in-memory admit row: created only by SubmitRun,
// keyed by run id, never persisted (space zone has no database).
type Admission struct {
	Run         string
	Job         string
	Space       string
	Digest      string // the spec digest the run was admitted under
	Input       string
	Attachments []hv1.Attachment
	Token       string // the run token, reused for report-back
	ctx         context.Context
	stop        context.CancelFunc
}

// Server owns the operation table and the admission registry.
type Server struct {
	sub   string
	deps  Deps
	mu    sync.Mutex
	admit map[string]*Admission
}

// New builds the server.
func New(sub string, deps Deps) *Server {
	return &Server{sub: sub, deps: deps, admit: map[string]*Admission{}}
}

// Mux mounts /v1 ops and /healthz. Every op is run-token verified.
func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	ops := hv1.Ops(s.handlers())
	verifier := httpapi.RunVerifier(s.deps.Issuer, s.deps.Now)
	for name, op := range ops {
		op := op
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
	return hv1.Handlers{SubmitRun: s.submitRun, CancelRun: s.cancelRun}
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
	if err != nil || c.Run != run {
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
	if c.Job != r.Job || c.Space != r.Space {
		return hv1.SubmitRunResp{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	tok := bearerOf(ctx)
	s.mu.Lock()
	if _, ok := s.admit[r.Run]; ok {
		s.mu.Unlock()
		return hv1.SubmitRunResp{Accepted: true}, nil // idempotent
	}
	rctx, stop := context.WithCancel(context.Background())
	s.admit[r.Run] = &Admission{
		Run: r.Run, Job: r.Job, Space: r.Space, Digest: c.Digest,
		Input: r.Input, Attachments: r.Attachments,
		Token: tok, ctx: rctx, stop: stop}
	s.mu.Unlock()
	go s.deps.Runner.Run(s.admit[r.Run].ctx, *s.admit[r.Run])
	return hv1.SubmitRunResp{Accepted: true}, nil
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
