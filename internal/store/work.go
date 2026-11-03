package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ljw097/remnant/internal/research"
)

// Statuses the orchestrator picks up, and the automatic stages a run may only
// be in while someone holds its lease.
var (
	claimableStatuses = []string{"queued", "validation_approved", "report_ready"}
	leasedStatuses    = []string{"collecting", "indexing", "surface_mapping", "hypothesis_generation", "static_validation", "lab_validation"}
)

// ClaimRun leases the oldest claimable run for owner. It returns ErrNotFound
// when there is nothing to do. SKIP LOCKED makes it safe across instances.
func (s *Store) ClaimRun(ctx context.Context, owner string, ttl time.Duration) (int64, research.RunStatus, error) {
	var id int64
	var status research.RunStatus
	err := s.pool.QueryRow(ctx, `
		UPDATE runs SET lease_owner = $1, lease_expires_at = now() + $2 * interval '1 second'
		WHERE id = (
			SELECT id FROM runs
			WHERE status = ANY($3) AND (lease_expires_at IS NULL OR lease_expires_at < now())
			ORDER BY queued_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1)
		RETURNING id, status`, owner, ttl.Seconds(), claimableStatuses).Scan(&id, &status)
	return id, status, mapErr(err)
}

// ErrLeaseLost means another owner holds the run now.
var ErrLeaseLost = fmt.Errorf("%w: run lease lost", ErrConflict)

// RenewLease extends owner's lease and returns the run's current status, so
// the heartbeat also observes cancellation.
func (s *Store) RenewLease(ctx context.Context, id int64, owner string, ttl time.Duration) (research.RunStatus, error) {
	var status research.RunStatus
	err := s.pool.QueryRow(ctx, `
		UPDATE runs SET lease_expires_at = now() + $3 * interval '1 second'
		WHERE id = $1 AND lease_owner = $2
		RETURNING status`, id, owner, ttl.Seconds()).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrLeaseLost
	}
	return status, err
}

func (s *Store) ReleaseLease(ctx context.Context, id int64, owner string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE runs SET lease_owner = '', lease_expires_at = NULL WHERE id = $1 AND lease_owner = $2`, id, owner)
	return err
}

// MaxAttempts is how many times a run is recovered after its orchestrator
// died before it is failed instead.
const MaxAttempts = 3

// ReapExpiredLeases recovers runs left in an automatic stage whose lease
// expired (the orchestrator holding them died): each goes back to its resume
// point, or fails once it has been recovered MaxAttempts times.
func (s *Store) ReapExpiredLeases(ctx context.Context) (requeued, failed []int64, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		type stale struct {
			ID       int64              `db:"id"`
			Status   research.RunStatus `db:"status"`
			Attempts int32              `db:"attempts"`
		}
		runs, err := collect[stale](tx.Query(ctx, `
			SELECT id, status, attempts FROM runs
			WHERE status = ANY($1) AND (lease_expires_at IS NULL OR lease_expires_at < now())
			FOR UPDATE SKIP LOCKED`, leasedStatuses))
		if err != nil {
			return err
		}
		for _, r := range runs {
			if r.Attempts >= MaxAttempts {
				summary := fmt.Sprintf("orchestrator lease expired %d times", r.Attempts+1)
				if _, err := tx.Exec(ctx, `UPDATE runs SET error_summary = $2, lease_owner = '', lease_expires_at = NULL WHERE id = $1`, r.ID, summary); err != nil {
					return err
				}
				if err := transitionRun(ctx, tx, r.ID, r.Status, research.RunFailed, summary); err != nil {
					return err
				}
				failed = append(failed, r.ID)
				continue
			}
			if err := requeue(ctx, tx, r.ID, r.Status, "orchestrator lease expired", true); err != nil {
				return err
			}
			requeued = append(requeued, r.ID)
		}
		return nil
	})
	return requeued, failed, err
}

// RequeueRun returns a run interrupted in an automatic stage to its resume
// point and releases its lease. countAttempt is false for orderly shutdowns,
// which are not the run's fault.
func (s *Store) RequeueRun(ctx context.Context, id int64, from research.RunStatus, reason string, countAttempt bool) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return requeue(ctx, tx, id, from, reason, countAttempt)
	})
}

func requeue(ctx context.Context, tx pgx.Tx, id int64, from research.RunStatus, reason string, countAttempt bool) error {
	back, stage := research.ResumePoint(from)
	if !from.Automatic() || back == "" {
		return fmt.Errorf("%w: a %s run cannot be requeued", ErrConflict, from)
	}
	inc := 0
	if countAttempt {
		inc = 1
	}
	if _, err := tx.Exec(ctx, `
		UPDATE runs SET attempts = attempts + $2, lease_owner = '', lease_expires_at = NULL WHERE id = $1`, id, inc); err != nil {
		return err
	}
	msg := fmt.Sprintf("%s; resuming", reason)
	if stage != "" {
		msg = fmt.Sprintf("%s; will resume at %s", reason, stage)
	}
	return transitionRun(ctx, tx, id, from, back, msg)
}

// PauseRun pauses a run that is waiting or being worked on. An orchestrator
// working it stops at its next heartbeat; work already recorded is kept.
func (s *Store) PauseRun(ctx context.Context, id int64, by string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status research.RunStatus
		if err := tx.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
			return mapErr(err)
		}
		if !status.Pausable() {
			return fmt.Errorf("%w: a %s run cannot be paused", ErrConflict, status)
		}
		return transitionRun(ctx, tx, id, status, research.RunPaused, "Paused by "+by)
	})
}

// ResumeRun puts a paused run back where it can be claimed.
func (s *Store) ResumeRun(ctx context.Context, id int64, by string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status, back research.RunStatus
		if err := tx.QueryRow(ctx, `SELECT status, resume_status FROM runs WHERE id = $1 FOR UPDATE`, id).Scan(&status, &back); err != nil {
			return mapErr(err)
		}
		if status != research.RunPaused {
			return fmt.Errorf("%w: run is %s, not paused", ErrConflict, status)
		}
		return transitionRun(ctx, tx, id, research.RunPaused, back, "Resumed by "+by)
	})
}

// AddActiveTime adds time spent working a run's source stages.
func (s *Store) AddActiveTime(ctx context.Context, id int64, d time.Duration) error {
	_, err := s.pool.Exec(ctx, `UPDATE runs SET active_ms = active_ms + $2 WHERE id = $1`, id, d.Milliseconds())
	return err
}

// CancelStaleExperiments marks experiments of a run still "running" as
// cancelled. It is called when a run is claimed, so they belong to a worker
// that is gone. An interrupted lab experiment consumes its approval; its
// hypothesis returns to accepted so a new approval can be requested.
func (s *Store) CancelStaleExperiments(ctx context.Context, runID int64) (int, error) {
	var n int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		ids, err := collect[struct {
			ID           int64  `db:"id"`
			HypothesisID *int64 `db:"hypothesis_id"`
			ApprovalID   *int64 `db:"approval_id"`
		}](tx.Query(ctx, `
			UPDATE experiments SET status = 'cancelled', completed_at = now(),
			       result_summary = 'interrupted: the worker running it is gone'
			WHERE run_id = $1 AND status IN ('queued','running')
			RETURNING id, hypothesis_id, approval_id`, runID))
		if err != nil {
			return err
		}
		n = len(ids)
		for _, e := range ids {
			if e.ApprovalID != nil && e.HypothesisID != nil {
				if _, err := tx.Exec(ctx, `
					UPDATE hypotheses SET status = 'accepted', updated_at = now()
					WHERE id = $1 AND status = 'validation_pending'`, *e.HypothesisID); err != nil {
					return err
				}
			}
			if err := appendEvent(ctx, tx, runID, "experiment.interrupted", "",
				fmt.Sprintf("Experiment %d was interrupted and marked cancelled", e.ID), map[string]any{"experiment_id": e.ID}); err != nil {
				return err
			}
		}
		return nil
	})
	return n, err
}

// FinishedStaticOps returns "<target>|<operation>" for static experiments of
// the run that finished, so a resumed run does not repeat them.
func (s *Store) FinishedStaticOps(ctx context.Context, runID int64) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT target_id || '|' || operation FROM experiments
		WHERE run_id = $1 AND approval_id IS NULL AND target_id IS NOT NULL
		  AND status IN ('passed','finding','failed','resource_limited')`, runID)
	if err != nil {
		return nil, err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	done := map[string]bool{}
	for _, k := range keys {
		done[k] = true
	}
	return done, err
}

// Candidate is a source region selected during surface mapping, recorded as
// run-level evidence. Outcome is empty until the region has been examined.
type Candidate struct {
	Evidence
	Key     string
	Outcome string
	Meta    struct {
		PatternID int64    `json:"pattern_id"`
		Score     int      `json:"score"`
		Matched   []string `json:"matched"`
		Countered []string `json:"countered"`
		Language  string   `json:"language"`
	}
}

// InsertCandidate records a candidate region once per run (keyed by
// metadata.candidate_key) and returns its evidence ID either way.
func (s *Store) InsertCandidate(ctx context.Context, runID int64, e EvidenceInput) (int64, error) {
	key, _ := e.Metadata["candidate_key"].(string)
	if key == "" {
		return 0, fmt.Errorf("%w: candidate without candidate_key", ErrInvalid)
	}
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO evidence(run_id, target_id, evidence_kind, file_path, line_start, line_end,
		                     symbol, revision, content_sha256, summary, excerpt, metadata)
		VALUES ($1, $2, $3, $4, NULLIF($5, 0), NULLIF($6, 0), $7, $8, $9, $10, $11, $12)
		ON CONFLICT (run_id, (metadata->>'candidate_key'))
		    WHERE hypothesis_id IS NULL AND (metadata->>'candidate_key') IS NOT NULL
		DO NOTHING`,
		runID, e.TargetID, e.Kind, e.FilePath, e.LineStart, e.LineEnd,
		e.Symbol, e.Revision, e.ContentSHA256, e.Summary, e.Excerpt, e.Metadata); err != nil {
		return 0, mapErr(err)
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM evidence
		WHERE run_id = $1 AND hypothesis_id IS NULL AND metadata->>'candidate_key' = $2`, runID, key).Scan(&id)
	return id, mapErr(err)
}

// ListCandidates returns the run's candidate regions in selection order.
func (s *Store) ListCandidates(ctx context.Context, runID int64) ([]Candidate, error) {
	ev, err := collect[Evidence](s.pool.Query(ctx, `
		SELECT id, hypothesis_id, target_id, evidence_kind, source_uri, file_path, line_start, line_end,
		       symbol, revision, content_sha256, summary, excerpt, metadata, created_at
		FROM evidence
		WHERE run_id = $1 AND hypothesis_id IS NULL AND (metadata->>'candidate_key') IS NOT NULL
		ORDER BY id`, runID))
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(ev))
	for _, e := range ev {
		c := Candidate{Evidence: e}
		var m struct {
			Key     string `json:"candidate_key"`
			Outcome string `json:"outcome"`
		}
		if err := json.Unmarshal(e.Metadata, &m); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(e.Metadata, &c.Meta); err != nil {
			return nil, err
		}
		c.Key, c.Outcome = m.Key, m.Outcome
		out = append(out, c)
	}
	return out, nil
}

// MarkCandidate records the outcome of examining a candidate region.
func (s *Store) MarkCandidate(ctx context.Context, evidenceID int64, outcome string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE evidence SET metadata = metadata || jsonb_build_object('outcome', $2::text) WHERE id = $1`, evidenceID, outcome)
	return err
}

// FailRun ends a run in to (failed or timed_out) with a summary.
func (s *Store) FailRun(ctx context.Context, id int64, from, to research.RunStatus, summary string) error {
	if len(summary) > 2000 {
		summary = summary[:2000]
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE runs SET error_summary = $2 WHERE id = $1`, id, summary); err != nil {
			return err
		}
		return transitionRun(ctx, tx, id, from, to, summary)
	})
}

func (s *Store) RunStatus(ctx context.Context, id int64) (research.RunStatus, error) {
	var status research.RunStatus
	err := s.pool.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1`, id).Scan(&status)
	return status, mapErr(err)
}

func (s *Store) AddTokenUsage(ctx context.Context, runID, input, output int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE runs SET input_tokens = input_tokens + $2, output_tokens = output_tokens + $3 WHERE id = $1`,
		runID, input, output)
	return err
}

// WorkTarget is a run target with the revision pinned for that run.
// CurrentRevision and TreeSHA256 describe the target now; a run may only
// read the snapshot while CurrentRevision equals the pinned Revision and
// the snapshot on disk still hashes to TreeSHA256.
type WorkTarget struct {
	ID              int64  `db:"id"`
	Kind            string `db:"kind"`
	SnapshotKey     string `db:"snapshot_key"`
	Revision        string `db:"revision"`
	CurrentRevision string `db:"current_revision"`
	TreeSHA256      string `db:"tree_sha256"`
	PrimaryLanguage string `db:"primary_language"`
}

type WorkScope struct {
	Run      Run
	Limits   research.Limits
	Targets  []WorkTarget
	Patterns []Pattern
}

func (s *Store) WorkScope(ctx context.Context, runID int64) (WorkScope, error) {
	run, err := one[Run](s.pool.Query(ctx, `SELECT `+runCols+` FROM runs WHERE id = $1`, runID))
	if err != nil {
		return WorkScope{}, err
	}
	w := WorkScope{Run: run}
	if err := jsonUnmarshal(run.Limits, &w.Limits); err != nil {
		return WorkScope{}, err
	}
	if w.Targets, err = collect[WorkTarget](s.pool.Query(ctx, `
		SELECT t.id, t.kind, t.snapshot_key, rt.revision, t.revision AS current_revision, t.tree_sha256, t.primary_language
		FROM run_targets rt JOIN targets t ON t.id = rt.target_id
		WHERE rt.run_id = $1 ORDER BY t.id`, runID)); err != nil {
		return WorkScope{}, err
	}
	w.Patterns, err = collect[Pattern](s.pool.Query(ctx, `
		SELECT `+patternCols+` FROM patterns
		WHERE id IN (SELECT pattern_id FROM run_patterns WHERE run_id = $1) ORDER BY id`, runID))
	return w, err
}

// AgentProfile returns the configuration for an agent role. Without a row
// the role is enabled, uses the run's model profile and has no extra
// settings.
func (s *Store) AgentProfile(ctx context.Context, role string) (enabled bool, llmProfileID *int64, cfg AgentConfig, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT enabled, llm_profile_id, config FROM agent_profiles WHERE role = $1 ORDER BY id LIMIT 1`, role).
		Scan(&enabled, &llmProfileID, &cfg)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil, AgentConfig{}, nil
	}
	return enabled, llmProfileID, cfg, err
}

const agentCols = `role, display_name, enabled, llm_profile_id, config, updated_at`

// ListAgents returns the agents the pipeline runs.
func (s *Store) ListAgents(ctx context.Context) ([]Agent, error) {
	return collect[Agent](s.pool.Query(ctx, `
		SELECT `+agentCols+` FROM agent_profiles
		WHERE role IN ('historian', 'hypothesis_researcher', 'skeptic', 'reporter')
		ORDER BY array_position(ARRAY['historian','hypothesis_researcher','skeptic','reporter'], role)`))
}

// AgentPatch changes an agent. ClearProfile makes it use the run's model
// profile again.
type AgentPatch struct {
	Enabled      *bool
	LLMProfileID *int64
	ClearProfile bool
	Config       *AgentConfig
}

func (s *Store) UpdateAgent(ctx context.Context, role string, p AgentPatch) (Agent, error) {
	a, err := one[Agent](s.pool.Query(ctx, `
		UPDATE agent_profiles SET
			enabled        = COALESCE($2, enabled),
			llm_profile_id = CASE WHEN $4 THEN NULL ELSE COALESCE($3, llm_profile_id) END,
			config         = COALESCE($5, config),
			updated_at     = now()
		WHERE role = $1
		RETURNING `+agentCols, role, p.Enabled, p.LLMProfileID, p.ClearProfile, p.Config))
	return a, mapErr(err)
}

type EvidenceInput struct {
	TargetID      int64
	Kind          string
	FilePath      string
	LineStart     int // 0 = unknown
	LineEnd       int
	Symbol        string
	Revision      string
	ContentSHA256 string
	Summary       string
	Excerpt       string
	Metadata      map[string]any
}

func insertEvidence(ctx context.Context, q querier, runID int64, hypothesisID *int64, e EvidenceInput) (int64, error) {
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	var id int64
	err := q.QueryRow(ctx, `
		INSERT INTO evidence(hypothesis_id, run_id, target_id, evidence_kind, file_path, line_start, line_end,
		                     symbol, revision, content_sha256, summary, excerpt, metadata)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, 0), NULLIF($7, 0), $8, $9, $10, $11, $12, $13)
		RETURNING id`,
		hypothesisID, runID, e.TargetID, e.Kind, e.FilePath, e.LineStart, e.LineEnd,
		e.Symbol, e.Revision, e.ContentSHA256, e.Summary, e.Excerpt, e.Metadata).Scan(&id)
	return id, mapErr(err)
}

// InsertEvidence records run-level evidence not yet tied to a hypothesis.
func (s *Store) InsertEvidence(ctx context.Context, runID int64, e EvidenceInput) (int64, error) {
	return insertEvidence(ctx, s.pool, runID, nil, e)
}

type HypothesisInput struct {
	TargetID            int64
	PatternID           *int64
	Title               string
	Claim               string
	VulnerabilityClass  string
	CWEID               string
	Facts               []string
	Preconditions       []string
	Unknowns            []string
	AffectedSymbols     []string
	ExpectedObservation string
	CounterEvidence     []string
	TriageConfidence    string
	DedupeKey           string
}

// ErrDuplicate is returned when a run already has a hypothesis with the same
// dedupe key.
var ErrDuplicate = fmt.Errorf("%w: duplicate hypothesis", ErrConflict)

// InsertHypothesis creates a draft hypothesis and attaches copies of the
// cited run-level evidence. Every cited ID must belong to the same run and
// target; the model never gets to point at evidence it was not given.
func (s *Store) InsertHypothesis(ctx context.Context, runID int64, h HypothesisInput, evidenceIDs []int64) (int64, error) {
	var id int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var dup bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM hypotheses WHERE run_id = $1 AND dedupe_key = $2)`,
			runID, h.DedupeKey).Scan(&dup); err != nil {
			return err
		}
		if dup {
			return ErrDuplicate
		}
		var n int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM evidence
			WHERE id = ANY($1) AND run_id = $2 AND target_id = $3 AND hypothesis_id IS NULL`,
			evidenceIDs, runID, h.TargetID).Scan(&n); err != nil {
			return err
		}
		if n == 0 || n != len(evidenceIDs) {
			return fmt.Errorf("%w: cited evidence does not belong to this run and target", ErrInvalid)
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO hypotheses(run_id, target_id, pattern_id, title, claim, vulnerability_class, cwe_id,
			                       triage_confidence, facts, preconditions, unknowns, affected_symbols,
			                       expected_observation, counter_evidence, dedupe_key)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING id`,
			runID, h.TargetID, h.PatternID, h.Title, h.Claim, h.VulnerabilityClass, h.CWEID,
			h.TriageConfidence, nonNil(h.Facts), nonNil(h.Preconditions), nonNil(h.Unknowns), nonNil(h.AffectedSymbols),
			h.ExpectedObservation, nonNil(h.CounterEvidence), h.DedupeKey).Scan(&id); err != nil {
			return mapErr(err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO evidence(hypothesis_id, run_id, target_id, evidence_kind, source_uri, file_path,
			                     line_start, line_end, symbol, revision, content_sha256, summary, excerpt, metadata)
			SELECT $1, run_id, target_id, evidence_kind, source_uri, file_path,
			       line_start, line_end, symbol, revision, content_sha256, summary, excerpt,
			       metadata || jsonb_build_object('copied_from', id)
			FROM evidence WHERE id = ANY($2)`, id, evidenceIDs); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE evidence SET metadata = metadata || jsonb_build_object('outcome', 'hypothesis')
			WHERE id = ANY($1) AND (metadata->>'candidate_key') IS NOT NULL`, evidenceIDs); err != nil {
			return err
		}
		return appendEvent(ctx, tx, runID, "hypothesis.created", string(research.RunHypothesisGeneration),
			fmt.Sprintf("Hypothesis %d drafted: %s", id, h.Title), map[string]any{"hypothesis_id": id})
	})
	return id, err
}

// RecordSkeptic stores the Skeptic's verdict, replaces the counter-evidence
// list with what it found, and moves the hypothesis to triage.
func (s *Store) RecordSkeptic(ctx context.Context, runID, hypothesisID int64, verdict, confidence string, counter []string, ev []EvidenceInput) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE hypotheses SET skeptic_verdict = $2, triage_confidence = $3, counter_evidence = $4,
			                      status = 'triage', updated_at = now()
			WHERE id = $1 AND run_id = $5 AND status = 'draft'`,
			hypothesisID, verdict, confidence, nonNil(counter), runID)
		if err != nil {
			return mapErr(err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: hypothesis %d is not a draft of run %d", ErrConflict, hypothesisID, runID)
		}
		for _, e := range ev {
			if _, err := insertEvidence(ctx, tx, runID, &hypothesisID, e); err != nil {
				return err
			}
		}
		return appendEvent(ctx, tx, runID, "hypothesis.challenged", string(research.RunHypothesisGeneration),
			fmt.Sprintf("Skeptic %s hypothesis %d", verdict, hypothesisID),
			map[string]any{"hypothesis_id": hypothesisID, "verdict": verdict})
	})
}

// SetHypothesisStatus moves a hypothesis between statuses with a
// compare-and-set on the current one.
func (s *Store) SetHypothesisStatus(ctx context.Context, id int64, from, to research.HypothesisStatus) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE hypotheses SET status = $3, updated_at = now() WHERE id = $1 AND status = $2`, id, string(from), string(to))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: hypothesis %d is not %s", ErrConflict, id, from)
	}
	return nil
}

type ExperimentInput struct {
	HypothesisID *int64
	ApprovalID   *int64
	TargetID     int64
	Operation    string
	RequestID    string
	Revision     string
	Config       map[string]any
}

func (s *Store) StartExperiment(ctx context.Context, runID int64, in ExperimentInput) (int64, error) {
	if in.Config == nil {
		in.Config = map[string]any{}
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO experiments(run_id, hypothesis_id, approval_id, target_id, operation, request_id,
		                        status, workspace_revision, config, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'running', $7, $8, now()) RETURNING id`,
		runID, in.HypothesisID, in.ApprovalID, in.TargetID, in.Operation, in.RequestID, in.Revision, in.Config).Scan(&id)
	return id, mapErr(err)
}

type ArtifactInput struct {
	ExperimentID   *int64
	Kind           string
	Name           string
	StorageKey     string
	SHA256         string
	SizeBytes      int64
	MediaType      string
	Classification string
}

func insertArtifact(ctx context.Context, q querier, runID int64, a ArtifactInput) (Artifact, error) {
	if a.Classification == "" {
		a.Classification = "research_internal"
	}
	return one[Artifact](q.Query(ctx, `
		INSERT INTO artifacts(run_id, experiment_id, artifact_kind, name, storage_key, sha256, size_bytes,
		                      media_type, classification)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (run_id, sha256) DO UPDATE SET name = artifacts.name
		RETURNING `+artifactCols,
		runID, a.ExperimentID, a.Kind, a.Name, a.StorageKey, a.SHA256, a.SizeBytes, a.MediaType, a.Classification))
}

func (s *Store) InsertArtifact(ctx context.Context, runID int64, a ArtifactInput) (Artifact, error) {
	return insertArtifact(ctx, s.pool, runID, a)
}

// FinishExperiment records the outcome and everything the (already
// validated) worker result produced.
func (s *Store) FinishExperiment(ctx context.Context, runID, id int64, status, summary string, exitCode *int,
	ev []EvidenceInput, arts []ArtifactInput) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE experiments SET status = $3, result_summary = $4, exit_code = $5, completed_at = now()
			WHERE id = $1 AND run_id = $2`, id, runID, status, summary, exitCode); err != nil {
			return mapErr(err)
		}
		for _, e := range ev {
			e.Metadata = withKey(e.Metadata, "experiment_id", id)
			if _, err := insertEvidence(ctx, tx, runID, nil, e); err != nil {
				return err
			}
		}
		for _, a := range arts {
			a.ExperimentID = &id
			if _, err := insertArtifact(ctx, tx, runID, a); err != nil {
				return err
			}
		}
		return appendEvent(ctx, tx, runID, "experiment.finished", "", fmt.Sprintf("Experiment %d %s: %s", id, status, summary),
			map[string]any{"experiment_id": id, "status": status})
	})
}

func (s *Store) ListExperiments(ctx context.Context, runID int64) ([]Experiment, error) {
	return collect[Experiment](s.pool.Query(ctx, `
		SELECT id, run_id, hypothesis_id, approval_id, target_id, operation, request_id, status, network_mode,
		       workspace_revision, config, result_summary, exit_code, started_at, completed_at, created_at
		FROM experiments WHERE run_id = $1 ORDER BY id`, runID))
}

// ApprovedLabWork lists approved approvals of a run that have no experiment.
func (s *Store) ApprovedLabWork(ctx context.Context, runID int64) ([]Approval, error) {
	return collect[Approval](s.pool.Query(ctx, `
		SELECT `+approvalCols+` FROM approvals a
		WHERE run_id = $1 AND decision = 'approved'
		  AND NOT EXISTS (SELECT 1 FROM experiments e WHERE e.approval_id = a.id)
		ORDER BY id`, runID))
}

// ApprovalStillValid recomputes an approved approval's scope right before
// execution. If anything moved, the approval is revoked.
func (s *Store) ApprovalStillValid(ctx context.Context, approvalID int64) (bool, error) {
	var fresh bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		a, err := one[Approval](tx.Query(ctx, `SELECT `+approvalCols+` FROM approvals WHERE id = $1 FOR UPDATE`, approvalID))
		if err != nil {
			return err
		}
		if a.Decision != "approved" {
			return nil
		}
		if _, fresh, err = checkApprovalScope(ctx, tx, a); err != nil || fresh {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE approvals SET decision = 'revoked' WHERE id = $1`, a.ID); err != nil {
			return err
		}
		return appendEvent(ctx, tx, a.RunID, "approval.revoked", "",
			fmt.Sprintf("Lab approval %d revoked: scope changed after approval", a.ID), map[string]any{"approval_id": a.ID})
	})
	return fresh, err
}

// FinalizeRun moves a reviewed run to report_ready. Pending approvals must be
// decided first.
func (s *Store) FinalizeRun(ctx context.Context, id int64, by string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status research.RunStatus
		if err := tx.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
			return mapErr(err)
		}
		if status != research.RunReviewRequired {
			return fmt.Errorf("%w: run is %s, not review_required", ErrConflict, status)
		}
		var pending int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM approvals WHERE run_id = $1 AND decision = 'pending'`, id).Scan(&pending); err != nil {
			return err
		}
		if pending > 0 {
			return fmt.Errorf("%w: %d lab approval(s) still pending", ErrConflict, pending)
		}
		return transitionRun(ctx, tx, id, research.RunReviewRequired, research.RunReportReady, "Finalized by "+by)
	})
}

// RunReport is everything the report is rendered from.
type RunReport struct {
	Run         RunDetail          `json:"run"`
	Targets     []WorkTarget       `json:"targets"`
	Patterns    []Pattern          `json:"patterns"`
	Hypotheses  []HypothesisDetail `json:"hypotheses"`
	Experiments []Experiment       `json:"experiments"`
	Approvals   []Approval         `json:"approvals"`
}

func (s *Store) RunReport(ctx context.Context, runID int64) (RunReport, error) {
	var r RunReport
	var err error
	if r.Run, err = s.GetRun(ctx, runID); err != nil {
		return r, err
	}
	scope, err := s.WorkScope(ctx, runID)
	if err != nil {
		return r, err
	}
	r.Targets, r.Patterns = scope.Targets, scope.Patterns
	hs, err := s.ListHypotheses(ctx, runID)
	if err != nil {
		return r, err
	}
	for _, h := range hs {
		d, err := s.GetHypothesis(ctx, h.ID)
		if err != nil {
			return r, err
		}
		r.Hypotheses = append(r.Hypotheses, d)
	}
	if r.Experiments, err = s.ListExperiments(ctx, runID); err != nil {
		return r, err
	}
	r.Approvals, err = collect[Approval](s.pool.Query(ctx, `SELECT `+approvalCols+` FROM approvals WHERE run_id = $1 ORDER BY id`, runID))
	return r, err
}

func withKey(m map[string]any, k string, v any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for key, val := range m {
		out[key] = val
	}
	out[k] = v
	return out
}
