package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ljw097/remnant/internal/research"
)

// AutoTriageResult reports what autopilot decided.
type AutoTriageResult struct {
	Accepted int
	Rejected int
}

// AutoTriage decides an auto run's untriaged hypotheses without a person and
// moves the run to report_ready. A hypothesis is accepted when the Skeptic
// did not refute it and its triage priority is high or medium; otherwise it
// is rejected. Each decision is marked auto_triaged with no reviewer, and an
// event records it, so the record never reads as a human decision.
func (s *Store) AutoTriage(ctx context.Context, runID int64) (AutoTriageResult, error) {
	var res AutoTriageResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var status research.RunStatus
		var auto bool
		if err := tx.QueryRow(ctx, `SELECT status, auto FROM runs WHERE id = $1 FOR UPDATE`, runID).Scan(&status, &auto); err != nil {
			return mapErr(err)
		}
		if !auto {
			return fmt.Errorf("%w: run %d is not an autopilot run", ErrConflict, runID)
		}
		if status != research.RunReviewRequired {
			return fmt.Errorf("%w: run is %s, not review_required", ErrConflict, status)
		}
		rows, err := tx.Query(ctx, `
			SELECT id,
			       (skeptic_verdict <> 'refutes' AND triage_confidence IN ('high','medium')) AS accept
			FROM hypotheses WHERE run_id = $1 AND status = 'triage' FOR UPDATE`, runID)
		if err != nil {
			return err
		}
		type decision struct {
			id     int64
			accept bool
		}
		var decisions []decision
		for rows.Next() {
			var d decision
			if err := rows.Scan(&d.id, &d.accept); err != nil {
				rows.Close()
				return err
			}
			decisions = append(decisions, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, d := range decisions {
			status := research.HypothesisRejected
			if d.accept {
				status = research.HypothesisAccepted
				res.Accepted++
			} else {
				res.Rejected++
			}
			if _, err := tx.Exec(ctx, `
				UPDATE hypotheses SET status = $2, auto_triaged = true, reviewed_by = NULL, reviewed_at = now(), updated_at = now()
				WHERE id = $1`, d.id, string(status)); err != nil {
				return err
			}
			if err := appendEvent(ctx, tx, runID, "hypothesis.triaged", "",
				fmt.Sprintf("Hypothesis %d %s by autopilot", d.id, status),
				map[string]any{"hypothesis_id": d.id, "decision": status, "auto": true}); err != nil {
				return err
			}
		}
		return transitionRun(ctx, tx, runID, research.RunReviewRequired, research.RunReportReady,
			fmt.Sprintf("Autopilot triaged %d hypotheses (%d accepted); finalising the report", len(decisions), res.Accepted))
	})
	return res, err
}
