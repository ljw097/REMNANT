import { useState } from "react";
import { ApiError, post } from "../api/client";
import { useI18n, type Key, type Lang } from "../i18n";
import { appName, type Gate } from "../session";
import { Alert, Button, Field, Segmented } from "../ui/components";
import { Icon } from "../ui/Icon";
import { NewPasswordFields, passwordProblem } from "./Password";

const usernameRe = /^[a-z0-9][a-z0-9._-]{1,63}$/;

export function SignIn({ onSignedIn, branding, lang, setLang, error, status }: Gate) {
  const { t } = useI18n();
  const [view, setView] = useState<"signin" | "request" | "requested">("signin");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  const [localError, setLocalError] = useState<string>();
  const shown = localError ?? (error ? t(error as Key) : undefined);

  const signIn = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setLocalError(undefined);
    try {
      await post("/auth/login", { username: username.trim().toLowerCase(), password });
      setPassword("");
      await onSignedIn();
    } catch (err) {
      setLocalError(signInError(err, t));
    } finally {
      setBusy(false);
    }
  };

  const request = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setLocalError(undefined);
    try {
      await post("/auth/request-account", { username, password });
      setPassword("");
      setConfirm("");
      setView("requested");
    } catch (err) {
      setLocalError(err instanceof ApiError ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const switchView = (v: typeof view) => {
    setView(v);
    setLocalError(undefined);
    setPassword("");
    setConfirm("");
  };

  return (
    <div className="signin">
      <aside className="signin-side">
        <div className="brand" style={{ padding: 0 }}>
          <span className="brand-mark" aria-hidden="true">
            <Icon name="shield" size={14} />
          </span>
          {appName(branding)}
        </div>
        <div className="stack" style={{ gap: 10 }}>
          <h1 style={{ fontSize: 28, maxWidth: "18ch" }}>{t("signin.pitch")}</h1>
          <p className="ink-2" style={{ maxWidth: "48ch" }}>
            {t("signin.pitchBody")}
          </p>
        </div>
        <ol>
          <li>
            <Icon name="hash" />
            <span>{t("signin.point.evidence")}</span>
          </li>
          <li>
            <Icon name="noNetwork" />
            <span>{t("signin.point.isolation")}</span>
          </li>
          <li>
            <Icon name="approvals" />
            <span>{t("signin.point.approval")}</span>
          </li>
        </ol>
      </aside>
      <main className="signin-main">
        <div className="signin-form">
          <div className="row" style={{ justifyContent: "space-between" }}>
            <h2>{view === "signin" ? t("signin.title") : t("signin.request")}</h2>
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

          {view === "signin" ? (
            <form className="form-grid" onSubmit={signIn} noValidate>
              <Field label={t("signin.username")}>
                {(id) => <input id={id} className="input" autoComplete="username" spellCheck={false} autoCapitalize="none" value={username} autoFocus onChange={(e) => setUsername(e.target.value)} />}
              </Field>
              <Field label={t("signin.password")}>
                {(id) => (
                  <div className="row" style={{ flexWrap: "nowrap" }}>
                    <input id={id} className="input" type={show ? "text" : "password"} autoComplete="current-password" value={password} aria-invalid={shown ? true : undefined} onChange={(e) => setPassword(e.target.value)} />
                    <Button iconOnly icon={show ? "eyeOff" : "eye"} aria-label={show ? t("signin.hide") : t("signin.show")} onClick={() => setShow((s) => !s)} />
                  </div>
                )}
              </Field>
              {shown ? (
                <Alert tone="bad" icon="alert">
                  {shown}
                </Alert>
              ) : null}
              <Button type="submit" variant="primary" size="lg" loading={busy} disabled={!username.trim() || !password}>
                {t("signin.submit")}
              </Button>
              {status?.account_requests ? (
                <p className="small ink-2">
                  {t("signin.noAccount")}{" "}
                  <button type="button" className="linklike" onClick={() => switchView("request")}>
                    {t("signin.request")}
                  </button>
                </p>
              ) : (
                <p className="small muted">{t("signin.forgot")}</p>
              )}
            </form>
          ) : null}

          {view === "request" ? (
            <form className="form-grid" onSubmit={request} noValidate>
              <p className="small ink-2">{t("signin.requestBody")}</p>
              <Field label={t("signin.username")} hint={t("setup.usernameHint")} error={username && !usernameRe.test(username) ? t("setup.usernameInvalid") : undefined}>
                {(id) => <input id={id} className="input" autoComplete="username" spellCheck={false} value={username} autoFocus onChange={(e) => setUsername(e.target.value.toLowerCase())} />}
              </Field>
              <NewPasswordFields username={username} value={password} confirm={confirm} onValue={setPassword} onConfirm={setConfirm} label={t("signin.password")} />
              {localError ? (
                <Alert tone="bad" icon="alert">
                  {localError}
                </Alert>
              ) : null}
              <div className="row" style={{ justifyContent: "space-between" }}>
                <Button variant="ghost" icon="chevronLeft" onClick={() => switchView("signin")}>
                  {t("setup.back")}
                </Button>
                <Button type="submit" variant="primary" loading={busy} disabled={!usernameRe.test(username) || !confirm || !!passwordProblem(username, password, confirm)}>
                  {t("signin.requestSubmit")}
                </Button>
              </div>
            </form>
          ) : null}

          {view === "requested" ? (
            <div className="form-grid">
              <Alert tone="ok" icon="check">
                {t("signin.requested", { name: username })}
              </Alert>
              <Button variant="primary" onClick={() => switchView("signin")}>
                {t("signin.backToSignIn")}
              </Button>
            </div>
          ) : null}
        </div>
      </main>
    </div>
  );
}

function signInError(err: unknown, t: (k: Key) => string): string {
  if (!(err instanceof ApiError)) return String(err);
  if (err.status === 401) return t("signin.error.wrong");
  if (err.code === "pending") return t("signin.error.pending");
  if (err.code === "locked") return t("signin.error.locked");
  if (err.status === 429) return t("signin.error.rate");
  if (err.status === 0) return t("signin.error.unreachable");
  return err.message;
}
