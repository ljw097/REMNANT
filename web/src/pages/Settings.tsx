import { useEffect, useRef, useState } from "react";
import { Frame } from "../App";
import { del, patch, post, put } from "../api/client";
import { useApi } from "../hooks";
import { useI18n, type Key, type Lang } from "../i18n";
import { Link } from "../router";
import { appName, useSession } from "../session";
import type { Branding, InstancePolicy, Role, UserRef } from "../api/types";
import { PolicyEditor } from "../ui/policy";
import { NewPasswordFields, PasswordForm, passwordProblem } from "./Password";
import { columnChoices, contrastOnWhite, startPages, type ColumnSet, type Prefs } from "../prefs";
import { Alert, Button, ConfirmButton, Drawer, errorText, Field, PageHead, Panel, Segmented, SkeletonRows, Time, useToast } from "../ui/components";
import { DashboardEditor } from "./Dashboard";
import { AgentsSection, ModelsSection, ResearchSection, ServerConfigPanel } from "./SettingsExpert";
import { Icon, type IconName } from "../ui/Icon";

type Section = "preferences" | "dashboard" | "account" | "mode" | "users" | "models" | "agents" | "research" | "branding" | "system";

const sections: { id: Section; icon: IconName; admin?: boolean }[] = [
  { id: "preferences", icon: "sliders" },
  { id: "dashboard", icon: "layout" },
  { id: "account", icon: "user" },
  { id: "mode", icon: "shield", admin: true },
  { id: "users", icon: "users", admin: true },
  { id: "models", icon: "key", admin: true },
  { id: "agents", icon: "cpu", admin: true },
  { id: "research", icon: "sliders", admin: true },
  { id: "branding", icon: "palette", admin: true },
  { id: "system", icon: "update" },
];

export function SettingsPage({ section }: { section: string }) {
  const { t } = useI18n();
  const { can, policy } = useSession();
  // On a personal instance the only user is the administrator; the badges add nothing.
  const badges = policy.profile !== "personal";
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
              {s.admin && badges ? <span className="count">{t("role.admin")}</span> : null}
            </Link>
          ))}
        </nav>
        <div className="stack">
          {current === "preferences" ? <PreferencesSection /> : null}
          {current === "dashboard" ? <DashboardSection /> : null}
          {current === "account" ? <AccountSection /> : null}
          {current === "mode" ? <ModeSection /> : null}
          {current === "users" ? <UsersSection /> : null}
          {current === "agents" ? <AgentsSection /> : null}
          {current === "research" ? <ResearchSection /> : null}
          {current === "models" ? <ModelsSection /> : null}
          {current === "branding" ? <BrandingSection /> : null}
          {current === "system" ? <SystemSection /> : null}
          {current === "system" && can("admin") ? <ServerConfigPanel /> : null}
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
  const toast = useToast();
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
        <div>
          <Button icon="logout" onClick={signOut}>
            {t("nav.signOut")}
          </Button>
        </div>
      </div>
      <div className="panel-body stack" style={{ gap: 12, borderTop: "1px solid var(--rule)" }}>
        <h3>{t("password.change")}</h3>
        <p className="small muted prose">{t("password.changeNote")}</p>
        <PasswordForm onDone={() => toast(t("password.changed"))} />
      </div>
    </Panel>
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
  const { policy, presets, setPolicy } = useSession();
  const users = useApi<UserRef[]>("/users");
  const toast = useToast();
  const [draft, setDraft] = useState<InstancePolicy>(policy);
  const [busy, setBusy] = useState(false);
  useEffect(() => setDraft(policy), [policy]);
  const active = (users.data ?? []).filter((u) => !u.disabled && !u.pending).length;
  const changed = JSON.stringify(draft) !== JSON.stringify(policy);
  const weakens = (draft.self_approval && !policy.self_approval) || (draft.self_review && !policy.self_review);
  const save = async () => {
    setBusy(true);
    try {
      const r = await put<{ policy: InstancePolicy }>("/instance", { policy: draft });
      setPolicy(r.policy);
      toast(t("policy.saved", { profile: t(`profile.${r.policy.profile}` as Key) }));
    } catch (e) {
      toast(errorText(e, t), "bad");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Panel title={t("settings.mode")} meta={t("mode.sub")}>
      <div className="panel-body stack" style={{ gap: 16 }}>
        <PolicyEditor value={draft} presets={presets ?? undefined} onChange={setDraft} />
        {weakens && active > 1 ? (
          <Alert tone="warn" icon="alert">
            {t("mode.personalWithTeam", { n: active })}
          </Alert>
        ) : null}
        <p className="small muted prose">{t("mode.changeNote")}</p>
        <div className="row">
          {weakens ? (
            <ConfirmButton variant="primary" icon="shield" loading={busy} disabled={!changed} confirmLabel={t("policy.confirmWeaken")} onConfirm={() => void save()}>
              {t("policy.save")}
            </ConfirmButton>
          ) : (
            <Button variant="primary" icon="shield" loading={busy} disabled={!changed} onClick={() => void save()}>
              {t("policy.save")}
            </Button>
          )}
          {changed ? (
            <Button variant="ghost" onClick={() => setDraft(policy)}>
              {t("common.cancel")}
            </Button>
          ) : null}
        </div>
      </div>
    </Panel>
  );
}

function UsersSection() {
  const { t } = useI18n();
  const { me, policy } = useSession();
  const users = useApi<UserRef[]>("/users");
  const toast = useToast();
  const [open, setOpen] = useState(false);
  const [issued, setIssued] = useState<{ username: string; secret: string; kind: "token" | "password" } | null>(null);

  const update = async (u: UserRef, p: { role?: Role; disabled?: boolean; approve?: boolean }) => {
    try {
      await patch(`/users/${u.id}`, p);
      users.reload();
      toast(t(p.approve ? "users.approved" : "users.updated", { name: u.username }));
    } catch (e) {
      toast(errorText(e, t), "bad");
    }
  };
  const newToken = async (u: UserRef) => {
    try {
      const r = await post<{ token: string }>(`/users/${u.id}/tokens`, { name: "api" });
      setIssued({ username: u.username, secret: r.token, kind: "token" });
    } catch (e) {
      toast(errorText(e, t), "bad");
    }
  };
  const resetPassword = async (u: UserRef) => {
    try {
      const r = await post<{ temporary_password: string }>(`/users/${u.id}/password`);
      setIssued({ username: u.username, secret: r.temporary_password, kind: "password" });
    } catch (e) {
      toast(errorText(e, t), "bad");
    }
  };
  const decline = async (u: UserRef) => {
    try {
      await del(`/users/${u.id}`);
      users.reload();
      toast(t("users.declined", { name: u.username }));
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
      {policy.self_approval || policy.self_review ? (
        <div className="panel-body">
          <Alert tone="info" icon="user">
            {t("users.personalNote")}
          </Alert>
        </div>
      ) : null}
      {issued ? (
        <div className="panel-body">
          <SecretOnce username={issued.username} secret={issued.secret} kind={issued.kind} onDone={() => setIssued(null)} />
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
                  <select className="select" style={{ width: "auto" }} aria-label={t("users.role")} value={u.role} disabled={u.disabled || u.pending} onChange={(e) => void update(u, { role: e.target.value as Role })}>
                    {(["researcher", "reviewer", "admin"] as const).map((r) => (
                      <option key={r} value={r}>
                        {t(`role.${r}` as Key)}
                      </option>
                    ))}
                  </select>
                </td>
                <td>
                  {u.pending ? (
                    <span className="state" data-tone="warn" data-form="waiting">
                      {t("users.pending")}
                    </span>
                  ) : u.disabled ? (
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
                  <div className="row" style={{ justifyContent: "flex-end", flexWrap: "nowrap", whiteSpace: "nowrap" }}>
                    {u.pending ? (
                      <>
                        <Button size="sm" variant="primary" icon="check" onClick={() => void update(u, { approve: true })}>
                          {t("users.approve")}
                        </Button>
                        <ConfirmButton size="sm" variant="ghost" confirmLabel={t("users.declineConfirm")} onConfirm={() => void decline(u)}>
                          {t("users.decline")}
                        </ConfirmButton>
                      </>
                    ) : null}
                    {!u.disabled && !u.pending && u.id !== me.user_id ? (
                      <ConfirmButton size="sm" icon="lock" confirmLabel={t("users.resetConfirm")} onConfirm={() => void resetPassword(u)}>
                        {t("users.resetPassword")}
                      </ConfirmButton>
                    ) : null}
                    {!u.disabled && !u.pending ? (
                      <Button size="sm" variant="ghost" icon="key" onClick={() => void newToken(u)}>
                        {t("users.issueToken")}
                      </Button>
                    ) : null}
                    {u.pending ? null : u.disabled ? (
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
        onCreated={(username, temporary) => {
          users.reload();
          if (temporary) setIssued({ username, secret: temporary, kind: "password" });
          else toast(t("users.created", { name: username }));
        }}
      />
    </Panel>
  );
}

/** A temporary password or API token, shown once right after it is issued. */
function SecretOnce({ username, secret, kind, onDone }: { username: string; secret: string; kind: "token" | "password"; onDone: () => void }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  return (
    <div className="stack" style={{ gap: 8 }}>
      <Alert tone="warn" icon="key">
        {kind === "password" ? t("users.passwordOnce", { name: username }) : t("users.tokenOnce", { name: username })}
      </Alert>
      <div className="row" style={{ flexWrap: "nowrap" }}>
        <input className="input mono" readOnly value={secret} aria-label={kind === "password" ? t("password.temporary") : t("users.apiToken")} onFocus={(e) => e.target.select()} />
        <Button icon={copied ? "check" : "copy"} onClick={() => void navigator.clipboard?.writeText(secret).then(() => setCopied(true))}>
          {copied ? t("common.copied") : t("common.copy")}
        </Button>
        <Button variant="ghost" onClick={onDone}>
          {t("common.close")}
        </Button>
      </div>
    </div>
  );
}

function UserForm({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (username: string, temporaryPassword?: string) => void }) {
  const { t } = useI18n();
  const [username, setUsername] = useState("");
  const [role, setRole] = useState<Role>("researcher");
  const [setOwn, setSetOwn] = useState(false);
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const ok = /^[a-z0-9][a-z0-9._-]{1,63}$/.test(username) && (!setOwn || (!!confirm && !passwordProblem(username, password, confirm)));
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      const r = await post<{ temporary_password?: string }>("/users", { username, role, password: setOwn ? password : "" });
      onCreated(username, r.temporary_password);
      setUsername("");
      setPassword("");
      setConfirm("");
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
        <div className="field">
          <span className="label">{t("users.password")}</span>
          <Segmented<"temp" | "own">
            label={t("users.password")}
            value={setOwn ? "own" : "temp"}
            onChange={(v) => setSetOwn(v === "own")}
            options={[
              { value: "temp", label: t("users.passwordTemp") },
              { value: "own", label: t("users.passwordOwn") },
            ]}
          />
          <span className="hint">{setOwn ? t("users.passwordOwnHint") : t("users.passwordTempHint")}</span>
        </div>
        {setOwn ? <NewPasswordFields username={username} value={password} confirm={confirm} onValue={setPassword} onConfirm={setConfirm} /> : null}
        {error ? (
          <Alert tone="bad" icon="alert">
            {error}
          </Alert>
        ) : null}
      </form>
    </Drawer>
  );
}
