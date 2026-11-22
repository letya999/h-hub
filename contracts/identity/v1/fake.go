package v1

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// ServiceTTL is the service-token lifetime (blueprint §6.1: 15 min).
const ServiceTTL = 900

// Signer mints a token over a claims payload. The contract keeps no
// crypto of its own: services wire shared/auth.Sign, tests wire a
// stub — depguard keeps contracts free of shared/auth.
type Signer func(purpose string, payload []byte) (string, error)

// Fake is the phase-3 identity implementation: it verifies the
// caller's signature against registered public keys and mints tokens
// through the injected signer. Contract-conformant by the suite.
type Fake struct {
	sign Signer
	kid  string
	pubs map[string]ed25519.PublicKey
	now  func() int64
}

// NewFake builds a Fake: sign mints tokens, kid marks the issuer,
// pubs maps each known subject to its public key, now supplies unix
// seconds.
func NewFake(sign Signer, kid string, pubs map[string]ed25519.PublicKey,
	now func() int64) *Fake {
	return &Fake{sign: sign, kid: kid, pubs: pubs, now: now}
}

// Kid derives the issuer key id from a public key.
func Kid(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:6])
}

// Handlers exposes the fake as the contract operation table.
func (f *Fake) Handlers() Handlers {
	return Handlers{
		IssueServiceToken: f.issueService,
		MintRunToken:      f.mintRun,
	}
}

func (f *Fake) issueService(_ context.Context, r IssueServiceTokenReq) (IssueServiceTokenResp, error) {
	var out IssueServiceTokenResp
	if err := herrors.Require(r.Sub != "" && r.Aud != "" && r.Nonce != "", "err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	pub, ok := f.pubs[r.Sub]
	if !ok {
		return out, herrors.New(herrors.CodeForbidden, "err.forbidden").With("sub", r.Sub) //nolint:wrapcheck // shared error form
	}
	proof, err := ProofBytes(r.Proof)
	if err != nil {
		return out, err
	}
	if !ed25519.Verify(pub, CanonicalProof(r.Sub, r.Aud, r.Nonce), proof) {
		return out, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	exp := f.now() + ServiceTTL
	claims, err := json.Marshal(ServiceClaims{
		Sub: r.Sub, Aud: []string{r.Aud}, Exp: exp, Kid: f.kid,
	})
	if err != nil {
		return out, fmt.Errorf("claims: %w", err)
	}
	tok, err := f.sign("service", claims)
	if err != nil {
		return out, fmt.Errorf("sign: %w", err)
	}
	return IssueServiceTokenResp{Token: tok, ExpiresAt: exp}, nil
}

func (f *Fake) mintRun(_ context.Context, r MintRunTokenReq) (MintRunTokenResp, error) {
	var out MintRunTokenResp
	if err := herrors.Require(r.Job != "" && r.Run != "" && r.Space != "" &&
		r.Owner != "" && len(r.Aud) > 0, "err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	exp := f.now() + ServiceTTL
	claims, err := json.Marshal(RunClaims{
		Sub: "run:" + r.Run, Job: r.Job, Run: r.Run, Space: r.Space,
		Owner: r.Owner, Digest: r.Digest, Aud: r.Aud, Exp: exp,
		Kid: f.kid,
	})
	if err != nil {
		return out, fmt.Errorf("claims: %w", err)
	}
	tok, err := f.sign("run", claims)
	if err != nil {
		return out, fmt.Errorf("sign: %w", err)
	}
	return MintRunTokenResp{Token: tok, ExpiresAt: exp}, nil
}
