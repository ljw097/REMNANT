// Package auth implements REMNANT's own authentication: opaque bearer tokens
// whose SHA-256 digest is the only form stored server-side.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

type Role string

const (
	RoleAdmin      Role = "admin"
	RoleResearcher Role = "researcher"
	RoleReviewer   Role = "reviewer"
)

func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleResearcher, RoleReviewer:
		return true
	}
	return false
}

// Principal is the authenticated caller of a request.
type Principal struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     Role   `json:"role"`
}

// Has reports whether the principal holds one of roles. Admin holds every role.
func (p Principal) Has(roles ...Role) bool {
	if p.Role == RoleAdmin {
		return true
	}
	for _, r := range roles {
		if p.Role == r {
			return true
		}
	}
	return false
}

const tokenPrefix = "rmt_"

// NewToken returns a fresh token for the caller and the digest to persist.
func NewToken() (token, digest string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = tokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	return token, Digest(token), nil
}

func Digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ErrInvalidToken is returned by a TokenLookup for unknown, expired, revoked
// or disabled-user tokens. The middleware does not distinguish them.
var ErrInvalidToken = errors.New("invalid token")

type TokenLookup interface {
	LookupToken(ctx context.Context, digest string) (Principal, error)
}

type ctxKey struct{}

func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// Middleware rejects requests without a valid "Authorization: Bearer rmt_…"
// header and stores the Principal in the request context.
func Middleware(lookup TokenLookup, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !strings.HasPrefix(token, tokenPrefix) {
			unauthorized(w)
			return
		}
		p, err := lookup.LookupToken(r.Context(), Digest(token))
		if err != nil {
			unauthorized(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="remnant"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"authentication required","code":"unauthorized"}`))
}
