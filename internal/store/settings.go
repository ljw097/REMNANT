package store

import (
	"context"
	"fmt"
	"slices"

	"github.com/ljw097/remnant/internal/policy"
	"github.com/ljw097/remnant/internal/research"
)

// ResearchSettings are the expert settings an administrator can change at
// runtime. They apply to runs and requests started afterwards.
type ResearchSettings struct {
	// RunDefaults fill the limits a new run leaves out.
	RunDefaults research.Limits `json:"run_defaults"`
	// CandidatesPerPattern is how many source regions surface mapping keeps
	// per pattern and target; MaxCandidates caps them per run.
	CandidatesPerPattern int `json:"candidates_per_pattern"`
	MaxCandidates        int `json:"max_candidates"`
	// MaxCallTokens caps max_tokens (thinking included) for one agent call.
	MaxCallTokens int `json:"max_call_tokens"`
	// LabDefaults fill the limits a lab approval request leaves out.
	LabDefaults policy.LabLimits `json:"lab_defaults"`
	// ApprovalTTLHours is how long a lab approval request stays open.
	ApprovalTTLHours int `json:"approval_ttl_hours"`
	// DisabledStaticOperations are configured analyzers that do not run.
	DisabledStaticOperations []string `json:"disabled_static_operations"`
}

// DefaultResearchSettings are the values used before an administrator
// changes anything.
func DefaultResearchSettings() ResearchSettings {
	return ResearchSettings{
		RunDefaults:              research.Limits{MaxWorkers: 2, WallSeconds: 900, MaxTokens: 200000},
		CandidatesPerPattern:     3,
		MaxCandidates:            24,
		MaxCallTokens:            16000,
		LabDefaults:              policy.DefaultLabLimits(),
		ApprovalTTLHours:         24,
		DisabledStaticOperations: []string{},
	}
}

// Validate checks every field's range.
func (r ResearchSettings) Validate() error {
	if _, err := r.RunDefaults.Normalize(); err != nil || r.RunDefaults.MaxWorkers == 0 || r.RunDefaults.WallSeconds == 0 || r.RunDefaults.MaxTokens == 0 {
		if err == nil {
			err = fmt.Errorf("every run default must be set")
		}
		return fmt.Errorf("%w: run_defaults: %v", ErrInvalid, err)
	}
	switch {
	case r.CandidatesPerPattern < 1 || r.CandidatesPerPattern > 10:
		return fmt.Errorf("%w: candidates_per_pattern must be 1–10", ErrInvalid)
	case r.MaxCandidates < 1 || r.MaxCandidates > 100:
		return fmt.Errorf("%w: max_candidates must be 1–100", ErrInvalid)
	case r.MaxCallTokens < 2048 || r.MaxCallTokens > 32000:
		// Calls are not streamed, so a single call must stay well inside
		// HTTP timeouts.
		return fmt.Errorf("%w: max_call_tokens must be 2048–32000", ErrInvalid)
	case r.ApprovalTTLHours < 1 || r.ApprovalTTLHours > 168:
		return fmt.Errorf("%w: approval_ttl_hours must be 1–168", ErrInvalid)
	}
	if err := r.LabDefaults.Validate(); err != nil {
		return fmt.Errorf("%w: lab_defaults: %v", ErrInvalid, err)
	}
	return nil
}

// StaticEnabled filters the server's configured analyzers by the settings.
func (r ResearchSettings) StaticEnabled(configured []string) []string {
	var out []string
	for _, op := range configured {
		if !slices.Contains(r.DisabledStaticOperations, op) {
			out = append(out, op)
		}
	}
	return out
}

const researchKey = "research"

// ResearchSettings returns the stored settings over the defaults; a field
// that was never set, or a stored value that no longer validates, falls
// back to its default.
func (s *Store) ResearchSettings(ctx context.Context) (ResearchSettings, error) {
	r := DefaultResearchSettings()
	if _, err := s.Setting(ctx, researchKey, &r); err != nil {
		return DefaultResearchSettings(), err
	}
	if r.Validate() != nil {
		return DefaultResearchSettings(), nil
	}
	if r.DisabledStaticOperations == nil {
		r.DisabledStaticOperations = []string{}
	}
	return r, nil
}

func (s *Store) SetResearchSettings(ctx context.Context, r ResearchSettings, by int64) (ResearchSettings, error) {
	if err := r.Validate(); err != nil {
		return ResearchSettings{}, err
	}
	if r.DisabledStaticOperations == nil {
		r.DisabledStaticOperations = []string{}
	}
	return r, s.SetSetting(ctx, researchKey, r, by)
}
