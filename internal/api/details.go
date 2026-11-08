package api

import (
	"net/http"
	"regexp"

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
