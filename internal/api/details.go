package api

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/ljw097/remnant/internal/store"
)

// Detail endpoints behind the numbers the UI shows: every model call with
// its prompt and answer, the candidate regions a run examined, and the
// diff of a commit an intake read.

func (s *Server) listModelCalls(f func(id int64) store.ModelCallFilter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		calls, err := s.store.ListModelCalls(r.Context(), f(id))
		if err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, calls)
	}
}

func (s *Server) getModelCall(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	c, err := s.store.GetModelCall(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// listCandidates returns the source regions a run selected for review, with
// their score, matched signatures and outcome (in metadata).
func (s *Server) listCandidates(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	cs, err := s.store.ListCandidates(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]store.Evidence, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Evidence)
	}
	writeJSON(w, http.StatusOK, out)
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// intakeCommit returns one commit an intake recorded, with its diff.
func (s *Server) intakeCommit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	sha := r.PathValue("sha")
	if !shaRe.MatchString(sha) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid commit sha")
		return
	}
	in, err := s.store.GetIntake(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.store.CommitRecord(r.Context(), in.TargetID, sha)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// searchModelCalls is the instance-wide LLM record: every agent call,
// newest first, filterable, with totals for the whole filter.
func (s *Server) searchModelCalls(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	q := store.ModelCallQuery{Role: v.Get("role"), Outcome: v.Get("outcome"), Owner: v.Get("owner"), Q: strings.TrimSpace(v.Get("q"))}
	checks := []error{maxLen("q", q.Q, 200)}
	if q.Role != "" {
		checks = append(checks, oneOf("role", q.Role, "hypothesis_researcher", "skeptic", "reporter", "historian"))
	}
	if q.Outcome != "" {
		checks = append(checks, oneOf("outcome", q.Outcome, "ok", "refused", "truncated", "error", "stopped"))
	}
	if q.Owner != "" {
		checks = append(checks, oneOf("owner", q.Owner, "run", "intake"))
	}
	if !validate(w, checks...) {
		return
	}
	if s := v.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 500 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit must be 1–500")
			return
		}
		q.Limit = n
	}
	for _, f := range []struct {
		name string
		dst  *int64
	}{{"run_id", &q.RunID}, {"intake_id", &q.IntakeID}, {"before_id", &q.BeforeID}} {
		if s := v.Get(f.name); s != "" {
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil || n < 1 {
				writeError(w, http.StatusBadRequest, "bad_request", "invalid "+f.name)
				return
			}
			*f.dst = n
		}
	}
	page, err := s.store.SearchModelCalls(r.Context(), q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
