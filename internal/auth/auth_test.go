package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type mapLookup map[string]Principal

func (m mapLookup) LookupToken(_ context.Context, digest string) (Principal, error) {
	p, ok := m[digest]
	if !ok {
		return Principal{}, ErrInvalidToken
	}
	return p, nil
}

func TestMiddleware(t *testing.T) {
	token, digest, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "rmt_") || len(digest) != 64 {
		t.Fatalf("unexpected token %q / digest %q", token, digest)
	}
	want := Principal{UserID: 3, Username: "alice", Role: RoleReviewer}
	h := Middleware(mapLookup{digest: want}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := FromContext(r.Context())
		if !ok || got != want {
			t.Errorf("principal = %+v, %v", got, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	cases := map[string]int{
		"Bearer " + token:  http.StatusNoContent,
		"Bearer rmt_wrong": http.StatusUnauthorized,
		"Bearer " + digest: http.StatusUnauthorized,
		"":                 http.StatusUnauthorized,
		token:              http.StatusUnauthorized,
	}
	for header, code := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != code {
			t.Errorf("Authorization %q: status %d, want %d", header, rec.Code, code)
		}
	}
}

func TestPrincipalHas(t *testing.T) {
	if !(Principal{Role: RoleAdmin}).Has(RoleReviewer) {
		t.Error("admin should hold reviewer")
	}
	if (Principal{Role: RoleResearcher}).Has(RoleReviewer) {
		t.Error("researcher should not hold reviewer")
	}
}
