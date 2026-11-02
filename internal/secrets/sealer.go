// Package secrets encrypts credentials at rest with a master key that is
// supplied outside the database.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// Sealer performs AES-256-GCM authenticated encryption. The sealed form is
// nonce || ciphertext || tag.
type Sealer struct {
	aead cipher.AEAD
}

func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts plaintext. aad binds the ciphertext to its purpose so a value
// copied into another column fails to open.
func (s *Sealer) Seal(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, plaintext, aad), nil
}

func (s *Sealer) Open(sealed, aad []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n+s.aead.Overhead() {
		return nil, errors.New("secrets: sealed value too short")
	}
	return s.aead.Open(nil, sealed[:n], sealed[n:], aad)
}

// Hint returns a display-safe suffix of a secret, e.g. "…a1B2".
func Hint(secret string) string {
	if len(secret) < 12 {
		return "…"
	}
	return "…" + secret[len(secret)-4:]
}
