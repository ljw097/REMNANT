// Package update finds, verifies and installs signed REMNANT releases.
//
// A release carries three kinds of assets:
//
//	remnant_<version>_<os>_<arch>.tar.gz   remnantd and remnant-worker
//	SHA256SUMS                             "<sha256>  <asset>" per archive
//	SHA256SUMS.sig                         base64 Ed25519 signature
//
// The signature covers "remnant-release-v1\n" followed by the exact bytes of
// SHA256SUMS. Only keys compiled into the binary are trusted, and the
// version is taken from the signed archive name, never from the release tag,
// so a compromised release host can neither substitute binaries nor roll
// an installation back to an older signed release.
package update

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const signatureContext = "remnant-release-v1\n"

// trustedKeys is a comma-separated list of base64 Ed25519 public keys.
// Release builds use the value below; test builds may replace it with
// -ldflags "-X github.com/ljw097/remnant/internal/update.trustedKeys=<key>".
var trustedKeys = productionKey

// TrustedKeys returns the public keys release signatures are checked
// against.
func TrustedKeys() ([]ed25519.PublicKey, error) {
	var keys []ed25519.PublicKey
	for _, s := range strings.Split(trustedKeys, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		k, err := ParsePublicKey(s)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return nil, errors.New("update: no trusted release keys")
	}
	return keys, nil
}

func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("update: invalid Ed25519 public key")
	}
	return ed25519.PublicKey(b), nil
}

// ParsePrivateKey accepts the base64 32-byte seed written by
// remnant-release keygen.
func ParsePrivateKey(s string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != ed25519.SeedSize {
		return nil, fmt.Errorf("update: invalid Ed25519 private key seed")
	}
	return ed25519.NewKeyFromSeed(b), nil
}

// Sign returns the base64 signature of a SHA256SUMS file.
func Sign(key ed25519.PrivateKey, sums []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, append([]byte(signatureContext), sums...)))
}

// Verify checks sig over sums against any of keys.
func Verify(keys []ed25519.PublicKey, sums []byte, sig string) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return errors.New("update: malformed signature")
	}
	msg := append([]byte(signatureContext), sums...)
	for _, k := range keys {
		if ed25519.Verify(k, msg, raw) {
			return nil
		}
	}
	return errors.New("update: SHA256SUMS signature does not verify against any trusted key")
}
