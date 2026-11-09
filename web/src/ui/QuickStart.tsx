// The shortest path from a fresh install to a first result: connect a model
// with one API key, then paste a repository URL. Used by the first-run setup,
// the dashboard (while the instance is empty) and the Analyze page.

import { useEffect, useState } from "react";
import { post } from "../api/client";
import type { LLMProfile, ModelChoice, ProfileCheck } from "../api/types";
import { useI18n, type Key } from "../i18n";
import { providerInfo, providers, type Provider } from "./providers";
import { Alert, Button, errorText, Field, Segmented, useToast } from "./components";

/** Loads the models a key can use (no tokens spent). */
export async function loadModels(provider: Provider, apiKey: string, baseURL = ""): Promise<{ ok: boolean; models: ModelChoice[]; error?: string }> {
  return post("/llm-profiles/models", { provider, api_key: apiKey, base_url: baseURL });
}

/** The recommended model among those available: REMNANT's default when offered, else the first active one. */
export function recommendedModel(provider: Provider, models: ModelChoice[]): string {
  const preferred = providerInfo[provider].models.find((m) => models.some((x) => x.id === m));
  return preferred ?? models.find((m) => !m.deprecated)?.id ?? models[0]?.id ?? providerInfo[provider].models[0]!;
}

/** A model picker: the loaded list when there is one, else a text field with suggestions. */
export function ModelPicker({ id, provider, value, models, onChange }: { id: string; provider: Provider; value: string; models: ModelChoice[] | null; onChange: (v: string) => void }) {
  const { t } = useI18n();
  if (models && models.length > 0) {
    return (
      <select id={id} className="select mono" value={value} onChange={(e) => onChange(e.target.value)}>
        {models.some((m) => m.id === value) ? null : <option value={value}>{value}</option>}
        {models.map((m) => (
          <option key={m.id} value={m.id}>
            {m.display_name && m.display_name !== m.id ? `${m.display_name} — ${m.id}` : m.id}
            {m.max_output ? ` · ${t("models.maxOut", { n: m.max_output.toLocaleString() })}` : ""}
            {m.deprecated ? ` · ${t("models.deprecated")}` : ""}
          </option>
        ))}
      </select>
    );
  }
  return (
    <>
      <input id={id} className="input mono" list={`${id}-list`} value={value} spellCheck={false} onChange={(e) => onChange(e.target.value)} />
      <datalist id={`${id}-list`}>
        {providerInfo[provider].models.map((m) => (
          <option key={m} value={m} />
        ))}
      </datalist>
    </>
  );
}

/** What ConnectModel creates: a profile body with sensible defaults. */
export function profileBody(t: (k: Key) => string, provider: Provider, apiKey: string, model: string) {
  return {
    name: t(`models.provider.${provider}` as Key),
    provider,
    model,
    effort: "high",
    base_url: "",
    api_key: apiKey.trim(),
    options: { thinking: provider === "anthropic" ? "adaptive" : "", timeout_seconds: 0, max_retries: null },
  };
}

/**
 * Model setup in one place: pick a provider, paste a key, and the models
 * that key can use are loaded to pick from (the recommended one is
 * preselected). Connect tests the key (no tokens spent) and creates the
 * profile; everything else stays editable under Settings → Model profiles.
 */
export function ConnectModel({ onConnected }: { onConnected: (p: LLMProfile) => void }) {
  const { t } = useI18n();
  const toast = useToast();
  const [provider, setProvider] = useState<Provider>("anthropic");
  const [key, setKey] = useState("");
  const [model, setModel] = useState(providerInfo.anthropic.models[0]!);
  const [models, setModels] = useState<ModelChoice[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<string>();

  // A pasted key loads its models once it looks complete.
  useEffect(() => {
    setModels(null);
    setFailed(undefined);
    const k = key.trim();
    if (k.length < 20) return;
    let stale = false;
    const timer = setTimeout(async () => {
      setLoading(true);
      try {
        const r = await loadModels(provider, k);
        if (stale) return;
        if (r.ok) {
          setModels(r.models);
          setModel(recommendedModel(provider, r.models));
        } else setFailed(r.error ?? "");
      } catch (err) {
        if (!stale) setFailed(errorText(err, t));
      } finally {
        if (!stale) setLoading(false);
      }
    }, 400);
    return () => {
      stale = true;
      clearTimeout(timer);
    };
  }, [key, provider]);

  const create = async () => {
    const p = await post<LLMProfile>("/llm-profiles", profileBody(t, provider, key, model));
    toast(t("quick.connected"));
    onConnected(p);
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setFailed(undefined);
    try {
      const b = profileBody(t, provider, key, model);
      const check = await post<ProfileCheck>("/llm-profiles/test", { provider: b.provider, model: b.model, base_url: "", api_key: b.api_key, options: b.options });
      if (!check.ok) {
        setFailed(check.error ?? "");
        return;
      }
      await create();
    } catch (err) {
      setFailed(errorText(err, t));
    } finally {
      setBusy(false);
    }
  };

  const saveAnyway = async () => {
    setBusy(true);
    try {
      await create();
    } catch (err) {
      setFailed(errorText(err, t));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="form-grid" onSubmit={submit}>
      <Segmented
        label={t("models.provider")}
        value={provider}
        onChange={(v) => {
          setProvider(v);
          setModel(providerInfo[v].models[0]!);
        }}
        options={providers.map((p) => ({ value: p, label: t(`models.provider.${p}` as Key) }))}
      />
      <Field label={t("models.key")} hint={loading ? t("quick.loadingModels") : t("quick.keyHint")}>
        {(id) => (
          <input
            id={id}
            className="input mono input-lg"
            type="password"
            autoComplete="off"
            spellCheck={false}
            value={key}
            placeholder={providerInfo[provider].keyHint}
            onChange={(e) => setKey(e.target.value)}
          />
        )}
      </Field>
      <Field label={t("models.model")} hint={models ? t("quick.modelsLoaded", { n: models.length }) : t("quick.modelHint")}>
        {(id) => (
          <div className="quick-row">
            <ModelPicker id={id} provider={provider} value={model} models={models} onChange={setModel} />
            <Button type="submit" variant="primary" size="lg" icon="key" loading={busy} disabled={key.trim().length < 8 || !model.trim()}>
              {t("quick.connect")}
            </Button>
          </div>
        )}
      </Field>
      {failed !== undefined ? (
        <Alert tone="bad" icon="alert">
          <div className="stack" style={{ gap: 8 }}>
            <span>{t("models.testFailed", { error: failed })}</span>
            <span>
              <Button size="sm" onClick={() => void saveAnyway()} disabled={busy || key.trim().length < 8}>
                {t("quick.saveAnyway")}
              </Button>
            </span>
          </div>
        </Alert>
      ) : null}
    </form>
  );
}
