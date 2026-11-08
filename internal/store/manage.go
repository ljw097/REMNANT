package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Management of targets, runs and patterns after they are created: edit,
// refresh, delete. Each refuses what would break a run in progress or erase
// an audit record.

// TargetUse counts the runs that reference a target, and those that will
// still read its tree (busy).
type TargetUse struct {
	Runs int64 `json:"runs"`
	Busy int64 `json:"busy"`
}

// busyRun matches runs that will read a target's tree again and verify it
// against the digest they were pinned to: runs working (or paused within)
// the source stages, and lab work. A run waiting for review or for its
// report does not; a lab request made after a re-pin is refused as stale by
// its scope hash.
const busyRun = `status IN ('queued','collecting','indexing','surface_mapping','hypothesis_generation',
	'static_validation','paused','validation_approved','lab_validation')`

func (s *Store) TargetUse(ctx context.Context, id int64) (TargetUse, error) {
	var u TargetUse
	err := s.pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE r.`+busyRun+`)
		FROM run_targets rt JOIN runs r ON r.id = rt.run_id WHERE rt.target_id = $1`, id).Scan(&u.Runs, &u.Busy)
	return u, err
}

// TargetPatch edits a target's labels. Nil fields are left as they are.
type TargetPatch struct {
	DisplayName     *string
	PrimaryLanguage *string
}

func (s *Store) UpdateTarget(ctx context.Context, id int64, p TargetPatch) (Target, error) {
	return one[Target](s.pool.Query(ctx, `
		UPDATE targets SET display_name = COALESCE($2, display_name), primary_language = COALESCE($3, primary_language)
		WHERE id = $1 RETURNING `+targetCols, id, p.DisplayName, p.PrimaryLanguage))
}

// RepinLocalTarget records a local snapshot's current revision and digest,
// after its files changed. It refuses while an unfinished run uses the
// target in a way that reads its tree again (see busyRun).
func (s *Store) RepinLocalTarget(ctx context.Context, id int64, revision, digest string) (Target, error) {
	var t Target
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := refuseIfBusy(ctx, tx, id); err != nil {
			return err
		}
		var err error
		t, err = one[Target](tx.Query(ctx, `
			UPDATE targets SET revision = $2, tree_sha256 = $3, status = 'ready',
			       metadata = metadata - 'fetch_error' || jsonb_build_object('repinned_at', now())
			WHERE id = $1 AND kind <> 'git_repository' RETURNING `+targetCols, id, revision, digest))
		return err
	})
	return t, err
}

// RequeueGitTarget points a git target at a new snapshot key (and optionally
// a new ref) and queues it for fetching, so the fetcher pins the ref's
// latest commit. The previous key is returned so its directory can be
// removed. Refused while a busy run uses the target (see busyRun).
func (s *Store) RequeueGitTarget(ctx context.Context, id int64, newKey string, ref *string) (Target, string, error) {
	var t Target
	var oldKey string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := refuseIfBusy(ctx, tx, id); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT snapshot_key FROM targets WHERE id = $1 AND kind = 'git_repository' FOR UPDATE`, id).Scan(&oldKey); err != nil {
			return mapErr(err)
		}
		var err error
		t, err = one[Target](tx.Query(ctx, `
			UPDATE targets SET snapshot_key = $2, source_ref = COALESCE($3, source_ref), status = 'pending',
			       fetch_owner = '', fetch_expires_at = NULL,
			       metadata = metadata - 'fetch_error' || jsonb_build_object('refetch_requested_at', now())
			WHERE id = $1 RETURNING `+targetCols, id, newKey, ref))
		return err
	})
	return t, oldKey, err
}

func refuseIfBusy(ctx context.Context, tx pgx.Tx, targetID int64) error {
	var n int64
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM run_targets rt JOIN runs r ON r.id = rt.run_id
		WHERE rt.target_id = $1 AND r.`+busyRun, targetID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: %d run(s) are still reading this target (working, paused or in lab validation); let them finish or cancel them first", ErrConflict, n)
	}
	return nil
}

// DeleteTarget removes a target no run has used (runs keep their evidence
// pinned to it). Its intakes and commit records go with it.
func (s *Store) DeleteTarget(ctx context.Context, id int64) (Target, error) {
	var t Target
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		if t, err = one[Target](tx.Query(ctx, `SELECT `+targetCols+` FROM targets WHERE id = $1 FOR UPDATE`, id)); err != nil {
			return err
		}
		var runs int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM run_targets WHERE target_id = $1`, id).Scan(&runs); err != nil {
			return err
		}
		if runs > 0 {
			return fmt.Errorf("%w: %d run(s) used this target and keep their evidence pinned to it; delete those runs first or archive the project", ErrConflict, runs)
		}
		_, err = tx.Exec(ctx, `DELETE FROM targets WHERE id = $1`, id)
		return mapErr(err)
	})
	return t, err
}

// SnapshotKeyShared reports whether another target uses the snapshot key,
// so its directory must stay.
func (s *Store) SnapshotKeyShared(ctx context.Context, key string, exceptID int64) (bool, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM targets WHERE snapshot_key = $1 AND id <> $2`, key, exceptID).Scan(&n)
	return n > 0, err
}

// DeleteRun removes a finished run with everything it produced (hypotheses,
// evidence, events, artifacts records, model calls). A run with lab
// approvals is kept: approvals are the audit trail of code that was allowed
// to execute.
func (s *Store) DeleteRun(ctx context.Context, id int64) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
			return mapErr(err)
		}
		switch status {
		case "completed", "cancelled", "failed", "timed_out":
		default:
			return fmt.Errorf("%w: a %s run cannot be deleted; cancel it first", ErrConflict, status)
		}
		var approvals int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM approvals WHERE run_id = $1`, id).Scan(&approvals); err != nil {
			return err
		}
		if approvals > 0 {
			return fmt.Errorf("%w: the run has %d lab approval(s), which are kept as the audit trail of code allowed to run", ErrConflict, approvals)
		}
		_, err := tx.Exec(ctx, `DELETE FROM runs WHERE id = $1`, id)
		return mapErr(err)
	})
}

// PatternPatch edits a pattern's content. Nil fields are left as they are.
type PatternPatch struct {
	Title, VulnerabilityClass, CWEID, Language, RootCause *string
	Preconditions, CodeSignatures, CounterSignatures      *[]string
}

// UpdatePattern edits an unreviewed pattern. A reviewed or validated
// pattern is not changed under its reviewers; create a new one instead.
func (s *Store) UpdatePattern(ctx context.Context, id int64, p PatternPatch) (Pattern, error) {
	js := func(v *[]string) any {
		if v == nil {
			return nil
		}
		b, _ := json.Marshal(nonNil(*v))
		return string(b)
	}
	var out Pattern
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT validation_status FROM patterns WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
			return mapErr(err)
		}
		if status != "unreviewed" {
			return fmt.Errorf("%w: a %s pattern cannot be edited; create a new pattern instead", ErrConflict, status)
		}
		var err error
		out, err = one[Pattern](tx.Query(ctx, `
			UPDATE patterns SET
			  title = COALESCE($2, title), vulnerability_class = COALESCE($3, vulnerability_class),
			  cwe_id = COALESCE($4, cwe_id), language = COALESCE($5, language), root_cause = COALESCE($6, root_cause),
			  preconditions = COALESCE($7::jsonb, preconditions), code_signatures = COALESCE($8::jsonb, code_signatures),
			  counter_signatures = COALESCE($9::jsonb, counter_signatures)
			WHERE id = $1 RETURNING `+patternCols,
			id, p.Title, p.VulnerabilityClass, p.CWEID, p.Language, p.RootCause, js(p.Preconditions), js(p.CodeSignatures), js(p.CounterSignatures)))
		return err
	})
	return out, err
}

// DeletePattern removes a pattern no run has used. Hypotheses keep their
// text; their pattern reference is cleared.
func (s *Store) DeletePattern(ctx context.Context, id int64) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT true FROM patterns WHERE id = $1 FOR UPDATE`, id).Scan(&exists); err != nil {
			return mapErr(err)
		}
		var runs int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM run_patterns WHERE pattern_id = $1`, id).Scan(&runs); err != nil {
			return err
		}
		if runs > 0 {
			return fmt.Errorf("%w: %d run(s) used this pattern; it stays as their record", ErrConflict, runs)
		}
		_, err := tx.Exec(ctx, `DELETE FROM patterns WHERE id = $1`, id)
		return mapErr(err)
	})
}
