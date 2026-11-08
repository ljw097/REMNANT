package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ljw097/remnant/internal/fetch"
	"github.com/ljw097/remnant/internal/llm"
	"github.com/ljw097/remnant/internal/policy"
	"github.com/ljw097/remnant/internal/source"
	"github.com/ljw097/remnant/internal/store"
)

func (s *Server) listLLMProfiles(w http.ResponseWriter, r *http.Request) {
	ps, err := s.store.ListLLMProfiles(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *Server) createLLMProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string       `json:"name"`
		Provider string       `json:"provider"`
		BaseURL  string       `json:"base_url"`
		Model    string       `json:"model"`
		Effort   string       `json:"effort"`
		APIKey   string       `json:"api_key"`
		Options  *llm.Options `json:"options"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Effort == "" {
		in.Effort = "high"
	}
	// New profiles think adaptively unless told otherwise.
	opts := llm.Options{Thinking: "adaptive"}
	if in.Options != nil {
		opts = *in.Options
	}
	if !validate(w,
		required("name", in.Name, 120),
		oneOf("provider", in.Provider, "anthropic", "openai"),
		required("model", in.Model, 200),
		oneOf("effort", in.Effort, "low", "medium", "high", "xhigh", "max"),
		required("api_key", in.APIKey, 4096),
		maxLen("base_url", in.BaseURL, 2048),
		checkBaseURL(in.BaseURL),
		checkOptions(&opts),
	) {
		return
	}
	p, err := s.store.CreateLLMProfile(r.Context(), store.LLMProfileInput{
		Name: in.Name, Provider: in.Provider, BaseURL: in.BaseURL, Model: in.Model, Effort: in.Effort, APIKey: in.APIKey, Options: opts,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.store.ListProjects(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name         string         `json:"name"`
		Description  string         `json:"description"`
		LLMProfileID *int64         `json:"llm_profile_id"`
		Policy       *policy.Policy `json:"policy"`
	}
	if !decode(w, r, &in) {
		return
	}
	pol := policy.Default()
	if in.Policy != nil {
		pol = *in.Policy
	}
	if !validate(w, required("name", in.Name, 120), maxLen("description", in.Description, 8000), pol.Validate()) {
		return
	}
	p, err := s.store.CreateProject(r.Context(), store.ProjectInput{
		Name: in.Name, Description: in.Description, LLMProfileID: in.LLMProfileID, Policy: pol,
	}, principal(r).UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetProject(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Name         *string        `json:"name"`
		Description  *string        `json:"description"`
		Status       *string        `json:"status"`
		LLMProfileID *int64         `json:"llm_profile_id"`
		ClearProfile bool           `json:"clear_llm_profile"`
		Policy       *policy.Policy `json:"policy"`
	}
	if !decode(w, r, &in) {
		return
	}
	var checks []error
	if in.Name != nil {
		checks = append(checks, required("name", *in.Name, 120))
	}
	if in.Description != nil {
		checks = append(checks, maxLen("description", *in.Description, 8000))
	}
	if in.Status != nil {
		checks = append(checks, oneOf("status", *in.Status, "active", "paused", "archived"))
	}
	if in.Policy != nil {
		checks = append(checks, in.Policy.Validate())
	}
	if !validate(w, checks...) {
		return
	}
	p, err := s.store.UpdateProject(r.Context(), id, store.ProjectPatch{
		Name: in.Name, Description: in.Description, Status: in.Status,
		SetLLMProfile: in.ClearProfile || in.LLMProfileID != nil, LLMProfileID: in.LLMProfileID,
		Policy: in.Policy,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) listTargets(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ts, err := s.store.ListTargets(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ts)
}

// createTarget registers a source. Local snapshots and lab fixtures are
// resolved under REMNANT_DATA_DIR/snapshots and pinned to their tree digest
// immediately. Git repositories are validated against the host allowlist and
// stored as pending until a fetcher pins a commit.
func (s *Server) createTarget(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Kind            string `json:"kind"`
		DisplayName     string `json:"display_name"`
		SourceURI       string `json:"source_uri"`
		SourceRef       string `json:"source_ref"`
		SnapshotKey     string `json:"snapshot_key"`
		PrimaryLanguage string `json:"primary_language"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validate(w,
		oneOf("kind", in.Kind, "git_repository", "local_snapshot", "lab_fixture"),
		required("display_name", in.DisplayName, 200),
		source.ValidateSnapshotKey(in.SnapshotKey),
		maxLen("source_ref", in.SourceRef, 255),
		maxLen("primary_language", in.PrimaryLanguage, 40),
	) {
		return
	}
	t := store.TargetInput{
		Kind: in.Kind, DisplayName: in.DisplayName, SourceRef: in.SourceRef,
		SnapshotKey: in.SnapshotKey, PrimaryLanguage: in.PrimaryLanguage, Status: "pending",
	}
	if in.Kind == "git_repository" {
		if err := fetch.ValidateRef(in.SourceRef); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		uri, err := source.CanonicalRepoURL(in.SourceURI, s.cfg.RepoHosts)
		if err != nil {
			writeError(w, http.StatusForbidden, "source_policy", err.Error())
			return
		}
		t.SourceURI = uri
		// Fetched snapshots live under one directory per key for the whole
		// instance; a key in use there would only fail later, at fetch time.
		if _, err := os.Lstat(filepath.Join(s.cfg.SnapshotDir(), in.SnapshotKey)); err == nil {
			writeError(w, http.StatusConflict, "conflict", fmt.Sprintf("snapshot key %q is already used on disk; choose another", in.SnapshotKey))
			return
		}
	} else {
		if in.SourceURI != "" {
			writeError(w, http.StatusBadRequest, "bad_request", "source_uri is only valid for git_repository targets")
			return
		}
		rev, digest, err := source.LocalSnapshot(s.cfg.SnapshotDir(), in.SnapshotKey)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "snapshot_unavailable", s.snapshotProblem(err))
			return
		}
		t.Revision, t.TreeSHA256, t.Status = rev, digest, "ready"
	}
	target, err := s.store.CreateTarget(r.Context(), projectID, t)
	if errors.Is(err, store.ErrConflict) && strings.Contains(err.Error(), "snapshot_key") {
		writeError(w, http.StatusConflict, "conflict", fmt.Sprintf("this project already has a target with snapshot key %q", in.SnapshotKey))
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, target)
}

// snapshotProblem describes a local snapshot error without the server's
// absolute data path.
func (s *Server) snapshotProblem(err error) string {
	return strings.ReplaceAll(err.Error(), s.cfg.SnapshotDir(), "$REMNANT_DATA_DIR/snapshots")
}
