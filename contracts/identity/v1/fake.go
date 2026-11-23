package v1

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"sync"
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
	sign   Signer
	kid    string
	pubs   map[string]ed25519.PublicKey
	now    func() int64
	mu     sync.Mutex
	people map[string]fakePerson
	codes  map[string]string
	links  map[string]string
}

// NewFake builds a Fake: sign mints tokens, kid marks the issuer,
// pubs maps each known subject to its public key, now supplies unix
// seconds.
func NewFake(sign Signer, kid string, pubs map[string]ed25519.PublicKey,
	now func() int64) *Fake {
	return &Fake{sign: sign, kid: kid, pubs: pubs, now: now,
		people: map[string]fakePerson{}, codes: map[string]string{}, links: map[string]string{}}
}

// Kid derives the issuer key id from a public key.
func Kid(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:6])
}

// Handlers exposes the fake as the contract operation table.
func (f *Fake) Handlers() Handlers {
	h := NewIssuer(f.sign, f.kid, f.pubs, f.now).Handlers()
	return Handlers{
		IssueServiceToken: h.IssueServiceToken,
		MintRunToken:      h.MintRunToken,
		ActAs:             h.ActAs,
		CreatePrincipal:   f.createPrincipal,
		DeactivateHuman:   f.deactivateHuman,
		LinkBegin:         f.linkBegin,
		LinkConfirm:       f.linkConfirm,
		Resolve:           f.resolve,
		MintLinkToken:     f.mintLinkToken,
	}
}
