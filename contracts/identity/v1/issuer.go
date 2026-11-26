package v1

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// Issuer implements durable service token issuance when wired to a persistent nonce store.
type Issuer struct {
	sign Signer
	kid  string
	pubs map[string]ed25519.PublicKey
	now  func() int64
}

func NewIssuer(sign Signer, kid string, pubs map[string]ed25519.PublicKey, now func() int64) *Issuer {
	return &Issuer{sign: sign, kid: kid, pubs: pubs, now: now}
}

func (i *Issuer) Handlers() Handlers {
	return Handlers{IssueServiceToken: i.issueService, MintRunToken: i.mintRun, ActAs: i.actAs}
}

func (i *Issuer) actAs(_ context.Context, r ActAsReq) (ActAsResp, error) {
	if r.Owner == "" || r.Aud == "" {
		return ActAsResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	exp := i.now() + ServiceTTL
	claims, err := json.Marshal(OwnerClaims{
		Sub: "operator", Owner: r.Owner, Space: r.Space, Aud: []string{r.Aud}, Exp: exp, Kid: i.kid,
	})
	if err != nil {
		return ActAsResp{}, fmt.Errorf("owner claims: %w", err)
	}
	tok, err := i.sign("owner", claims)
	if err != nil {
		return ActAsResp{}, fmt.Errorf("sign owner: %w", err)
	}
	return ActAsResp{Token: tok, ExpiresAt: exp}, nil
}

func (i *Issuer) issueService(_ context.Context, r IssueServiceTokenReq) (IssueServiceTokenResp, error) {
	var out IssueServiceTokenResp
	if err := herrors.Require(r.Sub != "" && r.Aud != "" && r.Nonce != "", "err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	if err := VerifyServiceProof(r, i.pubs); err != nil {
		return out, err
	}
	exp := i.now() + ServiceTTL
	claims, err := json.Marshal(ServiceClaims{
		Sub: r.Sub, Aud: []string{r.Aud}, Exp: exp, Kid: i.kid,
	})
	if err != nil {
		return out, fmt.Errorf("claims: %w", err)
	}
	tok, err := i.sign("service", claims)
	if err != nil {
		return out, fmt.Errorf("sign: %w", err)
	}
	return IssueServiceTokenResp{Token: tok, ExpiresAt: exp}, nil
}

// VerifyServiceProof authenticates the one signed issuance request.
func VerifyServiceProof(r IssueServiceTokenReq, pubs map[string]ed25519.PublicKey) error {
	pub, ok := pubs[r.Sub]
	if !ok {
		return herrors.New(herrors.CodeForbidden, "err.forbidden").With("sub", r.Sub) //nolint:wrapcheck // shared error form
	}
	proof, err := ProofBytes(r.Proof)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, CanonicalProof(r.Sub, r.Aud, r.Nonce), proof) {
		return herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	return nil
}

func (i *Issuer) mintRun(_ context.Context, r MintRunTokenReq) (MintRunTokenResp, error) {
	var out MintRunTokenResp
	if err := herrors.Require(r.Job != "" && r.Run != "" && r.Space != "" &&
		r.Owner != "" && len(r.Aud) > 0, "err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	exp := i.now() + ServiceTTL
	claims, err := json.Marshal(RunClaims{
		Sub: "run:" + r.Run, Job: r.Job, Run: r.Run, Space: r.Space,
		Owner: r.Owner, Conversation: r.Conversation, Digest: r.Digest, Aud: r.Aud, Exp: exp,
		Kid: i.kid,
	})
	if err != nil {
		return out, fmt.Errorf("claims: %w", err)
	}
	tok, err := i.sign("run", claims)
	if err != nil {
		return out, fmt.Errorf("sign: %w", err)
	}
	return MintRunTokenResp{Token: tok, ExpiresAt: exp}, nil
}
