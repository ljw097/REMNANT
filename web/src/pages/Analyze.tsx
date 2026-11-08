// Repository intake: paste a repository URL; REMNANT fetches it with its
// history, the Historian drafts root-cause patterns from past security
// fixes, and a run hunts for the same mistakes in the current code.
//
// The page is built so a fresh install needs two inputs to get a result: an
// API key (only while no model profile exists) and a repository URL. Every
// other choice has a sensible default and sits behind "Options".

import { useEffect, useState } from "react";
import { Frame } from "../App";
import { post } from "../api/client";
import type { Intake, IntakeOptions, LLMProfile, Pattern, Project } from "../api/types";
import { useApi } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { Link, navigate, useLocation } from "../router";
import { useSession } from "../session";
import { Alert, Button, errorText, Field, PageHead, Panel, SkeletonRows, Time } from "../ui/components";
import { ConnectModel } from "../ui/QuickStart";
import { IntakeState } from "./Intake";

interface IntakeConfig {
  repo_hosts: string[];
  defaults: IntakeOptions;
}

export function AnalyzePage() {
  const { t } = useI18n();
  const recent = useApi<Intake[]>("/intakes", 5000);
  const none = !recent.loading && (recent.data ?? []).length === 0;
  return (
    <Frame crumbs={[{ label: t("nav.analyze") }]}>
      <PageHead title={t("analyze.title")} sub={t("analyze.sub")} />
      <div className="stack" style={{ gap: 16 }}>
        <AnalyzeStart />
        {none ? <HowItWorks /> : null}
        {!none ? (
          <Panel title={t("analyze.recent")}>
            {recent.loading && !recent.data ? (
              <SkeletonRows rows={3} cols={3} />
            ) : (
              <ul className="item-list">
                {(recent.data ?? []).map((i) => (
                  <li key={i.id}>
                    <Link to={`/analyze/${i.id}`}>
                      <span className="t mono">{i.source_uri.replace(/^https:\/\//, "")}{i.source_ref ? `@${i.source_ref}` : ""}</span>
                      <IntakeState intake={i} />
                      <span className="m">
                        <span className="ref">[I-{i.id}]</span>
                        <span>{i.project_name}</span>
                        {i.status !== "fetching" ? <span>{t("analyze.counts", { found: i.commits_found, patterns: i.patterns.length })}</span> : null}
                        <Time iso={i.created_at} />
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </Panel>
        ) : null}
      </div>
    </Frame>
  );
}

function HowItWorks() {
  const { t } = useI18n();
  return (
    <ol className="flow-steps" aria-label={t("analyze.how")}>
      {(["fetch", "history", "patterns", "hunt"] as const).map((s, i) => (
        <li key={s}>
          <span className="flow-no">{i + 1}</span>
          <span>
            <strong>{t(`analyze.step.${s}` as Key)}</strong>
            <span className="small ink-2">{t(`analyze.step.${s}.what` as Key)}</span>
          </span>
        </li>
      ))}
    </ol>
  );
}

/**
 * Everything needed to start an analysis, in order: remote repositories
 * allowed, a model connected, then the URL. Shown on the Analyze page and on
 * the dashboard of an empty instance.
 */
export function AnalyzeStart() {
  const { t } = useI18n();
  const { can } = useSession();
  const cfg = useApi<IntakeConfig>("/intakes/config");
  const profiles = useApi<LLMProfile[]>("/llm-profiles");
  if (!can("researcher")) return null;
  if (!cfg.data || !profiles.data) {
    return cfg.error || profiles.error ? (
      <Alert tone="bad" icon="alert">
        {errorText((cfg.error ?? profiles.error)!, t)}
      </Alert>
    ) : (
      <Panel>
        <SkeletonRows rows={2} cols={2} />
      </Panel>
    );
  }
  if (cfg.data.repo_hosts.length === 0) {
    return (
      <Alert tone="warn" icon="lock">
        {t("analyze.hostsOff")}
      </Alert>
    );
  }
  if (profiles.data.length === 0) {
    return (
      <Panel title={t("quick.step1")} meta={t("quick.step1Sub")}>
        <div className="panel-body">
          {can("admin") ? (
            <ConnectModel onConnected={() => profiles.reload()} />
          ) : (
            <Alert icon="key">{t("analyze.noProfile")}</Alert>
          )}
        </div>
      </Panel>
    );
  }
  return <IntakeForm cfg={cfg.data} profiles={profiles.data} />;
}

function IntakeForm({ cfg, profiles }: { cfg: IntakeConfig; profiles: LLMProfile[] }) {
  const { t } = useI18n();
  const { query } = useLocation();
  const projects = useApi<Project[]>("/projects");
  const patterns = useApi<Pattern[]>("/patterns");
  const [url, setUrl] = useState(() => query.get("url") ?? "");
  const [ref, setRef] = useState("");
  const [projectID, setProjectID] = useState(0);
  const [profileID, setProfileID] = useState(profiles[0]?.id ?? 0);
  const [opts, setOpts] = useState<IntakeOptions>(cfg.defaults);
  const [showOptions, setShowOptions] = useState(false);
  const [advanced, setAdvanced] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  useEffect(() => {
    if (!profileID && profiles.length) setProfileID(profiles[0]!.id);
  }, [profiles]);
  const set = (p: Partial<IntakeOptions>) => setOpts((o) => ({ ...o, ...p }));
  const usable = (patterns.data ?? []).filter((p) => p.validation_status !== "rejected");
  const huntsSomething = opts.mine_history || opts.include_patterns.length > 0 || opts.auto_discover;
  const ok = url.trim().length > 3 && huntsSomething && (profileID || projectID);
  const profile = profiles.find((p) => p.id === profileID);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      const r = await post<Intake>("/intakes", {
        url: url.trim(),
        ref: ref.trim(),
        project_id: projectID || null,
        llm_profile_id: profileID || null,
        options: opts,
      });
      navigate(`/analyze/${r.id}`);
    } catch (err) {
      setError(errorText(err, t));
    } finally {
      setBusy(false);
    }
  };

  // A one-line account of what pressing the button will do.
  const plan = [
    opts.mine_history ? t("quick.plan.history") : null,
    opts.auto_discover ? t("quick.plan.discover") : null,
    opts.include_patterns.length ? t("quick.plan.patterns", { n: opts.include_patterns.length }) : null,
    opts.auto ? t("quick.plan.auto") : t("quick.plan.manual"),
    projectID ? (projects.data ?? []).find((p) => p.id === projectID)?.name : t("quick.plan.newProject"),
    profileID && profile ? `${profile.name} · ${profile.model}` : t("analyze.projectProfile"),
  ].filter(Boolean);

  return (
    <Panel title={t("analyze.form")} meta={t("analyze.hosts", { hosts: cfg.repo_hosts.join(", ") })}>
      <form className="panel-body form-grid" onSubmit={submit}>
        <Field label={t("analyze.url")} hint={t("analyze.urlHint")}>
          {(id) => (
            <div className="quick-row">
              <input id={id} className="input mono input-lg" value={url} autoFocus spellCheck={false} placeholder="https://github.com/org/repo" onChange={(e) => setUrl(e.target.value)} />
              <Button type="submit" variant="primary" size="lg" icon="play" loading={busy} disabled={!ok}>
                {t("analyze.start")}
              </Button>
            </div>
          )}
        </Field>
        <div className="quick-plan">
          <span className="small ink-2">{plan.join(" · ")}</span>
          <Button variant="ghost" size="sm" icon={showOptions ? "chevronUp" : "chevronDown"} aria-expanded={showOptions} onClick={() => setShowOptions((s) => !s)}>
            {t("quick.options")}
          </Button>
        </div>
        {!huntsSomething ? (
          <Alert tone="warn" icon="alert">
            {t("analyze.needPatterns")}
          </Alert>
        ) : null}
        {error ? (
          <Alert tone="bad" icon="alert">
            {error}
          </Alert>
        ) : null}
        {showOptions ? (
          <div className="stack quick-options" style={{ gap: 12 }}>
            <label className="rule boxed">
              <input type="checkbox" checked={opts.auto} onChange={(e) => set({ auto: e.target.checked })} />
              <span>
                <strong>{t("analyze.auto")}</strong>
                <span className="small ink-2">{t("analyze.autoHint")}</span>
              </span>
            </label>
            <label className="rule boxed">
              <input type="checkbox" checked={opts.mine_history} onChange={(e) => set({ mine_history: e.target.checked })} />
              <span>
                <strong>{t("analyze.mine")}</strong>
                <span className="small ink-2">{t("analyze.mineHint")}</span>
              </span>
            </label>
            <label className="rule boxed">
              <input type="checkbox" checked={opts.auto_discover} onChange={(e) => set({ auto_discover: e.target.checked })} />
              <span>
                <strong>{t("analyze.discover")}</strong>
                <span className="small ink-2">{t("analyze.discoverHint")}</span>
              </span>
            </label>
            <div className="form-grid two">
              <Field label={t("analyze.ref")} hint={t("analyze.refHint")}>
                {(id) => <input id={id} className="input mono" value={ref} spellCheck={false} placeholder="main" onChange={(e) => setRef(e.target.value)} />}
              </Field>
              <Field label={t("analyze.profile")} hint={t("analyze.profileHint")}>
                {(id) => (
                  <select id={id} className="select" value={profileID} onChange={(e) => setProfileID(Number(e.target.value))}>
                    {projectID ? <option value={0}>{t("analyze.projectProfile")}</option> : null}
                    {profiles.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name} ({p.model})
                      </option>
                    ))}
                  </select>
                )}
              </Field>
            </div>
            <Field label={t("analyze.project")} hint={t("analyze.projectHint")}>
              {(id) => (
                <select id={id} className="select" value={projectID} onChange={(e) => setProjectID(Number(e.target.value))}>
                  <option value={0}>{t("analyze.newProject")}</option>
                  {(projects.data ?? []).map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </select>
              )}
            </Field>
            <div>
              <Button variant="ghost" size="sm" icon={advanced ? "chevronUp" : "chevronDown"} aria-expanded={advanced} onClick={() => setAdvanced((a) => !a)}>
                {t("analyze.advanced")}
              </Button>
            </div>
            {advanced ? (
              <div className="stack" style={{ gap: 12 }}>
                <div className="advanced-grid" style={{ padding: 0 }}>
                  <NumField label={t("analyze.depth")} hint={t("analyze.depthHint")} value={opts.history_depth} min={10} max={5000} onChange={(v) => set({ history_depth: v })} disabled={!opts.mine_history} />
                  <NumField label={t("analyze.commits")} hint={t("analyze.commitsHint")} value={opts.max_commits} min={1} max={40} onChange={(v) => set({ max_commits: v })} disabled={!opts.mine_history} />
                  <NumField label={t("analyze.maxPatterns")} hint={t("analyze.maxPatternsHint")} value={opts.max_patterns} min={1} max={30} onChange={(v) => set({ max_patterns: v })} disabled={!opts.mine_history} />
                  <NumField label={t("analyze.tokens")} hint={t("analyze.tokensHint")} value={opts.max_tokens} min={4000} max={1000000} step={10000} onChange={(v) => set({ max_tokens: v })} disabled={!opts.mine_history} />
                </div>
                <Field label={t("analyze.include")} hint={t("analyze.includeHint")}>
                  {(id) => (
                    <select
                      id={id}
                      className="select"
                      multiple
                      size={Math.min(6, Math.max(3, usable.length))}
                      value={opts.include_patterns.map(String)}
                      onChange={(e) => set({ include_patterns: [...e.target.selectedOptions].map((o) => Number(o.value)) })}
                    >
                      {usable.map((p) => (
                        <option key={p.id} value={p.id}>
                          [P-{p.id}] {p.title} ({p.language || "—"})
                        </option>
                      ))}
                    </select>
                  )}
                </Field>
                <p className="small muted">{t("analyze.limitsNote")}</p>
              </div>
            ) : null}
          </div>
        ) : null}
      </form>
    </Panel>
  );
}

function NumField({ label, hint, value, min, max, step = 1, onChange, disabled }: { label: string; hint: string; value: number; min: number; max: number; step?: number; onChange: (v: number) => void; disabled?: boolean }) {
  return (
    <Field label={label} hint={hint}>
      {(id) => <input id={id} className="input" type="number" min={min} max={max} step={step} value={value} disabled={disabled} onChange={(e) => onChange(Number(e.target.value) || min)} />}
    </Field>
  );
}
