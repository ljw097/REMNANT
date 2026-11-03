import { useState } from "react";
import { ApiError, post } from "../api/client";
import { useI18n, type Key, type Lang } from "../i18n";
import { Alert, Button, Field, Segmented } from "../ui/components";
import { Icon, type IconName } from "../ui/Icon";

type Mode = "team" | "personal";

/** First-run setup: choose team or personal use and create the first administrator. */
export function Setup({ lang, setLang, onDone }: { lang: Lang; setLang: (l: Lang) => void; onDone: (token: string) => Promise<void> }) {
  const { t } = useI18n();
  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [mode, setMode] = useState<Mode | null>(null);
  const [username, setUsername] = useState("");
  const [instanceName, setInstanceName] = useState("");
  const [code, setCode] = useState("");
  const [token, setToken] = useState("");
  const [copied, setCopied] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const usernameOk = /^[a-z0-9][a-z0-9._-]{1,63}$/.test(username);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!mode) return;
    setBusy(true);
    setError(undefined);
    try {
      const res = await post<{ token: string }>("/setup", { mode, username, code, instance_name: instanceName.trim() });
      setToken(res.token);
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
              <Icon name={step > n ? "check" : n === 1 ? "users" : n === 2 ? "key" : "lock"} />
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
        <div className="signin-form" style={{ width: "min(560px, 100%)" }}>
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
              <div className="mode-cards" role="radiogroup" aria-label={t("setup.step1")}>
                <ModeCard value="personal" current={mode} onPick={setMode} icon="user" />
                <ModeCard value="team" current={mode} onPick={setMode} icon="users" />
              </div>
              <p className="small muted">{t("setup.modeLater")}</p>
              <Button variant="primary" size="lg" disabled={!mode} onClick={() => setStep(2)}>
                {t("setup.next")}
              </Button>
            </>
          ) : null}

          {step === 2 ? (
            <form className="form-grid" onSubmit={submit}>
              <Field label={t("setup.username")} hint={t("setup.usernameHint")} error={username && !usernameOk ? t("setup.usernameInvalid") : undefined}>
                {(id) => (
                  <input id={id} className="input" value={username} autoFocus autoComplete="username" spellCheck={false} aria-invalid={username && !usernameOk ? true : undefined} onChange={(e) => setUsername(e.target.value.toLowerCase())} />
                )}
              </Field>
              <Field label={t("setup.instanceName")} hint={t("setup.instanceNameHint")}>
                {(id) => <input id={id} className="input" value={instanceName} maxLength={60} placeholder="REMNANT" onChange={(e) => setInstanceName(e.target.value)} />}
              </Field>
              <Field label={t("setup.code")} hint={t("setup.codeHint")}>
                {(id) => <input id={id} className="input mono" value={code} spellCheck={false} autoComplete="one-time-code" placeholder="XXXX-XXXX-XXXX" onChange={(e) => setCode(e.target.value.toUpperCase())} />}
              </Field>
              <code className="small" style={{ display: "block", padding: "8px 10px", background: "var(--code-bg)", borderRadius: 4, overflowX: "auto" }}>
                REMNANT first-run setup … enter this setup code: XXXX-XXXX-XXXX
              </code>
              {error ? (
                <Alert tone="bad" icon="alert">
                  {error}
                </Alert>
              ) : null}
              <div className="row" style={{ justifyContent: "space-between" }}>
                <Button variant="ghost" icon="chevronLeft" onClick={() => setStep(1)}>
                  {t("setup.back")}
                </Button>
                <Button type="submit" variant="primary" size="lg" loading={busy} disabled={!usernameOk || code.replace(/-/g, "").length < 12}>
                  {t(mode === "personal" ? "setup.submitPersonal" : "setup.submitTeam")}
                </Button>
              </div>
            </form>
          ) : null}

          {step === 3 ? (
            <div className="form-grid">
              <Alert tone="warn" icon="key">
                {t("setup.tokenOnce")}
              </Alert>
              <div className="row" style={{ flexWrap: "nowrap" }}>
                <input className="input mono" readOnly value={token} aria-label={t("signin.token")} onFocus={(e) => e.target.select()} />
                <Button
                  icon={copied ? "check" : "copy"}
                  onClick={() => void navigator.clipboard?.writeText(token).then(() => setCopied(true))}
                >
                  {copied ? t("common.copied") : t("common.copy")}
                </Button>
              </div>
              {mode === "team" ? <p className="small ink-2">{t("setup.teamNext")}</p> : <p className="small ink-2">{t("setup.personalNext")}</p>}
              <Button variant="primary" size="lg" icon="arrowRight" onClick={() => void onDone(token)}>
                {t("setup.continue")}
              </Button>
            </div>
          ) : null}
        </div>
      </main>
    </div>
  );
}

function ModeCard({ value, current, onPick, icon }: { value: Mode; current: Mode | null; onPick: (m: Mode) => void; icon: IconName }) {
  const { t } = useI18n();
  const selected = current === value;
  return (
    <button type="button" role="radio" aria-checked={selected} className="mode-card" onClick={() => onPick(value)}>
      <span className="row" style={{ gap: 10 }}>
        <Icon name={icon} size={18} />
        <strong>{t(`mode.${value}` as Key)}</strong>
      </span>
      <span className="small ink-2">{t(`mode.${value}.for` as Key)}</span>
      <ul className="bullets small">
        <li>{t(`mode.${value}.approval` as Key)}</li>
        <li>{t(`mode.${value}.patterns` as Key)}</li>
        <li>{t(`mode.${value}.users` as Key)}</li>
      </ul>
    </button>
  );
}
