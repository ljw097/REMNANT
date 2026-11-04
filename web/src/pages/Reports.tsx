// Top-level Reports: run reports in their own place, apart from the runs list
// and the review queue. A compact rail of finished runs on the left; the
// selected run's report rendered to read on the right.

import { useMemo } from "react";
import { Frame } from "../App";
import { useApi } from "../hooks";
import { useI18n } from "../i18n";
import { Link, navigate } from "../router";
import type { Project, Run } from "../api/types";
import { Empty, ErrorNote, Panel, SkeletonRows, State, Time } from "../ui/components";
import { ReportView } from "../ui/ReportView";
import { runLook, terminalRun } from "../ui/status";

export function ReportsPage({ id }: { id?: number }) {
  const { t } = useI18n();
  const runs = useApi<Run[]>("/runs");
  const projects = useApi<Project[]>("/projects");
  const names = useMemo(() => new Map((projects.data ?? []).map((p) => [p.id, p.name])), [projects.data]);
  // A report exists once a run has reached its report; show finished runs.
  const withReports = (runs.data ?? []).filter((r) => terminalRun(r.status) || r.status === "report_ready");
  const selected = id ?? withReports[0]?.id;
  const selectedRun = withReports.find((r) => r.id === selected);
  const nameOf = (r: Run) => names.get(r.project_id) ?? t("common.project", { id: r.project_id });

  return (
    <Frame crumbs={[{ label: t("nav.reports") }]}>
      <div className="grid-12">
        <aside className="span-4 stack reports-rail">
          <Panel title={t("reports.title")} meta={t("reports.sub")}>
            {runs.loading && !runs.data ? (
              <SkeletonRows rows={6} cols={2} />
            ) : runs.error ? (
              <ErrorNote error={runs.error} retry={runs.reload} />
            ) : withReports.length === 0 ? (
              <Empty title={t("reports.emptyTitle")} body={t("reports.emptyBody")} />
            ) : (
              <ul className="report-list">
                {withReports.map((r) => (
                  <li key={r.id}>
                    <button type="button" className="report-row" aria-current={r.id === selected} onClick={() => navigate(`/reports/${r.id}`)}>
                      <span className="report-row-name">{nameOf(r)}</span>
                      <span className="report-row-meta">
                        <span className="ref">R-{r.id}</span>
                        <State look={runLook(r.status)} />
                        <span className="spacer" />
                        <Time iso={r.completed_at ?? r.queued_at} />
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </Panel>
        </aside>
        <div className="span-8 stack">
          {selected && selectedRun ? (
            <>
              <div className="reports-head">
                <div className="stack" style={{ gap: 2, minWidth: 0 }}>
                  <h2>{nameOf(selectedRun)}</h2>
                  <span className="small ink-2">
                    <span className="ref">R-{selected}</span> · <Time iso={selectedRun.completed_at ?? selectedRun.queued_at} />
                  </span>
                </div>
                <Link to={`/runs/${selected}`} className="btn sm">
                  {t("reports.openRun")}
                </Link>
              </div>
              <ReportView runId={selected} />
            </>
          ) : (
            <Panel>
              <Empty title={t("reports.emptyTitle")} body={t("reports.emptyBody")} />
            </Panel>
          )}
        </div>
      </div>
    </Frame>
  );
}
