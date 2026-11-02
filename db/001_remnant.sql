-- REMNANT additive schema for ARTEX (PostgreSQL).
-- Apply after the existing ARTEX db/schema.sql has created tasks and llm_profiles.
-- Idempotent by design; keep this block in the startup schema loader if that is the
-- deployment's migration convention. The startup schema loader owns transaction policy.

CREATE TABLE IF NOT EXISTS research_projects (
    id                  BIGSERIAL PRIMARY KEY,
    name                TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active','paused','archived')),
    llm_profile_id      BIGINT REFERENCES llm_profiles(id) ON DELETE SET NULL,
    policy              JSONB NOT NULL DEFAULT '{"source_mode":"read_only","dynamic_mode":"local_lab_only","network":"none"}'::jsonb,
    created_by          TEXT NOT NULL DEFAULT 'ARTEX',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_research_projects_status ON research_projects(status, updated_at DESC);

CREATE TABLE IF NOT EXISTS research_targets (
    id                  BIGSERIAL PRIMARY KEY,
    project_id          BIGINT NOT NULL REFERENCES research_projects(id) ON DELETE CASCADE,
    kind                TEXT NOT NULL CHECK (kind IN ('git_repository','local_snapshot','lab_fixture')),
    display_name        TEXT NOT NULL,
    source_uri          TEXT NOT NULL DEFAULT '',
    source_ref          TEXT NOT NULL DEFAULT '',
    commit_sha          TEXT NOT NULL DEFAULT '',
    snapshot_key        TEXT NOT NULL,
    tree_sha256         TEXT NOT NULL DEFAULT '',
    primary_language    TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'ready'
                        CHECK (status IN ('pending','ready','unavailable','quarantined')),
    metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, snapshot_key)
);
CREATE INDEX IF NOT EXISTS idx_research_targets_project ON research_targets(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_research_targets_commit ON research_targets(commit_sha) WHERE commit_sha <> '';

CREATE TABLE IF NOT EXISTS research_patterns (
    id                  BIGSERIAL PRIMARY KEY,
    parent_pattern_id   BIGINT REFERENCES research_patterns(id) ON DELETE SET NULL,
    source_kind         TEXT NOT NULL DEFAULT 'historical'
                        CHECK (source_kind IN ('historical','validated_internal','researcher_authored')),
    source_ref          TEXT NOT NULL DEFAULT '',
    title               TEXT NOT NULL,
    vulnerability_class TEXT NOT NULL,
    cwe_id              TEXT NOT NULL DEFAULT '',
    root_cause          TEXT NOT NULL,
    preconditions       JSONB NOT NULL DEFAULT '[]'::jsonb,
    lifecycle_points    JSONB NOT NULL DEFAULT '[]'::jsonb,
    code_signatures     JSONB NOT NULL DEFAULT '[]'::jsonb,
    counter_signatures  JSONB NOT NULL DEFAULT '[]'::jsonb,
    patch_semantics     JSONB NOT NULL DEFAULT '{}'::jsonb,
    version             INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    validation_status   TEXT NOT NULL DEFAULT 'unreviewed'
                        CHECK (validation_status IN ('unreviewed','reviewed','validated','rejected')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(source_ref, title, version)
);
CREATE INDEX IF NOT EXISTS idx_research_patterns_class ON research_patterns(vulnerability_class, validation_status);

CREATE TABLE IF NOT EXISTS research_runs (
    id                  BIGSERIAL PRIMARY KEY,
    project_id          BIGINT NOT NULL REFERENCES research_projects(id) ON DELETE CASCADE,
    task_id             BIGINT REFERENCES tasks(id) ON DELETE SET NULL,
    status              TEXT NOT NULL DEFAULT 'queued'
                        CHECK (status IN ('queued','collecting','indexing','surface_mapping','hypothesis_generation','static_validation','review_required','validation_approved','lab_validation','report_ready','completed','cancelled','failed','timed_out')),
    mode                TEXT NOT NULL DEFAULT 'source_only'
                        CHECK (mode IN ('source_only')),
    stage               TEXT NOT NULL DEFAULT 'queued',
    config              JSONB NOT NULL DEFAULT '{}'::jsonb,
    policy_hash         TEXT NOT NULL DEFAULT '',
    target_revision_set JSONB NOT NULL DEFAULT '[]'::jsonb,
    llm_profile_id      BIGINT REFERENCES llm_profiles(id) ON DELETE SET NULL,
    started_by          TEXT NOT NULL DEFAULT 'ARTEX',
    error_summary       TEXT NOT NULL DEFAULT '',
    queued_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at          TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_research_runs_project ON research_runs(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_research_runs_status ON research_runs(status, queued_at) WHERE status NOT IN ('completed','cancelled','failed','timed_out');

CREATE TABLE IF NOT EXISTS research_run_targets (
    run_id              BIGINT NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
    target_id           BIGINT NOT NULL REFERENCES research_targets(id) ON DELETE RESTRICT,
    commit_sha          TEXT NOT NULL,
    PRIMARY KEY(run_id, target_id)
);

CREATE TABLE IF NOT EXISTS research_run_patterns (
    run_id              BIGINT NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
    pattern_id          BIGINT NOT NULL REFERENCES research_patterns(id) ON DELETE RESTRICT,
    PRIMARY KEY(run_id, pattern_id)
);

CREATE TABLE IF NOT EXISTS research_agent_profiles (
    id                  BIGSERIAL PRIMARY KEY,
    agent_key           TEXT NOT NULL UNIQUE,
    role                TEXT NOT NULL CHECK (role IN ('historian','archaeologist','surface_mapper','hypothesis_researcher','skeptic','static_analyst','validator','reporter')),
    display_name        TEXT NOT NULL,
    enabled             BOOLEAN NOT NULL DEFAULT true,
    llm_profile_id      BIGINT REFERENCES llm_profiles(id) ON DELETE SET NULL,
    prompt_version      INTEGER NOT NULL DEFAULT 1,
    config              JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS research_hypotheses (
    id                  BIGSERIAL PRIMARY KEY,
    run_id              BIGINT NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
    target_id           BIGINT NOT NULL REFERENCES research_targets(id) ON DELETE RESTRICT,
    pattern_id          BIGINT REFERENCES research_patterns(id) ON DELETE SET NULL,
    title               TEXT NOT NULL,
    claim               TEXT NOT NULL,
    vulnerability_class TEXT NOT NULL,
    cwe_id              TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'draft'
                        CHECK (status IN ('draft','triage','accepted','rejected','validation_pending','validated','not_reproducible','duplicate')),
    triage_confidence   TEXT NOT NULL DEFAULT 'low'
                        CHECK (triage_confidence IN ('low','medium','high')),
    preconditions       JSONB NOT NULL DEFAULT '[]'::jsonb,
    affected_symbols    JSONB NOT NULL DEFAULT '[]'::jsonb,
    expected_observation TEXT NOT NULL DEFAULT '',
    counter_evidence    JSONB NOT NULL DEFAULT '[]'::jsonb,
    dedupe_key          TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_research_hypotheses_run ON research_hypotheses(run_id, status, id);
CREATE INDEX IF NOT EXISTS idx_research_hypotheses_dedupe ON research_hypotheses(target_id, dedupe_key) WHERE dedupe_key <> '';

CREATE TABLE IF NOT EXISTS research_evidence (
    id                  BIGSERIAL PRIMARY KEY,
    hypothesis_id       BIGINT REFERENCES research_hypotheses(id) ON DELETE CASCADE,
    run_id              BIGINT NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
    target_id           BIGINT NOT NULL REFERENCES research_targets(id) ON DELETE RESTRICT,
    evidence_kind       TEXT NOT NULL CHECK (evidence_kind IN ('source_location','call_path','data_flow','static_finding','build_result','runtime_observation','counter_evidence','external_reference')),
    source_uri          TEXT NOT NULL DEFAULT '',
    file_path           TEXT NOT NULL DEFAULT '',
    line_start          INTEGER,
    line_end            INTEGER,
    symbol              TEXT NOT NULL DEFAULT '',
    commit_sha          TEXT NOT NULL DEFAULT '',
    content_sha256      TEXT NOT NULL DEFAULT '',
    summary             TEXT NOT NULL,
    excerpt             TEXT NOT NULL DEFAULT '',
    metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (line_start IS NULL OR line_start > 0),
    CHECK (line_end IS NULL OR line_end >= COALESCE(line_start, 1))
);
CREATE INDEX IF NOT EXISTS idx_research_evidence_hypothesis ON research_evidence(hypothesis_id, id);
CREATE INDEX IF NOT EXISTS idx_research_evidence_target_path ON research_evidence(target_id, file_path, line_start);

CREATE TABLE IF NOT EXISTS research_approvals (
    id                  BIGSERIAL PRIMARY KEY,
    run_id              BIGINT NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
    target_id           BIGINT NOT NULL REFERENCES research_targets(id) ON DELETE RESTRICT,
    action_class        TEXT NOT NULL CHECK (action_class IN ('local_lab_validation')),
    target_commit_sha   TEXT NOT NULL,
    policy_hash         TEXT NOT NULL,
    request_summary     TEXT NOT NULL,
    decision            TEXT NOT NULL DEFAULT 'pending'
                        CHECK (decision IN ('pending','approved','denied','expired','revoked')),
    decided_by          TEXT,
    decided_at          TIMESTAMPTZ,
    expires_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_research_approvals_run ON research_approvals(run_id, decision, created_at DESC);

CREATE TABLE IF NOT EXISTS research_experiments (
    id                  BIGSERIAL PRIMARY KEY,
    run_id              BIGINT NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
    hypothesis_id       BIGINT REFERENCES research_hypotheses(id) ON DELETE SET NULL,
    approval_id         BIGINT REFERENCES research_approvals(id) ON DELETE RESTRICT,
    executor            TEXT NOT NULL CHECK (executor IN ('semgrep','codeql','tree_sitter','clang_analyzer','asan_test','ubsan_test','libfuzzer','aflplusplus','unit_test')),
    status              TEXT NOT NULL DEFAULT 'queued'
                        CHECK (status IN ('queued','running','passed','finding','failed','resource_limited','cancelled')),
    network_mode        TEXT NOT NULL DEFAULT 'none' CHECK (network_mode = 'none'),
    workspace_revision TEXT NOT NULL,
    config              JSONB NOT NULL DEFAULT '{}'::jsonb,
    result_summary      TEXT NOT NULL DEFAULT '',
    exit_code           INTEGER,
    started_at          TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_research_experiments_run ON research_experiments(run_id, created_at DESC);

CREATE TABLE IF NOT EXISTS research_artifacts (
    id                  BIGSERIAL PRIMARY KEY,
    run_id              BIGINT NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
    experiment_id       BIGINT REFERENCES research_experiments(id) ON DELETE SET NULL,
    artifact_kind       TEXT NOT NULL CHECK (artifact_kind IN ('source_index','analyzer_output','test_log','crash_sample','minimized_testcase','report','patch_review','other')),
    storage_key         TEXT NOT NULL,
    sha256              TEXT NOT NULL,
    size_bytes          BIGINT NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    media_type          TEXT NOT NULL DEFAULT 'application/octet-stream',
    classification      TEXT NOT NULL DEFAULT 'research_internal'
                        CHECK (classification IN ('research_internal','sensitive','redacted')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(run_id, sha256)
);

CREATE TABLE IF NOT EXISTS research_events (
    id                  BIGSERIAL PRIMARY KEY,
    run_id              BIGINT NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
    event_type          TEXT NOT NULL,
    stage               TEXT NOT NULL DEFAULT '',
    message             TEXT NOT NULL DEFAULT '',
    payload             JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_research_events_stream ON research_events(run_id, id);

