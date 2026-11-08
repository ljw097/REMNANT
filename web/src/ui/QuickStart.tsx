// The shortest path from a fresh install to a first result: connect a model
// with one API key, then paste a repository URL. Used by the dashboard (while
// the instance is empty) and the Analyze page.

import { useState } from "react";
import { post } from "../api/client";
import type { LLMProfile, ProfileCheck } from "../api/types";
import { useI18n, type Key } from "../i18n";
import { providerInfo, providers, type Provider } from "../pages/SettingsExpert";
import { Alert, Button, errorText, Field, Segmented, useToast } from "./components";

/**
 * One-field model setup: pick a provider, paste a key. The key is tested
 * (no tokens spent) before the profile is created with sensible defaults;
 * everything else stays editable under Settings → Model profiles.
 */
export function ConnectModel({ onConnected }: { onConnected: (p: LLMProfile) => void }) {
  const { t } = useI18n();
  const toast = useToast();
  const [provider, setProvider] = useState<Provider>("anthropic");
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<string>();

  const body = () => ({
    name: t(`models.provider.${provider}` as Key),
    provider,
    model: providerInfo[provider].models[0],
    effort: "high",
    base_url: "",
    api_key: key.trim(),
    options: { thinking: provider === "anthropic" ? "adaptive" : "", timeout_seconds: 0, max_retries: null },
  });

  const create = async () => {
    const p = await post<LLMProfile>("/llm-profiles", body());
    toast(t("quick.connected"));
    onConnected(p);
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setFailed(undefined);
    try {
      const b = body();
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
          setFailed(undefined);
        }}
        options={providers.map((p) => ({ value: p, label: t(`models.provider.${p}` as Key) }))}
      />
      <Field label={t("models.key")} hint={t("quick.keyHint")}>
        {(id) => (
          <div className="quick-row">
            <input
              id={id}
              className="input mono input-lg"
              type="password"
              autoComplete="off"
              spellCheck={false}
              value={key}
              placeholder={providerInfo[provider].keyHint}
              onChange={(e) => {
                setKey(e.target.value);
                setFailed(undefined);
              }}
            />
            <Button type="submit" variant="primary" size="lg" icon="key" loading={busy} disabled={key.trim().length < 8}>
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
              <Button size="sm" onClick={() => void saveAnyway()} disabled={busy}>
                {t("quick.saveAnyway")}
              </Button>
            </span>
          </div>
        </Alert>
      ) : null}
    </form>
  );
}
