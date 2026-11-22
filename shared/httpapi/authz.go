package httpapi

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"slices"
	"time"

	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/edges"
	herrors "github.com/letya999/h-hub/shared/errors"
)

// Principal is the verified identity attached to a request. Payload
// stays raw: services own their claims schema and parse the fields
// the transport envelope does not cover.
type Principal struct {
	Purpose string
	Sub     string
	Aud     []string
	Exp     time.Time
	Kid     string
	Payload []byte
}

// Verifier authenticates a request and returns its principal.
type Verifier func(r *http.Request) (Principal, error)

// AuthHandler is a Handler that receives the verified principal.
type AuthHandler func(w http.ResponseWriter, r *http.Request, p Principal) error

// HandleAuth wraps an op handler: bounded body, bearer verification,
// then the shared error form.
func HandleAuth(v Verifier, next AuthHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := v(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
		if err := next(w, r, p); err != nil {
			writeError(w, r, err)
		}
	}
}

// HandleOpen wraps an op that carries its own proof in the request
// body or needs none at all (healthz, token issuance).
func HandleOpen(next AuthHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
		if err := next(w, r, Principal{}); err != nil {
			writeError(w, r, err)
		}
	}
}

// envelope is the transport-level claims convention: every token
// payload carries these fields plus service-owned extras.
type envelope struct {
	Sub string   `json:"sub"`
	Aud []string `json:"aud"`
	Exp int64    `json:"exp"`
	Kid string   `json:"kid"`
}

// ServiceVerifier authenticates purpose="service" tokens: signature,
// expiry, audience containing self, then the descriptor edge
// (sub, op) — the single implementation of rule 31.
func ServiceVerifier(pub ed25519.PublicKey, self string, tab edges.Table,
	now func() time.Time) Verifier {
	return func(r *http.Request) (Principal, error) {
		p, err := verify(bearer(r), pub, "service", now)
		if err != nil {
			return p, err
		}
		if !slices.Contains(p.Aud, self) {
			return p, herrors.New(herrors.CodeForbidden, "err.forbidden").With("aud", self)
		}
		if !tab.Allows(self, p.Sub, opOf(r)) {
			return p, herrors.New(herrors.CodeForbidden, "err.forbidden").With("sub", p.Sub)
		}
		return p, nil
	}
}

// RunVerifier authenticates purpose="run" tokens: signature and
// expiry. Handlers bind the run-scoped claims themselves.
func RunVerifier(pub ed25519.PublicKey, now func() time.Time) Verifier {
	return func(r *http.Request) (Principal, error) {
		return verify(bearer(r), pub, "run", now)
	}
}

func opOf(r *http.Request) string { return path.Base(r.URL.Path) }

func bearer(r *http.Request) string {
	const pre = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) <= len(pre) || h[:len(pre)] != pre {
		return ""
	}
	return h[len(pre):]
}

func verify(tok string, pub ed25519.PublicKey, purpose string,
	now func() time.Time) (Principal, error) {
	var p Principal
	if tok == "" || pub == nil {
		return p, herrors.New(herrors.CodeUnauthorized, "err.unauthorized")
	}
	t, err := auth.Parse(tok)
	if err != nil {
		return p, fmt.Errorf("token: %w", err)
	}
	if err := t.Verify(pub); err != nil {
		return p, fmt.Errorf("token: %w", err)
	}
	if t.Purpose != purpose {
		return p, herrors.New(herrors.CodeUnauthorized, "err.token").With("purpose", t.Purpose) //nolint:wrapcheck // shared error form
	}
	var env envelope
	if err := json.Unmarshal(t.Payload, &env); err != nil {
		return p, herrors.Wrap(herrors.CodeUnauthorized, "err.token", err)
	}
	if env.Sub == "" || env.Exp <= now().Unix() {
		return p, herrors.New(herrors.CodeUnauthorized, "err.token").With("sub", env.Sub) //nolint:wrapcheck // shared error form
	}
	return Principal{
		Purpose: t.Purpose, Sub: env.Sub, Aud: env.Aud,
		Exp: time.Unix(env.Exp, 0), Kid: env.Kid, Payload: t.Payload,
	}, nil
}
