// Package svc is identity-hub's HTTP surface and persistent identity store.
package svc

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"time"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/shared/edges"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/seal"
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
	opPub ed25519.PublicKey
	pubs  map[string]ed25519.PublicKey
	tab   edges.Table
	nonce Noncer
	now   func() int64
	ops   idv1.Handlers
	seal  *seal.Seal
	sign  idv1.Signer
	kid   string
}

// Deps are the issuer collaborators: the signer bound to the issuer
// key, the registered subject pubs, this service's inbound edge
// table, the single-use nonce store and the unix-second clock.
type Deps struct {
	Sign        idv1.Signer
	Pub         ed25519.PublicKey
	OperatorPub ed25519.PublicKey
	Kid         string
	Pubs        map[string]ed25519.PublicKey
	Tab         edges.Table
	Nonce       Noncer
	Now         func() int64
	Seal        *seal.Seal
}

// New builds a Server.
func New(sub string, db *sql.DB, deps Deps) *Server {
	issuer := idv1.NewIssuer(deps.Sign, deps.Kid, deps.Pubs, deps.Now)
	s := &Server{sub: sub, db: db, pub: deps.Pub, opPub: deps.OperatorPub, pubs: deps.Pubs, tab: deps.Tab,
		nonce: deps.Nonce, now: deps.Now, seal: deps.Seal, sign: deps.Sign, kid: deps.Kid}
	h := issuer.Handlers()
	base := h.IssueServiceToken
	h.IssueServiceToken = func(ctx context.Context, r idv1.IssueServiceTokenReq) (idv1.IssueServiceTokenResp, error) {
		if err := deps.Nonce.Claim(ctx, r.Sub, r.Nonce, time.Unix(deps.Now()+idv1.ServiceTTL, 0)); err != nil {
			return idv1.IssueServiceTokenResp{}, fmt.Errorf("nonce: %w", err)
		}
		return base(ctx, r)
	}
	issueOwner := h.ActAs
	h.ActAs = func(ctx context.Context, r idv1.ActAsReq) (idv1.ActAsResp, error) {
		p := httpapi.PrincipalFrom(ctx)
		if p == nil {
			return idv1.ActAsResp{}, herrors.New(herrors.CodeUnauthorized, "err.unauthorized")
		}
		claims, err := idv1.ParseOwnerClaims(p.Payload)
		if err != nil {
			return idv1.ActAsResp{}, fmt.Errorf("operator claims: %w", err)
		}
		if claims.Owner != r.Owner || claims.Space != r.Space || claims.Target != r.Aud {
			return idv1.ActAsResp{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
		}
		out, err := issueOwner(ctx, r)
		if err != nil {
			return out, err
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO operator_audit
			(owner, space, audience, operation) VALUES ($1,$2,$3,'ActAs')`, r.Owner, r.Space, r.Aud); err != nil {
			return idv1.ActAsResp{}, fmt.Errorf("audit act-as: %w", err)
		}
		return out, nil
	}
	h.CreatePrincipal = s.createPrincipal
	h.DeactivateHuman = s.deactivateHuman
	h.LinkBegin = s.linkBegin
	h.LinkConfirm = s.linkConfirm
	h.Resolve = s.resolve
	h.MintLinkToken = s.mintLinkToken
	s.ops = h
	return s
}

// Mux mounts /v1 ops and /healthz. Issuance verifies the caller's
// signed proof; all other operations verify a bearer token.
func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	ops := idv1.Ops(s.ops)
	verifier := httpapi.ServiceVerifier(s.pub, s.sub, s.tab,
		func() time.Time { return time.Unix(s.now(), 0) })
	mux.Handle("/v1/IssueServiceToken", httpapi.HandleAuth(s.issueVerifier,
		func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
			return ops["IssueServiceToken"](w, r)
		}))
	mux.Handle("/v1/MintRunToken", httpapi.HandleAuth(verifier,
		func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
			return ops["MintRunToken"](w, r)
		}))
	opVer := httpapi.EdgeVerifier(s.opPub, "operator", s.sub, s.tab,
		func() time.Time { return time.Unix(s.now(), 0) })
	mux.Handle("/v1/ActAs", httpapi.HandleAuth(opVer,
		func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
			return ops["ActAs"](w, r)
		}))
	for _, name := range []string{"CreatePrincipal", "DeactivateHuman", "LinkBegin"} {
		mux.Handle("/v1/"+name, httpapi.HandleAuth(opVer,
			func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
				return ops[name](w, r)
			}))
	}
	for _, name := range []string{"LinkConfirm", "Resolve", "MintLinkToken"} {
		mux.Handle("/v1/"+name, httpapi.HandleAuth(verifier,
			func(w http.ResponseWriter, r *http.Request, _ httpapi.Principal) error {
				return ops[name](w, r)
			}))
	}
	mux.HandleFunc("/healthz", s.healthz)
	return mux
}

func (s *Server) issueVerifier(r *http.Request) (httpapi.Principal, error) {
	var p httpapi.Principal
	body := r.Body
	data, readErr := io.ReadAll(io.LimitReader(body, httpapi.MaxBody+1))
	if err := errors.Join(readErr, body.Close()); err != nil {
		return p, fmt.Errorf("issue body: %w", err)
	}
	if int64(len(data)) > httpapi.MaxBody {
		return p, herrors.New(herrors.CodeInvalid, "err.decode")
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	var req idv1.IssueServiceTokenReq
	if err := json.Unmarshal(data, &req); err != nil {
		return p, herrors.Wrap(herrors.CodeInvalid, "err.decode", err)
	}
	if err := idv1.VerifyServiceProof(req, s.pubs); err != nil {
		return p, fmt.Errorf("issue proof: %w", err)
	}
	return httpapi.Principal{Sub: req.Sub, Purpose: "proof"}, nil
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.db.PingContext(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		httpapi.WriteJSON(w, map[string]string{"status": "down"})
		return
	}
	httpapi.WriteJSON(w, map[string]string{"status": "ok"})
}
