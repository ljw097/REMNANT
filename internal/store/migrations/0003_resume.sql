-- Pause/resume and recovery from interruption.

ALTER TABLE remnant.runs DROP CONSTRAINT runs_status_check;
ALTER TABLE remnant.runs ADD CONSTRAINT runs_status_check
    CHECK (status IN ('queued','collecting','indexing','surface_mapping','hypothesis_generation','static_validation',
                      'review_required','validation_approved','lab_validation','report_ready','paused',
                      'completed','cancelled','failed','timed_out'));

-- resume_stage: the source stage a run continues from after a pause or an
--   interruption; stages before it are not repeated.
-- resume_status: where a paused run returns when resumed.
-- attempts: times the run was recovered after its orchestrator died.
-- active_ms: time spent in automatic source stages; wall_seconds applies to
--   this, so paused time does not count.
ALTER TABLE remnant.runs
    ADD COLUMN resume_stage  TEXT NOT NULL DEFAULT '',
    ADD COLUMN resume_status TEXT NOT NULL DEFAULT '',
    ADD COLUMN attempts      INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    ADD COLUMN active_ms     BIGINT NOT NULL DEFAULT 0 CHECK (active_ms >= 0),
    ADD CONSTRAINT runs_paused_has_resume_status CHECK ((status = 'paused') = (resume_status <> ''));

-- Each candidate region is recorded once per run, so surface mapping can be
-- repeated after an interruption without duplicating evidence. The outcome of
-- examining it is kept in metadata.outcome.
CREATE UNIQUE INDEX idx_evidence_candidate ON remnant.evidence (run_id, (metadata->>'candidate_key'))
    WHERE hypothesis_id IS NULL AND (metadata->>'candidate_key') IS NOT NULL;
