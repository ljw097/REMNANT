// Package auth implements REMNANT's own authentication: password sign-in
// with browser sessions, and opaque bearer tokens for scripts. Sessions and
// tokens are both random secrets whose SHA-256 digest is the only form
// stored server-side.
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
	// MustChangePassword limits a session to changing the password and
	// signing out (a temporary password was issued).
	MustChangePassword bool `json:"must_change_password"`
	// SessionID is set when the request was authenticated by a browser
	// session rather than an API token.
	SessionID int64 `json:"-"`
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

const (
	tokenPrefix   = "rmt_"
	sessionPrefix = "rms_"
	// SessionCookie holds the browser session token.
	SessionCookie = "remnant_session"
	// CSRFHeader must accompany every state-changing request made with a
	// session cookie. Browsers cannot attach a custom header cross-origin
	// without a CORS preflight, which remnantd never grants.
	CSRFHeader = "X-Remnant-Request"
)

// NewToken returns a fresh token for the caller and the digest to persist.
func NewToken() (token, digest string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = tokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	return token, Digest(token), nil
}

// NewSessionToken returns a fresh browser session token and its digest.
func NewSessionToken() (token, digest string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = sessionPrefix + base64.RawURLEncoding.EncodeToString(b)
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
	LookupSession(ctx context.Context, digest string) (Principal, error)
}

type ctxKey struct{}

func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// Middleware authenticates a request by its "Authorization: Bearer rmt_…"
// header or, failing that, its session cookie, and stores the Principal in
// the request context. Requests with neither are rejected. A state-changing
// request authenticated by cookie must carry CSRFHeader and, when the
// browser sends Origin, come from the same origin.
func Middleware(lookup TokenLookup, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("Authorization"); h != "" {
			token, ok := strings.CutPrefix(h, "Bearer ")
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
			return
		}
		c, err := r.Cookie(SessionCookie)
		if err != nil || !strings.HasPrefix(c.Value, sessionPrefix) {
			unauthorized(w)
			return
		}
		if !SameOriginRequest(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"cross-site request refused","code":"forbidden"}`))
			return
		}
		p, err := lookup.LookupSession(r.Context(), Digest(c.Value))
		if err != nil {
			unauthorized(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
	})
}

// SameOriginRequest reports whether a cookie-carrying request may change
// state: safe methods always may; others need CSRFHeader and, if the
// browser sent Origin, an Origin whose host is the request's Host.
func SameOriginRequest(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	if r.Header.Get(CSRFHeader) != "1" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	_, host, ok := strings.Cut(origin, "://")
	return ok && strings.EqualFold(host, r.Host)
}

// SessionTokenFrom returns the session token in r's cookie, if any.
func SessionTokenFrom(r *http.Request) (string, bool) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || !strings.HasPrefix(c.Value, sessionPrefix) {
		return "", false
	}
	return c.Value, true
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="remnant"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"authentication required","code":"unauthorized"}`))
}
