-- Allow OpenAI-family model profiles alongside Anthropic.
ALTER TABLE remnant.llm_profiles DROP CONSTRAINT llm_profiles_provider_check;
ALTER TABLE remnant.llm_profiles ADD CONSTRAINT llm_profiles_provider_check
    CHECK (provider IN ('anthropic', 'openai'));
