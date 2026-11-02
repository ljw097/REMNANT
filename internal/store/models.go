package store

import (
	"encoding/json"
	"time"
)

type User struct {
	ID        int64     `db:"id" json:"id"`
	Username  string    `db:"username" json:"username"`
	Role      string    `db:"role" json:"role"`
	Disabled  bool      `db:"disabled" json:"disabled"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// LLMProfile is the API view of a provider profile; it never carries key
// material.
type LLMProfile struct {
	ID         int64     `db:"id" json:"id"`
	Name       string    `db:"name" json:"name"`
	Provider   string    `db:"provider" json:"provider"`
	BaseURL    string    `db:"base_url" json:"base_url"`
	Model      string    `db:"model" json:"model"`
	Effort     string    `db:"effort" json:"effort"`
	HasAPIKey  bool      `db:"has_api_key" json:"has_api_key"`
	APIKeyHint string    `db:"api_key_hint" json:"api_key_hint"`
	UpdatedAt  time.Time `db:"updated_at" json:"updated_at"`
}

type Project struct {
	ID           int64           `db:"id" json:"id"`
	Name         string          `db:"name" json:"name"`
	Description  string          `db:"description" json:"description"`
	Status       string          `db:"status" json:"status"`
	LLMProfileID *int64          `db:"llm_profile_id" json:"llm_profile_id"`
	Policy       json.RawMessage `db:"policy" json:"policy"`
	TargetCount  int64           `db:"target_count" json:"target_count"`
	ActiveRuns   int64           `db:"active_runs" json:"active_runs"`
	CreatedAt    time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time       `db:"updated_at" json:"updated_at"`
}

type Target struct {
	ID              int64           `db:"id" json:"id"`
	ProjectID       int64           `db:"project_id" json:"project_id"`
	Kind            string          `db:"kind" json:"kind"`
	DisplayName     string          `db:"display_name" json:"display_name"`
	SourceURI       string          `db:"source_uri" json:"source_uri"`
	SourceRef       string          `db:"source_ref" json:"source_ref"`
	SnapshotKey     string          `db:"snapshot_key" json:"snapshot_key"`
	Revision        string          `db:"revision" json:"revision"`
	TreeSHA256      string          `db:"tree_sha256" json:"tree_sha256"`
	PrimaryLanguage string          `db:"primary_language" json:"primary_language"`
	Status          string          `db:"status" json:"status"`
	Metadata        json.RawMessage `db:"metadata" json:"metadata"`
	CreatedAt       time.Time       `db:"created_at" json:"created_at"`
}

type Pattern struct {
	ID                 int64           `db:"id" json:"id"`
	ParentPatternID    *int64          `db:"parent_pattern_id" json:"parent_pattern_id"`
	SourceKind         string          `db:"source_kind" json:"source_kind"`
	SourceRef          string          `db:"source_ref" json:"source_ref"`
	Title              string          `db:"title" json:"title"`
	VulnerabilityClass string          `db:"vulnerability_class" json:"vulnerability_class"`
	CWEID              string          `db:"cwe_id" json:"cwe_id"`
	Language           string          `db:"language" json:"language"`
	RootCause          string          `db:"root_cause" json:"root_cause"`
	Preconditions      json.RawMessage `db:"preconditions" json:"preconditions"`
	LifecyclePoints    json.RawMessage `db:"lifecycle_points" json:"lifecycle_points"`
	CodeSignatures     json.RawMessage `db:"code_signatures" json:"code_signatures"`
	CounterSignatures  json.RawMessage `db:"counter_signatures" json:"counter_signatures"`
	PatchSemantics     json.RawMessage `db:"patch_semantics" json:"patch_semantics"`
	Version            int32           `db:"version" json:"version"`
	ValidationStatus   string          `db:"validation_status" json:"validation_status"`
	CreatedBy          *int64          `db:"created_by" json:"created_by"`
	ReviewedBy         *int64          `db:"reviewed_by" json:"reviewed_by"`
	ReviewedAt         *time.Time      `db:"reviewed_at" json:"reviewed_at"`
	CreatedAt          time.Time       `db:"created_at" json:"created_at"`
}

type Run struct {
	ID           int64           `db:"id" json:"id"`
	ProjectID    int64           `db:"project_id" json:"project_id"`
	Status       string          `db:"status" json:"status"`
	Mode         string          `db:"mode" json:"mode"`
	Limits       json.RawMessage `db:"limits" json:"limits"`
	Policy       json.RawMessage `db:"policy" json:"policy"`
	ScopeHash    string          `db:"scope_hash" json:"scope_hash"`
	LLMProfileID *int64          `db:"llm_profile_id" json:"llm_profile_id"`
	StartedBy    *int64          `db:"started_by" json:"started_by"`
	ErrorSummary string          `db:"error_summary" json:"error_summary"`
	InputTokens  int64           `db:"input_tokens" json:"input_tokens"`
	OutputTokens int64           `db:"output_tokens" json:"output_tokens"`
	QueuedAt     time.Time       `db:"queued_at" json:"queued_at"`
	StartedAt    *time.Time      `db:"started_at" json:"started_at"`
	CompletedAt  *time.Time      `db:"completed_at" json:"completed_at"`
}

type RunTarget struct {
	TargetID int64  `db:"target_id" json:"target_id"`
	Revision string `db:"revision" json:"revision"`
}

// RunDetail is a run with its pinned scope and counters.
type RunDetail struct {
	Run
	Targets         []RunTarget      `json:"targets"`
	PatternIDs      []int64          `json:"pattern_ids"`
	HypothesisCount map[string]int64 `json:"hypothesis_counts"`
}

type Hypothesis struct {
	ID                  int64           `db:"id" json:"id"`
	RunID               int64           `db:"run_id" json:"run_id"`
	TargetID            int64           `db:"target_id" json:"target_id"`
	PatternID           *int64          `db:"pattern_id" json:"pattern_id"`
	Title               string          `db:"title" json:"title"`
	Claim               string          `db:"claim" json:"claim"`
	VulnerabilityClass  string          `db:"vulnerability_class" json:"vulnerability_class"`
	CWEID               string          `db:"cwe_id" json:"cwe_id"`
	Status              string          `db:"status" json:"status"`
	TriageConfidence    string          `db:"triage_confidence" json:"triage_confidence"`
	SkepticVerdict      string          `db:"skeptic_verdict" json:"skeptic_verdict"`
	Facts               json.RawMessage `db:"facts" json:"facts"`
	Preconditions       json.RawMessage `db:"preconditions" json:"preconditions"`
	Unknowns            json.RawMessage `db:"unknowns" json:"unknowns"`
	AffectedSymbols     json.RawMessage `db:"affected_symbols" json:"affected_symbols"`
	ExpectedObservation string          `db:"expected_observation" json:"expected_observation"`
	CounterEvidence     json.RawMessage `db:"counter_evidence" json:"counter_evidence"`
	ReviewerNote        string          `db:"reviewer_note" json:"reviewer_note"`
	ReviewedBy          *int64          `db:"reviewed_by" json:"reviewed_by"`
	ReviewedAt          *time.Time      `db:"reviewed_at" json:"reviewed_at"`
	EvidenceCount       int64           `db:"evidence_count" json:"evidence_count"`
	CreatedAt           time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt           time.Time       `db:"updated_at" json:"updated_at"`
}

type Evidence struct {
	ID            int64           `db:"id" json:"id"`
	HypothesisID  *int64          `db:"hypothesis_id" json:"hypothesis_id"`
	TargetID      int64           `db:"target_id" json:"target_id"`
	EvidenceKind  string          `db:"evidence_kind" json:"evidence_kind"`
	SourceURI     string          `db:"source_uri" json:"source_uri"`
	FilePath      string          `db:"file_path" json:"file_path"`
	LineStart     *int32          `db:"line_start" json:"line_start"`
	LineEnd       *int32          `db:"line_end" json:"line_end"`
	Symbol        string          `db:"symbol" json:"symbol"`
	Revision      string          `db:"revision" json:"revision"`
	ContentSHA256 string          `db:"content_sha256" json:"content_sha256"`
	Summary       string          `db:"summary" json:"summary"`
	Excerpt       string          `db:"excerpt" json:"excerpt"`
	Metadata      json.RawMessage `db:"metadata" json:"metadata"`
	CreatedAt     time.Time       `db:"created_at" json:"created_at"`
}

type HypothesisDetail struct {
	Hypothesis
	Evidence []Evidence `json:"evidence"`
}

type Approval struct {
	ID             int64           `db:"id" json:"id"`
	RunID          int64           `db:"run_id" json:"run_id"`
	HypothesisID   int64           `db:"hypothesis_id" json:"hypothesis_id"`
	TargetID       int64           `db:"target_id" json:"target_id"`
	ActionClass    string          `db:"action_class" json:"action_class"`
	TargetRevision string          `db:"target_revision" json:"target_revision"`
	Scope          json.RawMessage `db:"scope" json:"scope"`
	ScopeHash      string          `db:"scope_hash" json:"scope_hash"`
	RequestSummary string          `db:"request_summary" json:"request_summary"`
	Decision       string          `db:"decision" json:"decision"`
	RequestedBy    int64           `db:"requested_by" json:"requested_by"`
	DecidedBy      *int64          `db:"decided_by" json:"decided_by"`
	ReviewerNote   string          `db:"reviewer_note" json:"reviewer_note"`
	DecidedAt      *time.Time      `db:"decided_at" json:"decided_at"`
	ExpiresAt      time.Time       `db:"expires_at" json:"expires_at"`
	CreatedAt      time.Time       `db:"created_at" json:"created_at"`
}

type Artifact struct {
	ID             int64     `db:"id" json:"id"`
	ExperimentID   *int64    `db:"experiment_id" json:"experiment_id"`
	ArtifactKind   string    `db:"artifact_kind" json:"artifact_kind"`
	Name           string    `db:"name" json:"name"`
	StorageKey     string    `db:"storage_key" json:"-"`
	SHA256         string    `db:"sha256" json:"sha256"`
	SizeBytes      int64     `db:"size_bytes" json:"size_bytes"`
	MediaType      string    `db:"media_type" json:"media_type"`
	Classification string    `db:"classification" json:"classification"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
}

type Event struct {
	ID        int64           `db:"id" json:"id"`
	RunID     int64           `db:"run_id" json:"run_id"`
	EventType string          `db:"event_type" json:"event_type"`
	Stage     string          `db:"stage" json:"stage"`
	Message   string          `db:"message" json:"message"`
	Payload   json.RawMessage `db:"payload" json:"payload"`
	CreatedAt time.Time       `db:"created_at" json:"created_at"`
}

type Experiment struct {
	ID                int64           `db:"id" json:"id"`
	RunID             int64           `db:"run_id" json:"run_id"`
	HypothesisID      *int64          `db:"hypothesis_id" json:"hypothesis_id"`
	ApprovalID        *int64          `db:"approval_id" json:"approval_id"`
	TargetID          *int64          `db:"target_id" json:"target_id"`
	Operation         string          `db:"operation" json:"operation"`
	RequestID         string          `db:"request_id" json:"request_id"`
	Status            string          `db:"status" json:"status"`
	NetworkMode       string          `db:"network_mode" json:"network_mode"`
	WorkspaceRevision string          `db:"workspace_revision" json:"workspace_revision"`
	Config            json.RawMessage `db:"config" json:"config"`
	ResultSummary     string          `db:"result_summary" json:"result_summary"`
	ExitCode          *int32          `db:"exit_code" json:"exit_code"`
	StartedAt         *time.Time      `db:"started_at" json:"started_at"`
	CompletedAt       *time.Time      `db:"completed_at" json:"completed_at"`
	CreatedAt         time.Time       `db:"created_at" json:"created_at"`
}
