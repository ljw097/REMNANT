-- Sign-in with a username and password, browser sessions, and account
-- requests.
--
-- Passwords are stored only as PBKDF2-SHA256 hashes. A browser session is an
-- opaque random token in an HttpOnly cookie; like API tokens, only its
-- SHA-256 digest is stored. API tokens stay for scripts and the CLI.

ALTER TABLE remnant.users
    ADD COLUMN password_hash        TEXT,
    ADD COLUMN must_change_password BOOLEAN NOT NULL DEFAULT false,
    -- An account someone requested on the sign-in page; it cannot sign in
    -- until an administrator approves it.
    ADD COLUMN pending              BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN failed_logins        INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN locked_until         TIMESTAMPTZ,
    ADD COLUMN password_changed_at  TIMESTAMPTZ;

CREATE TABLE remnant.sessions (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES remnant.users(id) ON DELETE CASCADE,
    token_sha256    TEXT NOT NULL UNIQUE CHECK (token_sha256 ~ '^[0-9a-f]{64}$'),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NOT NULL,
    idle_seconds    INTEGER NOT NULL CHECK (idle_seconds > 0),
    user_agent      TEXT NOT NULL DEFAULT '',
    revoked_at      TIMESTAMPTZ
);
CREATE INDEX idx_sessions_user ON remnant.sessions(user_id);

-- The deployment mode becomes an instance policy: a profile plus the
-- individual rules it presets. Existing instances keep what they had.
INSERT INTO remnant.instance_settings(key, value)
SELECT 'policy',
       CASE WHEN value = '"personal"'::jsonb
            THEN '{"profile":"personal","self_approval":true,"self_review":true,"admin_lab_approval":false,"account_requests":false,"session_hours":720}'::jsonb
            ELSE '{"profile":"team","self_approval":false,"self_review":false,"admin_lab_approval":false,"account_requests":false,"session_hours":168}'::jsonb
       END
FROM remnant.instance_settings WHERE key = 'deployment_mode'
ON CONFLICT (key) DO NOTHING;
DELETE FROM remnant.instance_settings WHERE key = 'deployment_mode';
