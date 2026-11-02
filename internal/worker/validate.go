package worker

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

var (
	sha256Hex   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	artifactRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	confidences = map[string]bool{"": true, "low": true, "medium": true, "high": true}
	resultState = map[string]bool{"completed": true, "finding": true, "failed": true}
)

// ValidateRequest checks a request before it is handed to a worker.
func ValidateRequest(req Request) error {
	if req.Protocol != Protocol || req.Message != "request" {
		return errors.New("worker: request must use protocol " + Protocol)
	}
	op, ok := Operations[req.Operation]
	if !ok {
		return fmt.Errorf("worker: operation %q is not allowlisted", req.Operation)
	}
	if op.RequiresApproval && req.ApprovalID == 0 {
		return fmt.Errorf("worker: operation %q requires an approval", req.Operation)
	}
	if req.Policy != DefaultPolicy() {
		return errors.New("worker: request policy must be the fixed read-only/no-network policy")
	}
	if req.Workspace.Revision == "" || req.Workspace.MountID == "" {
		return errors.New("worker: workspace revision and mount_id are required")
	}
	if err := ValidateRelPath(req.Workspace.RelativeRoot); err != nil {
		return fmt.Errorf("worker: relative_root: %w", err)
	}
	l := req.Limits
	if l.WallSeconds <= 0 || l.MemoryMB <= 0 || l.CPUCores <= 0 || l.MaxOutputBytes <= 0 || l.MaxProcesses <= 0 {
		return errors.New("worker: every limit must be positive")
	}
	return nil
}

// ValidateResult checks a worker result against the request that produced
// it. Nothing a worker returns is trusted before this passes.
func ValidateResult(req Request, res Result) error {
	if res.Protocol != Protocol || res.Message != "result" {
		return errors.New("worker: result has wrong protocol or message type")
	}
	if res.RequestID != req.RequestID {
		return fmt.Errorf("worker: result for %q does not match request %q", res.RequestID, req.RequestID)
	}
	if !resultState[res.Status] {
		return fmt.Errorf("worker: invalid status %q", res.Status)
	}
	for i, o := range res.Observations {
		if o.Kind == "" || o.Message == "" {
			return fmt.Errorf("worker: observation %d: kind and message are required", i)
		}
		if !confidences[o.ToolConfidence] {
			return fmt.Errorf("worker: observation %d: invalid tool_confidence %q", i, o.ToolConfidence)
		}
		if o.FilePath != "" {
			if err := ValidateRelPath(o.FilePath); err != nil {
				return fmt.Errorf("worker: observation %d: %w", i, err)
			}
		}
		if o.LineStart < 0 || o.LineEnd < 0 || (o.LineEnd > 0 && o.LineEnd < o.LineStart) {
			return fmt.Errorf("worker: observation %d: invalid line range %d-%d", i, o.LineStart, o.LineEnd)
		}
	}
	var total int64
	for i, a := range res.Artifacts {
		if !artifactRe.MatchString(a.Name) {
			return fmt.Errorf("worker: artifact %d: invalid name %q", i, a.Name)
		}
		if !sha256Hex.MatchString(a.SHA256) {
			return fmt.Errorf("worker: artifact %d: sha256 must be 64 lowercase hex chars", i)
		}
		if a.SizeBytes < 0 {
			return fmt.Errorf("worker: artifact %d: negative size", i)
		}
		total += a.SizeBytes
	}
	if total > req.Limits.MaxOutputBytes {
		return fmt.Errorf("worker: artifacts total %d bytes, limit %d", total, req.Limits.MaxOutputBytes)
	}
	return nil
}

// ValidateRelPath accepts clean, relative, slash-separated paths that stay
// inside the workspace root.
func ValidateRelPath(p string) error {
	switch {
	case p == "":
		return errors.New("empty path")
	case strings.ContainsAny(p, "\\\x00"):
		return fmt.Errorf("path %q contains a forbidden character", p)
	case path.IsAbs(p):
		return fmt.Errorf("path %q is absolute", p)
	case path.Clean(p) != p:
		return fmt.Errorf("path %q is not clean", p)
	case p == ".." || strings.HasPrefix(p, "../"):
		return fmt.Errorf("path %q escapes the workspace", p)
	}
	return nil
}
