-- Web UI support: per-user preferences and instance-wide settings
-- (organization branding).

CREATE TABLE remnant.user_preferences (
    user_id     BIGINT PRIMARY KEY REFERENCES remnant.users(id) ON DELETE CASCADE,
    prefs       JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(prefs) = 'object'),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE remnant.instance_settings (
    key         TEXT PRIMARY KEY,
    value       JSONB NOT NULL,
    updated_by  BIGINT REFERENCES remnant.users(id) ON DELETE SET NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
