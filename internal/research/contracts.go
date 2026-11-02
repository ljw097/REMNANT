// Package research defines the stable contracts between ARTEX's Go server,
// REMNANT orchestration and the isolated analysis worker.
package research

import (
	"context"
	"time"
)

type RunStatus string

const (
	RunQueued               RunStatus = "queued"
	RunCollecting           RunStatus = "collecting"
	RunIndexing             RunStatus = "indexing"
	RunSurfaceMapping       RunStatus = "surface_mapping"
	RunHypothesisGeneration RunStatus = "hypothesis_generation"
	RunStaticValidation     RunStatus = "static_validation"
	RunReviewRequired       RunStatus = "review_required"
	RunValidationApproved   RunStatus = "validation_approved"
	RunLabValidation        RunStatus = "lab_validation"
	RunReportReady          RunStatus = "report_ready"
	RunCompleted            RunStatus = "completed"
	RunCancelled            RunStatus = "cancelled"
	RunFailed               RunStatus = "failed"
	RunTimedOut             RunStatus = "timed_out"
)

type TargetSnapshot struct {
	TargetID     int64  `json:"target_id"`
	MountID      string `json:"mount_id"`
	RelativeRoot string `json:"relative_root"`
	CommitSHA    string `json:"commit_sha"`
	TreeSHA256   string `json:"tree_sha256,omitempty"`
}

type ResourceLimits struct {
	WallSeconds    int `json:"wall_seconds"`
	MemoryMB       int `json:"memory_mb"`
	CPUCores       int `json:"cpu_cores"`
	MaxOutputBytes int `json:"max_output_bytes"`
	MaxProcesses   int `json:"max_processes"`
}

type WorkerPolicy struct {
	Filesystem string `json:"filesystem"` // workspace_read_only_output_write_only
	Network    string `json:"network"`    // none
	Subprocess string `json:"subprocess"` // allowlisted_tool_only
}

type WorkerRequest struct {
	Protocol  string         `json:"protocol"`
	Message   string         `json:"message"`
	RequestID string         `json:"request_id"`
	RunID     int64          `json:"run_id"`
	Operation string         `json:"operation"`
	Workspace TargetSnapshot `json:"workspace"`
	Inputs    map[string]any `json:"inputs"`
	Limits    ResourceLimits `json:"limits"`
	Policy    WorkerPolicy   `json:"policy"`
}

type WorkerObservation struct {
	Kind           string `json:"kind"`
	RuleID         string `json:"rule_id,omitempty"`
	FilePath       string `json:"file_path,omitempty"`
	LineStart      int    `json:"line_start,omitempty"`
	LineEnd        int    `json:"line_end,omitempty"`
	Message        string `json:"message"`
	ToolConfidence string `json:"tool_confidence,omitempty"`
}

type WorkerArtifact struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

type WorkerResult struct {
	Protocol     string              `json:"protocol"`
	Message      string              `json:"message"`
	RequestID    string              `json:"request_id"`
	Status       string              `json:"status"`
	ExitCode     int                 `json:"exit_code"`
	Summary      string              `json:"summary"`
	Observations []WorkerObservation `json:"observations"`
	Artifacts    []WorkerArtifact    `json:"artifacts,omitempty"`
}

type Worker interface {
	Execute(ctx context.Context, request WorkerRequest) (WorkerResult, error)
	Cancel(requestID string) error
}

type ModelRequest struct {
	ProfileID int64          `json:"profile_id"`
	AgentKey  string         `json:"agent_key"`
	System    string         `json:"system"`
	Input     map[string]any `json:"input"`
	Schema    map[string]any `json:"schema"`
	MaxTokens int            `json:"max_tokens"`
}

type ModelResponse struct {
	JSON         map[string]any `json:"json"`
	InputTokens  int            `json:"input_tokens"`
	OutputTokens int            `json:"output_tokens"`
}

type ModelClient interface {
	CompleteJSON(ctx context.Context, request ModelRequest) (ModelResponse, error)
}

type Approval struct {
	ID           int64
	RunID        int64
	TargetID     int64
	TargetCommit string
	PolicyHash   string
	ActionClass  string
	Decision     string
	ExpiresAt    time.Time
}

type ApprovalStore interface {
	RequireApproval(ctx context.Context, runID, targetID int64, commitSHA, policyHash, summary string) (Approval, error)
	CheckApproval(ctx context.Context, approvalID, runID, targetID int64, commitSHA, policyHash string) (Approval, error)
}

type RunStore interface {
	AppendEvent(ctx context.Context, runID int64, eventType, stage, message string, payload map[string]any) error
	SetStatus(ctx context.Context, runID int64, status RunStatus, stage string) error
}
