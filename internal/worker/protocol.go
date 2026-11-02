// Package worker defines the versioned JSONL protocol between remnantd and
// the isolated analysis worker, and validates everything a worker returns.
package worker

import (
	"context"
	"encoding/json"
	"fmt"
)

const Protocol = "remnant.worker.v1"

// Operations is the hard-coded allowlist. The caller names an operation; it
// never supplies an executable, command line, environment or mount path.
var Operations = map[string]struct{ RequiresApproval bool }{
	"index.symbols":      {},
	"static.patterns":    {},
	"static.semgrep":     {},
	"static.codeql":      {},
	"static.tree_sitter": {},
	"lab.unit_test":      {RequiresApproval: true},
	"lab.asan_test":      {RequiresApproval: true},
	"lab.ubsan_test":     {RequiresApproval: true},
	"lab.libfuzzer":      {RequiresApproval: true},
}

type Workspace struct {
	MountID      string `json:"mount_id"`
	RelativeRoot string `json:"relative_root"`
	Revision     string `json:"revision"`
}

type Limits struct {
	WallSeconds    int   `json:"wall_seconds"`
	MemoryMB       int   `json:"memory_mb"`
	CPUCores       int   `json:"cpu_cores"`
	MaxOutputBytes int64 `json:"max_output_bytes"`
	MaxProcesses   int   `json:"max_processes"`
}

type Policy struct {
	Filesystem string `json:"filesystem"` // workspace_read_only_output_write_only
	Network    string `json:"network"`    // none
	Subprocess string `json:"subprocess"` // allowlisted_tool_only
}

func DefaultPolicy() Policy {
	return Policy{Filesystem: "workspace_read_only_output_write_only", Network: "none", Subprocess: "allowlisted_tool_only"}
}

type Request struct {
	Protocol   string         `json:"protocol"`
	Message    string         `json:"message"` // "request"
	RequestID  string         `json:"request_id"`
	RunID      int64          `json:"run_id"`
	Operation  string         `json:"operation"`
	ApprovalID int64          `json:"approval_id,omitempty"`
	Workspace  Workspace      `json:"workspace"`
	Inputs     map[string]any `json:"inputs"`
	Limits     Limits         `json:"limits"`
	Policy     Policy         `json:"policy"`
}

type Progress struct {
	Protocol  string `json:"protocol"`
	Message   string `json:"message"` // "progress"
	RequestID string `json:"request_id"`
	Stage     string `json:"stage"`
	Percent   int    `json:"percent"`
	Summary   string `json:"summary"`
}

type Observation struct {
	Kind           string `json:"kind"`
	RuleID         string `json:"rule_id,omitempty"`
	FilePath       string `json:"file_path,omitempty"`
	LineStart      int    `json:"line_start,omitempty"`
	LineEnd        int    `json:"line_end,omitempty"`
	Message        string `json:"message"`
	ToolConfidence string `json:"tool_confidence,omitempty"`
}

type Artifact struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

type Result struct {
	Protocol     string        `json:"protocol"`
	Message      string        `json:"message"` // "result"
	RequestID    string        `json:"request_id"`
	Status       string        `json:"status"`
	ExitCode     int           `json:"exit_code"`
	Summary      string        `json:"summary"`
	Observations []Observation `json:"observations"`
	Artifacts    []Artifact    `json:"artifacts,omitempty"`
}

type Error struct {
	Protocol  string `json:"protocol"`
	Message   string `json:"message"` // "error"
	RequestID string `json:"request_id"`
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
	Summary   string `json:"summary"`
}

func (e *Error) Error() string { return fmt.Sprintf("worker %s: %s", e.Code, e.Summary) }

// Worker executes one request in an isolated sandbox. Implementations own
// process/container creation, limit enforcement and cancellation.
type Worker interface {
	Execute(ctx context.Context, req Request, onProgress func(Progress)) (Result, error)
	Cancel(requestID string) error
}

// ParseLine decodes one stdout line into *Progress, *Result or *Error.
func ParseLine(line []byte) (any, error) {
	var head struct {
		Protocol string `json:"protocol"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		return nil, fmt.Errorf("worker: malformed line: %w", err)
	}
	if head.Protocol != Protocol {
		return nil, fmt.Errorf("worker: unsupported protocol %q", head.Protocol)
	}
	var v any
	switch head.Message {
	case "progress":
		v = &Progress{}
	case "result":
		v = &Result{}
	case "error":
		v = &Error{}
	default:
		return nil, fmt.Errorf("worker: unexpected message %q", head.Message)
	}
	if err := json.Unmarshal(line, v); err != nil {
		return nil, fmt.Errorf("worker: decode %s: %w", head.Message, err)
	}
	return v, nil
}
