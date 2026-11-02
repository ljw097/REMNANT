package api

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
)

// Branding is the organization branding an administrator may set.
type Branding struct {
	Name   string `json:"name"`   // shown instead of "REMNANT"; empty keeps the default
	Accent string `json:"accent"` // #rrggbb for actionable elements; empty keeps the default
}

const brandingKey = "branding"

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// getBranding is public: the sign-in page shows it before any token exists.
func (s *Server) getBranding(w http.ResponseWriter, r *http.Request) {
	var b Branding
	if _, err := s.store.Setting(r.Context(), brandingKey, &b); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) setBranding(w http.ResponseWriter, r *http.Request) {
	var b Branding
	if !decode(w, r, &b) {
		return
	}
	b.Name = strings.TrimSpace(b.Name)
	var colorErr error
	if b.Accent != "" && !hexColor.MatchString(b.Accent) {
		colorErr = badRequest("accent must be a #rrggbb colour")
	}
	if !validate(w, maxLen("name", b.Name, 60), colorErr) {
		return
	}
	if err := s.store.SetSetting(r.Context(), brandingKey, b, principal(r).UserID); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) getPreferences(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.Preferences(r.Context(), principal(r).UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(p)
}

// setPreferences stores the caller's UI preferences: any JSON object up to
// 16 KiB. The UI owns the shape and ignores keys it does not know.
func (s *Server) setPreferences(w http.ResponseWriter, r *http.Request) {
	var prefs map[string]json.RawMessage
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	if err := dec.Decode(&prefs); err != nil || prefs == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "preferences must be a JSON object of at most 16 KiB")
		return
	}
	raw, _ := json.Marshal(prefs)
	if err := s.store.SetPreferences(r.Context(), principal(r).UserID, raw); err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

func (s *Server) listApprovals(w http.ResponseWriter, r *http.Request) {
	d := r.URL.Query().Get("decision")
	if d != "" && !validate(w, oneOf("decision", d, "pending", "approved", "denied", "expired", "revoked")) {
		return
	}
	items, err := s.store.ListApprovals(r.Context(), d)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) searchHypotheses(w http.ResponseWriter, r *http.Request) {
	var statuses []string
	for _, st := range strings.Split(r.URL.Query().Get("status"), ",") {
		if st = strings.TrimSpace(st); st == "" {
			continue
		}
		if !validate(w, oneOf("status", st, "draft", "triage", "accepted", "rejected", "validation_pending",
			"validated", "not_reproducible", "duplicate")) {
			return
		}
		statuses = append(statuses, st)
	}
	items, err := s.store.SearchHypotheses(r.Context(), statuses, 200)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	sum, err := s.store.Summary(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

// uiCSP allows only same-origin resources. Styles may be set from script
// (CSSOM) for the brandable accent, but no inline <style> or <script>.
const uiCSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; " +
	"connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// staticUI serves the embedded single-page app. Unknown paths that are not
// files get index.html, so deep links work; assets under /assets/ are
// content-hashed and cached for a year.
func staticUI(dist fs.FS) http.Handler {
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		index = []byte(`<!doctype html><meta charset="utf-8"><title>REMNANT</title>` +
			`<p>The web UI was not built into this binary. Build it with <code>npm --prefix web run build</code> and rebuild remnantd.</p>`)
	}
	files := http.FileServerFS(dist)
	modTime := time.Now()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", uiCSP)
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" && p != "index.html" {
			if fi, err := fs.Stat(dist, p); err == nil && !fi.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					h.Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		h.Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", modTime, bytes.NewReader(index))
	})
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) getPattern(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetPattern(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
