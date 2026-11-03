package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ljw097/remnant/internal/policy"
	"github.com/ljw097/remnant/internal/research"
)

const runCols = `id, project_id, status, mode, auto, limits, policy, scope_hash, llm_profile_id, started_by,
	error_summary, input_tokens, output_tokens, resume_stage, resume_status, attempts, active_ms,
	queued_at, started_at, completed_at`

type RunInput struct {
	ProjectID    int64
	TargetIDs    []int64 // deduplicated by the caller
	PatternIDs   []int64 // deduplicated by the caller
	LLMProfileID *int64  // nil: use the project's profile
	Limits       research.Limits
	// Auto runs autopilot: the run triages its own hypotheses and finalises
	// a report without a person. Lab experiments still need approval.
	Auto bool
}

// CreateRun pins every target to its current revision, computes the scope
// hash and queues the run, all in one transaction.
func (s *Store) CreateRun(ctx context.Context, in RunInput, startedBy int64) (RunDetail, error) {
	var runID int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		var pol policy.Policy
		var projectProfile *int64
		err := tx.QueryRow(ctx, `SELECT status, policy, llm_profile_id FROM projects WHERE id = $1 FOR SHARE`, in.ProjectID).
			Scan(&status, &pol, &projectProfile)
		if err != nil {
			return mapErr(err)
		}
		if status != "active" {
			return fmt.Errorf("%w: project is %s", ErrConflict, status)
		}

		rows, err := tx.Query(ctx, `
			SELECT id AS target_id, revision FROM targets
			WHERE project_id = $1 AND id = ANY($2) AND status = 'ready' AND revision <> ''
			FOR SHARE`, in.ProjectID, in.TargetIDs)
		targets, err := collect[RunTarget](rows, err)
		if err != nil {
			return err
		}
		if len(targets) != len(in.TargetIDs) {
			return fmt.Errorf("%w: every target must belong to the project and be ready", ErrInvalid)
		}

		var usable int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM patterns WHERE id = ANY($1) AND validation_status <> 'rejected'`,
			in.PatternIDs).Scan(&usable); err != nil {
			return err
		}
		if usable != len(in.PatternIDs) {
			return fmt.Errorf("%w: unknown or rejected pattern", ErrInvalid)
		}

		profile := in.LLMProfileID
		if profile == nil {
			profile = projectProfile
		}
		if profile == nil {
			return fmt.Errorf("%w: no LLM profile on the request or the project", ErrInvalid)
		}
		var hasKey bool
		if err := tx.QueryRow(ctx, `SELECT api_key_sealed IS NOT NULL FROM llm_profiles WHERE id = $1`, *profile).Scan(&hasKey); err != nil {
			if err == pgx.ErrNoRows {
				return fmt.Errorf("%w: unknown LLM profile", ErrInvalid)
			}
			return err
		}
		if !hasKey {
			return fmt.Errorf("%w: LLM profile has no API key", ErrInvalid)
		}

		revs := make([]policy.TargetRevision, len(targets))
		for i, t := range targets {
			revs[i] = policy.TargetRevision{TargetID: t.TargetID, Revision: t.Revision}
		}
		scopeHash, err := policy.RunScope{Policy: pol, Targets: revs, Patterns: in.PatternIDs, Limits: in.Limits}.Hash()
		if err != nil {
			return err
		}

		if err := tx.QueryRow(ctx, `
			INSERT INTO runs(project_id, limits, policy, scope_hash, llm_profile_id, started_by, auto)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
			in.ProjectID, in.Limits, pol, scopeHash, *profile, startedBy, in.Auto).Scan(&runID); err != nil {
			return mapErr(err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO run_targets(run_id, target_id, revision)
			SELECT $1, t.id, t.revision FROM targets t WHERE t.id = ANY($2)`, runID, in.TargetIDs); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO run_patterns(run_id, pattern_id) SELECT $1, unnest($2::bigint[])`, runID, in.PatternIDs); err != nil {
			return err
		}
		return appendEvent(ctx, tx, runID, "run.queued", string(research.RunQueued), "Run queued",
			map[string]any{"scope_hash": scopeHash, "targets": revs})
	})
	if err != nil {
		return RunDetail{}, err
	}
	return s.GetRun(ctx, runID)
}

func (s *Store) ListRuns(ctx context.Context, projectID int64, status string) ([]Run, error) {
	return collect[Run](s.pool.Query(ctx, `
		SELECT `+runCols+` FROM runs
		WHERE ($1 = 0 OR project_id = $1) AND ($2 = '' OR status = $2)
		ORDER BY queued_at DESC LIMIT 200`, projectID, status))
}

func (s *Store) GetRun(ctx context.Context, id int64) (RunDetail, error) {
	run, err := one[Run](s.pool.Query(ctx, `SELECT `+runCols+` FROM runs WHERE id = $1`, id))
	if err != nil {
		return RunDetail{}, err
	}
	d := RunDetail{Run: run, HypothesisCount: map[string]int64{}}
	if d.Targets, err = collect[RunTarget](s.pool.Query(ctx, `
		SELECT target_id, revision FROM run_targets WHERE run_id = $1 ORDER BY target_id`, id)); err != nil {
		return RunDetail{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT pattern_id FROM run_patterns WHERE run_id = $1 ORDER BY pattern_id`, id)
	if err != nil {
		return RunDetail{}, err
	}
	if d.PatternIDs, err = pgx.CollectRows(rows, pgx.RowTo[int64]); err != nil {
		return RunDetail{}, err
	}
	rows, err = s.pool.Query(ctx, `SELECT status, count(*) FROM hypotheses WHERE run_id = $1 GROUP BY status`, id)
	if err != nil {
		return RunDetail{}, err
	}
	var st string
	var n int64
	_, err = pgx.ForEachRow(rows, []any{&st, &n}, func() error { d.HypothesisCount[st] = n; return nil })
	return d, err
}

// TransitionRun moves a run along the state machine. It fails with
// ErrConflict if the run is not currently in from or the move is not allowed.
func (s *Store) TransitionRun(ctx context.Context, id int64, from, to research.RunStatus, message string) error {
	if !research.CanTransition(from, to) {
		return fmt.Errorf("%w: %s -> %s is not a valid transition", ErrConflict, from, to)
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return transitionRun(ctx, tx, id, from, to, message)
	})
}

func transitionRun(ctx context.Context, q querier, id int64, from, to research.RunStatus, message string) error {
	// Resume bookkeeping travels with the status so the CHECK constraint
	// "paused iff resume_status is set" holds after every statement.
	resumeStatus := ""
	var resumeStage *string // nil keeps the current value
	clear := ""
	switch back, stage := research.ResumePoint(from); {
	case to == research.RunPaused:
		resumeStatus = string(back)
		if stage != "" {
			resumeStage = (*string)(&stage)
		}
	case from.Automatic() && to == back:
		if stage != "" {
			resumeStage = (*string)(&stage)
		}
	case to == research.RunQueued || to.Automatic():
	default:
		resumeStage = &clear
	}
	tag, err := q.Exec(ctx, `
		UPDATE runs SET status = $3,
			started_at    = CASE WHEN $2 = 'queued' AND started_at IS NULL THEN now() ELSE started_at END,
			completed_at  = CASE WHEN $4 THEN now() ELSE completed_at END,
			resume_status = $5,
			resume_stage  = COALESCE($6, resume_stage)
		WHERE id = $1 AND status = $2`, id, string(from), string(to), to.Terminal(), resumeStatus, resumeStage)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: run %d is not %s", ErrConflict, id, from)
	}
	return appendEvent(ctx, q, id, "run.status", string(to), message, map[string]any{"from": from, "to": to})
}

// CancelRun cancels a non-terminal run. Worker termination is the
// orchestrator's job; it observes the cancelled status.
func (s *Store) CancelRun(ctx context.Context, id int64, by string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status research.RunStatus
		if err := tx.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
			return mapErr(err)
		}
		if status.Terminal() {
			return fmt.Errorf("%w: run is already %s", ErrConflict, status)
		}
		return transitionRun(ctx, tx, id, status, research.RunCancelled, "Cancelled by "+by)
	})
}

func (s *Store) ListArtifacts(ctx context.Context, runID int64) ([]Artifact, error) {
	return collect[Artifact](s.pool.Query(ctx, `
		SELECT `+artifactCols+` FROM artifacts WHERE run_id = $1 ORDER BY id`, runID))
}

const artifactCols = `id, experiment_id, artifact_kind, name, storage_key, sha256, size_bytes, media_type, classification, created_at`

func (s *Store) GetArtifact(ctx context.Context, runID, id int64) (Artifact, error) {
	return one[Artifact](s.pool.Query(ctx, `SELECT `+artifactCols+` FROM artifacts WHERE run_id = $1 AND id = $2`, runID, id))
}

func appendEvent(ctx context.Context, q querier, runID int64, eventType, stage, message string, payload any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO events(run_id, event_type, stage, message, payload) VALUES ($1, $2, $3, $4, $5)`,
		runID, eventType, stage, message, b)
	return err
}

func (s *Store) AppendEvent(ctx context.Context, runID int64, eventType, stage, message string, payload any) error {
	return appendEvent(ctx, s.pool, runID, eventType, stage, message, payload)
}

// EventsSince returns up to limit events with id > after, oldest first, and
// the run's current status.
func (s *Store) EventsSince(ctx context.Context, runID, after int64, limit int) ([]Event, research.RunStatus, error) {
	var status research.RunStatus
	if err := s.pool.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1`, runID).Scan(&status); err != nil {
		return nil, "", mapErr(err)
	}
	events, err := collect[Event](s.pool.Query(ctx, `
		SELECT id, run_id, event_type, stage, message, payload, created_at FROM events
		WHERE run_id = $1 AND id > $2 ORDER BY id LIMIT $3`, runID, after, limit))
	return events, status, err
}
