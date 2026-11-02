-- REMNANT schema v1. Applied once by remnantd's migration runner, inside a
-- transaction, and recorded in remnant.schema_migrations.

-- Identity --------------------------------------------------------------------

CREATE TABLE remnant.users (
    id                  BIGSERIAL PRIMARY KEY,
    username            TEXT NOT NULL UNIQUE CHECK (username ~ '^[a-z0-9][a-z0-9._-]{1,63}$'),
    role                TEXT NOT NULL CHECK (role IN ('admin','researcher','reviewer')),
    disabled            BOOLEAN NOT NULL DEFAULT false,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Only the SHA-256 digest of a bearer token is stored.
CREATE TABLE remnant.api_tokens (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT NOT NULL REFERENCES remnant.users(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    token_sha256        TEXT NOT NULL UNIQUE CHECK (token_sha256 ~ '^[0-9a-f]{64}$'),
    expires_at          TIMESTAMPTZ,
    last_used_at        TIMESTAMPTZ,
    revoked_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_api_tokens_user ON remnant.api_tokens(user_id);

-- Model providers --------------------------------------------------------------

-- api_key_sealed is AES-256-GCM ciphertext under REMNANT_MASTER_KEY; the key
-- material is never returned by the API.
CREATE TABLE remnant.llm_profiles (
    id                  BIGSERIAL PRIMARY KEY,
    name                TEXT NOT NULL UNIQUE,
    provider            TEXT NOT NULL CHECK (provider IN ('anthropic')),
    base_url            TEXT NOT NULL DEFAULT '',
    model               TEXT NOT NULL,
    effort              TEXT NOT NULL DEFAULT 'high'
                        CHECK (effort IN ('low','medium','high','xhigh','max')),
    api_key_sealed      BYTEA,
    api_key_hint        TEXT NOT NULL DEFAULT '',
    key_version         INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Research ----------------------------------------------------------------------

CREATE TABLE remnant.projects (
    id                  BIGSERIAL PRIMARY KEY,
    name                TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active','paused','archived')),
    llm_profile_id      BIGINT REFERENCES remnant.llm_profiles(id) ON DELETE SET NULL,
    policy              JSONB NOT NULL DEFAULT '{"source_mode":"read_only","dynamic_mode":"disabled","network":"none"}'::jsonb,
    created_by          BIGINT REFERENCES remnant.users(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_projects_status ON remnant.projects(status, updated_at DESC);

-- revision is a Git commit SHA or "sha256:<tree digest>" for local snapshots.
CREATE TABLE remnant.targets (
    id                  BIGSERIAL PRIMARY KEY,
    project_id          BIGINT NOT NULL REFERENCES remnant.projects(id) ON DELETE CASCADE,
    kind                TEXT NOT NULL CHECK (kind IN ('git_repository','local_snapshot','lab_fixture')),
    display_name        TEXT NOT NULL,
    source_uri          TEXT NOT NULL DEFAULT '',
    source_ref          TEXT NOT NULL DEFAULT '',
    snapshot_key        TEXT NOT NULL,
    revision            TEXT NOT NULL DEFAULT '',
    tree_sha256         TEXT NOT NULL DEFAULT '',
    primary_language    TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','ready','unavailable','quarantined')),
    metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, snapshot_key),
    CHECK (status <> 'ready' OR revision <> '')
);
CREATE INDEX idx_targets_project ON remnant.targets(project_id, created_at DESC);

CREATE TABLE remnant.patterns (
    id                  BIGSERIAL PRIMARY KEY,
    parent_pattern_id   BIGINT REFERENCES remnant.patterns(id) ON DELETE SET NULL,
    source_kind         TEXT NOT NULL DEFAULT 'historical'
                        CHECK (source_kind IN ('historical','validated_internal','researcher_authored')),
    source_ref          TEXT NOT NULL DEFAULT '',
    title               TEXT NOT NULL,
    vulnerability_class TEXT NOT NULL,
    cwe_id              TEXT NOT NULL DEFAULT '',
    language            TEXT NOT NULL DEFAULT '',
    root_cause          TEXT NOT NULL,
    preconditions       JSONB NOT NULL DEFAULT '[]'::jsonb,
    lifecycle_points    JSONB NOT NULL DEFAULT '[]'::jsonb,
    code_signatures     JSONB NOT NULL DEFAULT '[]'::jsonb,
    counter_signatures  JSONB NOT NULL DEFAULT '[]'::jsonb,
    patch_semantics     JSONB NOT NULL DEFAULT '{}'::jsonb,
    version             INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    validation_status   TEXT NOT NULL DEFAULT 'unreviewed'
                        CHECK (validation_status IN ('unreviewed','reviewed','validated','rejected')),
    created_by          BIGINT REFERENCES remnant.users(id) ON DELETE SET NULL,
    reviewed_by         BIGINT REFERENCES remnant.users(id) ON DELETE SET NULL,
    reviewed_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(source_ref, title, version),
    CHECK (reviewed_by IS NULL OR created_by IS NULL OR reviewed_by <> created_by)
);
CREATE INDEX idx_patterns_class ON remnant.patterns(vulnerability_class, validation_status);

CREATE TABLE remnant.runs (
    id                  BIGSERIAL PRIMARY KEY,
    project_id          BIGINT NOT NULL REFERENCES remnant.projects(id) ON DELETE CASCADE,
    status              TEXT NOT NULL DEFAULT 'queued'
                        CHECK (status IN ('queued','collecting','indexing','surface_mapping','hypothesis_generation','static_validation','review_required','validation_approved','lab_validation','report_ready','completed','cancelled','failed','timed_out')),
    mode                TEXT NOT NULL DEFAULT 'source_only' CHECK (mode IN ('source_only')),
    limits              JSONB NOT NULL DEFAULT '{}'::jsonb,
    policy              JSONB NOT NULL,
    scope_hash          TEXT NOT NULL,
    llm_profile_id      BIGINT REFERENCES remnant.llm_profiles(id) ON DELETE SET NULL,
    started_by          BIGINT REFERENCES remnant.users(id) ON DELETE SET NULL,
    error_summary       TEXT NOT NULL DEFAULT '',
    queued_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at          TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ
);
CREATE INDEX idx_runs_project ON remnant.runs(project_id, queued_at DESC);
CREATE INDEX idx_runs_active ON remnant.runs(status, queued_at)
    WHERE status NOT IN ('completed','cancelled','failed','timed_out');

CREATE TABLE remnant.run_targets (
    run_id              BIGINT NOT NULL REFERENCES remnant.runs(id) ON DELETE CASCADE,
    target_id           BIGINT NOT NULL REFERENCES remnant.targets(id) ON DELETE RESTRICT,
    revision            TEXT NOT NULL CHECK (revision <> ''),
    PRIMARY KEY(run_id, target_id)
);

CREATE TABLE remnant.run_patterns (
    run_id              BIGINT NOT NULL REFERENCES remnant.runs(id) ON DELETE CASCADE,
    pattern_id          BIGINT NOT NULL REFERENCES remnant.patterns(id) ON DELETE RESTRICT,
    PRIMARY KEY(run_id, pattern_id)
);

CREATE TABLE remnant.agent_profiles (
    id                  BIGSERIAL PRIMARY KEY,
    agent_key           TEXT NOT NULL UNIQUE,
    role                TEXT NOT NULL CHECK (role IN ('historian','archaeologist','surface_mapper','hypothesis_researcher','skeptic','static_analyst','validator','reporter')),
    display_name        TEXT NOT NULL,
    enabled             BOOLEAN NOT NULL DEFAULT true,
    llm_profile_id      BIGINT REFERENCES remnant.llm_profiles(id) ON DELETE SET NULL,
    prompt_version      INTEGER NOT NULL DEFAULT 1,
    config              JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE remnant.hypotheses (
    id                  BIGSERIAL PRIMARY KEY,
    run_id              BIGINT NOT NULL REFERENCES remnant.runs(id) ON DELETE CASCADE,
    target_id           BIGINT NOT NULL REFERENCES remnant.targets(id) ON DELETE RESTRICT,
    pattern_id          BIGINT REFERENCES remnant.patterns(id) ON DELETE SET NULL,
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
    reviewer_note       TEXT NOT NULL DEFAULT '',
    reviewed_by         BIGINT REFERENCES remnant.users(id) ON DELETE SET NULL,
    reviewed_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_hypotheses_run ON remnant.hypotheses(run_id, status, id);
CREATE INDEX idx_hypotheses_dedupe ON remnant.hypotheses(target_id, dedupe_key) WHERE dedupe_key <> '';

CREATE TABLE remnant.evidence (
    id                  BIGSERIAL PRIMARY KEY,
    hypothesis_id       BIGINT REFERENCES remnant.hypotheses(id) ON DELETE CASCADE,
    run_id              BIGINT NOT NULL REFERENCES remnant.runs(id) ON DELETE CASCADE,
    target_id           BIGINT NOT NULL REFERENCES remnant.targets(id) ON DELETE RESTRICT,
    evidence_kind       TEXT NOT NULL CHECK (evidence_kind IN ('source_location','call_path','data_flow','static_finding','build_result','runtime_observation','counter_evidence','external_reference')),
    source_uri          TEXT NOT NULL DEFAULT '',
    file_path           TEXT NOT NULL DEFAULT '',
    line_start          INTEGER,
    line_end            INTEGER,
    symbol              TEXT NOT NULL DEFAULT '',
    revision            TEXT NOT NULL DEFAULT '',
    content_sha256      TEXT NOT NULL DEFAULT '',
    summary             TEXT NOT NULL,
    excerpt             TEXT NOT NULL DEFAULT '',
    metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (line_start IS NULL OR line_start > 0),
    CHECK (line_end IS NULL OR line_end >= COALESCE(line_start, 1))
);
CREATE INDEX idx_evidence_hypothesis ON remnant.evidence(hypothesis_id, id);
CREATE INDEX idx_evidence_target_path ON remnant.evidence(target_id, file_path, line_start);

-- An approval is bound to scope_hash (policy.LabScope). The requester can
-- never decide their own request.
CREATE TABLE remnant.approvals (
    id                  BIGSERIAL PRIMARY KEY,
    run_id              BIGINT NOT NULL REFERENCES remnant.runs(id) ON DELETE CASCADE,
    hypothesis_id       BIGINT NOT NULL REFERENCES remnant.hypotheses(id) ON DELETE CASCADE,
    target_id           BIGINT NOT NULL REFERENCES remnant.targets(id) ON DELETE RESTRICT,
    action_class        TEXT NOT NULL CHECK (action_class IN ('local_lab_validation')),
    target_revision     TEXT NOT NULL,
    scope               JSONB NOT NULL,
    scope_hash          TEXT NOT NULL,
    request_summary     TEXT NOT NULL,
    decision            TEXT NOT NULL DEFAULT 'pending'
                        CHECK (decision IN ('pending','approved','denied','expired','revoked')),
    requested_by        BIGINT NOT NULL REFERENCES remnant.users(id) ON DELETE RESTRICT,
    decided_by          BIGINT REFERENCES remnant.users(id) ON DELETE RESTRICT,
    reviewer_note       TEXT NOT NULL DEFAULT '',
    decided_at          TIMESTAMPTZ,
    expires_at          TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (decided_by IS NULL OR decided_by <> requested_by)
);
CREATE INDEX idx_approvals_run ON remnant.approvals(run_id, decision, created_at DESC);
CREATE UNIQUE INDEX idx_approvals_one_pending ON remnant.approvals(hypothesis_id) WHERE decision = 'pending';

CREATE TABLE remnant.experiments (
    id                  BIGSERIAL PRIMARY KEY,
    run_id              BIGINT NOT NULL REFERENCES remnant.runs(id) ON DELETE CASCADE,
    hypothesis_id       BIGINT REFERENCES remnant.hypotheses(id) ON DELETE SET NULL,
    approval_id         BIGINT REFERENCES remnant.approvals(id) ON DELETE RESTRICT,
    operation           TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'queued'
                        CHECK (status IN ('queued','running','passed','finding','failed','resource_limited','cancelled')),
    network_mode        TEXT NOT NULL DEFAULT 'none' CHECK (network_mode = 'none'),
    workspace_revision  TEXT NOT NULL,
    config              JSONB NOT NULL DEFAULT '{}'::jsonb,
    result_summary      TEXT NOT NULL DEFAULT '',
    exit_code           INTEGER,
    started_at          TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (operation NOT LIKE 'lab.%' OR approval_id IS NOT NULL)
);
CREATE INDEX idx_experiments_run ON remnant.experiments(run_id, created_at DESC);

CREATE TABLE remnant.artifacts (
    id                  BIGSERIAL PRIMARY KEY,
    run_id              BIGINT NOT NULL REFERENCES remnant.runs(id) ON DELETE CASCADE,
    experiment_id       BIGINT REFERENCES remnant.experiments(id) ON DELETE SET NULL,
    artifact_kind       TEXT NOT NULL CHECK (artifact_kind IN ('source_index','analyzer_output','test_log','crash_sample','minimized_testcase','report','patch_review','other')),
    name                TEXT NOT NULL,
    storage_key         TEXT NOT NULL,
    sha256              TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    size_bytes          BIGINT NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    media_type          TEXT NOT NULL DEFAULT 'application/octet-stream',
    classification      TEXT NOT NULL DEFAULT 'research_internal'
                        CHECK (classification IN ('research_internal','sensitive','redacted')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(run_id, sha256)
);

CREATE TABLE remnant.events (
    id                  BIGSERIAL PRIMARY KEY,
    run_id              BIGINT NOT NULL REFERENCES remnant.runs(id) ON DELETE CASCADE,
    event_type          TEXT NOT NULL,
    stage               TEXT NOT NULL DEFAULT '',
    message             TEXT NOT NULL DEFAULT '',
    payload             JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_events_stream ON remnant.events(run_id, id);
