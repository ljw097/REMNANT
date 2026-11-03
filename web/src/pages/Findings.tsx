import { useState } from "react";
import { Frame } from "../App";
import { useApi } from "../hooks";
import { useI18n, type Key } from "../i18n";
import type { Hypothesis } from "../api/types";
import { Empty, ErrorNote, PageHead, Panel, SkeletonRows, Tabs } from "../ui/components";
import { ColumnPicker, useColumns } from "../ui/ColumnPicker";
import { HypothesisRows } from "./RunDetail";

type Tab = "validated" | "pending" | "closed";

export function FindingsPage() {
  const { t } = useI18n();
  const [tab, setTab] = useState<Tab>("validated");
  const q = useApi<Hypothesis[]>("/hypotheses?status=validated,validation_pending,accepted,not_reproducible", 60_000);
  const col = useColumns("hypotheses");
  const all = q.data ?? [];
  const groups: Record<Tab, Hypothesis[]> = {
    validated: all.filter((h) => h.status === "validated"),
    pending: all.filter((h) => h.status === "validation_pending" || h.status === "accepted"),
    closed: all.filter((h) => h.status === "not_reproducible"),
  };
  return (
    <Frame crumbs={[{ label: t("nav.findings") }]}>
      <PageHead title={t("nav.findings")} sub={t("findings.sub")} />
      <Tabs<Tab>
        value={tab}
        onChange={setTab}
        tabs={[
          { value: "validated", label: t("findings.tab.validated"), count: groups.validated.length },
          { value: "pending", label: t("findings.tab.pending"), count: groups.pending.length },
          { value: "closed", label: t("findings.tab.closed"), count: groups.closed.length },
        ]}
      />
      <Panel>
        <div className="toolbar">
          <span className="small ink-2">{t(`findings.explain.${tab}` as Key)}</span>
          <span className="spacer" />
          <ColumnPicker set="hypotheses" />
        </div>
        {q.loading && !q.data ? (
          <SkeletonRows rows={5} cols={5} />
        ) : q.error ? (
          <ErrorNote error={q.error} retry={q.reload} />
        ) : groups[tab].length === 0 ? (
          <Empty title={t("findings.emptyTitle")} body={t("findings.emptyBody")} />
        ) : (
          <HypothesisRows rows={groups[tab]} col={col} />
        )}
      </Panel>
    </Frame>
  );
}
