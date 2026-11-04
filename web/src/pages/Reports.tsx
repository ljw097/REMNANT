// Top-level Reports: run reports in their own place, apart from the runs list
// and the review queue. The list is the finished runs; selecting one renders
// its report in the page.

import { Frame } from "../App";
import { useApi } from "../hooks";
import { useI18n } from "../i18n";
import { Link, navigate } from "../router";
import type { Run } from "../api/types";
import { Empty, ErrorNote, Panel, SkeletonRows, State, Time } from "../ui/components";
import { ReportView } from "../ui/ReportView";
import { runLook, terminalRun } from "../ui/status";

export function ReportsPage({ id }: { id?: number }) {
  const { t } = useI18n();
  const runs = useApi<Run[]>("/runs");
  // A report exists once a run has reached its report; show finished runs.
  const withReports = (runs.data ?? []).filter((r) => terminalRun(r.status) || r.status === "report_ready");
  const selected = id ?? withReports[0]?.id;

  return (
    <Frame crumbs={[{ label: t("nav.reports") }]}>
      <div className="grid">
        <aside className="span-4 stack">
          <Panel title={t("reports.title")} meta={t("reports.sub")}>
            {runs.loading && !runs.data ? (
              <SkeletonRows rows={5} cols={2} />
            ) : runs.error ? (
              <ErrorNote error={runs.error} retry={runs.reload} />
            ) : withReports.length === 0 ? (
              <Empty title={t("reports.emptyTitle")} body={t("reports.emptyBody")} />
            ) : (
              <ul className="report-list">
                {withReports.map((r) => (
                  <li key={r.id}>
                    <button type="button" className="report-row" aria-current={r.id === selected} onClick={() => navigate(`/reports/${r.id}`)}>
                      <span className="report-row-main">
                        <strong>R-{r.id}</strong>
                        <State look={runLook(r.status)} />
                      </span>
                      <span className="small ink-2">
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
          {selected ? (
            <>
              <div className="row" style={{ justifyContent: "space-between", alignItems: "baseline" }}>
                <h2 style={{ margin: 0 }}>{t("reports.forRun", { id: selected })}</h2>
                <Link to={`/runs/${selected}`} className="small">
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
