// Package api is remnantd's HTTP interface: a JSON REST API under /api/v1
// and a Server-Sent Events stream per run.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/ljw097/remnant/internal/auth"
	"github.com/ljw097/remnant/internal/config"
	"github.com/ljw097/remnant/internal/store"
)

type Server struct {
	store *store.Store
	cfg   config.Config
	log   *slog.Logger
}

func New(st *store.Store, cfg config.Config, log *slog.Logger) http.Handler {
	s := &Server{store: st, cfg: cfg, log: log}

	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/me", s.me)

	api.HandleFunc("GET /api/v1/llm-profiles", s.listLLMProfiles)
	api.HandleFunc("POST /api/v1/llm-profiles", s.requireRole(s.createLLMProfile, auth.RoleAdmin))

	api.HandleFunc("GET /api/v1/projects", s.listProjects)
	api.HandleFunc("POST /api/v1/projects", s.requireRole(s.createProject, auth.RoleResearcher))
	api.HandleFunc("GET /api/v1/projects/{id}", s.getProject)
	api.HandleFunc("PATCH /api/v1/projects/{id}", s.requireRole(s.updateProject, auth.RoleResearcher))
	api.HandleFunc("GET /api/v1/projects/{id}/targets", s.listTargets)
	api.HandleFunc("POST /api/v1/projects/{id}/targets", s.requireRole(s.createTarget, auth.RoleResearcher))

	api.HandleFunc("GET /api/v1/patterns", s.searchPatterns)
	api.HandleFunc("POST /api/v1/patterns", s.requireRole(s.createPattern, auth.RoleResearcher))
	api.HandleFunc("POST /api/v1/patterns/{id}/review", s.requireRole(s.reviewPattern, auth.RoleReviewer))

	api.HandleFunc("GET /api/v1/runs", s.listRuns)
	api.HandleFunc("POST /api/v1/runs", s.requireRole(s.createRun, auth.RoleResearcher))
	api.HandleFunc("GET /api/v1/runs/{id}", s.getRun)
	api.HandleFunc("POST /api/v1/runs/{id}/cancel", s.requireRole(s.cancelRun, auth.RoleResearcher))
	api.HandleFunc("GET /api/v1/runs/{id}/events", s.runEvents)
	api.HandleFunc("GET /api/v1/runs/{id}/hypotheses", s.listHypotheses)
	api.HandleFunc("POST /api/v1/runs/{id}/finalize", s.requireRole(s.finalizeRun, auth.RoleResearcher, auth.RoleReviewer))
	api.HandleFunc("GET /api/v1/runs/{id}/experiments", s.listExperiments)
	api.HandleFunc("GET /api/v1/runs/{id}/artifacts", s.listArtifacts)
	api.HandleFunc("GET /api/v1/runs/{id}/artifacts/{artifactId}", s.artifactContent)

	api.HandleFunc("GET /api/v1/hypotheses/{id}", s.getHypothesis)
	api.HandleFunc("PATCH /api/v1/hypotheses/{id}", s.requireRole(s.triageHypothesis, auth.RoleReviewer))
	api.HandleFunc("POST /api/v1/hypotheses/{id}/lab-approval", s.requireRole(s.requestLabApproval, auth.RoleResearcher))
	api.HandleFunc("POST /api/v1/approvals/{id}", s.requireRole(s.decideApproval, auth.RoleReviewer))

	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := st.Ping(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "database unreachable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	root.Handle("/api/", auth.Middleware(st, api))
	return securityHeaders(root)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireRole(h http.HandlerFunc, roles ...auth.Role) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		if !p.Has(roles...) {
			writeError(w, http.StatusForbidden, "forbidden", "role "+string(p.Role)+" cannot perform this action")
			return
		}
		h(w, r)
	}
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	writeJSON(w, http.StatusOK, p)
}

func principal(r *http.Request) auth.Principal {
	p, _ := auth.FromContext(r.Context())
	return p
}

// --- helpers -----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

// fail maps store errors to HTTP responses and logs unexpected ones.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, store.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid", err.Error())
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	default:
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

type badRequest string

func (e badRequest) Error() string { return string(e) }

// decode reads a JSON body of at most 1 MiB, rejecting unknown fields.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}

func validate(w http.ResponseWriter, checks ...error) bool {
	for _, err := range checks {
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return false
		}
	}
	return true
}

func required(field, v string, max int) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return badRequest(field + " is required")
	}
	return maxLen(field, v, max)
}

func maxLen(field, v string, max int) error {
	if len(v) > max {
		return badRequest(field + " is too long")
	}
	return nil
}

func oneOf(field, v string, allowed ...string) error {
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return badRequest(field + " must be one of " + strings.Join(allowed, ", "))
}

func dedupe(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
