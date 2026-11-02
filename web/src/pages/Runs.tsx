import { useMemo, useState } from "react";
import { Frame } from "../App";
import { useApi } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { Link, navigate, useLocation } from "../router";
import { useSession } from "../session";
import type { Project, Run, RunStatus } from "../api/types";
import { Empty, ErrorNote, PageHead, Panel, SkeletonRows, State, Time } from "../ui/components";
import { StageLine } from "../ui/domain";
import { ColumnPicker, useColumns } from "../ui/ColumnPicker";
import { runLook } from "../ui/status";
import { Icon } from "../ui/Icon";

const statusFilters: { value: string; label: Key; match: (s: RunStatus) => boolean }[] = [
  { value: "", label: "runs.filter.all", match: () => true },
  { value: "active", label: "runs.filter.active", match: (s) => !["completed", "cancelled", "failed", "timed_out", "review_required", "paused"].includes(s) },
  { value: "review", label: "runs.filter.review", match: (s) => s === "review_required" },
  { value: "paused", label: "runs.filter.paused", match: (s) => s === "paused" },
  { value: "done", label: "runs.filter.done", match: (s) => s === "completed" },
  { value: "stopped", label: "runs.filter.stopped", match: (s) => ["cancelled", "failed", "timed_out"].includes(s) },
];

export function RunsPage() {
  const { t, num } = useI18n();
  const { can } = useSession();
  const { query } = useLocation();
  const [status, setStatus] = useState(query.get("status") ?? "");
  const [project, setProject] = useState(query.get("project") ?? "");
  const runs = useApi<Run[]>(`/runs${project ? `?project_id=${project}` : ""}`, 15_000);
  const projects = useApi<Project[]>("/projects");
  const names = useMemo(() => new Map((projects.data ?? []).map((p) => [p.id, p.name])), [projects.data]);
  const col = useColumns("runs");
  const f = statusFilters.find((x) => x.value === status) ?? statusFilters[0]!;
  const rows = (runs.data ?? []).filter((r) => f.match(r.status));

  return (
    <Frame crumbs={[{ label: t("nav.runs") }]}>
      <PageHead
        title={t("nav.runs")}
        sub={t("runs.sub")}
        actions={
          can("researcher") ? (
            <Link to="/runs/new" className="btn primary">
              <Icon name="plus" />
              {t("runs.new")}
            </Link>
          ) : null
        }
      />
      <Panel>
        <div className="toolbar">
          <select className="select" aria-label={t("runs.filter.status")} value={status} onChange={(e) => setStatus(e.target.value)}>
            {statusFilters.map((x) => (
              <option key={x.value} value={x.value}>
                {t(x.label)}
              </option>
            ))}
          </select>
          <select className="select" aria-label={t("runs.filter.project")} value={project} onChange={(e) => setProject(e.target.value)}>
            <option value="">{t("runs.filter.allProjects")}</option>
            {(projects.data ?? []).map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          <span className="spacer" />
          <span className="small muted num">{t("common.nRows", { n: rows.length })}</span>
          <ColumnPicker set="runs" />
        </div>
        {runs.loading && !runs.data ? (
          <SkeletonRows rows={6} cols={5} />
        ) : runs.error ? (
          <ErrorNote error={runs.error} retry={runs.reload} />
        ) : rows.length === 0 ? (
          <Empty title={t("runs.emptyTitle")} body={status || project ? t("runs.emptyFiltered") : t("runs.emptyBody")} action={can("researcher") && !status ? <Link to="/runs/new">{t("runs.new")}</Link> : undefined} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t("runs.col.run")}</th>
                  {col("project") ? <th>{t("columns.runs.project")}</th> : null}
                  <th>{t("runs.col.status")}</th>
                  {col("progress") ? <th style={{ minWidth: 220 }}>{t("columns.runs.progress")}</th> : null}
                  {col("tokens") ? <th className="right">{t("columns.runs.tokens")}</th> : null}
                  {col("attempts") ? <th className="right">{t("columns.runs.attempts")}</th> : null}
                  {col("queued") ? <th>{t("columns.runs.queued")}</th> : null}
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.id} data-href onClick={(e) => !(e.target as HTMLElement).closest("a") && navigate(`/runs/${r.id}`)}>
                    <td className="title-cell">
                      <Link to={`/runs/${r.id}`}>{t("runs.runN", { id: r.id })}</Link>
                      {r.error_summary ? <span className="sub" style={{ color: "var(--bad)" }}>{r.error_summary.slice(0, 90)}</span> : null}
                    </td>
                    {col("project") ? <td>{names.get(r.project_id) ?? `#${r.project_id}`}</td> : null}
                    <td>
                      <State look={runLook(r.status)} />
                    </td>
                    {col("progress") ? (
                      <td>
                        <StageLine status={r.status} resumeStage={r.resume_stage} resumeStatus={r.resume_status} mini />
                      </td>
                    ) : null}
                    {col("tokens") ? <td className="right num">{num(r.output_tokens)}</td> : null}
                    {col("attempts") ? <td className="right num">{r.attempts || "—"}</td> : null}
                    {col("queued") ? (
                      <td className="small ink-2">
                        <Time iso={r.queued_at} />
                      </td>
                    ) : null}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Panel>
    </Frame>
  );
}
