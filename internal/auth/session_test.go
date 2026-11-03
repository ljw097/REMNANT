package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionCookieAndCSRF(t *testing.T) {
	token, digest, err := NewSessionToken()
	if err != nil || !strings.HasPrefix(token, "rms_") {
		t.Fatalf("session token %q, %v", token, err)
	}
	want := Principal{UserID: 4, Username: "rita", Role: RoleResearcher, SessionID: 9}
	h := Middleware(mapLookup{digest: want}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, _ := FromContext(r.Context()); got != want {
			t.Errorf("principal = %+v", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	cases := []struct {
		name, method, cookie, csrf, origin string
		want                               int
	}{
		{"get with cookie", "GET", token, "", "", 204},
		{"post without csrf header", "POST", token, "", "", 403},
		{"post with csrf header", "POST", token, "1", "", 204},
		{"post same origin", "POST", token, "1", "http://remnant.test", 204},
		{"post cross origin", "POST", token, "1", "https://evil.example", 403},
		{"post null origin", "POST", token, "1", "null", 403},
		{"unknown session", "GET", "rms_nope", "", "", 401},
		{"token in cookie", "GET", "rmt_" + token[4:], "", "", 401},
		{"no credentials", "GET", "", "", "", 401},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, "http://remnant.test/api/v1/me", nil)
		if c.cookie != "" {
			r.AddCookie(&http.Cookie{Name: SessionCookie, Value: c.cookie})
		}
		if c.csrf != "" {
			r.Header.Set(CSRFHeader, c.csrf)
		}
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s: status %d, want %d", c.name, w.Code, c.want)
		}
	}
}

func TestPasswords(t *testing.T) {
	h, err := hashWith("correct horse battery", 1000)
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := hashWith("correct horse battery", 1000)
	if h == h2 {
		t.Error("hashes must be salted")
	}
	if !VerifyPassword(h, "correct horse battery") {
		t.Error("right password rejected")
	}
	for _, bad := range []string{"correct horse batterY", "", "correct horse battery "} {
		if VerifyPassword(h, bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	if VerifyPassword("", "remnant-no-such-user") {
		t.Error("empty hash matched the dummy password")
	}
	for _, mangled := range []string{"plain", "pbkdf2-sha256$x$aa$bb", "md5$1$aa$bb", "pbkdf2-sha256$1000$!!$bb"} {
		if VerifyPassword(mangled, "x") {
			t.Errorf("%q accepted", mangled)
		}
	}
	if !strings.HasPrefix(h, "pbkdf2-sha256$1000$") {
		t.Errorf("encoding %q", h)
	}
	for pw, ok := range map[string]bool{"short": false, "alice12345": true, "Alice": false, "longenough!": true, strings.Repeat("x", 300): false} {
		if err := CheckPassword("alice", pw); (err == nil) != ok {
			t.Errorf("CheckPassword(%q) = %v", pw, err)
		}
	}
	if CheckPassword("alicealice", "AliceAlice") == nil {
		t.Error("username accepted as password")
	}
	if p := NewTemporaryPassword(); len(p) != 16 || CheckPassword("x", p) != nil {
		t.Errorf("temporary password %q", p)
	}
}
