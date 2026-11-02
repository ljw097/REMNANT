package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Preferences returns a user's UI preferences as a JSON object ({} when the
// user never saved any).
func (s *Store) Preferences(ctx context.Context, userID int64) (json.RawMessage, error) {
	var prefs json.RawMessage
	err := s.pool.QueryRow(ctx, `SELECT prefs FROM user_preferences WHERE user_id = $1`, userID).Scan(&prefs)
	if errors.Is(err, pgx.ErrNoRows) {
		return json.RawMessage(`{}`), nil
	}
	return prefs, err
}

// SetPreferences replaces a user's UI preferences. The caller validates the
// shape; the database only insists on a JSON object.
func (s *Store) SetPreferences(ctx context.Context, userID int64, prefs json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_preferences(user_id, prefs) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET prefs = EXCLUDED.prefs, updated_at = now()`, userID, prefs)
	return mapErr(err)
}

// Setting reads an instance setting into v. It reports false when unset.
func (s *Store) Setting(ctx context.Context, key string, v any) (bool, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT value FROM instance_settings WHERE key = $1`, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(raw, v)
}

func (s *Store) SetSetting(ctx context.Context, key string, v any, by int64) error {
	// Marshal explicitly: a bare Go string would otherwise be sent as text,
	// which is not valid JSON.
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO instance_settings(key, value, updated_by) VALUES ($1, $2, NULLIF($3, 0))
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		key, raw, by)
	return mapErr(err)
}

// ApprovalItem is an approval with what a reviewer needs to recognise it.
type ApprovalItem struct {
	Approval
	ProjectID       int64  `db:"project_id" json:"project_id"`
	HypothesisTitle string `db:"hypothesis_title" json:"hypothesis_title"`
	RequestedByName string `db:"requested_by_name" json:"requested_by_name"`
}

// ListApprovals lists approvals, newest first, optionally by decision.
func (s *Store) ListApprovals(ctx context.Context, decision string) ([]ApprovalItem, error) {
	return collect[ApprovalItem](s.pool.Query(ctx, `
		SELECT a.id, a.run_id, a.hypothesis_id, a.target_id, a.action_class, a.target_revision, a.scope, a.scope_hash,
		       a.request_summary, a.decision, a.requested_by, a.decided_by, a.reviewer_note, a.decided_at,
		       a.expires_at, a.created_at, a.self_approved,
		       r.project_id, h.title AS hypothesis_title, u.username AS requested_by_name
		FROM approvals a
		JOIN runs r ON r.id = a.run_id
		JOIN hypotheses h ON h.id = a.hypothesis_id
		JOIN users u ON u.id = a.requested_by
		WHERE ($1 = '' OR a.decision = $1)
		ORDER BY a.created_at DESC LIMIT 200`, decision))
}

// HypothesisItem is a hypothesis listed across runs.
type HypothesisItem struct {
	Hypothesis
	ProjectID int64 `db:"project_id" json:"project_id"`
}

// SearchHypotheses lists hypotheses across runs with one of statuses (all
// when empty), newest first.
func (s *Store) SearchHypotheses(ctx context.Context, statuses []string, limit int) ([]HypothesisItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	return collect[HypothesisItem](s.pool.Query(ctx, `
		SELECT `+hypothesisCols+`, r.project_id
		FROM hypotheses h JOIN runs r ON r.id = h.run_id
		WHERE (cardinality($1::text[]) = 0 OR h.status = ANY($1))
		ORDER BY h.updated_at DESC LIMIT $2`, statuses, limit))
}

// Summary holds the counts behind the dashboard.
type Summary struct {
	Projects          int64            `json:"projects"`
	Targets           int64            `json:"targets"`
	ActiveRuns        int64            `json:"active_runs"`
	PausedRuns        int64            `json:"paused_runs"`
	AwaitingTriage    int64            `json:"awaiting_triage"`
	PendingApprovals  int64            `json:"pending_approvals"`
	ValidationPending int64            `json:"validation_pending"`
	Validated         int64            `json:"validated"`
	RunsByStatus      map[string]int64 `json:"runs_by_status"`
	GeneratedAt       time.Time        `json:"generated_at"`
}

func (s *Store) Summary(ctx context.Context) (Summary, error) {
	sum := Summary{RunsByStatus: map[string]int64{}, GeneratedAt: time.Now().UTC()}
	err := s.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM projects WHERE status <> 'archived'),
		       (SELECT count(*) FROM targets),
		       (SELECT count(*) FROM runs WHERE status NOT IN ('completed','cancelled','failed','timed_out','paused','review_required')),
		       (SELECT count(*) FROM runs WHERE status = 'paused'),
		       (SELECT count(*) FROM hypotheses WHERE status IN ('draft','triage')),
		       (SELECT count(*) FROM approvals WHERE decision = 'pending'),
		       (SELECT count(*) FROM hypotheses WHERE status = 'validation_pending'),
		       (SELECT count(*) FROM hypotheses WHERE status = 'validated')`).
		Scan(&sum.Projects, &sum.Targets, &sum.ActiveRuns, &sum.PausedRuns, &sum.AwaitingTriage,
			&sum.PendingApprovals, &sum.ValidationPending, &sum.Validated)
	if err != nil {
		return sum, err
	}
	rows, err := s.pool.Query(ctx, `SELECT status, count(*) FROM runs GROUP BY status`)
	if err != nil {
		return sum, err
	}
	var st string
	var n int64
	_, err = pgx.ForEachRow(rows, []any{&st, &n}, func() error { sum.RunsByStatus[st] = n; return nil })
	return sum, err
}

// UserRef is the public view of a user: enough to show who did what.
type UserRef struct {
	ID       int64  `db:"id" json:"id"`
	Username string `db:"username" json:"username"`
	Role     string `db:"role" json:"role"`
	Disabled bool   `db:"disabled" json:"disabled"`
}

func (s *Store) ListUsers(ctx context.Context) ([]UserRef, error) {
	return collect[UserRef](s.pool.Query(ctx, `SELECT id, username, role, disabled FROM users ORDER BY username`))
}

func (s *Store) GetPattern(ctx context.Context, id int64) (Pattern, error) {
	return one[Pattern](s.pool.Query(ctx, `SELECT `+patternCols+` FROM patterns WHERE id = $1`, id))
}
