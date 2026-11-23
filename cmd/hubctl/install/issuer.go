package install

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/seal"
)

const issuerBootstrap = "issuer.bootstrap"

type bootstrap struct {
	Kid    string `json:"kid"`
	Pub    []byte `json:"pub"`
	Sealed []byte `json:"sealed"`
}

// ensureIssuerKey keeps the issuer seed sealed, including during first boot.
func ensureIssuerKey(serviceDir, hostDir string) error {
	sealer, err := seal.Load(filepath.Join(hostDir, fileKEK))
	if err != nil {
		return fmt.Errorf("issuer KEK: %w", err)
	}
	bootPath := filepath.Join(hostDir, issuerBootstrap)
	keyPath := filepath.Join(serviceDir, "service.key")
	pubPath := filepath.Join(serviceDir, "service.pub")
	key, err := readBootstrap(bootPath, sealer)
	if err == nil {
		return finishIssuerKey(key, keyPath, pubPath)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return initializeIssuerKey(sealer, bootPath, keyPath, pubPath)
}

func initializeIssuerKey(sealer *seal.Seal, bootPath, keyPath, pubPath string) error {
	key, err := previousIssuerKey(keyPath)
	if err != nil {
		return err
	}
	if key != nil {
		return storeIssuerKey(key, sealer, bootPath, keyPath, pubPath)
	}
	if _, err := os.Stat(pubPath); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("issuer pub stat: %w", err)
	}
	_, key, err = ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate issuer: %w", err)
	}
	return storeIssuerKey(key, sealer, bootPath, "", pubPath)
}

func previousIssuerKey(path string) (ed25519.PrivateKey, error) {
	key, err := auth.LoadPrivateKey(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("old issuer key: %w", err)
	}
	return key, nil
}

func storeIssuerKey(key ed25519.PrivateKey, sealer *seal.Seal, bootPath, keyPath, pubPath string) error {
	pub := ed25519.PublicKey(key[ed25519.SeedSize:])
	kid := idv1.Kid(pub)
	sealed, err := sealer.Seal("signing-key", kid, key)
	if err != nil {
		return fmt.Errorf("seal issuer: %w", err)
	}
	data, err := json.Marshal(bootstrap{Kid: kid, Pub: pub, Sealed: sealed})
	if err != nil {
		return fmt.Errorf("bootstrap json: %w", err)
	}
	created, err := writeOnce(bootPath, data, 0o600)
	if err != nil {
		return err
	}
	if !created {
		existing, err := readBootstrap(bootPath, sealer)
		if err != nil {
			return err
		}
		if !bytes.Equal(existing, key) {
			return fmt.Errorf("issuer bootstrap differs")
		}
	}
	return finishIssuerKey(key, keyPath, pubPath)
}

func finishIssuerKey(key ed25519.PrivateKey, keyPath, pubPath string) error {
	pub := ed25519.PublicKey(key[ed25519.SeedSize:])
	if err := ensureIssuerPub(pubPath, pub); err != nil {
		return err
	}
	if keyPath == "" {
		return nil
	}
	return removeOldIssuerKey(keyPath, key)
}

func removeOldIssuerKey(keyPath string, key ed25519.PrivateKey) error {
	old, err := previousIssuerKey(keyPath)
	if err != nil {
		return err
	}
	if old == nil {
		return nil
	}
	if !bytes.Equal(old, key) {
		return fmt.Errorf("old issuer differs from sealed bootstrap")
	}
	if err := os.Remove(keyPath); err != nil {
		return fmt.Errorf("remove plaintext issuer: %w", err)
	}
	return nil
}

func ensureIssuerPub(path string, pub ed25519.PublicKey) error {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return fmt.Errorf("marshal issuer pub: %w", err)
	}
	data := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	created, err := writeOnce(path, data, 0o644)
	if err != nil {
		return err
	}
	if !created {
		return verifyIssuerPub(path, pub)
	}
	return nil
}

func verifyIssuerPub(path string, pub ed25519.PublicKey) error {
	existing, err := auth.LoadPublicKey(path)
	if err != nil {
		return fmt.Errorf("issuer pub: %w", err)
	}
	if !bytes.Equal(existing, pub) {
		return fmt.Errorf("issuer pub differs")
	}
	return nil
}

func readBootstrap(path string, sealer *seal.Seal) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read issuer bootstrap: %w", err)
	}
	var b bootstrap
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("decode issuer bootstrap: %w", err)
	}
	if len(b.Pub) != ed25519.PublicKeySize || b.Kid != idv1.Kid(b.Pub) {
		return nil, fmt.Errorf("issuer bootstrap public key invalid")
	}
	plain, err := sealer.Open("signing-key", b.Kid, b.Sealed)
	if err != nil {
		return nil, fmt.Errorf("open issuer bootstrap: %w", err)
	}
	if len(plain) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("issuer seed length invalid")
	}
	key := ed25519.PrivateKey(plain)
	if !bytes.Equal(key[ed25519.SeedSize:], b.Pub) {
		return nil, fmt.Errorf("issuer bootstrap key pair differs")
	}
	return key, nil
}
