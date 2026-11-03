package store

import (
	"context"
	"fmt"
)

const patternCols = `id, parent_pattern_id, source_kind, source_ref, title, vulnerability_class, cwe_id,
	language, root_cause, preconditions, lifecycle_points, code_signatures, counter_signatures,
	patch_semantics, version, validation_status, created_by, reviewed_by, reviewed_at, created_at, self_reviewed, origin`

type PatternInput struct {
	SourceKind         string
	SourceRef          string
	Title              string
	VulnerabilityClass string
	CWEID              string
	Language           string
	RootCause          string
	Preconditions      []string
	LifecyclePoints    []string
	CodeSignatures     []string
	CounterSignatures  []string
	PatchSemantics     map[string]any
	// Origin is recorded for mined patterns.
	Origin map[string]any
}

func (s *Store) CreatePattern(ctx context.Context, in PatternInput, createdBy int64) (Pattern, error) {
	if in.PatchSemantics == nil {
		in.PatchSemantics = map[string]any{}
	}
	if in.Origin == nil {
		in.Origin = map[string]any{}
	}
	p, err := one[Pattern](s.pool.Query(ctx, `
		INSERT INTO patterns(source_kind, source_ref, title, vulnerability_class, cwe_id, language,
		                     root_cause, preconditions, lifecycle_points, code_signatures,
		                     counter_signatures, patch_semantics, created_by, origin)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING `+patternCols,
		in.SourceKind, in.SourceRef, in.Title, in.VulnerabilityClass, in.CWEID, in.Language,
		in.RootCause, nonNil(in.Preconditions), nonNil(in.LifecyclePoints), nonNil(in.CodeSignatures),
		nonNil(in.CounterSignatures), in.PatchSemantics, createdBy, in.Origin))
	return p, mapErr(err)
}

// SearchPatterns matches q against title, root cause, CWE and source
// reference, optionally filtered by class and language.
func (s *Store) SearchPatterns(ctx context.Context, q, class, language string) ([]Pattern, error) {
	return collect[Pattern](s.pool.Query(ctx, `
		SELECT `+patternCols+` FROM patterns
		WHERE ($1 = '' OR title ILIKE '%' || $1 || '%' OR root_cause ILIKE '%' || $1 || '%'
		       OR cwe_id ILIKE $1 OR source_ref ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR vulnerability_class = $2)
		  AND ($3 = '' OR language = $3)
		ORDER BY id DESC LIMIT 200`, q, class, language))
}

// ReviewPattern records a human review. "validated" additionally requires
// at least one validated hypothesis derived from the pattern. Reviewing a
// pattern you authored needs allowSelf (the policy allows self-review) and is recorded as
// self_reviewed; the schema refuses it otherwise.
func (s *Store) ReviewPattern(ctx context.Context, id int64, status string, reviewer int64, allowSelf bool) (Pattern, error) {
	var author *int64
	if err := s.pool.QueryRow(ctx, `SELECT created_by FROM patterns WHERE id = $1`, id).Scan(&author); err != nil {
		return Pattern{}, mapErr(err)
	}
	self := author != nil && *author == reviewer
	if self && !allowSelf {
		return Pattern{}, fmt.Errorf("%w: another reviewer must review a pattern you authored", ErrForbidden)
	}
	p, err := one[Pattern](s.pool.Query(ctx, `
		UPDATE patterns SET validation_status = $2, reviewed_by = $3, reviewed_at = now(), self_reviewed = $4
		WHERE id = $1
		  AND ($2 <> 'validated' OR EXISTS (
		        SELECT 1 FROM hypotheses h WHERE h.pattern_id = patterns.id AND h.status = 'validated'))
		RETURNING `+patternCols, id, status, reviewer, self))
	if err == ErrNotFound {
		if _, getErr := one[Pattern](s.pool.Query(ctx, `SELECT `+patternCols+` FROM patterns WHERE id = $1`, id)); getErr == nil {
			return Pattern{}, fmt.Errorf("%w: pattern has no validated finding to support validation", ErrConflict)
		}
	}
	return p, err
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
