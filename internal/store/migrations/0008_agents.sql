-- Model profile options and the two agents the pipeline runs.
--
-- options holds per-profile request settings (thinking, timeout, retries);
-- the API key stays sealed in api_key_sealed. Agent rows let administrators
-- turn an agent off, give it its own model profile, and add instructions.

ALTER TABLE remnant.llm_profiles ADD COLUMN options JSONB NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(options) = 'object');

INSERT INTO remnant.agent_profiles(agent_key, role, display_name)
VALUES ('hypothesis_researcher', 'hypothesis_researcher', 'Hypothesis Researcher'),
       ('skeptic', 'skeptic', 'Skeptic')
ON CONFLICT (agent_key) DO NOTHING;
