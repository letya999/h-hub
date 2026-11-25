// Package v1 is the identity-hub contract: token issuance and the
// claims schema every verifier parses (rules 7-9).
package v1

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
)

// ServiceClaims is the service-token payload (purpose "service").
type ServiceClaims struct {
	Sub string   `json:"sub"`
	Aud []string `json:"aud"`
	Exp int64    `json:"exp"`
	Kid string   `json:"kid"`
}

// OwnerClaims binds an operator delegation to one owner and callee.
type OwnerClaims struct {
	Sub    string   `json:"sub"`
	Owner  string   `json:"owner"`
	Space  string   `json:"space,omitempty"`
	Target string   `json:"target,omitempty"`
	Aud    []string `json:"aud"`
	Exp    int64    `json:"exp"`
	Kid    string   `json:"kid"`
}

// ParseOwnerClaims extracts owner scope from a verified token.
func ParseOwnerClaims(payload []byte) (OwnerClaims, error) {
	var c OwnerClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return c, herrors.Wrap(herrors.CodeUnauthorized, "err.token", err)
	}
	if (c.Sub != "operator" && c.Sub != "channel-hub") || c.Owner == "" {
		return c, herrors.New(herrors.CodeUnauthorized, "err.token")
	}
	return c, nil
}

// ScopeFrom returns the owner and space from a verified request principal.
func ScopeFrom(ctx context.Context) (OwnerClaims, error) {
	p := httpapi.PrincipalFrom(ctx)
	if p == nil {
		return OwnerClaims{}, herrors.New(herrors.CodeUnauthorized, "err.unauthorized")
	}
	return ParseOwnerClaims(p.Payload)
}

// OwnerFrom returns the owner from a verified request principal.
func OwnerFrom(ctx context.Context) (string, error) {
	c, err := ScopeFrom(ctx)
	return c.Owner, err
}

// ActAsReq asks identity to issue an owner-scoped token.
type ActAsReq struct {
	Owner string `json:"owner"`
	Space string `json:"space,omitempty"`
	Aud   string `json:"aud"`
}

// ActAsResp carries the owner token.
type ActAsResp struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// RunClaims is the run-token payload (purpose "run"): the launch is
// bound to the space, job, run and spec digest it was admitted under.
type RunClaims struct {
	Sub    string   `json:"sub"`
	Job    string   `json:"job"`
	Run    string   `json:"run"`
	Space  string   `json:"space"`
	Owner  string   `json:"owner"`
	Digest string   `json:"digest"`
	Conv   string   `json:"conv,omitempty"`
	Aud    []string `json:"aud"`
	Exp    int64    `json:"exp"`
	Kid    string   `json:"kid"`
}

// ParseRunClaims extracts run claims from a verified principal payload.
func ParseRunClaims(payload []byte) (RunClaims, error) {
	var c RunClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return c, herrors.Wrap(herrors.CodeUnauthorized, "err.token", err)
	}
	if c.Run == "" || c.Job == "" {
		return c, herrors.New(herrors.CodeUnauthorized, "err.token")
	}
	return c, nil
}

// IssueServiceTokenReq is a signed issuance request: the caller proves
// ownership of Sub by signing Proof over CanonicalProof.
type IssueServiceTokenReq struct {
	Sub   string `json:"sub"`
	Aud   string `json:"aud"`
	Nonce string `json:"nonce"`
	Proof string `json:"proof"`
}

// IssueServiceTokenResp carries the minted service token.
type IssueServiceTokenResp struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// CanonicalProof is the exact string a caller signs for
// IssueServiceToken: "service|<sub>|<aud>|<nonce>".
func CanonicalProof(sub, aud, nonce string) []byte {
	return fmt.Appendf(nil, "service|%s|%s|%s", sub, aud, nonce)
}

// MintRunTokenReq asks for a run token; the caller must hold a service
// token (edge task-engine -> identity-hub).
type MintRunTokenReq struct {
	Job    string   `json:"job"`
	Run    string   `json:"run"`
	Space  string   `json:"space"`
	Owner  string   `json:"owner"`
	Digest string   `json:"digest"`
	Conv   string   `json:"conv,omitempty"`
	Aud    []string `json:"aud"`
}

// MintRunTokenResp carries the minted run token.
type MintRunTokenResp struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// ProofBytes decodes a base64 signature; anything else is invalid.
func ProofBytes(proof string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(proof)
	if err != nil {
		return nil, herrors.Wrap(herrors.CodeInvalid, "err.decode", err)
	}
	return b, nil
}

// Handlers is the server-side operation table: one field per
// operation, filled by the service implementation or the fake.
type Handlers struct {
	IssueServiceToken func(context.Context, IssueServiceTokenReq) (IssueServiceTokenResp, error)
	MintRunToken      func(context.Context, MintRunTokenReq) (MintRunTokenResp, error)
	ActAs             func(context.Context, ActAsReq) (ActAsResp, error)
	CreatePrincipal   func(context.Context, CreatePrincipalReq) (CreatePrincipalResp, error)
	DeactivateHuman   func(context.Context, DeactivateHumanReq) (DeactivateHumanResp, error)
	LinkBegin         func(context.Context, LinkBeginReq) (LinkBeginResp, error)
	LinkConfirm       func(context.Context, LinkConfirmReq) (LinkConfirmResp, error)
	Resolve           func(context.Context, ResolveReq) (ResolveResp, error)
	MintLinkToken     func(context.Context, MintLinkTokenReq) (MintLinkTokenResp, error)
}

// Ops maps operation names to request handlers (unauthenticated: auth
// wraps each route in the owning service).
func Ops(h Handlers) map[string]httpapi.Handler {
	return map[string]httpapi.Handler{
		"IssueServiceToken": op(h.IssueServiceToken),
		"MintRunToken":      op(h.MintRunToken),
		"ActAs":             op(h.ActAs),
		"CreatePrincipal":   op(h.CreatePrincipal),
		"DeactivateHuman":   op(h.DeactivateHuman),
		"LinkBegin":         op(h.LinkBegin),
		"LinkConfirm":       op(h.LinkConfirm),
		"Resolve":           op(h.Resolve),
		"MintLinkToken":     op(h.MintLinkToken),
	}
}

func op[Req, Resp any](fn func(context.Context, Req) (Resp, error)) httpapi.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req Req
		if err := httpapi.Decode(r, &req); err != nil {
			return fmt.Errorf("decode: %w", err)
		}
		resp, err := fn(r.Context(), req)
		if err != nil {
			return err
		}
		httpapi.WriteJSON(w, resp)
		return nil
	}
}
