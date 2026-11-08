package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/ljw097/remnant/internal/fetch"
	"github.com/ljw097/remnant/internal/source"
	"github.com/ljw097/remnant/internal/store"
)

// Managing targets, runs and patterns after creation: inspect, edit,
// refresh and delete. The store refuses what would break a run in progress
// or erase an audit record; these handlers also tidy the files a deleted
// record owned (a fetched snapshot, a run's artifacts). Local snapshot
// directories belong to the person who placed them and are never removed.

func (s *Server) getTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	t, err := s.store.GetTarget(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	use, err := s.store.TargetUse(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"target": t, "use": use})
}

// updateTarget edits a target's name and language; for a git target a new
// source_ref re-fetches it at that ref.
func (s *Server) updateTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		DisplayName     *string `json:"display_name"`
		PrimaryLanguage *string `json:"primary_language"`
		SourceRef       *string `json:"source_ref"`
	}
	if !decode(w, r, &in) {
		return
	}
	var checks []error
	if in.DisplayName != nil {
		checks = append(checks, required("display_name", *in.DisplayName, 200))
	}
	if in.PrimaryLanguage != nil {
		checks = append(checks, maxLen("primary_language", *in.PrimaryLanguage, 40))
	}
	if !validate(w, checks...) {
		return
	}
	t, err := s.store.GetTarget(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if in.SourceRef != nil && *in.SourceRef != t.SourceRef {
		if t.Kind != "git_repository" {
			writeError(w, http.StatusBadRequest, "bad_request", "source_ref applies only to git_repository targets")
			return
		}
		if err := fetch.ValidateRef(*in.SourceRef); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
	} else {
		in.SourceRef = nil
	}
	if in.DisplayName != nil || in.PrimaryLanguage != nil {
		if t, err = s.store.UpdateTarget(r.Context(), id, store.TargetPatch{DisplayName: in.DisplayName, PrimaryLanguage: in.PrimaryLanguage}); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if in.SourceRef != nil {
		if t, err = s.refetch(r, t, in.SourceRef); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, t)
}

// refreshTarget re-pins a target to what its source holds now: a local
// snapshot is re-hashed; a git target is fetched again at its ref.
func (s *Server) refreshTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	t, err := s.store.GetTarget(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if t.Kind == "git_repository" {
		if t, err = s.refetch(r, t, nil); err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusAccepted, t)
		return
	}
	rev, digest, err := source.LocalSnapshot(s.cfg.SnapshotDir(), t.SnapshotKey)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "snapshot_unavailable", s.snapshotProblem(err))
		return
	}
	if t, err = s.store.RepinLocalTarget(r.Context(), id, rev, digest); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// refetch queues a git target for a fresh fetch into a new snapshot
// directory (a fetched snapshot is never overwritten in place) and removes
// the old directory.
func (s *Server) refetch(r *http.Request, t store.Target, ref *string) (store.Target, error) {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	base := refetchSuffix.ReplaceAllString(t.SnapshotKey, "")
	if len(base) > 100 {
		base = base[:100]
	}
	nt, oldKey, err := s.store.RequeueGitTarget(r.Context(), t.ID, base+"-"+hex.EncodeToString(b), ref)
	if err != nil {
		return nt, err
	}
	s.removeSnapshot(r, oldKey, t.ID)
	s.log.Info("target queued for re-fetch", "target", t.ID, "ref", nt.SourceRef, "by", principal(r).Username)
	return nt, nil
}

var refetchSuffix = regexp.MustCompile(`-[0-9a-f]{8}$`)

// removeSnapshot deletes a fetched snapshot directory nobody else uses.
func (s *Server) removeSnapshot(r *http.Request, key string, owner int64) {
	if source.ValidateSnapshotKey(key) != nil {
		return
	}
	if shared, err := s.store.SnapshotKeyShared(r.Context(), key, owner); err != nil || shared {
		return
	}
	dir := filepath.Join(s.cfg.SnapshotDir(), key)
	if err := os.RemoveAll(dir); err != nil {
		s.log.Warn("remove snapshot", "dir", dir, "err", err)
	}
}

func (s *Server) deleteTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	t, err := s.store.DeleteTarget(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if t.Kind == "git_repository" {
		s.removeSnapshot(r, t.SnapshotKey, t.ID)
	}
	s.log.Info("target deleted", "target", id, "by", principal(r).Username)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteRun(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	dir := filepath.Join(s.cfg.ArtifactDir(), "runs", strconv.FormatInt(id, 10))
	if err := os.RemoveAll(dir); err != nil {
		s.log.Warn("remove run artifacts", "dir", dir, "err", err)
	}
	s.log.Info("run deleted", "run", id, "by", principal(r).Username)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) updatePattern(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Title              *string   `json:"title"`
		VulnerabilityClass *string   `json:"vulnerability_class"`
		CWEID              *string   `json:"cwe_id"`
		Language           *string   `json:"language"`
		RootCause          *string   `json:"root_cause"`
		Preconditions      *[]string `json:"preconditions"`
		CodeSignatures     *[]string `json:"code_signatures"`
		CounterSignatures  *[]string `json:"counter_signatures"`
	}
	if !decode(w, r, &in) {
		return
	}
	var checks []error
	for _, f := range []struct {
		name string
		v    *string
		max  int
		req  bool
	}{{"title", in.Title, 300, true}, {"vulnerability_class", in.VulnerabilityClass, 100, true}, {"root_cause", in.RootCause, 20000, true},
		{"cwe_id", in.CWEID, 20, false}, {"language", in.Language, 40, false}} {
		if f.v == nil {
			continue
		}
		if f.req {
			checks = append(checks, required(f.name, *f.v, f.max))
		} else {
			checks = append(checks, maxLen(f.name, *f.v, f.max))
		}
	}
	if !validate(w, checks...) {
		return
	}
	p, err := s.store.UpdatePattern(r.Context(), id, store.PatternPatch{
		Title: in.Title, VulnerabilityClass: in.VulnerabilityClass, CWEID: in.CWEID, Language: in.Language, RootCause: in.RootCause,
		Preconditions: in.Preconditions, CodeSignatures: in.CodeSignatures, CounterSignatures: in.CounterSignatures,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deletePattern(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeletePattern(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("pattern deleted", "pattern", id, "by", principal(r).Username)
	w.WriteHeader(http.StatusNoContent)
}
