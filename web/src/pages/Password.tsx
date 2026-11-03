import { useState } from "react";
import { ApiError, put } from "../api/client";
import { useI18n, type Key } from "../i18n";
import { appName, useSession } from "../session";
import { Alert, Button, Field } from "../ui/components";
import { Icon } from "../ui/Icon";

/** Client-side check matching auth.CheckPassword; the server decides. */
export function passwordProblem(username: string, pw: string, confirm: string): Key | null {
  if ([...pw].length < 10) return "password.tooShort";
  if (pw.length > 256) return "password.tooLong";
  if (pw.trim().toLowerCase() === username.toLowerCase()) return "password.isUsername";
  if (confirm && pw !== confirm) return "password.mismatch";
  return null;
}

/** New-password and confirmation fields with live feedback. */
export function NewPasswordFields({ username, value, confirm, onValue, onConfirm, label }: { username: string; value: string; confirm: string; onValue: (v: string) => void; onConfirm: (v: string) => void; label?: string }) {
  const { t } = useI18n();
  const problem = value ? passwordProblem(username, value, "") : null;
  return (
    <>
      <Field label={label ?? t("password.new")} hint={t("password.rule")} error={problem ? t(problem) : undefined}>
        {(id) => <input id={id} className="input" type="password" autoComplete="new-password" value={value} aria-invalid={problem ? true : undefined} onChange={(e) => onValue(e.target.value)} />}
      </Field>
      <Field label={t("password.confirm")} error={confirm && confirm !== value ? t("password.mismatch") : undefined}>
        {(id) => <input id={id} className="input" type="password" autoComplete="new-password" value={confirm} aria-invalid={confirm && confirm !== value ? true : undefined} onChange={(e) => onConfirm(e.target.value)} />}
      </Field>
    </>
  );
}

/** Changes the signed-in user's password. */
export function PasswordForm({ forced, onDone }: { forced?: boolean; onDone: () => void }) {
  const { t } = useI18n();
  const { me } = useSession();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const ok = current && next && confirm && !passwordProblem(me.username, next, confirm);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      await put("/me/password", { current_password: current, new_password: next });
      setCurrent("");
      setNext("");
      setConfirm("");
      onDone();
    } catch (err) {
      setError(err instanceof ApiError && err.status === 403 ? t("password.wrongCurrent") : err instanceof ApiError ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="form-grid" onSubmit={submit}>
      <input type="text" autoComplete="username" value={me.username} readOnly hidden />
      <Field label={forced ? t("password.temporary") : t("password.current")}>
        {(id) => <input id={id} className="input" type="password" autoComplete="current-password" value={current} autoFocus={forced} onChange={(e) => setCurrent(e.target.value)} />}
      </Field>
      <NewPasswordFields username={me.username} value={next} confirm={confirm} onValue={setNext} onConfirm={setConfirm} />
      {error ? (
        <Alert tone="bad" icon="alert">
          {error}
        </Alert>
      ) : null}
      <div>
        <Button type="submit" variant="primary" icon="key" loading={busy} disabled={!ok}>
          {t("password.change")}
        </Button>
      </div>
    </form>
  );
}

/** Shown instead of the app while the user holds a temporary password. */
export function ForcedPasswordChange() {
  const { t } = useI18n();
  const { me, branding, refreshMe, signOut } = useSession();
  return (
    <div className="signin single">
      <main className="signin-main">
        <div className="signin-form">
          <div className="brand" style={{ padding: 0 }}>
            <span className="brand-mark" aria-hidden="true">
              <Icon name="shield" size={14} />
            </span>
            {appName(branding)}
          </div>
          <h2>{t("password.forcedTitle")}</h2>
          <p className="ink-2">{t("password.forcedBody", { name: me.username })}</p>
          <PasswordForm forced onDone={() => void refreshMe()} />
          <Button variant="ghost" icon="logout" onClick={signOut}>
            {t("nav.signOut")}
          </Button>
        </div>
      </main>
    </div>
  );
}
