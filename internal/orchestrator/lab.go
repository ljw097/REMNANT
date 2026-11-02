package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ljw097/remnant/internal/policy"
	"github.com/ljw097/remnant/internal/research"
	"github.com/ljw097/remnant/internal/store"
	"github.com/ljw097/remnant/internal/worker"
)

var staticLimits = worker.Limits{WallSeconds: 600, MemoryMB: 4096, CPUCores: 2, MaxOutputBytes: 50 << 20, MaxProcesses: 64}

const labMaxOutputBytes = 50 << 20

// static runs the configured analyzer operations on every target. Analyzer
// failures are recorded on the experiment; they do not fail the run.
func (j *job) static(ctx context.Context, scope store.WorkScope) error {
	if j.o.opts.Worker == nil {
		j.event(ctx, "stage.skipped", "No worker runtime configured; static analyzers did not run", nil)
		return nil
	}
	for _, t := range scope.Targets {
		for _, op := range j.o.opts.StaticOperations {
			inputs := map[string]any{"language": t.PrimaryLanguage}
			if _, err := j.runWorker(ctx, t.ID, t.Revision, op, inputs, nil, nil, staticLimits); err != nil {
				return err
			}
		}
	}
	return nil
}

// lab executes every approved, not yet executed lab experiment of the run,
// re-checking each approval's scope immediately before execution.
func (j *job) lab(ctx context.Context) error {
	if err := j.advance(ctx, research.RunLabValidation, "Running approved lab experiments"); err != nil {
		return err
	}
	approvals, err := j.o.store.ApprovedLabWork(ctx, j.runID)
	if err != nil {
		return err
	}
	for _, a := range approvals {
		if err := j.labExperiment(ctx, a); err != nil {
			return err
		}
	}
	return j.advance(ctx, research.RunReviewRequired, fmt.Sprintf("%d lab experiment(s) processed; results await review", len(approvals)))
}

func (j *job) labExperiment(ctx context.Context, a store.Approval) error {
	fresh, err := j.o.store.ApprovalStillValid(ctx, a.ID)
	if err != nil {
		return err
	}
	if !fresh {
		return j.o.store.SetHypothesisStatus(ctx, a.HypothesisID, research.HypothesisValidationPending, research.HypothesisAccepted)
	}
	var scope policy.LabScope
	if err := json.Unmarshal(a.Scope, &scope); err != nil {
		return err
	}
	hyp, err := j.o.store.GetHypothesis(ctx, a.HypothesisID)
	if err != nil {
		return err
	}
	inputs := map[string]any{
		"hypothesis_id":        hyp.ID,
		"affected_symbols":     hyp.AffectedSymbols,
		"preconditions":        hyp.Preconditions,
		"expected_observation": hyp.ExpectedObservation,
	}
	limits := worker.Limits{
		WallSeconds: scope.Limits.WallSeconds, MemoryMB: scope.Limits.MemoryMB, CPUCores: scope.Limits.CPUCores,
		MaxProcesses: scope.Limits.MaxProcesses, MaxOutputBytes: labMaxOutputBytes,
	}
	hypID, approvalID := a.HypothesisID, a.ID
	status, err := j.runWorker(ctx, a.TargetID, a.TargetRevision, scope.Operation, inputs, &hypID, &approvalID, limits)
	if err != nil {
		return err
	}
	if status == "not_run" {
		// Nothing executed: give the hypothesis back so a new request can be made.
		return j.o.store.SetHypothesisStatus(ctx, a.HypothesisID, research.HypothesisValidationPending, research.HypothesisAccepted)
	}
	return nil
}

// runWorker executes one allowlisted operation and records it as an
// experiment. It returns the experiment status, or "not_run" when no worker
// runtime is configured. Only bookkeeping failures are returned as errors.
func (j *job) runWorker(ctx context.Context, targetID int64, revision, op string, inputs map[string]any,
	hypothesisID, approvalID *int64, limits worker.Limits) (string, error) {
	req := worker.Request{
		Protocol: worker.Protocol, Message: "request",
		RequestID: fmt.Sprintf("req_%d_%s", j.runID, randomHex(8)),
		RunID:     j.runID, Operation: op,
		Workspace: worker.Workspace{MountID: fmt.Sprintf("target_%d", targetID), RelativeRoot: ".", Revision: revision},
		Inputs:    inputs, Limits: limits, Policy: worker.DefaultPolicy(),
	}
	if approvalID != nil {
		req.ApprovalID = *approvalID
	}
	if err := worker.ValidateRequest(req); err != nil {
		return "", err
	}
	expID, err := j.o.store.StartExperiment(ctx, j.runID, store.ExperimentInput{
		HypothesisID: hypothesisID, ApprovalID: approvalID, TargetID: targetID, Operation: op,
		RequestID: req.RequestID, Revision: revision,
		Config: map[string]any{"inputs": inputs, "limits": limits, "policy": req.Policy},
	})
	if err != nil {
		return "", err
	}

	// Recording the outcome must survive cancellation of the work context.
	finish := func(status, summary string, exit *int, ev []store.EvidenceInput, arts []store.ArtifactInput) (string, error) {
		bg, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer done()
		return status, j.o.store.FinishExperiment(bg, j.runID, expID, status, summary, exit, ev, arts)
	}

	if j.o.opts.Worker == nil {
		if _, err := finish("failed", "not run: no worker runtime configured", nil, nil, nil); err != nil {
			return "", err
		}
		return "not_run", nil
	}

	wctx, cancel := context.WithTimeout(ctx, time.Duration(limits.WallSeconds)*time.Second+30*time.Second)
	defer cancel()
	res, err := j.o.opts.Worker.Execute(wctx, req, func(p worker.Progress) {})
	var werr *worker.Error
	switch {
	case ctx.Err() != nil:
		_ = j.o.opts.Worker.Cancel(req.RequestID)
		if _, ferr := finish("cancelled", "run stopped while the experiment was running", nil, nil, nil); ferr != nil {
			return "", ferr
		}
		return "", stageErr(ctx, ctx.Err())
	case errors.As(err, &werr):
		status := "failed"
		if werr.Code == "RESOURCE_LIMIT" {
			status = "resource_limited"
		}
		return finish(status, werr.Code+": "+werr.Summary, nil, nil, nil)
	case err != nil:
		return finish("failed", "worker error: "+err.Error(), nil, nil, nil)
	}
	if err := worker.ValidateResult(req, res); err != nil {
		return finish("failed", "worker result rejected: "+err.Error(), nil, nil, nil)
	}

	status := map[string]string{"completed": "passed", "finding": "finding", "failed": "failed"}[res.Status]
	kind := "static_finding"
	if approvalID != nil {
		kind = "runtime_observation"
	}
	var ev []store.EvidenceInput
	for _, o := range res.Observations {
		ev = append(ev, store.EvidenceInput{
			TargetID: targetID, Kind: kind, FilePath: o.FilePath, LineStart: o.LineStart, LineEnd: o.LineEnd,
			Revision: revision, Summary: o.Message,
			Metadata: map[string]any{"operation": op, "rule_id": o.RuleID, "observation_kind": o.Kind, "tool_confidence": o.ToolConfidence},
		})
	}
	var arts []store.ArtifactInput
	for _, a := range res.Artifacts {
		artKind := "analyzer_output"
		if approvalID != nil {
			artKind = "test_log"
		}
		arts = append(arts, store.ArtifactInput{Kind: artKind, Name: a.Name, StorageKey: "worker/" + req.RequestID + "/" + a.Name,
			SHA256: a.SHA256, SizeBytes: a.SizeBytes, MediaType: a.MediaType})
	}
	exit := res.ExitCode
	return finish(status, res.Summary, &exit, ev, arts)
}
