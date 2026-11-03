package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ljw097/remnant/internal/policy"
	"github.com/ljw097/remnant/internal/research"
)

const hypothesisCols = `h.id, h.run_id, h.target_id, h.pattern_id, h.title, h.claim, h.vulnerability_class,
	h.cwe_id, h.status, h.triage_confidence, h.skeptic_verdict, h.facts, h.preconditions, h.unknowns, h.affected_symbols,
	h.expected_observation, h.counter_evidence, h.reviewer_note, h.reviewed_by, h.reviewed_at,
	(SELECT count(*) FROM evidence e WHERE e.hypothesis_id = h.id) AS evidence_count,
	h.created_at, h.updated_at`

func (s *Store) ListHypotheses(ctx context.Context, runID int64) ([]Hypothesis, error) {
	return collect[Hypothesis](s.pool.Query(ctx, `
		SELECT `+hypothesisCols+` FROM hypotheses h WHERE h.run_id = $1 ORDER BY h.id`, runID))
}

func (s *Store) GetHypothesis(ctx context.Context, id int64) (HypothesisDetail, error) {
	h, err := one[Hypothesis](s.pool.Query(ctx, `SELECT `+hypothesisCols+` FROM hypotheses h WHERE h.id = $1`, id))
	if err != nil {
		return HypothesisDetail{}, err
	}
	ev, err := collect[Evidence](s.pool.Query(ctx, `
		SELECT id, hypothesis_id, target_id, evidence_kind, source_uri, file_path, line_start, line_end,
		       symbol, revision, content_sha256, summary, excerpt, metadata, created_at
		FROM evidence WHERE hypothesis_id = $1 ORDER BY id`, id))
	return HypothesisDetail{Hypothesis: h, Evidence: ev}, err
}

// TriageHypothesis records a human decision. validated requires a lab
// experiment for the hypothesis that ended in "finding"; not_reproducible
// requires one that finished at all.
func (s *Store) TriageHypothesis(ctx context.Context, id int64, d research.TriageDecision, note string, reviewer int64) (HypothesisDetail, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var runID int64
		var status research.HypothesisStatus
		if err := tx.QueryRow(ctx, `SELECT run_id, status FROM hypotheses WHERE id = $1 FOR UPDATE`, id).Scan(&runID, &status); err != nil {
			return mapErr(err)
		}
		if !research.CanTriage(status, d) {
			return fmt.Errorf("%w: cannot record %s on a %s hypothesis", ErrConflict, d, status)
		}
		if d == research.DecisionValidated || d == research.DecisionNotReproducible {
			outcomes := []string{"finding"}
			if d == research.DecisionNotReproducible {
				outcomes = []string{"passed", "finding", "failed", "resource_limited"}
			}
			var ok bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM experiments
				               WHERE hypothesis_id = $1 AND approval_id IS NOT NULL AND status = ANY($2))`,
				id, outcomes).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%w: no lab experiment supports %s", ErrConflict, d)
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE hypotheses SET status = $2, reviewer_note = $3, reviewed_by = $4, reviewed_at = now(), updated_at = now()
			WHERE id = $1`, id, string(d), note, reviewer); err != nil {
			return err
		}
		return appendEvent(ctx, tx, runID, "hypothesis.triaged", "", fmt.Sprintf("Hypothesis %d %s", id, d),
			map[string]any{"hypothesis_id": id, "decision": d})
	})
	if err != nil {
		return HypothesisDetail{}, err
	}
	return s.GetHypothesis(ctx, id)
}

const approvalCols = `id, run_id, hypothesis_id, target_id, action_class, target_revision, scope, scope_hash,
	request_summary, decision, requested_by, decided_by, reviewer_note, decided_at, expires_at, created_at, self_approved`

// labContext is everything an approval scope is derived from, read under lock.
type labContext struct {
	runID          int64
	runStatus      research.RunStatus
	runScopeHash   string
	targetID       int64
	pinnedRevision string
	currentRev     string
	dynamicMode    string
}

func loadLabContext(ctx context.Context, tx pgx.Tx, hypothesisID int64) (labContext, research.HypothesisStatus, error) {
	var c labContext
	var hs research.HypothesisStatus
	var pol policy.Policy
	err := tx.QueryRow(ctx, `
		SELECT h.status, r.id, r.status, r.scope_hash, h.target_id, rt.revision, t.revision, p.policy
		FROM hypotheses h
		JOIN runs r         ON r.id = h.run_id
		JOIN projects p     ON p.id = r.project_id
		JOIN run_targets rt ON rt.run_id = r.id AND rt.target_id = h.target_id
		JOIN targets t      ON t.id = h.target_id
		WHERE h.id = $1
		FOR UPDATE OF h, r`, hypothesisID).
		Scan(&hs, &c.runID, &c.runStatus, &c.runScopeHash, &c.targetID, &c.pinnedRevision, &c.currentRev, &pol)
	c.dynamicMode = pol.DynamicMode
	return c, hs, mapErr(err)
}

func (c labContext) scope(hypothesisID int64, operation string, limits policy.LabLimits) policy.LabScope {
	return policy.LabScope{
		ActionClass:  policy.ActionLocalLab,
		Operation:    operation,
		RunScopeHash: c.runScopeHash,
		HypothesisID: hypothesisID,
		TargetID:     c.targetID,
		Revision:     c.pinnedRevision,
		Network:      policy.NetworkNone,
		Limits:       limits,
	}
}

// RequestLabApproval creates a pending approval for a local, no-network lab
// experiment on an accepted hypothesis. It never starts the experiment.
func (s *Store) RequestLabApproval(ctx context.Context, hypothesisID, requester int64, operation string, limits policy.LabLimits, summary string, ttl time.Duration) (Approval, error) {
	var id int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		c, hs, err := loadLabContext(ctx, tx, hypothesisID)
		if err != nil {
			return err
		}
		switch {
		case c.dynamicMode != policy.DynamicLocalLab:
			return fmt.Errorf("%w: project policy disables dynamic testing", ErrForbidden)
		case hs != research.HypothesisAccepted:
			return fmt.Errorf("%w: hypothesis must be accepted, is %s", ErrConflict, hs)
		case c.runStatus != research.RunReviewRequired:
			return fmt.Errorf("%w: run must be review_required, is %s", ErrConflict, c.runStatus)
		case c.currentRev != c.pinnedRevision:
			return fmt.Errorf("%w: target revision changed since the run was pinned", ErrConflict)
		}
		scope := c.scope(hypothesisID, operation, limits)
		hash, err := scope.Hash()
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO approvals(run_id, hypothesis_id, target_id, action_class, target_revision, scope,
			                      scope_hash, request_summary, requested_by, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
			c.runID, hypothesisID, c.targetID, policy.ActionLocalLab, c.pinnedRevision, scope, hash,
			summary, requester, time.Now().Add(ttl)).Scan(&id); err != nil {
			return mapErr(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE hypotheses SET status = 'validation_pending', updated_at = now() WHERE id = $1`, hypothesisID); err != nil {
			return err
		}
		return appendEvent(ctx, tx, c.runID, "approval.requested", string(c.runStatus),
			fmt.Sprintf("Lab approval %d requested for hypothesis %d", id, hypothesisID),
			map[string]any{"approval_id": id, "scope_hash": hash})
	})
	if err != nil {
		return Approval{}, err
	}
	return one[Approval](s.pool.Query(ctx, `SELECT `+approvalCols+` FROM approvals WHERE id = $1`, id))
}

// checkApprovalScope recomputes an approval's scope from current state and
// reports whether it still matches what was approved or requested.
func checkApprovalScope(ctx context.Context, tx pgx.Tx, a Approval) (labContext, bool, error) {
	c, _, err := loadLabContext(ctx, tx, a.HypothesisID)
	if err != nil {
		return c, false, err
	}
	var stored policy.LabScope
	if err := json.Unmarshal(a.Scope, &stored); err != nil {
		return c, false, err
	}
	current, err := c.scope(a.HypothesisID, stored.Operation, stored.Limits).Hash()
	if err != nil {
		return c, false, err
	}
	fresh := current == a.ScopeHash && c.currentRev == c.pinnedRevision && c.dynamicMode == policy.DynamicLocalLab
	return c, fresh, nil
}

// ErrStale is returned when an approval no longer matches the current run
// scope or target revision, or has expired. The approval is marked expired.
var ErrStale = fmt.Errorf("%w: approval is stale", ErrConflict)

// DecideApproval records an approve/deny decision. The decider must differ
// from the requester unless allowSelf (the instance policy allows self-approval), in which case the
// approval is marked self_approved. The scope hash is recomputed from
// current state.
func (s *Store) DecideApproval(ctx context.Context, approvalID, decider int64, approve bool, note string, allowSelf bool) (Approval, error) {
	var stale bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		a, err := one[Approval](tx.Query(ctx, `SELECT `+approvalCols+` FROM approvals WHERE id = $1 FOR UPDATE`, approvalID))
		if err != nil {
			return err
		}
		if a.Decision != "pending" {
			return fmt.Errorf("%w: approval is already %s", ErrConflict, a.Decision)
		}
		self := a.RequestedBy == decider
		if self && !allowSelf {
			return fmt.Errorf("%w: the requester cannot decide their own approval", ErrForbidden)
		}
		c, fresh, err := checkApprovalScope(ctx, tx, a)
		if err != nil {
			return err
		}
		if !fresh || time.Now().After(a.ExpiresAt) {
			stale = true
			if _, err := tx.Exec(ctx, `UPDATE approvals SET decision = 'expired', decided_at = now() WHERE id = $1`, a.ID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE hypotheses SET status = 'accepted', updated_at = now() WHERE id = $1`, a.HypothesisID)
			return err
		}

		decision, hypStatus := "denied", research.HypothesisAccepted
		if approve {
			decision = "approved"
			hypStatus = research.HypothesisValidationPending
		}
		if _, err := tx.Exec(ctx, `
			UPDATE approvals SET decision = $2, decided_by = $3, reviewer_note = $4, decided_at = now(), self_approved = $5
			WHERE id = $1`,
			a.ID, decision, decider, note, self); err != nil {
			return mapErr(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE hypotheses SET status = $2, updated_at = now() WHERE id = $1`, a.HypothesisID, string(hypStatus)); err != nil {
			return err
		}
		if approve && c.runStatus == research.RunReviewRequired {
			if err := transitionRun(ctx, tx, c.runID, research.RunReviewRequired, research.RunValidationApproved,
				fmt.Sprintf("Lab approval %d granted", a.ID)); err != nil {
				return err
			}
		}
		msg := fmt.Sprintf("Lab approval %d %s", a.ID, decision)
		if self {
			msg += " by its requester (self-approval allowed by the instance policy)"
		}
		return appendEvent(ctx, tx, c.runID, "approval.decided", "", msg,
			map[string]any{"approval_id": a.ID, "decision": decision, "self_approved": self})
	})
	if err != nil {
		return Approval{}, err
	}
	if stale {
		return Approval{}, ErrStale
	}
	return one[Approval](s.pool.Query(ctx, `SELECT `+approvalCols+` FROM approvals WHERE id = $1`, approvalID))
}
