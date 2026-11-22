package seal

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/testkit"
)

func key(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, KeyLen)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func mustSeal(t *testing.T) *Seal {
	t.Helper()
	s, err := New(key(t))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func sealed(t *testing.T, s *Seal, purpose, kid, plain string) []byte {
	t.Helper()
	blob, err := s.Seal(purpose, kid, []byte(plain))
	if err != nil {
		t.Fatal(err)
	}
	return blob
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.secrets-at-rest.gcm-aad")
	s := mustSeal(t)
	blob := sealed(t, s, "identity-seed", "k1", "top secret")
	plain, err := s.Open("identity-seed", "k1", blob)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "top secret" {
		t.Fatalf("plain=%q", plain)
	}
}

func TestWrongPurposeOrKidFails(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.secrets-at-rest.gcm-aad")
	s := mustSeal(t)
	blob := sealed(t, s, "p", "k1", "x")
	for _, tc := range [][2]string{{"other", "k1"}, {"p", "k2"}} {
		if _, err := s.Open(tc[0], tc[1], blob); err == nil {
			t.Errorf("open with %s/%s must fail", tc[0], tc[1])
		}
	}
}

func TestWrongKeyAndTamperFail(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.secrets-at-rest.gcm-aad")
	s1, s2 := mustSeal(t), mustSeal(t)
	blob := sealed(t, s1, "p", "k", "x")
	if _, err := s2.Open("p", "k", blob); err == nil {
		t.Fatal("wrong key must fail")
	}
	blob[len(blob)-1] ^= 0xff
	if _, err := s1.Open("p", "k", blob); err == nil {
		t.Fatal("tampered blob must fail")
	}
}

func TestLoadRejectsBadLen(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.secrets-at-rest.kek-file")
	dir := t.TempDir()
	p := filepath.Join(dir, "kek")
	if err := os.WriteFile(p, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("short kek must fail")
	}
	if _, err := Load(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing kek must fail")
	}
}

func TestNoPlaintextLeak(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.secrets-at-rest.no-plaintext")
	s := mustSeal(t)
	blob := sealed(t, s, "p", "k", "a-very-secret-string")
	if bytes.Contains(blob, []byte("a-very-secret-string")) {
		t.Fatal("ciphertext contains plaintext")
	}
	var herr *herrors.Error
	_, err := s.Open("q", "k", blob)
	if errors.As(err, &herr) && strings.Contains(herr.Error(), "a-very-secret") {
		t.Fatal("error leaks plaintext")
	}
}
