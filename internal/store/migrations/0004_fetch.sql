-- Fetch leases for git_repository targets: one instance pins a pending
-- target at a time; an expired lease (a crashed fetch) can be claimed again.
ALTER TABLE remnant.targets
    ADD COLUMN fetch_owner      TEXT NOT NULL DEFAULT '',
    ADD COLUMN fetch_expires_at TIMESTAMPTZ;
CREATE INDEX idx_targets_pending_fetch ON remnant.targets(created_at)
    WHERE kind = 'git_repository' AND status = 'pending';
