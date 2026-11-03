package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHostGuard(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	local := hostGuard("127.0.0.1:8080", ok)
	for host, want := range map[string]int{
		"127.0.0.1:8080": 204, "localhost:8080": 204, "LOCALHOST": 204, "[::1]:8080": 204,
		"evil.example:8080": 421, "192.168.0.5:8080": 421, "127.0.0.1.evil.example": 421,
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = host
		w := httptest.NewRecorder()
		local.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("loopback listener, Host %q: %d, want %d", host, w.Code, want)
		}
	}
	// A listener on all interfaces is reached by other names; no guard.
	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "remnant.example.org"
	w := httptest.NewRecorder()
	hostGuard("0.0.0.0:8080", ok).ServeHTTP(w, r)
	if w.Code != 204 {
		t.Errorf("public listener: %d", w.Code)
	}
}

func TestLimiter(t *testing.T) {
	l := newLimiter(3, time.Hour)
	for i := range 3 {
		if !l.allow("a") {
			t.Fatalf("attempt %d refused", i+1)
		}
	}
	if l.allow("a") {
		t.Fatal("fourth attempt allowed")
	}
	if !l.allow("b") {
		t.Fatal("another client was limited")
	}
}
