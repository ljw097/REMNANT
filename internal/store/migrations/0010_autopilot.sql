-- Autopilot and the Reporter agent.
--
-- An automatic run triages its own hypotheses and finalises a report
-- without a person, so pasting a repository URL can reach a written report
-- in one flow. Code execution (lab experiments) and sending a disclosure
-- stay with a person. A machine-triaged hypothesis is marked as such and
-- has no reviewer, so the record never pretends a person decided.

ALTER TABLE remnant.runs ADD COLUMN auto BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE remnant.hypotheses ADD COLUMN auto_triaged BOOLEAN NOT NULL DEFAULT false;
-- A triage decision is recorded either by a person (reviewed_by) or by
-- autopilot (auto_triaged); never both, never a person-less decision that
-- is not marked automatic.
ALTER TABLE remnant.hypotheses ADD CONSTRAINT hypotheses_triage_author CHECK (
    status IN ('draft','triage') OR reviewed_by IS NOT NULL OR auto_triaged
);

-- The Reporter writes the narrative report and the disclosure draft.
INSERT INTO remnant.agent_profiles(agent_key, role, display_name)
VALUES ('reporter', 'reporter', 'Reporter')
ON CONFLICT (agent_key) DO NOTHING;

-- The disclosure draft is a new artifact kind.
ALTER TABLE remnant.artifacts DROP CONSTRAINT artifacts_artifact_kind_check;
ALTER TABLE remnant.artifacts ADD CONSTRAINT artifacts_artifact_kind_check
    CHECK (artifact_kind IN ('source_index','analyzer_output','test_log','crash_sample','minimized_testcase','report','disclosure','patch_review','other'));
