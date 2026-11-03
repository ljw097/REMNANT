package api

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/ljw097/remnant/internal/fetch"
	"github.com/ljw097/remnant/internal/source"
	"github.com/ljw097/remnant/internal/store"
)

// treeRe matches the "/tree/<ref>" part of a pasted repository page URL.
var treeRe = regexp.MustCompile(`^(/[^/]+/[^/]+)/(?:-/)?tree/([^?#]+)$`)

// pastedRepo turns what a person pastes — "github.com/org/repo",
// "https://github.com/org/repo.git", ".../tree/v1.2" — into a repository
// URL and an optional ref.
func pastedRepo(raw string) (uri, ref string) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw, ""
	}
	u.RawQuery, u.Fragment = "", ""
	p := strings.TrimSuffix(u.Path, "/")
	if m := treeRe.FindStringSubmatch(p); m != nil {
		p, ref = m[1], strings.TrimSuffix(m[2], "/")
	}
	u.Path = p
	return u.String(), ref
}

// createIntake starts a repository intake from a pasted URL: the
// repository is fetched with history, the Historian drafts patterns from
// its security fixes, and a run hunts for the same mistakes.
func (s *Server) createIntake(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL          string              `json:"url"`
		Ref          string              `json:"ref"`
		ProjectID    *int64              `json:"project_id"`
		ProjectName  string              `json:"project_name"`
		LLMProfileID *int64              `json:"llm_profile_id"`
		Options      store.IntakeOptions `json:"options"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validate(w, required("url", in.URL, 2048), maxLen("ref", in.Ref, 255), maxLen("project_name", in.ProjectName, 120)) {
		return
	}
	if len(s.cfg.RepoHosts) == 0 {
		writeError(w, http.StatusForbidden, "source_policy",
			"remote repositories are off on this server; an administrator enables them with REMNANT_REPO_HOSTS (for example github.com)")
		return
	}
	raw, ref := pastedRepo(in.URL)
	if in.Ref != "" {
		ref = in.Ref
	}
	if err := fetch.ValidateRef(ref); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	uri, err := source.CanonicalRepoURL(raw, s.cfg.RepoHosts)
	if err != nil {
		writeError(w, http.StatusForbidden, "source_policy", err.Error())
		return
	}
	if in.ProjectID == nil && in.ProjectName == "" {
		in.ProjectName = strings.TrimPrefix(uri, "https://")
	}
	if in.LLMProfileID != nil {
		if _, err := s.store.GetLLMProfile(r.Context(), *in.LLMProfileID); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "unknown llm_profile_id")
			return
		}
	} else if in.ProjectID == nil {
		// A new project needs a model; take the only profile if there is one.
		ps, err := s.store.ListLLMProfiles(r.Context())
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if len(ps) != 1 {
			writeError(w, http.StatusBadRequest, "bad_request", "choose a model profile (llm_profile_id)")
			return
		}
		in.LLMProfileID = &ps[0].ID
	}
	intake, err := s.store.CreateIntake(r.Context(), store.IntakeInput{
		SourceURI: uri, SourceRef: ref, ProjectID: in.ProjectID, ProjectName: in.ProjectName,
		LLMProfileID: in.LLMProfileID, Options: in.Options,
	}, principal(r).UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("repository intake started", "intake", intake.ID, "source", uri, "ref", ref, "by", principal(r).Username)
	writeJSON(w, http.StatusAccepted, intake)
}

func (s *Server) listIntakes(w http.ResponseWriter, r *http.Request) {
	is, err := s.store.ListIntakes(r.Context(), 50)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, is)
}

// getIntake returns an intake with the fix candidates found in its history
// (without diffs).
func (s *Server) getIntake(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	in, err := s.store.GetIntake(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	commits, err := s.store.CommitRecords(r.Context(), in.TargetID, 100, false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if commits == nil {
		commits = []store.CommitRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"intake": in, "commits": commits, "repo_hosts": s.cfg.RepoHosts})
}

// intakeConfig tells the form whether remote repositories are enabled.
func (s *Server) intakeConfig(w http.ResponseWriter, r *http.Request) {
	hosts := s.cfg.RepoHosts
	if hosts == nil {
		hosts = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"repo_hosts": hosts, "defaults": store.DefaultIntakeOptions()})
}
