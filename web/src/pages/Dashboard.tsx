import { useMemo, useState } from "react";
import { Frame } from "../App";
import { useApi } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { Link } from "../router";
import { appName, useSession } from "../session";
import type { Approval, Hypothesis, Project, Run } from "../api/types";
import { defaultDashboard, type PanelId, type PanelPref } from "../prefs";
import { Button, Empty, ErrorNote, PageHead, Panel, Priority, SkeletonRows, State, Time, useToast } from "../ui/components";
import { StageLine } from "../ui/domain";
import { hypothesisLook, runLook, terminalRun } from "../ui/status";
import { Icon, type IconName } from "../ui/Icon";
import { AnalyzeStart } from "./Analyze";

function browserZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

function StatCards() {
  const { t } = useI18n();
  const { summary: s, can } = useSession();
  const usage = useApi<{ today: { output_tokens: number; input_tokens: number; calls: number } }>(`/usage?days=1&tz=${encodeURIComponent(browserZone())}`, 60_000);
  const today = usage.data?.today;
  const cards: { to: string; icon: IconName; label: Key; value?: number | string; sub?: string; tone?: "ok" | "warn" | "info"; show?: boolean }[] = [
    { to: "/runs", icon: "runs", label: "stat.activeRuns", value: s?.active_runs, sub: s && s.paused_runs > 0 ? t("stat.paused", { n: s.paused_runs }) : undefined, tone: "info" },
    { to: "/review", icon: "review", label: "stat.awaitingTriage", value: s?.awaiting_triage, tone: s && s.awaiting_triage > 0 ? "warn" : undefined, show: can("reviewer") },
    { to: "/approvals", icon: "approvals", label: "stat.pendingApprovals", value: s?.pending_approvals, tone: s && s.pending_approvals > 0 ? "warn" : undefined },
    { to: "/findings", icon: "findings", label: "stat.validated", value: s?.validated, sub: s && s.validation_pending > 0 ? t("stat.inValidation", { n: s.validation_pending }) : undefined, tone: "ok" },
    { to: "/projects", icon: "projects", label: "stat.projects", value: s?.projects, sub: t("stat.targets", { n: s?.targets ?? 0 }) },
    {
      to: "/usage",
      icon: "chart",
      label: "stat.tokensToday",
      value: today ? today.output_tokens.toLocaleString() : undefined,
      sub: today ? t("stat.tokensTodaySub", { i: today.input_tokens.toLocaleString(), c: today.calls }) : undefined,
    },
  ];
  return (
    <div className="statcards">
      {cards
        .filter((c) => c.show !== false)
        .map((c) => (
          <Link key={c.to} to={c.to} className="statcard" data-tone={c.tone}>
            <span className="statcard-top">
              <Icon name={c.icon} size={15} />
              {t(c.label)}
            </span>
            <span className="statcard-value">{c.value ?? "—"}</span>
            <span className="statcard-sub">{c.sub ?? "\u00a0"}</span>
          </Link>
        ))}
    </div>
  );
}

export function Dashboard() {
  const { t } = useI18n();
  const { prefs, savePrefs, me, branding, summary, can } = useSession();
  const [editing, setEditing] = useState(false);
  const panels = prefs.dashboard.filter((p) => p.visible && (p.id !== "system" || me.role === "admin"));

  // A fresh instance has nothing to show yet: instead of a wall of zeros and
  // empty panels, offer the one thing to do next.
  if (summary && summary.projects === 0 && can("researcher")) {
    return (
      <Frame crumbs={[{ label: t("nav.dashboard") }]}>
        <PageHead title={t("dash.start.title")} sub={t("dash.start.sub")} />
        <div className="stack" style={{ gap: 16, maxWidth: 880 }}>
          <AnalyzeStart />
          <p className="small muted">
            <Link to="/projects?new=1">{t("dash.start.manual")}</Link>
          </p>
        </div>
      </Frame>
    );
  }

  return (
    <Frame crumbs={[{ label: t("nav.dashboard") }]}>
      <PageHead
        title={t("dash.greeting", { name: me.username })}
        sub={t("dash.sub", { app: appName(branding) })}
        actions={
          <Button icon="layout" onClick={() => setEditing((e) => !e)} aria-expanded={editing}>
            {editing ? t("dash.doneEditing") : t("dash.customize")}
          </Button>
        }
      />
      {editing ? <DashboardEditor value={prefs.dashboard} onSave={(d) => savePrefs({ dashboard: d })} onClose={() => setEditing(false)} /> : null}
      <StatCards />
      {panels.length === 0 ? (
        <Panel>
          <Empty title={t("dash.emptyTitle")} body={t("dash.emptyBody")} action={<Button onClick={() => setEditing(true)}>{t("dash.customize")}</Button>} />
        </Panel>
      ) : (
        <div className="dash">
          {panels.map((p) => (
            <DashPanel key={p.id} id={p.id} />
          ))}
        </div>
      )}
    </Frame>
  );
}

const panelSize: Record<PanelId, "wide" | "half" | "third"> = {
  waiting: "wide",
  runs: "wide",
  findings: "half",
  recentRuns: "half",
  projects: "half",
  system: "half",
};

function DashPanel({ id }: { id: PanelId }) {
  switch (id) {
    case "waiting":
      return <WaitingPanel />;
    case "runs":
      return <RunsInMotion />;
    case "findings":
      return <FindingsPanel />;
    case "recentRuns":
      return <RecentRuns />;
    case "projects":
      return <ProjectsPanel />;
    case "system":
      return <SystemPanel />;
  }
}

function WaitingPanel() {
  const { t } = useI18n();
  const { summary } = useSession();
  const hyps = useApi<Hypothesis[]>("/hypotheses?status=draft,triage", 60_000);
  const approvals = useApi<Approval[]>("/approvals?decision=pending", 60_000);
  return (
    <Panel title={t("dash.waiting")} size={panelSize.waiting}>
      <div className="waiting">
        <section>
          <h3>
            <span className="big">{summary?.awaiting_triage ?? "–"}</span>
            {t("dash.waiting.triage")}
            <span className="spacer" />
            <Link to="/review" className="small">
              {t("dash.waiting.startReview")}
            </Link>
          </h3>
          {hyps.loading && !hyps.data ? (
            <SkeletonRows rows={3} cols={2} />
          ) : hyps.error ? (
            <ErrorNote error={hyps.error} retry={hyps.reload} />
          ) : (hyps.data ?? []).length === 0 ? (
            <p className="small muted" style={{ padding: "0 var(--pad) 14px" }}>
              {t("dash.waiting.noTriage")}
            </p>
          ) : (
            <ul className="item-list">
              {(hyps.data ?? []).slice(0, 5).map((h) => (
                <li key={h.id}>
                  <Link to={`/hypotheses/${h.id}?queue=triage`}>
                    <span className="t">{h.title}</span>
                    <Priority level={h.triage_confidence} />
                    <span className="m">
                      <span className="ref">[H-{h.id}]</span>
                      <span>{h.vulnerability_class}</span>
                      <Time iso={h.updated_at} />
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </section>
        <section>
          <h3>
            <span className="big">{summary?.pending_approvals ?? "–"}</span>
            {t("dash.waiting.approvals")}
            <span className="spacer" />
            <Link to="/approvals" className="small">
              {t("common.viewAll")}
            </Link>
          </h3>
          {approvals.loading && !approvals.data ? (
            <SkeletonRows rows={3} cols={2} />
          ) : approvals.error ? (
            <ErrorNote error={approvals.error} retry={approvals.reload} />
          ) : (approvals.data ?? []).length === 0 ? (
            <p className="small muted" style={{ padding: "0 var(--pad) 14px" }}>
              {t("dash.waiting.noApprovals")}
            </p>
          ) : (
            <ul className="item-list">
              {(approvals.data ?? []).slice(0, 5).map((a) => (
                <li key={a.id}>
                  <Link to={`/approvals?open=${a.id}`}>
                    <span className="t">{a.hypothesis_title}</span>
                    <span className="mono small">{a.scope.operation}</span>
                    <span className="m">
                      <span className="ref">[A-{a.id}]</span>
                      <span>{t("approvals.requestedBy", { name: a.requested_by_name ?? "" })}</span>
                      <span>
                        {t("approvals.expires")} <Time iso={a.expires_at} />
                      </span>
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>
    </Panel>
  );
}

function useProjectNames(): Map<number, string> {
  const p = useApi<Project[]>("/projects");
  return useMemo(() => new Map((p.data ?? []).map((x) => [x.id, x.name])), [p.data]);
}

function RunsInMotion() {
  const { t } = useI18n();
  const runs = useApi<Run[]>("/runs", 15_000);
  const names = useProjectNames();
  const moving = (runs.data ?? []).filter((r) => !terminalRun(r.status)).slice(0, 8);
  return (
    <Panel
      title={t("dash.runs")}
      size={panelSize.runs}
      meta={moving.length ? t("dash.runs.count", { n: moving.length }) : undefined}
      tools={
        <Link to="/runs/new" className="btn sm">
          <Icon name="plus" />
          {t("runs.new")}
        </Link>
      }
    >
      {runs.loading && !runs.data ? (
        <SkeletonRows rows={3} cols={3} />
      ) : runs.error ? (
        <ErrorNote error={runs.error} retry={runs.reload} />
      ) : moving.length === 0 ? (
        <Empty title={t("dash.runs.emptyTitle")} body={t("dash.runs.emptyBody")} action={<Link to="/analyze">{t("nav.newAnalysis")}</Link>} />
      ) : (
        <div className="run-lines">
          {moving.map((r) => (
            <Link key={r.id} to={`/runs/${r.id}`} className="run-line">
              <span style={{ minWidth: 0 }}>
                <strong style={{ display: "block", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  {names.get(r.project_id) ?? t("common.project", { id: r.project_id })}
                </strong>
                <span className="small muted">
                  <span className="ref">[R-{r.id}]</span> <Time iso={r.queued_at} />
                </span>
              </span>
              <StageLine status={r.status} resumeStage={r.resume_stage} resumeStatus={r.resume_status} mini />
              <State look={runLook(r.status)} />
            </Link>
          ))}
        </div>
      )}
    </Panel>
  );
}

function FindingsPanel() {
  const { t } = useI18n();
  const f = useApi<Hypothesis[]>("/hypotheses?status=validated,validation_pending", 60_000);
  return (
    <Panel title={t("dash.findings")} size={panelSize.findings} tools={<Link to="/findings" className="small">{t("common.viewAll")}</Link>}>
      {f.loading && !f.data ? (
        <SkeletonRows rows={3} cols={2} />
      ) : f.error ? (
        <ErrorNote error={f.error} retry={f.reload} />
      ) : (f.data ?? []).length === 0 ? (
        <Empty title={t("findings.emptyTitle")} body={t("findings.emptyBody")} />
      ) : (
        <ul className="item-list">
          {(f.data ?? []).slice(0, 6).map((h) => (
            <li key={h.id}>
              <Link to={`/hypotheses/${h.id}`}>
                <span className="t">{h.title}</span>
                <State look={hypothesisLook(h.status)} />
                <span className="m">
                  <span className="ref">[H-{h.id}]</span>
                  {h.cwe_id ? <span>{h.cwe_id}</span> : null}
                  <Time iso={h.updated_at} />
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

function RecentRuns() {
  const { t } = useI18n();
  const runs = useApi<Run[]>("/runs", 30_000);
  const names = useProjectNames();
  const done = (runs.data ?? []).filter((r) => terminalRun(r.status) || r.status === "review_required").slice(0, 6);
  return (
    <Panel title={t("dash.recentRuns")} size={panelSize.recentRuns} tools={<Link to="/runs" className="small">{t("common.viewAll")}</Link>}>
      {runs.loading && !runs.data ? (
        <SkeletonRows rows={3} cols={2} />
      ) : done.length === 0 ? (
        <Empty title={t("dash.recentRuns.empty")} />
      ) : (
        <ul className="item-list">
          {done.map((r) => (
            <li key={r.id}>
              <Link to={`/runs/${r.id}`}>
                <span className="t">{names.get(r.project_id) ?? t("common.project", { id: r.project_id })}</span>
                <State look={runLook(r.status)} />
                <span className="m">
                  <span className="ref">[R-{r.id}]</span>
                  <Time iso={r.completed_at ?? r.queued_at} />
                  {r.error_summary ? <span style={{ color: "var(--bad)" }}>{r.error_summary.slice(0, 80)}</span> : null}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

function ProjectsPanel() {
  const { t } = useI18n();
  const p = useApi<Project[]>("/projects");
  return (
    <Panel title={t("nav.projects")} size={panelSize.projects} tools={<Link to="/projects" className="small">{t("common.viewAll")}</Link>}>
      {p.loading && !p.data ? (
        <SkeletonRows rows={3} cols={2} />
      ) : (p.data ?? []).length === 0 ? (
        <Empty title={t("projects.emptyTitle")} body={t("projects.emptyBody")} action={<Link to="/projects?new=1">{t("projects.new")}</Link>} />
      ) : (
        <ul className="item-list">
          {(p.data ?? []).slice(0, 6).map((x) => (
            <li key={x.id}>
              <Link to={`/projects/${x.id}`}>
                <span className="t">{x.name}</span>
                <span className="small muted num">{t("projects.targetsN", { n: x.target_count })}</span>
                <span className="m">
                  {x.active_runs ? <span>{t("projects.activeRunsN", { n: x.active_runs })}</span> : null}
                  <span>{x.policy.dynamic_mode === "local_lab_only" ? t("policy.labAllowedShort") : t("policy.labDisabledShort")}</span>
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

function SystemPanel() {
  const { t } = useI18n();
  const { version } = useSession();
  const u = version?.update;
  return (
    <Panel title={t("dash.system")} size={panelSize.system} tools={<Link to="/settings/system" className="small">{t("common.details")}</Link>}>
      <div className="panel-body stack" style={{ gap: 10 }}>
        <div className="row">
          <strong className="mono">{version?.version ?? "…"}</strong>
          {u?.available ? (
            <span className="state" data-tone="info" data-form="waiting">
              {t("system.updateTo", { v: u.latest ?? "" })}
            </span>
          ) : u ? (
            <span className="state" data-tone={u.status === "disabled" ? "muted" : "ok"} data-form={u.status === "disabled" ? "stopped" : "solid"}>
              {u.status === "disabled" ? t("system.disabled") : t("system.upToDate")}
            </span>
          ) : null}
        </div>
        {u?.last_checked ? (
          <p className="small muted">
            {t("system.lastChecked")} <Time iso={u.last_checked} />
          </p>
        ) : null}
        {u?.last_error ? <p className="small" style={{ color: "var(--bad)" }}>{u.last_error}</p> : null}
      </div>
    </Panel>
  );
}

export function DashboardEditor({ value, onSave, onClose }: { value: PanelPref[]; onSave: (d: PanelPref[]) => Promise<void>; onClose?: () => void }) {
  const { t } = useI18n();
  const { me } = useSession();
  const toast = useToast();
  const [items, setItems] = useState(value.filter((p) => p.id !== "system" || me.role === "admin"));
  const [saving, setSaving] = useState(false);
  const move = (i: number, d: -1 | 1) =>
    setItems((xs) => {
      const j = i + d;
      if (j < 0 || j >= xs.length) return xs;
      const next = [...xs];
      [next[i], next[j]] = [next[j]!, next[i]!];
      return next;
    });
  const save = async () => {
    setSaving(true);
    try {
      await onSave(items);
      toast(t("dash.saved"));
      onClose?.();
    } catch {
      toast(t("error.save"), "bad");
    } finally {
      setSaving(false);
    }
  };
  return (
    <Panel
      title={t("dash.editTitle")}
      meta={t("dash.editHint")}
      foot={
        <div className="row" style={{ justifyContent: "flex-end" }}>
          <Button variant="ghost" onClick={() => setItems(defaultDashboard(me.role))}>
            {t("dash.reset")}
          </Button>
          {onClose ? <Button onClick={onClose}>{t("common.cancel")}</Button> : null}
          <Button variant="primary" loading={saving} onClick={save}>
            {t("common.save")}
          </Button>
        </div>
      }
    >
      <ol className="dash-edit list-plain panel-body">
        {items.map((p, i) => (
          <li key={p.id} data-hidden={!p.visible}>
            <label className="check" style={{ flex: 1 }}>
              <input type="checkbox" checked={p.visible} onChange={(e) => setItems((xs) => xs.map((x) => (x.id === p.id ? { ...x, visible: e.target.checked } : x)))} />
              <span className="lbl">{t(`dash.panel.${p.id}` as Key)}</span>
            </label>
            <Button size="sm" variant="ghost" iconOnly icon="chevronUp" aria-label={t("dash.moveUp")} disabled={i === 0} onClick={() => move(i, -1)} />
            <Button size="sm" variant="ghost" iconOnly icon="chevronDown" aria-label={t("dash.moveDown")} disabled={i === items.length - 1} onClick={() => move(i, 1)} />
          </li>
        ))}
      </ol>
    </Panel>
  );
}
