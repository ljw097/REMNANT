-- Team and personal deployments.
--
-- Team mode keeps two-person rules: a lab approval is decided by someone
-- other than its requester, and a pattern is reviewed by someone other than
-- its author. Personal mode lets one person do both, but only as an explicit,
-- recorded exception: the row carries self_approved / self_reviewed, so the
-- database still refuses an unmarked self-decision and the audit trail says
-- what happened.

ALTER TABLE remnant.approvals ADD COLUMN self_approved BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE remnant.approvals DROP CONSTRAINT approvals_check;
ALTER TABLE remnant.approvals ADD CONSTRAINT approvals_second_person
    CHECK (decided_by IS NULL OR decided_by <> requested_by OR self_approved);

ALTER TABLE remnant.patterns ADD COLUMN self_reviewed BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE remnant.patterns DROP CONSTRAINT patterns_check;
ALTER TABLE remnant.patterns ADD CONSTRAINT patterns_second_reviewer
    CHECK (reviewed_by IS NULL OR created_by IS NULL OR reviewed_by <> created_by OR self_reviewed);

-- Installations that already have users were set up as teams.
INSERT INTO remnant.instance_settings(key, value)
SELECT 'deployment_mode', '"team"'::jsonb
WHERE EXISTS (SELECT 1 FROM remnant.users)
ON CONFLICT (key) DO NOTHING;
