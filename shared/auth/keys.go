package auth

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

// LoadPrivateKey reads a PEM PKCS8 Ed25519 private key as written by
// `hubctl init`.
func LoadPrivateKey(path string) (ed25519.PrivateKey, error) {
	key, err := loadKey(path, "PRIVATE KEY", x509.ParsePKCS8PrivateKey)
	if err != nil {
		return nil, err
	}
	sk, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("auth: %s is not Ed25519", path)
	}
	return sk, nil
}

// LoadPublicKey reads a PEM PKIX Ed25519 public key.
func LoadPublicKey(path string) (ed25519.PublicKey, error) {
	key, err := loadKey(path, "PUBLIC KEY", x509.ParsePKIXPublicKey)
	if err != nil {
		return nil, err
	}
	pk, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("auth: %s is not Ed25519", path)
	}
	return pk, nil
}

func loadKey(path, typ string, parse func([]byte) (any, error)) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("auth: read key %s: %w", path, err)
	}
	blk, _ := pem.Decode(data)
	if blk == nil || blk.Type != typ {
		return nil, fmt.Errorf("auth: %s is not a PEM %s", path, typ)
	}
	key, err := parse(blk.Bytes)
	if err != nil {
		return nil, fmt.Errorf("auth: parse %s: %w", path, err)
	}
	return key, nil
}
