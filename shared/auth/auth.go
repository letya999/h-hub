// Package auth owns the token wire format and signature checks. It
// does not decide authorization: services own their claims schema and
// policy; this package only parses and verifies Ed25519 signatures.
package auth

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// Format: hhub_<purpose>.<payload-b64url>.<sig-b64url>
const prefix = "hhub_"

// Token is a parsed bearer token.
type Token struct {
	Purpose   string
	Payload   []byte
	Signature []byte
}

// Parse checks the token's structure without verifying it.
func Parse(tok string) (Token, error) {
	var t Token
	if !strings.HasPrefix(tok, prefix) {
		return t, tokenErr("prefix")
	}
	parts := strings.SplitN(strings.TrimPrefix(tok, prefix), ".", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return t, tokenErr("shape")
	}
	var err error
	if t.Payload, err = base64.RawURLEncoding.DecodeString(parts[1]); err != nil {
		return t, herrors.Wrap(herrors.CodeUnauthorized, "err.token", err)
	}
	if t.Signature, err = base64.RawURLEncoding.DecodeString(parts[2]); err != nil {
		return t, herrors.Wrap(herrors.CodeUnauthorized, "err.token", err)
	}
	t.Purpose = parts[0]
	return t, nil
}

func signedBytes(purpose string, payload []byte) []byte {
	head := fmt.Sprintf("%s.%s", purpose, base64.RawURLEncoding.EncodeToString(payload))
	return []byte(head)
}

func tokenErr(reason string) *herrors.Error {
	return herrors.New(herrors.CodeUnauthorized, "err.token").With("reason", reason)
}

// Verify checks the Ed25519 signature over purpose.payload.
func (t Token) Verify(pub ed25519.PublicKey) error {
	if !ed25519.Verify(pub, signedBytes(t.Purpose, t.Payload), t.Signature) {
		return tokenErr("signature")
	}
	return nil
}

// Sign is the low-level format primitive used by the single issuing
// service (identity-hub) and by tests. Authorization decisions stay in
// services.
func Sign(priv ed25519.PrivateKey, purpose string, payload []byte) (string, error) {
	body := signedBytes(purpose, payload)
	sig := ed25519.Sign(priv, body)
	out := fmt.Sprintf("%s%s.%s", prefix, string(body), base64.RawURLEncoding.EncodeToString(sig))
	return out, nil
}
