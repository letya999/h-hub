// Package svc is task-engine's HTTP surface and dispatch core: the
// job/run machines, FIFO conversation claiming, admission ceilings,
// cancellation/holds and the run event journal.
package svc

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"sync"
	"time"

	hv1 "github.com/letya999/h-hub/contracts/harness/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	orv1 "github.com/letya999/h-hub/contracts/ownerreq/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/edges"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/log"
)

//go:embed migrations
var migrations embed.FS

// Migrations returns the domain migration set.
func Migrations() fs.FS { return migrations }

// Deps are the service's collaborators: dependency endpoints, key
// material and the run-token issuer's public key.
type Deps struct {
	IdentityURL string
	SpawnURL    string
	ChannelURL  string
	ToolURL     string
	Issuer      ed25519.PublicKey // issuer of service/run tokens
	SelfKey     ed25519.PrivateKey
	EdgeTab     edges.Table // inbound edge table for this service
	Now         func() time.Time
}

// Server owns the shared plumbing; each surface lives on a role
// struct (rule 12: one type owns one surface, max 20 methods).
type Server struct {
	sub  string
	db   *sql.DB
	deps Deps
	jobM *fsm.Machine
	runM *fsm.Machine
	toks *tokenSource
	ops  *jobOps      // operator-facing job ops
	rep  *runReporter // ReportRun + settlement path
	disp *dispatcher  // claim -> spec -> runtime -> submit chain
	run  *loopRunner  // dispatch/sweep/outbox/tick loops
}

// New builds the server.
func New(sub string, db *sql.DB, deps Deps) *Server {
	s := &Server{
		sub: sub, db: db, deps: deps,
		jobM: JobMachine(), runM: RunMachine(),
		toks: &tokenSource{url: deps.IdentityURL, key: deps.SelfKey,
			sub: sub, now: deps.Now, cache: map[string]cached{}},
	}
	s.ops = &jobOps{s}
	s.rep = &runReporter{s}
	s.disp = &dispatcher{s}
	s.run = &loopRunner{s}
	return s
}

// jobOps implements the operator-facing contract ops.
type jobOps struct{ s *Server }

// runReporter owns the run event journal and settlement.
type runReporter struct{ s *Server }

// dispatcher owns the admitted-job dispatch chain.
type dispatcher struct{ s *Server }

// loopRunner owns the background loops and outbox executor.
type loopRunner struct{ s *Server }

// identityClient builds a client carrying the given service token.
func (s *Server) identityClient(token string) *idv1.Client {
	return &idv1.Client{T: idv1.HTTP{
		C: httpapi.NewClient(s.deps.IdentityURL, token)}}
}

// spawnClient mints a service token for spawn-controller and builds
// the client that carries it.
func (s *Server) spawnClient(ctx context.Context) (*sv1.Client, error) {
	tok, err := s.toks.Token(ctx, "spawn-controller")
	if err != nil {
		return nil, err
	}
	return &sv1.Client{T: sv1.HTTP{
		C: httpapi.NewClient(s.deps.SpawnURL, tok)}}, nil
}

// toolClient mints a service token for tool-hub and builds the
// owner-request client that carries it.
func (s *Server) toolClient(ctx context.Context) (*orv1.Client, error) {
	tok, err := s.toks.Token(ctx, "tool-hub")
	if err != nil {
		return nil, err
	}
	return &orv1.Client{T: orv1.HTTP{
		C: httpapi.NewClient(s.deps.ToolURL, tok)}}, nil
}

// harnessClient builds a client for one runtime endpoint carrying the
// run token that authenticates the submit.
func (s *Server) harnessClient(endpoint, runToken string) *hv1.Client {
	return &hv1.Client{T: hv1.HTTP{
		C: httpapi.NewClient(endpoint, runToken)}}
}

// Mux mounts /v1 ops and /healthz. Every operation verifies a token.
func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	ops := tv1.Ops(s.handlers())
	ownerVer := httpapi.EdgeVerifier(s.deps.Issuer, "owner", s.sub,
		s.deps.EdgeTab, s.deps.Now)
	for _, name := range []string{"CreateJob", "GetJob", "ListJobs",
		"CancelJob", "RetryJob", "UnquarantineJob", "SetHold",
		"GetRun"} {
		op := ops[name]
		mux.Handle("/v1/"+name, httpapi.HandleAuth(ownerVer,
			func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
				return op(w, r)
			}))
	}
	runVer := httpapi.RunVerifier(s.deps.Issuer,
		func() time.Time { return s.deps.Now() })
	mux.Handle("/v1/ReportRun", httpapi.HandleAuth(runVer,
		func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
			return ops["ReportRun"](w, r)
		}))
	mux.HandleFunc("/healthz", s.healthz)
	return mux
}

// Loops delegates to the loop runner (main.go's entry point).
func (s *Server) Loops(ctx context.Context, l *log.L) {
	s.run.Loops(ctx, l)
}

// RunPrincipal returns the authenticated run claims, nil for open ops.
func RunPrincipal(ctx context.Context) *httpapi.Principal {
	return httpapi.PrincipalFrom(ctx)
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.db.PingContext(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		httpapi.WriteJSON(w, map[string]string{"status": "down"})
		return
	}
	httpapi.WriteJSON(w, map[string]string{"status": "ok"})
}

// tokenSource issues and caches service tokens per audience.
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

// Token returns a service token for aud, minting when the cached one
// is within a minute of expiry. IssueServiceToken is proof-signed,
// so the client itself carries no token.
func (t *tokenSource) Token(ctx context.Context, aud string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.cache[aud]
	if ok && c.exp > t.now().Unix()+60 {
		return c.token, nil
	}
	t.seq++
	cl := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(t.url, "")}}
	res, err := cl.IssueServiceToken(ctx, t.sub, aud,
		fmt.Sprintf("%s-%d-%d", t.sub, t.now().Unix(), t.seq), t.key)
	if err != nil {
		return "", fmt.Errorf("issue service token %s: %w", aud, err)
	}
	t.cache[aud] = cached{token: res.Token, exp: res.ExpiresAt}
	return res.Token, nil
}
