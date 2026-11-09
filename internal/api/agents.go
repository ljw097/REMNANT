package api

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/ljw097/remnant/internal/llm"
	"github.com/ljw097/remnant/internal/store"
	"github.com/ljw097/remnant/internal/worker"
)

var efforts = []string{"low", "medium", "high", "xhigh", "max"}

func checkBaseURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return badRequest("base_url must be an http(s) URL without credentials")
	}
	return nil
}

func checkOptions(o *llm.Options) error {
	if o == nil {
		return nil
	}
	if err := o.Validate(); err != nil {
		return badRequest(err.Error())
	}
	return nil
}

// updateLLMProfile edits a profile. A non-empty api_key replaces the stored
// key; the old one cannot be recovered.
func (s *Server) updateLLMProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Name    *string      `json:"name"`
		BaseURL *string      `json:"base_url"`
		Model   *string      `json:"model"`
		Effort  *string      `json:"effort"`
		APIKey  *string      `json:"api_key"`
		Options *llm.Options `json:"options"`
	}
	if !decode(w, r, &in) {
		return
	}
	var checks []error
	if in.Name != nil {
		checks = append(checks, required("name", *in.Name, 120))
	}
	if in.Model != nil {
		checks = append(checks, required("model", *in.Model, 200))
	}
	if in.Effort != nil {
		checks = append(checks, oneOf("effort", *in.Effort, efforts...))
	}
	if in.BaseURL != nil {
		checks = append(checks, maxLen("base_url", *in.BaseURL, 2048), checkBaseURL(*in.BaseURL))
	}
	if in.APIKey != nil && *in.APIKey == "" {
		in.APIKey = nil // keep the stored key
	}
	if in.APIKey != nil {
		checks = append(checks, required("api_key", *in.APIKey, 4096))
	}
	checks = append(checks, checkOptions(in.Options))
	if !validate(w, checks...) {
		return
	}
	p, err := s.store.UpdateLLMProfile(r.Context(), id, store.LLMProfilePatch{
		Name: in.Name, BaseURL: in.BaseURL, Model: in.Model, Effort: in.Effort, APIKey: in.APIKey, Options: in.Options,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if in.APIKey != nil {
		s.log.Info("model profile key replaced", "profile", p.ID, "by", principal(r).Username)
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteLLMProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteLLMProfile(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("model profile deleted", "profile", id, "by", principal(r).Username)
	w.WriteHeader(http.StatusNoContent)
}

// testLLMProfile checks a stored profile's key, endpoint and model by
// looking the model up at the provider. It spends no tokens.
func (s *Server) testLLMProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	creds, err := s.store.LLMCredentials(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.checkProfile(w, r, creds)
}

// testUnsavedProfile checks settings before they are saved, so a wrong key
// is caught at registration.
func (s *Server) testUnsavedProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Provider string       `json:"provider"`
		BaseURL  string       `json:"base_url"`
		Model    string       `json:"model"`
		APIKey   string       `json:"api_key"`
		Options  *llm.Options `json:"options"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Provider == "" {
		in.Provider = "anthropic"
	}
	if !validate(w, oneOf("provider", in.Provider, "anthropic", "openai"), required("model", in.Model, 200),
		required("api_key", in.APIKey, 4096), maxLen("base_url", in.BaseURL, 2048), checkBaseURL(in.BaseURL), checkOptions(in.Options)) {
		return
	}
	p := llm.Profile{Provider: in.Provider, BaseURL: in.BaseURL, Model: in.Model, APIKey: in.APIKey}
	if in.Options != nil {
		p.Options = *in.Options
	}
	s.checkProfile(w, r, p)
}

func (s *Server) checkProfile(w http.ResponseWriter, r *http.Request, p llm.Profile) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	zero := 0
	p.Options.MaxRetries = &zero // a check should answer quickly
	name, err := llm.Check(ctx, p)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "model": name})
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	as, err := s.store.ListAgents(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, as)
}

// updateAgent turns an agent on or off, gives it its own model profile
// (llm_profile_id 0 returns it to the run's profile) and sets its extra
// instructions and per-call token cap.
func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	role := r.PathValue("role")
	if role != "historian" && role != "hypothesis_researcher" && role != "skeptic" && role != "reporter" {
		writeError(w, http.StatusNotFound, "not_found", "unknown agent")
		return
	}
	var in struct {
		Enabled      *bool              `json:"enabled"`
		LLMProfileID *int64             `json:"llm_profile_id"`
		Config       *store.AgentConfig `json:"config"`
	}
	if !decode(w, r, &in) {
		return
	}
	var cfgErr error
	if c := in.Config; c != nil && (len(c.Instructions) > 4000 || (c.MaxCallTokens != 0 && (c.MaxCallTokens < 2048 || c.MaxCallTokens > 32000))) {
		cfgErr = badRequest("instructions may be at most 4000 bytes; max_call_tokens must be 0 or 2048–32000")
	}
	if !validate(w, cfgErr) {
		return
	}
	patch := store.AgentPatch{Enabled: in.Enabled, Config: in.Config}
	if in.LLMProfileID != nil {
		if *in.LLMProfileID == 0 {
			patch.ClearProfile = true
		} else {
			if _, err := s.store.GetLLMProfile(r.Context(), *in.LLMProfileID); err != nil {
				s.fail(w, r, err)
				return
			}
			patch.LLMProfileID = in.LLMProfileID
		}
	}
	a, err := s.store.UpdateAgent(r.Context(), role, patch)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("agent changed", "role", role, "enabled", a.Enabled, "by", principal(r).Username)
	writeJSON(w, http.StatusOK, a)
}

// getResearchSettings is readable by everyone: the new-run form shows the
// defaults.
func (s *Server) getResearchSettings(w http.ResponseWriter, r *http.Request) {
	rs, err := s.store.ResearchSettings(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"settings":          rs,
		"defaults":          store.DefaultResearchSettings(),
		"static_operations": s.cfg.StaticOperations,
	})
}

func (s *Server) setResearchSettings(w http.ResponseWriter, r *http.Request) {
	var in store.ResearchSettings
	if !decode(w, r, &in) {
		return
	}
	for _, op := range in.DisabledStaticOperations {
		if !slices.Contains(s.cfg.StaticOperations, op) {
			writeError(w, http.StatusBadRequest, "bad_request", "disabled_static_operations: "+op+" is not configured on this server")
			return
		}
	}
	rs, err := s.store.SetResearchSettings(r.Context(), in, principal(r).UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("research settings changed", "by", principal(r).Username)
	writeJSON(w, http.StatusOK, map[string]any{
		"settings":          rs,
		"defaults":          store.DefaultResearchSettings(),
		"static_operations": s.cfg.StaticOperations,
	})
}

// systemConfig shows the settings that come from remnantd's environment.
// They change only with a restart; nothing secret is included.
func (s *Server) systemConfig(w http.ResponseWriter, r *http.Request) {
	c := s.cfg
	keySource := "file"
	if c.MasterKeyFromEnv {
		keySource = "environment"
	}
	var lab []string
	for op, spec := range worker.Operations {
		if spec.RequiresApproval {
			lab = append(lab, op)
		}
	}
	slices.Sort(lab)
	writeJSON(w, http.StatusOK, map[string]any{
		"listen":                   c.ListenAddr,
		"data_dir":                 c.DataDir,
		"master_key_source":        keySource,
		"orchestrator":             c.Orchestrator,
		"orchestrator_concurrency": c.OrchestratorConcurrency,
		"worker":                   c.Worker,
		"worker_rules":             c.WorkerRules != "",
		"static_operations":        c.StaticOperations,
		"lab_operations":           lab,
		"repo_hosts":               c.RepoHosts,
		"advisory_feed":            c.AdvisoryFeed,
		"auto_update":              c.AutoUpdate,
		"update_interval":          c.UpdateInterval.String(),
		"update_repo":              c.UpdateRepo,
	})
}

// listUnsavedModels lists the models a key can use before a profile is
// saved, so the model can be picked instead of typed. No tokens are spent.
func (s *Server) listUnsavedModels(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Provider string `json:"provider"`
		BaseURL  string `json:"base_url"`
		APIKey   string `json:"api_key"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Provider == "" {
		in.Provider = "anthropic"
	}
	if !validate(w, oneOf("provider", in.Provider, "anthropic", "openai"), required("api_key", in.APIKey, 4096),
		maxLen("base_url", in.BaseURL, 2048), checkBaseURL(in.BaseURL)) {
		return
	}
	s.listModels(w, r, llm.Profile{Provider: in.Provider, BaseURL: in.BaseURL, APIKey: in.APIKey})
}

// listProfileModels lists the models a saved profile's key can use.
func (s *Server) listProfileModels(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	creds, err := s.store.LLMCredentials(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.listModels(w, r, creds)
}

func (s *Server) listModels(w http.ResponseWriter, r *http.Request, p llm.Profile) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	zero := 0
	p.Options.MaxRetries = &zero
	models, err := llm.ListModels(ctx, p)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "models": []llm.ModelChoice{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "models": models})
}
