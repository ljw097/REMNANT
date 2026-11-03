package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Passwords are hashed with PBKDF2-HMAC-SHA256 (standard library), stored as
// "pbkdf2-sha256$<iterations>$<salt>$<key>" with unpadded base64.
const (
	passwordScheme = "pbkdf2-sha256"
	// PasswordIterations follows OWASP's 2023 guidance for PBKDF2-SHA256.
	PasswordIterations = 600_000
	MinPasswordLength  = 10
	MaxPasswordLength  = 256
)

// ErrWeakPassword describes why a new password is refused.
var ErrWeakPassword = errors.New("weak password")

// CheckPassword enforces the password rules: 10–256 characters and not the
// username.
func CheckPassword(username, password string) error {
	n := utf8.RuneCountInString(password)
	switch {
	case n < MinPasswordLength:
		return fmt.Errorf("%w: use at least %d characters", ErrWeakPassword, MinPasswordLength)
	case len(password) > MaxPasswordLength:
		return fmt.Errorf("%w: use at most %d bytes", ErrWeakPassword, MaxPasswordLength)
	case strings.EqualFold(strings.TrimSpace(password), username):
		return fmt.Errorf("%w: it must not be the username", ErrWeakPassword)
	}
	return nil
}

// HashPassword returns the encoded hash of password with a fresh salt.
func HashPassword(password string) (string, error) {
	return hashWith(password, PasswordIterations)
}

func hashWith(password string, iterations int) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return passwordScheme + "$" + strconv.Itoa(iterations) + "$" + enc.EncodeToString(salt) + "$" + enc.EncodeToString(key), nil
}

// dummyHash is verified against when a user does not exist or has no
// password, so a failed sign-in takes the same time either way.
var dummyHash, _ = HashPassword("remnant-no-such-user")

// VerifyPassword reports whether password matches encoded. An empty encoded
// hash never matches but costs the same as a real check.
func VerifyPassword(encoded, password string) bool {
	real := encoded != ""
	if !real {
		encoded = dummyHash
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != passwordScheme {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 || iterations > 10_000_000 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1 && real
}

// NewTemporaryPassword returns a random password (16 base32 characters, 80
// bits) for an account an administrator created or reset; the user must
// change it at the next sign-in.
func NewTemporaryPassword() string {
	return rand.Text()[:16]
}
