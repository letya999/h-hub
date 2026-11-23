package install

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/seal"
)

func TestIssuerMigration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	serviceDir := filepath.Join(dir, "identity")
	hostDir := filepath.Join(dir, "host")
	mustIssuer(t, mkdir(serviceDir))
	mustIssuer(t, mkdir(hostDir))
	mustIssuer(t, ensureKEK(hostDir))
	mustIssuer(t, ensureKey(serviceDir, svcKey))
	keyPath := filepath.Join(serviceDir, "service.key")
	pubPath := filepath.Join(serviceDir, "service.pub")
	old, err := auth.LoadPrivateKey(keyPath)
	mustIssuer(t, err)
	pub, err := auth.LoadPublicKey(pubPath)
	mustIssuer(t, err)
	mustIssuer(t, ensureIssuerKey(serviceDir, hostDir))
	if _, err := os.Stat(keyPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("plaintext key remains: %v", err)
	}
	sealer, err := seal.Load(filepath.Join(hostDir, fileKEK))
	mustIssuer(t, err)
	got, err := readBootstrap(filepath.Join(hostDir, issuerBootstrap), sealer)
	if err != nil || !bytes.Equal(got, old) {
		t.Fatalf("sealed key changed: %v", err)
	}
	if !bytes.Equal(pub, got[32:]) {
		t.Fatal("public key changed")
	}
	mustIssuer(t, ensureIssuerKey(serviceDir, hostDir))
	mustIssuer(t, os.WriteFile(filepath.Join(hostDir, fileKEK), bytes.Repeat([]byte{1}, seal.KeyLen), 0o600))
	if err := ensureIssuerKey(serviceDir, hostDir); err == nil {
		t.Fatal("wrong KEK accepted")
	}
}

func mustIssuer(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
