// Package research holds the domain rules of a REMNANT run: the run state
// machine, hypothesis triage and run limits.
package research

import (
	"fmt"
	"slices"
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
	RunPaused               RunStatus = "paused"
	RunCompleted            RunStatus = "completed"
	RunCancelled            RunStatus = "cancelled"
	RunFailed               RunStatus = "failed"
	RunTimedOut             RunStatus = "timed_out"
)

// forward lists the non-terminal successor of each stage. Any non-terminal
// status may additionally move to cancelled, failed or timed_out.
var forward = map[RunStatus][]RunStatus{
	RunQueued:               {RunCollecting},
	RunCollecting:           {RunIndexing},
	RunIndexing:             {RunSurfaceMapping},
	RunSurfaceMapping:       {RunHypothesisGeneration},
	RunHypothesisGeneration: {RunStaticValidation},
	RunStaticValidation:     {RunReviewRequired},
	RunReviewRequired:       {RunValidationApproved, RunReportReady},
	RunValidationApproved:   {RunLabValidation},
	RunLabValidation:        {RunReviewRequired, RunReportReady},
	RunReportReady:          {RunCompleted},
	RunPaused:               nil, // see CanTransition
}

// SourceStages are the automatic stages of a source run, in order.
var SourceStages = []RunStatus{RunCollecting, RunIndexing, RunSurfaceMapping, RunHypothesisGeneration, RunStaticValidation}

// Automatic reports whether s is a stage an orchestrator works on while it
// holds the run's lease.
func (s RunStatus) Automatic() bool {
	return s == RunLabValidation || slices.Contains(SourceStages, s)
}

// Pausable reports whether a run in s may be paused: while waiting to be
// claimed or while being worked on.
func (s RunStatus) Pausable() bool {
	return s == RunQueued || s == RunValidationApproved || s == RunReportReady || s.Automatic()
}

// ResumePoint is where a run paused or interrupted in s continues: the
// claimable status it returns to and, for source runs, the stage to
// continue from (empty means from the beginning of that phase).
func ResumePoint(s RunStatus) (status, stage RunStatus) {
	switch {
	case slices.Contains(SourceStages, s):
		return RunQueued, s
	case s == RunLabValidation:
		return RunValidationApproved, ""
	case s.Pausable():
		return s, ""
	}
	return "", ""
}

func (s RunStatus) Terminal() bool {
	switch s {
	case RunCompleted, RunCancelled, RunFailed, RunTimedOut:
		return true
	}
	return false
}

func (s RunStatus) Valid() bool {
	_, ok := forward[s]
	return ok || s.Terminal()
}

// CanTransition reports whether a run may move from one status to another.
// Besides the forward stages: any non-terminal run may end cancelled,
// failed or timed out; a pausable run may be paused; a paused run resumes to
// a claimable status; and an interrupted automatic stage is requeued to its
// resume point.
func CanTransition(from, to RunStatus) bool {
	if from.Terminal() || !to.Valid() {
		return false
	}
	switch {
	case to == RunCancelled || to == RunFailed || to == RunTimedOut:
		return true
	case to == RunPaused:
		return from.Pausable()
	case from == RunPaused:
		return to == RunQueued || to == RunValidationApproved || to == RunReportReady
	case from.Automatic():
		if back, _ := ResumePoint(from); to == back {
			return true
		}
	}
	for _, next := range forward[from] {
		if next == to {
			return true
		}
	}
	return false
}

type HypothesisStatus string

const (
	HypothesisDraft             HypothesisStatus = "draft"
	HypothesisTriage            HypothesisStatus = "triage"
	HypothesisAccepted          HypothesisStatus = "accepted"
	HypothesisRejected          HypothesisStatus = "rejected"
	HypothesisValidationPending HypothesisStatus = "validation_pending"
	HypothesisValidated         HypothesisStatus = "validated"
	HypothesisNotReproducible   HypothesisStatus = "not_reproducible"
	HypothesisDuplicate         HypothesisStatus = "duplicate"
)

// TriageDecision is a human decision on a hypothesis. The first three apply
// to draft or triaged hypotheses; validated and not_reproducible conclude a
// lab validation.
type TriageDecision string

const (
	DecisionAccepted        TriageDecision = "accepted"
	DecisionRejected        TriageDecision = "rejected"
	DecisionDuplicate       TriageDecision = "duplicate"
	DecisionValidated       TriageDecision = "validated"
	DecisionNotReproducible TriageDecision = "not_reproducible"
)

func (d TriageDecision) Valid() bool {
	switch d {
	case DecisionAccepted, DecisionRejected, DecisionDuplicate, DecisionValidated, DecisionNotReproducible:
		return true
	}
	return false
}

// CanTriage reports whether decision d may be applied in status s.
func CanTriage(s HypothesisStatus, d TriageDecision) bool {
	if d == DecisionValidated || d == DecisionNotReproducible {
		return s == HypothesisValidationPending
	}
	return s == HypothesisDraft || s == HypothesisTriage
}

// Limits bound a single run. Zero fields take defaults in Normalize.
// MaxTokens is the output-token budget (thinking included) summed over every
// model call in the run.
type Limits struct {
	MaxWorkers  int `json:"max_workers"`
	WallSeconds int `json:"wall_seconds"`
	MaxTokens   int `json:"max_tokens"`
}

func (l Limits) Normalize() (Limits, error) {
	if l.MaxWorkers == 0 {
		l.MaxWorkers = 2
	}
	if l.WallSeconds == 0 {
		l.WallSeconds = 900
	}
	if l.MaxTokens == 0 {
		l.MaxTokens = 200000
	}
	switch {
	case l.MaxWorkers < 1 || l.MaxWorkers > 4:
		return l, fmt.Errorf("max_workers must be 1–4")
	case l.WallSeconds < 60 || l.WallSeconds > 86400:
		return l, fmt.Errorf("wall_seconds must be 60 to 86400")
	case l.MaxTokens < 4000 || l.MaxTokens > 2000000:
		return l, fmt.Errorf("max_tokens must be 4000–2000000")
	}
	return l, nil
}
