// Package seal encrypts secrets at rest with AES-256-GCM. The KEK is a
// 32-byte file kept outside volumes and backups; AAD binds each sealed
// value to its purpose and key id.
package seal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"os"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// KeyLen is the required KEK length in bytes.
const KeyLen = 32

// Seal holds the installation KEK.
type Seal struct {
	gcm cipher.AEAD
}

// Load reads the KEK file. The file must contain exactly KeyLen bytes.
func Load(path string) (*Seal, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read kek %s: %w", path, err)
	}
	return New(raw)
}

// New builds a Seal from raw key bytes.
func New(kek []byte) (*Seal, error) {
	if len(kek) != KeyLen {
		return nil, herrors.New(herrors.CodeInvalid, "err.seal"). //nolint:wrapcheck // shared error form
										With("field", "kek").With("want", KeyLen)
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return &Seal{gcm: gcm}, nil
}

// AAD binds a sealed value to purpose and key id.
func AAD(purpose, kid string) []byte {
	return []byte(purpose + "\x00" + kid)
}

// Seal encrypts plaintext; the output is nonce||ciphertext.
func (s *Seal) Seal(purpose, kid string, plain []byte) ([]byte, error) {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	out := s.gcm.Seal(nonce, nonce, plain, AAD(purpose, kid))
	return out, nil
}

// Open decrypts nonce||ciphertext bound to purpose and kid.
func (s *Seal) Open(purpose, kid string, blob []byte) ([]byte, error) {
	ns := s.gcm.NonceSize()
	if len(blob) < ns+s.gcm.Overhead() {
		return nil, herrors.New(herrors.CodeInvalid, "err.seal"). //nolint:wrapcheck // shared error form
										With("reason", "short")
	}
	plain, err := s.gcm.Open(nil, blob[:ns], blob[ns:], AAD(purpose, kid))
	if err != nil {
		return nil, herrors.Wrap(herrors.CodeInvalid, "err.seal", err)
	}
	return plain, nil
}
