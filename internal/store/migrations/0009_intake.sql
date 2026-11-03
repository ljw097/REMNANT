-- Repository intake: start from a repository URL. remnantd fetches the
-- repository with part of its history, keeps the commits that look like
-- security fixes, has the Historian agent draft root-cause patterns from
-- them, and starts a run that hunts for the same mistakes in the current
-- code.

-- A git target may ask for history: the fetch then keeps up to this many
-- recent commits' worth of fix candidates (0: the tree only).
ALTER TABLE remnant.targets ADD COLUMN history_depth INTEGER NOT NULL DEFAULT 0
    CHECK (history_depth BETWEEN 0 AND 5000);

-- Commits from a target's history that look like fixes. The diff is limited
-- to source files and truncated; it is untrusted repository text.
CREATE TABLE remnant.commit_records (
    id              BIGSERIAL PRIMARY KEY,
    target_id       BIGINT NOT NULL REFERENCES remnant.targets(id) ON DELETE CASCADE,
    sha             TEXT NOT NULL CHECK (sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    committed_at    TIMESTAMPTZ NOT NULL,
    subject         TEXT NOT NULL,
    body            TEXT NOT NULL DEFAULT '',
    files           TEXT[] NOT NULL DEFAULT '{}',
    diff            TEXT NOT NULL DEFAULT '',
    signals         TEXT[] NOT NULL DEFAULT '{}',
    score           INTEGER NOT NULL DEFAULT 0,
    UNIQUE (target_id, sha)
);

-- Patterns the Historian drafted from a fix commit. They start unreviewed
-- and say where they came from.
ALTER TABLE remnant.patterns DROP CONSTRAINT patterns_source_kind_check;
ALTER TABLE remnant.patterns ADD CONSTRAINT patterns_source_kind_check
    CHECK (source_kind IN ('historical','validated_internal','researcher_authored','mined'));
ALTER TABLE remnant.patterns ADD COLUMN origin JSONB NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(origin) = 'object');

CREATE TABLE remnant.intakes (
    id              BIGSERIAL PRIMARY KEY,
    source_uri      TEXT NOT NULL,
    source_ref      TEXT NOT NULL DEFAULT '',
    project_id      BIGINT NOT NULL REFERENCES remnant.projects(id) ON DELETE CASCADE,
    target_id       BIGINT NOT NULL REFERENCES remnant.targets(id) ON DELETE CASCADE,
    run_id          BIGINT REFERENCES remnant.runs(id) ON DELETE SET NULL,
    llm_profile_id  BIGINT REFERENCES remnant.llm_profiles(id) ON DELETE SET NULL,
    status          TEXT NOT NULL DEFAULT 'fetching'
                    CHECK (status IN ('fetching','mining','started','failed')),
    -- options: mine_history, history_depth, max_commits, max_patterns,
    -- max_tokens, include_patterns (existing pattern IDs), limits.
    options         JSONB NOT NULL DEFAULT '{}'::jsonb,
    commits_found   INTEGER NOT NULL DEFAULT 0,
    commits_read    INTEGER NOT NULL DEFAULT 0,
    patterns        BIGINT[] NOT NULL DEFAULT '{}',
    input_tokens    BIGINT NOT NULL DEFAULT 0,
    output_tokens   BIGINT NOT NULL DEFAULT 0,
    error_summary   TEXT NOT NULL DEFAULT '',
    lease_owner     TEXT NOT NULL DEFAULT '',
    lease_expires_at TIMESTAMPTZ,
    created_by      BIGINT REFERENCES remnant.users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_intakes_active ON remnant.intakes(created_at) WHERE status IN ('fetching','mining');

INSERT INTO remnant.agent_profiles(agent_key, role, display_name)
VALUES ('historian', 'historian', 'Historian')
ON CONFLICT (agent_key) DO NOTHING;
