package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ljw097/remnant/internal/policy"
	"github.com/ljw097/remnant/internal/research"
)

// IntakeOptions say how far an intake looks into history and what the run
// it starts may spend.
type IntakeOptions struct {
	// MineHistory reads the repository's history for security fixes and
	// drafts patterns from them.
	MineHistory bool `json:"mine_history"`
	// HistoryDepth is how many recent commits are scanned (10–5000).
	HistoryDepth int `json:"history_depth"`
	// MaxCommits is how many fix candidates the Historian reads (1–40).
	MaxCommits int `json:"max_commits"`
	// MaxPatterns caps the patterns drafted (1–30).
	MaxPatterns int `json:"max_patterns"`
	// MaxTokens is the Historian's output-token budget (4000–1000000).
	MaxTokens int `json:"max_tokens"`
	// IncludePatterns are existing patterns the run also hunts for.
	IncludePatterns []int64 `json:"include_patterns"`
	// Limits are the run's limits; zero fields take the run defaults.
	Limits research.Limits `json:"limits"`
	// Auto runs the hunt on autopilot: it triages its own hypotheses and
	// finalises a report without a person.
	Auto bool `json:"auto"`
	// AutoDiscover also audits the source directly (general mode), so the hunt
	// finds vulnerabilities even when history yields no pattern.
	AutoDiscover bool `json:"auto_discover"`
}

func DefaultIntakeOptions() IntakeOptions {
	return IntakeOptions{MineHistory: true, HistoryDepth: 500, MaxCommits: 12, MaxPatterns: 8, MaxTokens: 150000, IncludePatterns: []int64{}, Auto: true, AutoDiscover: true}
}

// Normalize fills zero fields with defaults and checks ranges.
func (o IntakeOptions) Normalize() (IntakeOptions, error) {
	d := DefaultIntakeOptions()
	if o.HistoryDepth == 0 {
		o.HistoryDepth = d.HistoryDepth
	}
	if o.MaxCommits == 0 {
		o.MaxCommits = d.MaxCommits
	}
	if o.MaxPatterns == 0 {
		o.MaxPatterns = d.MaxPatterns
	}
	if o.MaxTokens == 0 {
		o.MaxTokens = d.MaxTokens
	}
	if o.IncludePatterns == nil {
		o.IncludePatterns = []int64{}
	}
	switch {
	case o.HistoryDepth < 10 || o.HistoryDepth > 5000:
		return o, fmt.Errorf("%w: history_depth must be 10–5000", ErrInvalid)
	case o.MaxCommits < 1 || o.MaxCommits > 40:
		return o, fmt.Errorf("%w: max_commits must be 1–40", ErrInvalid)
	case o.MaxPatterns < 1 || o.MaxPatterns > 30:
		return o, fmt.Errorf("%w: max_patterns must be 1–30", ErrInvalid)
	case o.MaxTokens < 4000 || o.MaxTokens > 1000000:
		return o, fmt.Errorf("%w: max_tokens must be 4000–1000000", ErrInvalid)
	case len(o.IncludePatterns) > 100:
		return o, fmt.Errorf("%w: include_patterns may list at most 100 patterns", ErrInvalid)
	case !o.MineHistory && len(o.IncludePatterns) == 0:
		return o, fmt.Errorf("%w: without history mining, choose at least one existing pattern to hunt for", ErrInvalid)
	}
	return o, nil
}

// Intake statuses.
const (
	IntakeFetching = "fetching"
	IntakeMining   = "mining"
	IntakeStarted  = "started"
	IntakeFailed   = "failed"
)

type Intake struct {
	ID           int64         `db:"id" json:"id"`
	SourceURI    string        `db:"source_uri" json:"source_uri"`
	SourceRef    string        `db:"source_ref" json:"source_ref"`
	ProjectID    int64         `db:"project_id" json:"project_id"`
	ProjectName  string        `db:"project_name" json:"project_name"`
	TargetID     int64         `db:"target_id" json:"target_id"`
	TargetStatus string        `db:"target_status" json:"target_status"`
	Revision     string        `db:"revision" json:"revision"`
	RunID        *int64        `db:"run_id" json:"run_id"`
	LLMProfileID *int64        `db:"llm_profile_id" json:"llm_profile_id"`
	Status       string        `db:"status" json:"status"`
	Options      IntakeOptions `db:"options" json:"options"`
	CommitsFound int           `db:"commits_found" json:"commits_found"`
	CommitsRead  int           `db:"commits_read" json:"commits_read"`
	Patterns     []int64       `db:"patterns" json:"patterns"`
	InputTokens  int64         `db:"input_tokens" json:"input_tokens"`
	OutputTokens int64         `db:"output_tokens" json:"output_tokens"`
	ErrorSummary string        `db:"error_summary" json:"error_summary"`
	CreatedBy    *int64        `db:"created_by" json:"created_by"`
	CreatedAt    time.Time     `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time     `db:"updated_at" json:"updated_at"`
}

const intakeSelect = `
	SELECT i.id, i.source_uri, i.source_ref, i.project_id, p.name AS project_name, i.target_id,
	       t.status AS target_status, t.revision, i.run_id, i.llm_profile_id, i.status, i.options,
	       i.commits_found, i.commits_read, i.patterns, i.input_tokens, i.output_tokens, i.error_summary,
	       i.created_by, i.created_at, i.updated_at
	FROM intakes i JOIN projects p ON p.id = i.project_id JOIN targets t ON t.id = i.target_id`

// IntakeInput starts an intake. Without ProjectID a project is created,
// named ProjectName, with the default read-only, no-network policy.
type IntakeInput struct {
	SourceURI    string // canonical https URL
	SourceRef    string
	ProjectID    *int64
	ProjectName  string
	LLMProfileID *int64
	Options      IntakeOptions
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// snapshotKeyFor names an intake's checkout: the repository's last path
// segments plus a random suffix, so the same repository can be taken in
// again.
func snapshotKeyFor(uri string) string {
	parts := strings.Split(strings.TrimSuffix(strings.TrimSuffix(uri, "/"), ".git"), "/")
	name := parts[len(parts)-1]
	if len(parts) >= 2 {
		name = parts[len(parts)-2] + "-" + name
	}
	slug := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(slug) > 80 {
		slug = slug[:80]
	}
	if slug == "" {
		slug = "repo"
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return slug + "-" + hex.EncodeToString(b)
}

// CreateIntake records an intake with its project and pending git target;
// the orchestrator's fetcher picks the target up.
func (s *Store) CreateIntake(ctx context.Context, in IntakeInput, by int64) (Intake, error) {
	opts, err := in.Options.Normalize()
	if err != nil {
		return Intake{}, err
	}
	depth := 0
	if opts.MineHistory {
		depth = opts.HistoryDepth
	}
	var id int64
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		projectID := int64(0)
		if in.ProjectID != nil {
			projectID = *in.ProjectID
			if err := tx.QueryRow(ctx, `SELECT id FROM projects WHERE id = $1 AND status <> 'archived'`, projectID).Scan(&projectID); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return fmt.Errorf("%w: unknown project", ErrInvalid)
				}
				return err
			}
		} else {
			if err := tx.QueryRow(ctx, `
				INSERT INTO projects(name, description, llm_profile_id, policy, created_by)
				VALUES ($1, $2, $3, $4, $5) RETURNING id`,
				in.ProjectName, "Created from "+in.SourceURI, in.LLMProfileID, policy.Default(), by).Scan(&projectID); err != nil {
				return mapErr(err)
			}
		}
		if len(opts.IncludePatterns) > 0 {
			var usable int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM patterns WHERE id = ANY($1) AND validation_status <> 'rejected'`,
				opts.IncludePatterns).Scan(&usable); err != nil {
				return err
			}
			if usable != len(opts.IncludePatterns) {
				return fmt.Errorf("%w: unknown or rejected pattern", ErrInvalid)
			}
		}
		display := strings.TrimPrefix(in.SourceURI, "https://")
		if in.SourceRef != "" {
			display += "@" + in.SourceRef
		}
		var targetID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO targets(project_id, kind, display_name, source_uri, source_ref, snapshot_key, status, history_depth)
			VALUES ($1, 'git_repository', $2, $3, $4, $5, 'pending', $6) RETURNING id`,
			projectID, display, in.SourceURI, in.SourceRef, snapshotKeyFor(in.SourceURI), depth).Scan(&targetID); err != nil {
			return mapErr(err)
		}
		return tx.QueryRow(ctx, `
			INSERT INTO intakes(source_uri, source_ref, project_id, target_id, llm_profile_id, options, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
			in.SourceURI, in.SourceRef, projectID, targetID, in.LLMProfileID, opts, by).Scan(&id)
	})
	if err != nil {
		return Intake{}, err
	}
	return s.GetIntake(ctx, id)
}

func (s *Store) GetIntake(ctx context.Context, id int64) (Intake, error) {
	i, err := one[Intake](s.pool.Query(ctx, intakeSelect+` WHERE i.id = $1`, id))
	return i, mapErr(err)
}

func (s *Store) ListIntakes(ctx context.Context, limit int) ([]Intake, error) {
	return collect[Intake](s.pool.Query(ctx, intakeSelect+` ORDER BY i.created_at DESC LIMIT $1`, limit))
}

// ClaimIntake leases the oldest intake that can move on: its target has
// been fetched (or failed), or it is mining. ErrNotFound when there is none.
func (s *Store) ClaimIntake(ctx context.Context, owner string, ttl time.Duration) (Intake, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		UPDATE intakes SET lease_owner = $1, lease_expires_at = now() + $2 * interval '1 second'
		WHERE id = (
			SELECT i.id FROM intakes i JOIN targets t ON t.id = i.target_id
			WHERE i.status IN ('fetching', 'mining')
			  AND (i.lease_expires_at IS NULL OR i.lease_expires_at < now())
			  AND (i.status = 'mining' OR t.status <> 'pending')
			ORDER BY i.created_at
			FOR UPDATE OF i SKIP LOCKED
			LIMIT 1)
		RETURNING id`, owner, ttl.Seconds()).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Intake{}, ErrNotFound
	}
	if err != nil {
		return Intake{}, err
	}
	return s.GetIntake(ctx, id)
}

// RenewIntake extends a lease; it fails if the lease was lost.
func (s *Store) RenewIntake(ctx context.Context, id int64, owner string, ttl time.Duration) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE intakes SET lease_expires_at = now() + $3 * interval '1 second'
		WHERE id = $1 AND lease_owner = $2`, id, owner, ttl.Seconds())
	if err == nil && tag.RowsAffected() == 0 {
		err = fmt.Errorf("%w: intake %d lease lost", ErrConflict, id)
	}
	return err
}

// IntakeProgress records how far mining got. It keeps the lease.
type IntakeProgress struct {
	Status       string
	CommitsFound *int
	CommitsRead  *int
	Patterns     []int64
	InputTokens  int64
	OutputTokens int64
	RunID        *int64
	Error        string
}

func (s *Store) UpdateIntake(ctx context.Context, id int64, owner string, p IntakeProgress) error {
	if len(p.Error) > 1000 {
		p.Error = p.Error[:1000]
	}
	release := p.Status == IntakeStarted || p.Status == IntakeFailed
	tag, err := s.pool.Exec(ctx, `
		UPDATE intakes SET
			status        = COALESCE(NULLIF($3, ''), status),
			commits_found = COALESCE($4, commits_found),
			commits_read  = COALESCE($5, commits_read),
			patterns      = CASE WHEN $6::bigint[] IS NULL THEN patterns ELSE $6 END,
			input_tokens  = input_tokens + $7,
			output_tokens = output_tokens + $8,
			run_id        = COALESCE($9, run_id),
			error_summary = CASE WHEN $10 = '' THEN error_summary ELSE $10 END,
			lease_owner   = CASE WHEN $11 THEN '' ELSE lease_owner END,
			lease_expires_at = CASE WHEN $11 THEN NULL ELSE lease_expires_at END,
			updated_at    = now()
		WHERE id = $1 AND lease_owner = $2`,
		id, owner, p.Status, p.CommitsFound, p.CommitsRead, p.Patterns, p.InputTokens, p.OutputTokens, p.RunID, p.Error, release)
	if err == nil && tag.RowsAffected() == 0 {
		err = fmt.Errorf("%w: intake %d lease lost", ErrConflict, id)
	}
	return err
}

// CommitRecord is a fix candidate from a target's history.
type CommitRecord struct {
	ID          int64     `db:"id" json:"id"`
	TargetID    int64     `db:"target_id" json:"target_id"`
	SHA         string    `db:"sha" json:"sha"`
	CommittedAt time.Time `db:"committed_at" json:"committed_at"`
	Subject     string    `db:"subject" json:"subject"`
	Body        string    `db:"body" json:"body"`
	Files       []string  `db:"files" json:"files"`
	Diff        string    `db:"diff" json:"diff,omitempty"`
	Signals     []string  `db:"signals" json:"signals"`
	Score       int       `db:"score" json:"score"`
}

// SaveCommitRecords stores a fetch's fix candidates, once per commit.
func (s *Store) SaveCommitRecords(ctx context.Context, targetID int64, cs []CommitRecord) error {
	if len(cs) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, c := range cs {
		b.Queue(`
			INSERT INTO commit_records(target_id, sha, committed_at, subject, body, files, diff, signals, score)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT (target_id, sha) DO NOTHING`,
			targetID, c.SHA, c.CommittedAt, c.Subject, c.Body, nonNil(c.Files), c.Diff, nonNil(c.Signals), c.Score)
	}
	return s.pool.SendBatch(ctx, b).Close()
}

// CommitRecords lists a target's fix candidates, strongest first. withDiff
// includes the diffs.
func (s *Store) CommitRecords(ctx context.Context, targetID int64, limit int, withDiff bool) ([]CommitRecord, error) {
	diff := "''"
	if withDiff {
		diff = "diff"
	}
	return collect[CommitRecord](s.pool.Query(ctx, `
		SELECT id, target_id, sha, committed_at, subject, body, files, `+diff+` AS diff, signals, score
		FROM commit_records WHERE target_id = $1
		ORDER BY score DESC, committed_at DESC LIMIT $2`, targetID, limit))
}
