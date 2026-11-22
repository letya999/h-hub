package install

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	fileID   = "install-id"
	fileKEK  = "seal-kek"
	dirKeys  = "keys"
	dirSvc   = "services"
	dirDB    = "db"
	opKey    = "operator"
	svcKey   = "service"
	dbSuffix = ".password"
)

// writeOnce writes data with perm only when the file is absent; an
// existing file is never overwritten (init idempotency contract).
func writeOnce(path string, data []byte, perm fs.FileMode) (created bool, err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("install: open %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		return false, errors.Join(fmt.Errorf("install: write %s: %w", path, err), f.Close())
	}
	if err := f.Close(); err != nil {
		return true, fmt.Errorf("install: close %s: %w", path, err)
	}
	return true, nil
}

func mkdir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("install: mkdir %s: %w", path, err)
	}
	return nil
}

// ensureID returns the install id, generating it on first run.
func ensureID(dir string) (string, error) {
	path := filepath.Join(dir, fileID)
	b, err := os.ReadFile(path)
	if err == nil {
		return string(b), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("install: read %s: %w", path, err)
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("install: rand install id: %w", err)
	}
	id := hex.EncodeToString(raw)
	_, err = writeOnce(path, []byte(id), 0o644)
	return id, err
}

// ensureKey writes a fresh Ed25519 pair unless both files exist.
func ensureKey(dir, name string) error {
	priv := filepath.Join(dir, name+".key")
	pub := filepath.Join(dir, name+".pub")
	if _, err := os.Stat(priv); err == nil {
		if _, err := os.Stat(pub); err == nil {
			return nil
		}
		return fmt.Errorf("install: %s exists but %s is missing", priv, pub)
	}
	_, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("install: generate key %s: %w", name, err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(sk)
	if err != nil {
		return fmt.Errorf("install: marshal key %s: %w", name, err)
	}
	pk, err := x509.MarshalPKIXPublicKey(sk.Public())
	if err != nil {
		return fmt.Errorf("install: marshal pub %s: %w", name, err)
	}
	if _, err := writeOnce(priv, pem.EncodeToMemory(
		&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return err
	}
	_, err = writeOnce(pub, pem.EncodeToMemory(
		&pem.Block{Type: "PUBLIC KEY", Bytes: pk}), 0o644)
	return err
}

// ensurePassword returns the stored password for a db role or generates
// a fresh one; never overwrites.
func ensurePassword(dir, role string) (string, error) {
	path := filepath.Join(dir, role+dbSuffix)
	b, err := os.ReadFile(path)
	if err == nil {
		return string(b), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("install: read %s: %w", path, err)
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("install: rand password %s: %w", role, err)
	}
	pw := hex.EncodeToString(raw)
	if _, err := writeOnce(path, []byte(pw), 0o600); err != nil {
		return "", err
	}
	return pw, nil
}

func ensureKEK(dir string) error {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("install: rand kek: %w", err)
	}
	_, err := writeOnce(filepath.Join(dir, fileKEK), raw, 0o600)
	return err
}
