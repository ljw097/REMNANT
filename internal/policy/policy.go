// Package policy defines the project safety policy and the scope hashes that
// bind runs and lab approvals to exact revisions and limits.
package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

const (
	SourceReadOnly  = "read_only"
	DynamicDisabled = "disabled"
	DynamicLocalLab = "local_lab_only"
	NetworkNone     = "none"
	ActionLocalLab  = "local_lab_validation"
)

// Policy is the per-project safety boundary. Source access is always
// read-only and workers never get network access; only dynamic testing is
// configurable, and it is disabled by default.
type Policy struct {
	SourceMode  string `json:"source_mode"`
	DynamicMode string `json:"dynamic_mode"`
	Network     string `json:"network"`
}

func Default() Policy {
	return Policy{SourceMode: SourceReadOnly, DynamicMode: DynamicDisabled, Network: NetworkNone}
}

func (p Policy) Validate() error {
	if p.SourceMode != SourceReadOnly {
		return fmt.Errorf("source_mode must be %q", SourceReadOnly)
	}
	if p.DynamicMode != DynamicDisabled && p.DynamicMode != DynamicLocalLab {
		return fmt.Errorf("dynamic_mode must be %q or %q", DynamicDisabled, DynamicLocalLab)
	}
	if p.Network != NetworkNone {
		return fmt.Errorf("network must be %q", NetworkNone)
	}
	return nil
}

// TargetRevision pins one target to an immutable revision: a Git commit SHA
// or "sha256:<tree digest>" for a local snapshot.
type TargetRevision struct {
	TargetID int64  `json:"target_id"`
	Revision string `json:"revision"`
}

// RunScope is everything a run is allowed to touch. Its hash is stored on the
// run and folded into every lab approval created for it.
type RunScope struct {
	Policy   Policy           `json:"policy"`
	Targets  []TargetRevision `json:"targets"`
	Patterns []int64          `json:"patterns"`
	Limits   any              `json:"limits"`
}

func (s RunScope) Hash() (string, error) {
	targets := append([]TargetRevision(nil), s.Targets...)
	sort.Slice(targets, func(i, j int) bool { return targets[i].TargetID < targets[j].TargetID })
	patterns := append([]int64(nil), s.Patterns...)
	sort.Slice(patterns, func(i, j int) bool { return patterns[i] < patterns[j] })
	s.Targets, s.Patterns = targets, patterns
	return hash(s)
}

// LabLimits are the resource ceilings shown to the approver.
type LabLimits struct {
	WallSeconds  int `json:"wall_seconds"`
	MemoryMB     int `json:"memory_mb"`
	CPUCores     int `json:"cpu_cores"`
	MaxProcesses int `json:"max_processes"`
}

func DefaultLabLimits() LabLimits {
	return LabLimits{WallSeconds: 300, MemoryMB: 2048, CPUCores: 2, MaxProcesses: 32}
}

// LabScope is what a human approves for one local-lab experiment. Any change
// to the run scope, target revision or limits produces a different hash and
// invalidates the approval.
type LabScope struct {
	ActionClass  string    `json:"action_class"`
	Operation    string    `json:"operation"`
	RunScopeHash string    `json:"run_scope_hash"`
	HypothesisID int64     `json:"hypothesis_id"`
	TargetID     int64     `json:"target_id"`
	Revision     string    `json:"revision"`
	Network      string    `json:"network"`
	Limits       LabLimits `json:"limits"`
}

func (s LabScope) Hash() (string, error) { return hash(s) }

// hash is SHA-256 over encoding/json output, which is deterministic for
// structs (declaration order) and maps (sorted keys).
func hash(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
