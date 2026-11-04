// Renders a run's Markdown report in the page, fetching the artifact itself.
// Used by the run detail Report tab and the top-level Reports page.

import { useEffect, useState } from "react";
import { download, getText } from "../api/client";
import { useApi } from "../hooks";
import { useI18n } from "../i18n";
import type { Artifact } from "../api/types";
import { Button, Empty, ErrorNote, Panel, SkeletonRows, useToast } from "./components";
import { Markdown } from "./Markdown";

export function ReportView({ runId }: { runId: number }) {
  const { t } = useI18n();
  const toast = useToast();
  const arts = useApi<Artifact[]>(`/runs/${runId}/artifacts`);
  const art = (arts.data ?? []).find((a) => a.artifact_kind === "report" && /markdown/.test(a.media_type));
  const [md, setMd] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (!art) return;
    setMd(null);
    setFailed(false);
    const ac = new AbortController();
    getText(`/runs/${runId}/artifacts/${art.id}`, ac.signal)
      .then(setMd)
      .catch((e) => {
        if ((e as Error).name !== "AbortError") setFailed(true);
      });
    return () => ac.abort();
  }, [art?.id, runId]);

  if (!art) {
    return <Panel>{arts.loading && !arts.data ? <SkeletonRows rows={4} cols={1} /> : <Empty title={t("run.noReport")} body={t("run.noReportBody")} />}</Panel>;
  }
  return (
    <Panel
      tools={
        <Button size="sm" icon="download" onClick={() => download(`/runs/${runId}/artifacts/${art.id}`, art.name).catch(() => toast(t("art.unavailable"), "bad"))}>
          {t("art.download")}
        </Button>
      }
    >
      {failed ? <ErrorNote error={t("art.unavailable")} /> : md == null ? <SkeletonRows rows={6} cols={1} /> : <Markdown source={md} refs />}
    </Panel>
  );
}
