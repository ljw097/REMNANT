-- Every model call an agent makes, so each number on screen (tokens, regions,
-- patterns, hypotheses) can be traced to the request, the answer and what
-- REMNANT did with it. A call belongs to a run or to a repository intake.
CREATE TABLE remnant.model_calls (
    id              BIGSERIAL PRIMARY KEY,
    run_id          BIGINT REFERENCES remnant.runs(id) ON DELETE CASCADE,
    intake_id       BIGINT REFERENCES remnant.intakes(id) ON DELETE CASCADE,
    role            TEXT NOT NULL,
    subject         TEXT NOT NULL DEFAULT '',
    evidence_id     BIGINT,
    hypothesis_id   BIGINT,
    commit_sha      TEXT NOT NULL DEFAULT '',
    model           TEXT NOT NULL DEFAULT '',
    max_tokens      BIGINT NOT NULL DEFAULT 0,
    input_tokens    BIGINT NOT NULL DEFAULT 0,
    output_tokens   BIGINT NOT NULL DEFAULT 0,
    duration_ms     BIGINT NOT NULL DEFAULT 0,
    -- ok, refused, truncated, error, stopped
    outcome         TEXT NOT NULL,
    -- what REMNANT made of the answer, e.g. "declined", "hypothesis 12", "pattern 3 created"
    result          TEXT NOT NULL DEFAULT '',
    error           TEXT NOT NULL DEFAULT '',
    request         TEXT NOT NULL DEFAULT '',
    response        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (run_id IS NOT NULL OR intake_id IS NOT NULL)
);
CREATE INDEX idx_model_calls_run ON remnant.model_calls(run_id, id) WHERE run_id IS NOT NULL;
CREATE INDEX idx_model_calls_intake ON remnant.model_calls(intake_id, id) WHERE intake_id IS NOT NULL;
CREATE INDEX idx_model_calls_hypothesis ON remnant.model_calls(hypothesis_id) WHERE hypothesis_id IS NOT NULL;
CREATE INDEX idx_model_calls_evidence ON remnant.model_calls(evidence_id) WHERE evidence_id IS NOT NULL;
