// The model providers REMNANT supports: a sensible default model for each
// (first in the list), suggestions shown before a key's own model list is
// loaded, and the base URL and key format shown as placeholders.

export type Provider = "anthropic" | "openai";
export const providers: Provider[] = ["anthropic", "openai"];

// Per-provider model suggestions, a sensible default model, and the base URL
// and key-hint shown as placeholders. The model field stays free text.
export const providerInfo: Record<Provider, { models: string[]; baseUrl: string; keyHint: string }> = {
  anthropic: {
    models: ["claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5-1"],
    baseUrl: "https://api.anthropic.com",
    keyHint: "sk-ant-…",
  },
  openai: {
    models: ["gpt-5", "gpt-5-mini", "gpt-4o", "gpt-4o-mini", "o3", "o4-mini"],
    baseUrl: "https://api.openai.com/v1",
    keyHint: "sk-…",
  },
};
