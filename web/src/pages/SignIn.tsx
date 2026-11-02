import { useState } from "react";
import type { Branding } from "../api/types";
import { useI18n, type Key, type Lang } from "../i18n";
import { appName } from "../session";
import { Button, Field, Segmented } from "../ui/components";
import { Icon } from "../ui/Icon";

export function SignIn({ onToken, branding, lang, setLang, error }: { onToken: (t: string) => Promise<void>; branding: Branding; lang: Lang; setLang: (l: Lang) => void; error?: string }) {
  const { t } = useI18n();
  const [token, setTokenValue] = useState("");
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  const [localError, setLocalError] = useState<string>();
  const shown = localError ?? error;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const v = token.trim();
    if (!v.startsWith("rmt_")) {
      setLocalError("signin.error.format");
      return;
    }
    setBusy(true);
    setLocalError(undefined);
    try {
      await onToken(v);
    } catch {
      // The provider sets the error message.
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
        <form className="signin-form" onSubmit={submit} noValidate>
          <div className="row" style={{ justifyContent: "space-between" }}>
            <h2>{t("signin.title")}</h2>
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
          <Field label={t("signin.token")} hint={t("signin.tokenHint")} error={shown ? t(shown as Key) : undefined}>
            {(id) => (
              <div className="row" style={{ flexWrap: "nowrap" }}>
                <input
                  id={id}
                  className="input mono"
                  type={show ? "text" : "password"}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder="rmt_…"
                  value={token}
                  aria-invalid={shown ? true : undefined}
                  onChange={(e) => setTokenValue(e.target.value)}
                  autoFocus
                />
                <Button iconOnly icon={show ? "eyeOff" : "eye"} aria-label={show ? t("signin.hide") : t("signin.show")} onClick={() => setShow((s) => !s)} />
              </div>
            )}
          </Field>
          <Button type="submit" variant="primary" size="lg" loading={busy} disabled={!token.trim()}>
            {t("signin.submit")}
          </Button>
          <p className="small muted">{t("signin.howTo")}</p>
          <code className="small" style={{ display: "block", padding: "8px 10px", background: "var(--code-bg)", borderRadius: 4, overflowX: "auto" }}>
            remnantd create-token -username {"<name>"}
          </code>
        </form>
      </main>
    </div>
  );
}
