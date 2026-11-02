package store

import (
	"context"

	"github.com/ljw097/remnant/internal/policy"
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
	revision, tree_sha256, primary_language, status, metadata, created_at`

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
}

func (s *Store) CreateTarget(ctx context.Context, projectID int64, in TargetInput) (Target, error) {
	t, err := one[Target](s.pool.Query(ctx, `
		INSERT INTO targets(project_id, kind, display_name, source_uri, source_ref, snapshot_key,
		                    revision, tree_sha256, primary_language, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+targetCols,
		projectID, in.Kind, in.DisplayName, in.SourceURI, in.SourceRef, in.SnapshotKey,
		in.Revision, in.TreeSHA256, in.PrimaryLanguage, in.Status))
	return t, mapErr(err)
}

func (s *Store) ListTargets(ctx context.Context, projectID int64) ([]Target, error) {
	return collect[Target](s.pool.Query(ctx, `
		SELECT `+targetCols+` FROM targets WHERE project_id = $1 ORDER BY created_at DESC`, projectID))
}

func (s *Store) GetTarget(ctx context.Context, id int64) (Target, error) {
	return one[Target](s.pool.Query(ctx, `SELECT `+targetCols+` FROM targets WHERE id = $1`, id))
}
