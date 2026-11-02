-- Orchestrator support: run leases, token accounting, skeptic verdicts and
-- worker experiment bookkeeping.

-- A run is worked on by whoever holds an unexpired lease. Leases are renewed
-- by heartbeat; a run left in an automatic stage with an expired lease is
-- failed by the reaper.
ALTER TABLE remnant.runs
    ADD COLUMN lease_owner      TEXT NOT NULL DEFAULT '',
    ADD COLUMN lease_expires_at TIMESTAMPTZ,
    ADD COLUMN input_tokens     BIGINT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    ADD COLUMN output_tokens    BIGINT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0);
CREATE INDEX idx_runs_claimable ON remnant.runs(queued_at)
    WHERE status IN ('queued','validation_approved','report_ready');

-- facts and unknowns keep what the code shows apart from what it cannot show.
ALTER TABLE remnant.hypotheses
    ADD COLUMN skeptic_verdict TEXT NOT NULL DEFAULT ''
        CHECK (skeptic_verdict IN ('','supports','weakens','refutes')),
    ADD COLUMN facts           JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN unknowns        JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE remnant.experiments
    ADD COLUMN request_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN target_id  BIGINT REFERENCES remnant.targets(id) ON DELETE RESTRICT;
-- An approval authorises exactly one experiment.
CREATE UNIQUE INDEX idx_experiments_one_per_approval ON remnant.experiments(approval_id)
    WHERE approval_id IS NOT NULL;
