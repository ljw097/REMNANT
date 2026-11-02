import { useEffect, useRef, useState } from "react";
import { Frame } from "../App";
import { patch, post, put } from "../api/client";
import { useApi } from "../hooks";
import { useI18n, type Key, type Lang } from "../i18n";
import { Link } from "../router";
import { appName, useSession } from "../session";
import type { Branding, LLMProfile, Role, UserRef } from "../api/types";
import { columnChoices, contrastOnWhite, startPages, type ColumnSet, type Prefs } from "../prefs";
import { Alert, Button, ConfirmButton, Drawer, Empty, errorText, Field, PageHead, Panel, Segmented, SkeletonRows, Time, useToast } from "../ui/components";
import { DashboardEditor } from "./Dashboard";
import { Icon, type IconName } from "../ui/Icon";

type Section = "preferences" | "dashboard" | "account" | "mode" | "users" | "models" | "branding" | "system";

const sections: { id: Section; icon: IconName; admin?: boolean }[] = [
  { id: "preferences", icon: "sliders" },
  { id: "dashboard", icon: "layout" },
  { id: "account", icon: "user" },
  { id: "mode", icon: "shield", admin: true },
  { id: "users", icon: "users", admin: true },
  { id: "models", icon: "cpu", admin: true },
  { id: "branding", icon: "palette", admin: true },
  { id: "system", icon: "update" },
];

export function SettingsPage({ section }: { section: string }) {
  const { t } = useI18n();
  const { can } = useSession();
  const visible = sections.filter((s) => !s.admin || can("admin"));
  const current = (visible.find((s) => s.id === section)?.id ?? "preferences") as Section;
  // On narrow screens the section nav is a horizontal strip; keep the open section in view.
  const navRef = useRef<HTMLElement>(null);
  useEffect(() => {
    const nav = navRef.current;
    const link = nav?.querySelector<HTMLElement>('a[aria-current="page"]');
    if (nav && link && nav.scrollWidth > nav.clientWidth) nav.scrollLeft = link.offsetLeft - (nav.clientWidth - link.offsetWidth) / 2;
  }, [current]);
  return (
    <Frame crumbs={[{ label: t("nav.settings"), to: "/settings" }, { label: t(`settings.${current}` as Key) }]}>
      <PageHead title={t("nav.settings")} />
      <div className="settings-layout">
        <nav ref={navRef} className="nav" aria-label={t("nav.settings")}>
          {visible.map((s) => (
            <Link key={s.id} to={`/settings/${s.id}`} aria-current={s.id === current ? "page" : undefined}>
              <Icon name={s.icon} />
              {t(`settings.${s.id}` as Key)}
              {s.admin ? <span className="count">{t("role.admin")}</span> : null}
            </Link>
          ))}
        </nav>
        <div className="stack">
          {current === "preferences" ? <PreferencesSection /> : null}
          {current === "dashboard" ? <DashboardSection /> : null}
          {current === "account" ? <AccountSection /> : null}
          {current === "mode" ? <ModeSection /> : null}
          {current === "users" ? <UsersSection /> : null}
          {current === "models" ? <ModelsSection /> : null}
          {current === "branding" ? <BrandingSection /> : null}
          {current === "system" ? <SystemSection /> : null}
        </div>
      </div>
    </Frame>
  );
}

function PreferencesSection() {
  const { t } = useI18n();
  const { prefs, savePrefs } = useSession();
  const toast = useToast();
  const save = (p: Partial<Prefs>) => savePrefs(p).then(() => toast(t("prefs.saved"))).catch(() => toast(t("error.save"), "bad"));
  return (
    <>
      <Panel title={t("prefs.appearance")}>
        <div className="panel-body form-grid">
          <div className="field">
            <span className="label">{t("prefs.theme")}</span>
            <Segmented<Prefs["theme"]>
              label={t("prefs.theme")}
              value={prefs.theme}
              onChange={(v) => void save({ theme: v })}
              options={[
                { value: "system", label: t("prefs.theme.system"), icon: "monitor" },
                { value: "light", label: t("prefs.theme.light"), icon: "sun" },
                { value: "dark", label: t("prefs.theme.dark"), icon: "moon" },
              ]}
            />
          </div>
          <div className="field">
            <span className="label">{t("prefs.density")}</span>
            <Segmented<Prefs["density"]>
              label={t("prefs.density")}
              value={prefs.density}
              onChange={(v) => void save({ density: v })}
              options={[
                { value: "comfortable", label: t("prefs.density.comfortable") },
                { value: "compact", label: t("prefs.density.compact") },
              ]}
            />
            <span className="hint">{t("prefs.densityHint")}</span>
          </div>
          <div className="field">
            <span className="label">{t("prefs.language")}</span>
            <Segmented<Lang>
              label={t("prefs.language")}
              value={prefs.language}
              onChange={(v) => void save({ language: v })}
              options={[
                { value: "ko", label: "한국어" },
                { value: "en", label: "English" },
              ]}
            />
          </div>
          <Field label={t("prefs.startPage")} hint={t("prefs.startPageHint")}>
            {(id) => (
              <select id={id} className="select" style={{ maxWidth: 320 }} value={prefs.startPage} onChange={(e) => void save({ startPage: e.target.value })}>
                {startPages.map((p) => (
                  <option key={p} value={p}>
                    {t(`prefs.start.${p === "/" ? "dashboard" : p.slice(1)}` as Key)}
                  </option>
                ))}
              </select>
            )}
          </Field>
        </div>
      </Panel>
      <Panel title={t("columns.title")} meta={t("prefs.columnsHint")}>
        <div className="panel-body grid-12" style={{ gap: 20 }}>
          {(Object.keys(columnChoices) as ColumnSet[]).map((set) => (
            <fieldset key={set} className="span-4" style={{ border: 0, padding: 0, margin: 0, display: "grid", gap: 8, gridColumn: "span 4" }}>
              <legend className="small" style={{ fontWeight: 600, marginBottom: 6 }}>
                {t(`columns.set.${set}` as Key)}
              </legend>
              {columnChoices[set].map((c) => (
                <label key={c} className="check small">
                  <input
                    type="checkbox"
                    checked={prefs.columns[set].includes(c)}
                    onChange={(e) =>
                      void save({ columns: { ...prefs.columns, [set]: columnChoices[set].filter((x) => (x === c ? e.target.checked : prefs.columns[set].includes(x))) } })
                    }
                  />
                  {t(`columns.${set}.${c}` as Key)}
                </label>
              ))}
            </fieldset>
          ))}
        </div>
      </Panel>
    </>
  );
}

function DashboardSection() {
  const { prefs, savePrefs } = useSession();
  return <DashboardEditor value={prefs.dashboard} onSave={(d) => savePrefs({ dashboard: d })} />;
}

function AccountSection() {
  const { t } = useI18n();
  const { me, signOut } = useSession();
  return (
    <Panel title={t("settings.account")}>
      <div className="panel-body stack" style={{ gap: 14 }}>
        <div className="row">
          <span className="avatar" aria-hidden="true">
            {me.username.slice(0, 2)}
          </span>
          <div>
            <strong>{me.username}</strong>
            <div className="small muted">{t(`role.${me.role}` as Key)}</div>
          </div>
        </div>
        <p className="small ink-2 prose">{t(`role.${me.role}.explain` as Key)}</p>
        <p className="small muted prose">{t("account.tokenNote")}</p>
        <div>
          <Button icon="logout" onClick={signOut}>
            {t("nav.signOut")}
          </Button>
        </div>
      </div>
    </Panel>
  );
}

function ModelsSection() {
  const { t } = useI18n();
  const q = useApi<LLMProfile[]>("/llm-profiles");
  const [open, setOpen] = useState(false);
  return (
    <Panel
      title={t("settings.models")}
      meta={t("models.sub")}
      tools={
        <Button size="sm" icon="plus" onClick={() => setOpen(true)}>
          {t("models.new")}
        </Button>
      }
    >
      {q.loading && !q.data ? (
        <SkeletonRows rows={2} cols={4} />
      ) : (q.data ?? []).length === 0 ? (
        <Empty title={t("models.emptyTitle")} body={t("models.emptyBody")} action={<Button onClick={() => setOpen(true)}>{t("models.new")}</Button>} />
      ) : (
        <div className="table-wrap">
        <table className="table">
          <thead>
            <tr>
              <th>{t("models.name")}</th>
              <th>{t("models.model")}</th>
              <th>{t("models.effort")}</th>
              <th>{t("models.key")}</th>
              <th>{t("projects.col.updated")}</th>
            </tr>
          </thead>
          <tbody>
            {(q.data ?? []).map((p) => (
              <tr key={p.id}>
                <td className="title-cell">
                  {p.name}
                  {p.base_url ? <span className="sub mono">{p.base_url}</span> : null}
                </td>
                <td className="mono small">{p.model}</td>
                <td className="small">{p.effort}</td>
                <td className="small">{p.has_api_key ? <span className="mono">{p.api_key_hint}</span> : <span style={{ color: "var(--bad)" }}>{t("models.noKey")}</span>}</td>
                <td className="small ink-2">
                  <Time iso={p.updated_at} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </div>
      )}
      <ModelForm open={open} onClose={() => setOpen(false)} onSaved={q.reload} />
    </Panel>
  );
}

function ModelForm({ open, onClose, onSaved }: { open: boolean; onClose: () => void; onSaved: () => void }) {
  const { t } = useI18n();
  const toast = useToast();
  const [f, setF] = useState({ name: "", model: "claude-opus-5-5", effort: "high", base_url: "", api_key: "" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      await post("/llm-profiles", { ...f, provider: "anthropic" });
      toast(t("models.created"));
      setF({ name: "", model: "claude-opus-5-5", effort: "high", base_url: "", api_key: "" });
      onSaved();
      onClose();
    } catch (err) {
      setError(errorText(err, t));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={t("models.new")}
      foot={
        <>
          <Button onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" type="submit" form="model-form" loading={busy} disabled={!f.name.trim() || !f.model.trim() || !f.api_key}>
            {t("models.create")}
          </Button>
        </>
      }
    >
      <form id="model-form" className="form-grid" onSubmit={submit}>
        <Alert icon="lock">{t("models.keyNote")}</Alert>
        <Field label={t("models.name")}>{(id) => <input id={id} className="input" value={f.name} maxLength={120} onChange={(e) => setF({ ...f, name: e.target.value })} />}</Field>
        <div className="form-grid two">
          <Field label={t("models.model")} hint={t("models.modelHint")}>
            {(id) => <input id={id} className="input mono" value={f.model} maxLength={200} onChange={(e) => setF({ ...f, model: e.target.value })} />}
          </Field>
          <Field label={t("models.effort")} hint={t("models.effortHint")}>
            {(id) => (
              <select id={id} className="select" value={f.effort} onChange={(e) => setF({ ...f, effort: e.target.value })}>
                {["low", "medium", "high", "xhigh", "max"].map((x) => (
                  <option key={x} value={x}>
                    {x}
                  </option>
                ))}
              </select>
            )}
          </Field>
        </div>
        <Field label={t("models.key")} hint={t("models.keyHint")}>
          {(id) => <input id={id} className="input mono" type="password" autoComplete="off" value={f.api_key} onChange={(e) => setF({ ...f, api_key: e.target.value })} />}
        </Field>
        <Field label={t("models.baseUrl")} hint={t("models.baseUrlHint")}>
          {(id) => <input id={id} className="input mono" type="url" value={f.base_url} placeholder="https://api.anthropic.com" onChange={(e) => setF({ ...f, base_url: e.target.value })} />}
        </Field>
        {error ? (
          <Alert tone="bad" icon="alert">
            {error}
          </Alert>
        ) : null}
      </form>
    </Drawer>
  );
}

function BrandingSection() {
  const { t } = useI18n();
  const { branding, setBranding } = useSession();
  const toast = useToast();
  const [name, setName] = useState(branding.name);
  const [accent, setAccent] = useState(branding.accent || "#1f3fbf");
  const [useDefault, setUseDefault] = useState(!branding.accent);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    setName(branding.name);
    setAccent(branding.accent || "#1f3fbf");
    setUseDefault(!branding.accent);
  }, [branding]);
  const valid = /^#[0-9a-fA-F]{6}$/.test(accent);
  const ratio = valid ? contrastOnWhite(accent) : 0;
  const save = async () => {
    setBusy(true);
    try {
      const b = await put<Branding>("/branding", { name, accent: useDefault ? "" : accent });
      setBranding(b);
      toast(t("branding.saved"));
    } catch (e) {
      toast(errorText(e, t), "bad");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Panel
      title={t("settings.branding")}
      meta={t("branding.sub")}
      foot={
        <div className="row" style={{ justifyContent: "flex-end" }}>
          <Button variant="primary" loading={busy} disabled={!useDefault && !valid} onClick={save}>
            {t("common.save")}
          </Button>
        </div>
      }
    >
      <div className="panel-body form-grid">
        <Field label={t("branding.name")} hint={t("branding.nameHint")}>
          {(id) => <input id={id} className="input" style={{ maxWidth: 360 }} value={name} maxLength={60} placeholder="REMNANT" onChange={(e) => setName(e.target.value)} />}
        </Field>
        <div className="field">
          <span className="label">{t("branding.accent")}</span>
          <label className="check">
            <input type="checkbox" checked={useDefault} onChange={(e) => setUseDefault(e.target.checked)} />
            {t("branding.useDefault")}
          </label>
          {!useDefault ? (
            <div className="row">
              <input type="color" className="swatch" aria-label={t("branding.accent")} value={valid ? accent : "#1f3fbf"} onChange={(e) => setAccent(e.target.value)} />
              <input className="input mono" style={{ width: 120 }} value={accent} maxLength={7} aria-invalid={!valid || undefined} onChange={(e) => setAccent(e.target.value)} />
              <span className="small muted num">{valid ? t("branding.contrast", { r: ratio.toFixed(1) }) : t("branding.invalid")}</span>
            </div>
          ) : null}
          {!useDefault && valid && ratio < 4.5 ? (
            <Alert tone="warn" icon="alert">
              {t("branding.lowContrast")}
            </Alert>
          ) : null}
        </div>
        <div className="field">
          <span className="label">{t("branding.preview")}</span>
          <div className="row" style={{ padding: 12, border: "1px solid var(--rule)", borderRadius: 6 }}>
            <span className="brand" style={{ padding: 0 }}>
              <span className="brand-mark" style={{ background: useDefault ? undefined : accent }} aria-hidden="true">
                <Icon name="shield" size={14} />
              </span>
              {appName({ name, accent })}
            </span>
            <span className="spacer" />
            <button type="button" className="btn primary" style={useDefault ? undefined : { background: accent, borderColor: accent }}>
              {t("branding.previewButton")}
            </button>
          </div>
        </div>
      </div>
    </Panel>
  );
}

function SystemSection() {
  const { t } = useI18n();
  const { version, reloadVersion, can } = useSession();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const u = version?.update;
  useEffect(() => {
    reloadVersion();
  }, []);
  const install = async () => {
    setBusy(true);
    try {
      await post("/system/update");
      toast(t("system.installing"));
      // The server restarts; poll until the new version answers.
      const until = Date.now() + 120_000;
      const tick = async () => {
        reloadVersion();
        if (Date.now() < until) setTimeout(tick, 4000);
      };
      setTimeout(tick, 4000);
    } catch (e) {
      toast(errorText(e, t), "bad");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Panel title={t("settings.system")}>
      <div className="panel-body stack" style={{ gap: 16 }}>
        <div className="docheader" style={{ border: 0 }}>
          <dl>
            <div>
              <dt>{t("system.version")}</dt>
              <dd className="mono">{version?.version ?? "…"}</dd>
            </div>
            <div>
              <dt>{t("system.commit")}</dt>
              <dd className="mono">{version?.commit || "—"}</dd>
            </div>
            <div>
              <dt>{t("system.mode")}</dt>
              <dd>{u ? t(`system.mode.${u.mode}` as Key) : "—"}</dd>
            </div>
            <div>
              <dt>{t("system.latest")}</dt>
              <dd>
                {u?.latest ? (
                  u.release_url ? (
                    <a href={u.release_url} target="_blank" rel="noreferrer noopener">
                      {u.latest} <Icon name="external" size={12} />
                    </a>
                  ) : (
                    u.latest
                  )
                ) : (
                  "—"
                )}
              </dd>
            </div>
            <div>
              <dt>{t("system.lastChecked")}</dt>
              <dd>
                <Time iso={u?.last_checked} />
              </dd>
            </div>
            <div>
              <dt>{t("system.status")}</dt>
              <dd>{u ? t(`system.status.${u.status}` as Key) : "—"}</dd>
            </div>
          </dl>
        </div>
        {u?.last_error ? (
          <Alert tone="bad" icon="alert">
            {u.last_error}
          </Alert>
        ) : null}
        {u?.status === "disabled" ? <Alert>{t("system.disabledExplain")}</Alert> : null}
        {u?.available ? (
          <Alert tone="info" icon="update">
            {t("system.availableExplain", { v: u.latest ?? "" })}
          </Alert>
        ) : null}
        <p className="small muted prose">{t("system.signedNote")}</p>
        {can("admin") ? (
          <div className="row">
            <Button icon="refresh" onClick={reloadVersion}>
              {t("system.refresh")}
            </Button>
            <ConfirmButton variant="primary" icon="update" loading={busy} disabled={!u?.available} confirmLabel={t("system.installConfirm")} onConfirm={() => void install()}>
              {t("system.install", { v: u?.latest ?? "" })}
            </ConfirmButton>
          </div>
        ) : (
          <p className="small muted">{t("system.adminOnly")}</p>
        )}
      </div>
    </Panel>
  );
}

function ModeSection() {
  const { t } = useI18n();
  const { mode, setMode } = useSession();
  const users = useApi<UserRef[]>("/users");
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const target = mode === "team" ? "personal" : "team";
  const active = (users.data ?? []).filter((u) => !u.disabled).length;
  const change = async () => {
    setBusy(true);
    try {
      await put("/instance", { mode: target });
      setMode(target);
      toast(t("mode.changed", { mode: t(`mode.${target}` as Key) }));
    } catch (e) {
      toast(errorText(e, t), "bad");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Panel title={t("settings.mode")} meta={t("mode.sub")}>
      <div className="panel-body stack" style={{ gap: 16 }}>
        <div className="mode-cards">
          {(["personal", "team"] as const).map((m) => (
            <div key={m} className="mode-card" aria-current={mode === m} style={mode === m ? { borderColor: "var(--accent)" } : undefined}>
              <span className="row" style={{ gap: 10 }}>
                <Icon name={m === "team" ? "users" : "user"} size={18} />
                <strong>{t(`mode.${m}` as Key)}</strong>
                {mode === m ? <span className="state" data-tone="info" data-form="solid">{t("mode.current")}</span> : null}
              </span>
              <span className="small ink-2">{t(`mode.${m}.for` as Key)}</span>
              <ul className="bullets small">
                <li>{t(`mode.${m}.approval` as Key)}</li>
                <li>{t(`mode.${m}.patterns` as Key)}</li>
                <li>{t(`mode.${m}.users` as Key)}</li>
              </ul>
            </div>
          ))}
        </div>
        {target === "personal" && active > 1 ? (
          <Alert tone="warn" icon="alert">
            {t("mode.personalWithTeam", { n: active })}
          </Alert>
        ) : null}
        <p className="small muted prose">{t("mode.changeNote")}</p>
        <div>
          <ConfirmButton icon="shield" loading={busy} confirmLabel={t("mode.switchConfirm", { mode: t(`mode.${target}` as Key) })} onConfirm={() => void change()}>
            {t("mode.switchTo", { mode: t(`mode.${target}` as Key) })}
          </ConfirmButton>
        </div>
      </div>
    </Panel>
  );
}

function UsersSection() {
  const { t } = useI18n();
  const { me, mode } = useSession();
  const users = useApi<UserRef[]>("/users");
  const toast = useToast();
  const [open, setOpen] = useState(false);
  const [issued, setIssued] = useState<{ username: string; token: string } | null>(null);

  const update = async (u: UserRef, p: { role?: Role; disabled?: boolean }) => {
    try {
      await patch(`/users/${u.id}`, p);
      users.reload();
      toast(t("users.updated", { name: u.username }));
    } catch (e) {
      toast(errorText(e, t), "bad");
    }
  };
  const newToken = async (u: UserRef) => {
    try {
      const r = await post<{ token: string }>(`/users/${u.id}/tokens`, { name: "web" });
      setIssued({ username: u.username, token: r.token });
    } catch (e) {
      toast(errorText(e, t), "bad");
    }
  };

  return (
    <Panel
      title={t("settings.users")}
      meta={t("users.sub")}
      tools={
        <Button size="sm" icon="plus" onClick={() => setOpen(true)}>
          {t("users.new")}
        </Button>
      }
    >
      {mode === "personal" ? (
        <div className="panel-body">
          <Alert tone="info" icon="user">
            {t("users.personalNote")}
          </Alert>
        </div>
      ) : null}
      {issued ? (
        <div className="panel-body">
          <TokenOnce username={issued.username} token={issued.token} onDone={() => setIssued(null)} />
        </div>
      ) : null}
      {users.loading && !users.data ? (
        <SkeletonRows rows={3} cols={3} />
      ) : (
        <div className="table-wrap">
        <table className="table">
          <thead>
            <tr>
              <th>{t("users.name")}</th>
              <th>{t("users.role")}</th>
              <th>{t("users.state")}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {(users.data ?? []).map((u) => (
              <tr key={u.id}>
                <td className="title-cell">
                  {u.username}
                  {u.id === me.user_id ? <span className="sub">{t("users.you")}</span> : null}
                </td>
                <td>
                  <select className="select" style={{ width: "auto" }} aria-label={t("users.role")} value={u.role} disabled={u.disabled} onChange={(e) => void update(u, { role: e.target.value as Role })}>
                    {(["researcher", "reviewer", "admin"] as const).map((r) => (
                      <option key={r} value={r}>
                        {t(`role.${r}` as Key)}
                      </option>
                    ))}
                  </select>
                </td>
                <td>
                  {u.disabled ? (
                    <span className="state" data-tone="muted" data-form="stopped">
                      {t("users.disabled")}
                    </span>
                  ) : (
                    <span className="state" data-tone="ok" data-form="solid">
                      {t("users.active")}
                    </span>
                  )}
                </td>
                <td className="right">
                  <div className="row" style={{ justifyContent: "flex-end" }}>
                    {!u.disabled ? (
                      <Button size="sm" icon="key" onClick={() => void newToken(u)}>
                        {t("users.issueToken")}
                      </Button>
                    ) : null}
                    {u.disabled ? (
                      <Button size="sm" variant="ghost" onClick={() => void update(u, { disabled: false })}>
                        {t("users.enable")}
                      </Button>
                    ) : u.id !== me.user_id ? (
                      <ConfirmButton size="sm" variant="ghost" confirmLabel={t("users.disableConfirm")} onConfirm={() => void update(u, { disabled: true })}>
                        {t("users.disable")}
                      </ConfirmButton>
                    ) : null}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </div>
      )}
      <UserForm
        open={open}
        onClose={() => setOpen(false)}
        onCreated={(username, token) => {
          users.reload();
          setIssued({ username, token });
        }}
      />
    </Panel>
  );
}

function TokenOnce({ username, token, onDone }: { username: string; token: string; onDone: () => void }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  return (
    <div className="stack" style={{ gap: 8 }}>
      <Alert tone="warn" icon="key">
        {t("users.tokenOnce", { name: username })}
      </Alert>
      <div className="row" style={{ flexWrap: "nowrap" }}>
        <input className="input mono" readOnly value={token} aria-label={t("signin.token")} onFocus={(e) => e.target.select()} />
        <Button icon={copied ? "check" : "copy"} onClick={() => void navigator.clipboard?.writeText(token).then(() => setCopied(true))}>
          {copied ? t("common.copied") : t("common.copy")}
        </Button>
        <Button variant="ghost" onClick={onDone}>
          {t("common.close")}
        </Button>
      </div>
    </div>
  );
}

function UserForm({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (username: string, token: string) => void }) {
  const { t } = useI18n();
  const [username, setUsername] = useState("");
  const [role, setRole] = useState<Role>("researcher");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const ok = /^[a-z0-9][a-z0-9._-]{1,63}$/.test(username);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      const r = await post<{ token: string }>("/users", { username, role });
      onCreated(username, r.token);
      setUsername("");
      onClose();
    } catch (err) {
      setError(errorText(err, t));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={t("users.new")}
      foot={
        <>
          <Button onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" type="submit" form="user-form" loading={busy} disabled={!ok}>
            {t("users.create")}
          </Button>
        </>
      }
    >
      <form id="user-form" className="form-grid" onSubmit={submit}>
        <Field label={t("users.name")} hint={t("setup.usernameHint")} error={username && !ok ? t("setup.usernameInvalid") : undefined}>
          {(id) => <input id={id} className="input" value={username} autoFocus spellCheck={false} onChange={(e) => setUsername(e.target.value.toLowerCase())} />}
        </Field>
        <div className="field">
          <span className="label">{t("users.role")}</span>
          <Segmented<Role>
            label={t("users.role")}
            value={role}
            onChange={setRole}
            options={[
              { value: "researcher", label: t("role.researcher") },
              { value: "reviewer", label: t("role.reviewer") },
              { value: "admin", label: t("role.admin") },
            ]}
          />
          <span className="hint">{t(`role.${role}.explain` as Key)}</span>
        </div>
        {error ? (
          <Alert tone="bad" icon="alert">
            {error}
          </Alert>
        ) : null}
      </form>
    </Drawer>
  );
}
