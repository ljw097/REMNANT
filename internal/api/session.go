package api

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ljw097/remnant/internal/auth"
	"github.com/ljw097/remnant/internal/store"
)

// login checks a username and password and sets the session cookie.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !s.limiter.allow(clientIP(r)) {
		tooManyAttempts(w)
		return
	}
	if in.Username == "" || in.Password == "" || len(in.Password) > auth.MaxPasswordLength {
		writeError(w, http.StatusUnauthorized, "unauthorized", store.ErrLogin.Error())
		return
	}
	p, ok := s.startSession(w, r, strings.ToLower(strings.TrimSpace(in.Username)), in.Password)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// startSession signs a user in and sets the cookie. It writes the error
// response itself when it fails.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, username, password string) (auth.Principal, bool) {
	idle := time.Duration(s.policy(r).SessionHours) * time.Hour
	p, token, err := s.store.Login(r.Context(), username, password, r.UserAgent(), idle)
	switch {
	case errors.Is(err, store.ErrLogin):
		s.log.Warn("sign-in failed", "username", username, "ip", clientIP(r))
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return p, false
	case errors.Is(err, store.ErrLoginLocked):
		s.log.Warn("sign-in refused: account locked", "username", username, "ip", clientIP(r))
		writeError(w, http.StatusTooManyRequests, "locked", err.Error())
		return p, false
	case errors.Is(err, store.ErrLoginPending):
		writeError(w, http.StatusForbidden, "pending", err.Error())
		return p, false
	case err != nil:
		s.fail(w, r, err)
		return p, false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
		HttpOnly: true,
		Secure:   secureRequest(r),
		SameSite: http.SameSiteStrictMode,
	})
	s.log.Info("signed in", "username", p.Username, "ip", clientIP(r))
	return p, true
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if p := principal(r); p.SessionID != 0 {
		if err := s.store.Logout(r.Context(), p.SessionID); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: auth.SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

// changePassword replaces the caller's password after checking the current
// one, and signs out their other sessions.
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	p := principal(r)
	if !validate(w, checkPassword(p.Username, in.New)) {
		return
	}
	if !s.limiter.allow(clientIP(r)) {
		tooManyAttempts(w)
		return
	}
	hash, err := s.store.PasswordHash(r.Context(), p.UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// An account without a password yet (API tokens only) sets its first one.
	if hash != "" && !auth.VerifyPassword(hash, in.Current) {
		writeError(w, http.StatusForbidden, "forbidden", "the current password is wrong")
		return
	}
	if in.New == in.Current {
		writeError(w, http.StatusBadRequest, "bad_request", "choose a password different from the current one")
		return
	}
	newHash, err := auth.HashPassword(in.New)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.store.SetPassword(r.Context(), p.UserID, newHash, false, p.SessionID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("password changed", "username", p.Username)
	w.WriteHeader(http.StatusNoContent)
}

// requestAccount records an account request from the sign-in page when the
// instance policy allows it. The account cannot sign in until approved.
func (s *Server) requestAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !s.policy(r).AccountRequests {
		writeError(w, http.StatusForbidden, "forbidden", "this instance does not take account requests; ask an administrator")
		return
	}
	if !validate(w, checkUsername(in.Username), checkPassword(in.Username, in.Password)) {
		return
	}
	if !s.requests.allow(clientIP(r)) {
		tooManyAttempts(w)
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.store.RequestAccount(r.Context(), in.Username, hash); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "that username is taken")
			return
		}
		s.fail(w, r, err)
		return
	}
	s.log.Info("account requested", "username", in.Username, "ip", clientIP(r))
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
}

// passwordGate limits a session that signed in with a temporary password
// to changing it, reading who it is, and signing out.
func passwordGate(next http.Handler) http.Handler {
	allowed := map[string]bool{
		"GET /api/v1/me":             true,
		"PUT /api/v1/me/password":    true,
		"POST /api/v1/auth/logout":   true,
		"GET /api/v1/instance":       true,
		"GET /api/v1/me/preferences": true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := principal(r); p.MustChangePassword && p.SessionID != 0 && !allowed[r.Method+" "+r.URL.Path] {
			writeError(w, http.StatusForbidden, "password_change_required", "change your temporary password first")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func tooManyAttempts(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts; wait a minute and try again")
}

// limiter allows max attempts per client in each window. It guards the
// password endpoints against guessing on top of the per-account lockout.
type limiter struct {
	max    int
	window time.Duration
	mu     sync.Mutex
	seen   map[string]*bucket
}

type bucket struct {
	start time.Time
	n     int
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, seen: map[string]*bucket{}}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.seen) > 10_000 {
		for k, b := range l.seen {
			if now.Sub(b.start) > l.window {
				delete(l.seen, k)
			}
		}
	}
	b := l.seen[key]
	if b == nil || now.Sub(b.start) > l.window {
		b = &bucket{start: now}
		l.seen[key] = b
	}
	b.n++
	return b.n <= l.max
}

// hostGuard refuses requests whose Host is not a loopback name when
// remnantd listens only on loopback. A web page elsewhere cannot then reach
// a researcher's local instance through DNS rebinding.
func hostGuard(listen string, next http.Handler) http.Handler {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return next
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Host
		if hh, _, err := net.SplitHostPort(h); err == nil {
			h = hh
		}
		h = strings.Trim(h, "[]")
		if ip := net.ParseIP(h); !strings.EqualFold(h, "localhost") && (ip == nil || !ip.IsLoopback()) {
			writeError(w, http.StatusMisdirectedRequest, "bad_host", "this instance only answers to localhost")
			return
		}
		next.ServeHTTP(w, r)
	})
}
