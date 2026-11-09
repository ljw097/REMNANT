import { useState } from "react";
import { ApiError, post } from "../api/client";
import type { InstancePolicy } from "../api/types";
import { useI18n, type Key, type Lang } from "../i18n";
import type { Gate } from "../session";
import { Alert, Button, Field, Segmented } from "../ui/components";
import { Icon } from "../ui/Icon";
import { ConnectModel } from "../ui/QuickStart";
import { fallbackPresets, PolicyEditor } from "../ui/policy";
import { NewPasswordFields, passwordProblem } from "./Password";

/** First-run setup: choose how the instance is used, then create the first administrator. */
export function Setup({ lang, setLang, onSignedIn, status }: Gate) {
  const { t } = useI18n();
  const presets = status?.presets ?? fallbackPresets;
  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [policy, setPolicy] = useState<InstancePolicy | null>(null);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [instanceName, setInstanceName] = useState("");
  // `remnantd local` opens this page with the setup code in the URL fragment
  // (never sent to the server); take it and clear it from the address bar.
  const [code, setCode] = useState(() => {
    const m = /(?:^#|&)setup=([A-Za-z0-9-]{8,32})/.exec(location.hash);
    if (m) history.replaceState(null, "", location.pathname + location.search);
    return m ? m[1]! : "";
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const usernameOk = /^[a-z0-9][a-z0-9._-]{1,63}$/.test(username);
  const ready = usernameOk && confirm && !passwordProblem(username, password, confirm) && code.replace(/-/g, "").length >= 12;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!policy) return;
    setBusy(true);
    setError(undefined);
    try {
      await post("/setup", { code, username, password, instance_name: instanceName.trim(), policy });
      // Signed in as the new administrator: connect a model before entering.
      setStep(3);
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) setError(t("setup.error.code"));
      else if (err instanceof ApiError && err.status === 409) setError(t("setup.error.done"));
      else setError(err instanceof ApiError ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="signin">
      <aside className="signin-side">
        <div className="brand" style={{ padding: 0 }}>
          <span className="brand-mark" aria-hidden="true">
            <Icon name="shield" size={14} />
          </span>
          REMNANT
        </div>
        <div className="stack" style={{ gap: 10 }}>
          <h1 style={{ fontSize: 28, maxWidth: "20ch" }}>{t("setup.pitch")}</h1>
          <p className="ink-2" style={{ maxWidth: "48ch" }}>
            {t("setup.pitchBody")}
          </p>
        </div>
        <ol aria-label={t("setup.steps")}>
          {([1, 2, 3] as const).map((n) => (
            <li key={n} aria-current={step === n ? "step" : undefined} style={{ color: step === n ? "var(--ink)" : undefined }}>
              <Icon name={step > n ? "check" : n === 1 ? "users" : n === 2 ? "key" : "cpu"} />
              <span>
                <strong>{t(`setup.step${n}` as Key)}</strong>
                <br />
                {t(`setup.step${n}.hint` as Key)}
              </span>
            </li>
          ))}
        </ol>
      </aside>
      <main className="signin-main">
        <div className="signin-form" style={{ width: "min(640px, 100%)" }}>
          <div className="row" style={{ justifyContent: "space-between" }}>
            <h2>{t(`setup.step${step}` as Key)}</h2>
            <Segmented<Lang>
              label={t("prefs.language")}
              value={lang}
              onChange={setLang}
              options={[
                { value: "ko", label: "한국어" },
                { value: "en", label: "English" },
              ]}
            />
          </div>

          {step === 1 ? (
            <>
              <PolicyEditor value={policy} presets={presets} onChange={setPolicy} />
              <p className="small muted">{t("setup.modeLater")}</p>
              <Button variant="primary" size="lg" disabled={!policy} onClick={() => setStep(2)}>
                {t("setup.next")}
              </Button>
            </>
          ) : null}

          {step === 2 && policy ? (
            <form className="form-grid" onSubmit={submit}>
              <Field label={t("setup.username")} hint={t("setup.usernameHint")} error={username && !usernameOk ? t("setup.usernameInvalid") : undefined}>
                {(id) => (
                  <input id={id} className="input" value={username} autoFocus autoComplete="username" spellCheck={false} aria-invalid={username && !usernameOk ? true : undefined} onChange={(e) => setUsername(e.target.value.toLowerCase())} />
                )}
              </Field>
              <NewPasswordFields username={username} value={password} confirm={confirm} onValue={setPassword} onConfirm={setConfirm} label={t("setup.password")} />
              <Field label={t("setup.instanceName")} hint={t("setup.instanceNameHint")}>
                {(id) => <input id={id} className="input" value={instanceName} maxLength={60} placeholder="REMNANT" onChange={(e) => setInstanceName(e.target.value)} />}
              </Field>
              <Field label={t("setup.code")} hint={t("setup.codeHint")}>
                {(id) => <input id={id} className="input mono" value={code} spellCheck={false} autoComplete="one-time-code" placeholder="XXXX-XXXX-XXXX" onChange={(e) => setCode(e.target.value.toUpperCase())} />}
              </Field>
              {policy.profile === "personal" ? <p className="small ink-2">{t("setup.personalLocal")}</p> : null}
              {error ? (
                <Alert tone="bad" icon="alert">
                  {error}
                </Alert>
              ) : null}
              <div className="row" style={{ justifyContent: "space-between" }}>
                <Button variant="ghost" icon="chevronLeft" onClick={() => setStep(1)}>
                  {t("setup.back")}
                </Button>
                <Button type="submit" variant="primary" size="lg" loading={busy} disabled={!ready}>
                  {t("setup.submit", { profile: t(`profile.${policy.profile}` as Key) })}
                </Button>
              </div>
            </form>
          ) : null}

          {step === 3 ? (
            <div className="stack" style={{ gap: 16 }}>
              <Alert tone="ok" icon="check">
                {t("setup.done")}
              </Alert>
              <p className="ink-2">{t("setup.modelIntro")}</p>
              <ConnectModel onConnected={() => void onSignedIn()} />
              <div className="row" style={{ justifyContent: "flex-end" }}>
                <Button variant="ghost" icon="arrowRight" onClick={() => void onSignedIn()}>
                  {t("setup.skipModel")}
                </Button>
              </div>
            </div>
          ) : null}
        </div>
      </main>
    </div>
  );
}
