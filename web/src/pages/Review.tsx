import { Frame } from "../App";
import { useApi } from "../hooks";
import { useI18n } from "../i18n";
import { Link, useLocation } from "../router";
import type { Hypothesis } from "../api/types";
import { Empty, ErrorNote, PageHead, Panel, SkeletonRows } from "../ui/components";
import { useColumns, ColumnPicker } from "../ui/ColumnPicker";
import { HypothesisRows } from "./RunDetail";
import { Icon } from "../ui/Icon";

const rank = { high: 0, medium: 1, low: 2 } as const;

export function ReviewQueuePage() {
  const { t } = useI18n();
  const { query } = useLocation();
  const runFilter = query.get("run");
  const q = useApi<Hypothesis[]>("/hypotheses?status=draft,triage", 30_000);
  const col = useColumns("hypotheses");
  let rows = (q.data ?? []).slice().sort((a, b) => rank[a.triage_confidence] - rank[b.triage_confidence] || b.id - a.id);
  if (runFilter) rows = rows.filter((h) => h.run_id === Number(runFilter));
  const suffix = `?queue=triage${runFilter ? `&run=${runFilter}` : ""}`;

  return (
    <Frame crumbs={[{ label: t("nav.review") }]}>
      <PageHead
        title={t("nav.review")}
        sub={runFilter ? t("review.subRun", { id: runFilter }) : t("review.sub")}
        actions={
          rows.length > 0 ? (
            <Link to={`/hypotheses/${rows[0]!.id}${suffix}`} className="btn primary">
              <Icon name="play" />
              {t("review.start")}
            </Link>
          ) : null
        }
      />
      <Panel>
        <div className="toolbar">
          <span className="small ink-2">{t("review.order")}</span>
          <span className="spacer" />
          <KeyHints text={t("review.keys")} />
          <ColumnPicker set="hypotheses" />
        </div>
        {q.loading && !q.data ? (
          <SkeletonRows rows={6} cols={5} />
        ) : q.error ? (
          <ErrorNote error={q.error} retry={q.reload} />
        ) : rows.length === 0 ? (
          <Empty title={t("review.emptyTitle")} body={t("review.emptyBody")} action={<Link to="/runs">{t("nav.runs")}</Link>} />
        ) : (
          <HypothesisRows rows={rows} col={col} queue={`triage${runFilter ? `&run=${runFilter}` : ""}`} />
        )}
      </Panel>
    </Frame>
  );
}

// "Lead: J next · K previous" → the lead, then each key as a chip.
function KeyHints({ text }: { text: string }) {
  const [lead, list = ""] = text.split(": ");
  return (
    <span className="small muted row" style={{ gap: 10, flexWrap: "wrap" }}>
      <span>{lead}</span>
      {list.split(" · ").map((item) => (
        <span key={item}>
          <kbd>{item.slice(0, 1)}</kbd> {item.slice(2)}
        </span>
      ))}
    </span>
  );
}
