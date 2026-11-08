package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ljw097/remnant/internal/policy"

	"github.com/jackc/pgx/v5"
)

const projectSelect = `
	SELECT p.id, p.name, p.description, p.status, p.llm_profile_id, p.policy,
	       (SELECT count(*) FROM targets t WHERE t.project_id = p.id) AS target_count,
	       (SELECT count(*) FROM runs r WHERE r.project_id = p.id
	          AND r.status NOT IN ('completed','cancelled','failed','timed_out')) AS active_runs,
	       p.created_at, p.updated_at
	FROM projects p`

type ProjectInput struct {
	Name         string
	Description  string
	LLMProfileID *int64
	Policy       policy.Policy
}

func (s *Store) CreateProject(ctx context.Context, in ProjectInput, createdBy int64) (Project, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO projects(name, description, llm_profile_id, policy, created_by)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		in.Name, in.Description, in.LLMProfileID, in.Policy, createdBy).Scan(&id)
	if err != nil {
		return Project{}, mapErr(err)
	}
	return s.GetProject(ctx, id)
}

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	return collect[Project](s.pool.Query(ctx, projectSelect+` ORDER BY p.updated_at DESC`))
}

func (s *Store) GetProject(ctx context.Context, id int64) (Project, error) {
	return one[Project](s.pool.Query(ctx, projectSelect+` WHERE p.id = $1`, id))
}

// ProjectPatch updates only non-nil fields. SetLLMProfile distinguishes
// "clear the profile" (true, nil) from "leave unchanged" (false).
type ProjectPatch struct {
	Name          *string
	Description   *string
	Status        *string
	SetLLMProfile bool
	LLMProfileID  *int64
	Policy        *policy.Policy
}

func (s *Store) UpdateProject(ctx context.Context, id int64, p ProjectPatch) (Project, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE projects SET
			name           = COALESCE($2, name),
			description    = COALESCE($3, description),
			status         = COALESCE($4, status),
			llm_profile_id = CASE WHEN $5 THEN $6 ELSE llm_profile_id END,
			policy         = COALESCE($7, policy),
			updated_at     = now()
		WHERE id = $1`,
		id, p.Name, p.Description, p.Status, p.SetLLMProfile, p.LLMProfileID, p.Policy)
	if err != nil {
		return Project{}, mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return Project{}, ErrNotFound
	}
	return s.GetProject(ctx, id)
}

const targetCols = `id, project_id, kind, display_name, source_uri, source_ref, snapshot_key,
	revision, tree_sha256, primary_language, status, metadata, history_depth, created_at`

type TargetInput struct {
	Kind            string
	DisplayName     string
	SourceURI       string
	SourceRef       string
	SnapshotKey     string
	Revision        string
	TreeSHA256      string
	PrimaryLanguage string
	Status          string
	// HistoryDepth asks the fetch to keep fix candidates from this many
	// recent commits (git targets only).
	HistoryDepth int
}

// CreateTarget adds a target to a project that is not archived (an archived
// project is read-only: it takes neither targets nor runs).
func (s *Store) CreateTarget(ctx context.Context, projectID int64, in TargetInput) (Target, error) {
	var t Target
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM projects WHERE id = $1 FOR SHARE`, projectID).Scan(&status); err != nil {
			return mapErr(err)
		}
		if status == "archived" {
			return fmt.Errorf("%w: project is archived", ErrConflict)
		}
		var err error
		t, err = one[Target](tx.Query(ctx, `
			INSERT INTO targets(project_id, kind, display_name, source_uri, source_ref, snapshot_key,
			                    revision, tree_sha256, primary_language, status, history_depth)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING `+targetCols,
			projectID, in.Kind, in.DisplayName, in.SourceURI, in.SourceRef, in.SnapshotKey,
			in.Revision, in.TreeSHA256, in.PrimaryLanguage, in.Status, in.HistoryDepth))
		return err
	})
	return t, err
}

func (s *Store) ListTargets(ctx context.Context, projectID int64) ([]Target, error) {
	return collect[Target](s.pool.Query(ctx, `
		SELECT `+targetCols+` FROM targets WHERE project_id = $1 ORDER BY created_at DESC`, projectID))
}

func (s *Store) GetTarget(ctx context.Context, id int64) (Target, error) {
	return one[Target](s.pool.Query(ctx, `SELECT `+targetCols+` FROM targets WHERE id = $1`, id))
}

// ClaimPendingFetch leases the oldest git_repository target waiting to be
// pinned. It returns ErrNotFound when there is none.
func (s *Store) ClaimPendingFetch(ctx context.Context, owner string, ttl time.Duration) (Target, error) {
	return one[Target](s.pool.Query(ctx, `
		UPDATE targets SET fetch_owner = $1, fetch_expires_at = now() + $2 * interval '1 second'
		WHERE id = (
			SELECT id FROM targets
			WHERE kind = 'git_repository' AND status = 'pending'
			  AND (fetch_expires_at IS NULL OR fetch_expires_at < now())
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1)
		RETURNING `+targetCols, owner, ttl.Seconds()))
}

// CompleteFetch pins a fetched target to its commit and tree digest.
func (s *Store) CompleteFetch(ctx context.Context, id int64, owner, commit, digest string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE targets SET status = 'ready', revision = $3, tree_sha256 = $4, fetch_owner = '', fetch_expires_at = NULL,
		       metadata = metadata - 'fetch_error' || jsonb_build_object('fetched_at', now())
		WHERE id = $1 AND fetch_owner = $2 AND status = 'pending'`, id, owner, commit, digest)
	if err == nil && tag.RowsAffected() == 0 {
		err = fmt.Errorf("%w: target %d is no longer leased by this fetcher", ErrConflict, id)
	}
	return err
}

// FailFetch marks a target unavailable with the reason.
func (s *Store) FailFetch(ctx context.Context, id int64, owner, reason string) error {
	if len(reason) > 1000 {
		reason = reason[:1000]
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE targets SET status = 'unavailable', fetch_owner = '', fetch_expires_at = NULL,
		       metadata = metadata || jsonb_build_object('fetch_error', $3::text)
		WHERE id = $1 AND fetch_owner = $2 AND status = 'pending'`, id, owner, reason)
	return err
}
