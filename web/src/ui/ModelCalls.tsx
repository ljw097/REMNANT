// Model-call (LLM record) views shared by the run, hypothesis and analysis
// pages and the LLM records page: the call table, its totals, and the
// drawer with one call's exact request and answer. Model and repository
// content is rendered as text, never HTML.

import { useMemo, useState, type ReactNode } from "react";
import type { ModelCall } from "../api/types";
import { useApi } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { Button, Drawer, Empty, ErrorNote, Hash, Ref, routeClick, SkeletonRows, Time } from "./components";

const roleKey = (r: string) => (["hypothesis_researcher", "skeptic", "reporter", "historian"].includes(r) ? (`detail.role.${r}` as Key) : null);

export function RoleName({ role }: { role: string }) {
  const { t } = useI18n();
  const k = roleKey(role);
  return <>{k ? t(k) : role}</>;
}

const outcomeTone: Record<ModelCall["outcome"], [string, string]> = {
  running: ["info", "live"],
  ok: ["ok", "solid"],
  refused: ["warn", "stopped"],
  truncated: ["bad", "stopped"],
  error: ["bad", "stopped"],
  stopped: ["muted", "stopped"],
};

export function CallOutcome({ outcome }: { outcome: ModelCall["outcome"] }) {
  const { t } = useI18n();
  const [tone, form] = outcomeTone[outcome] ?? ["muted", "solid"];
  return (
    <span className="state" data-tone={tone} data-form={form}>
      {t(`detail.outcome.${outcome}` as Key)}
    </span>
  );
}

export function ms(n: number): string {
  if (n < 1000) return `${n} ms`;
  if (n < 60_000) return `${(n / 1000).toFixed(1)} s`;
  return `${Math.floor(n / 60_000)}m ${Math.round((n % 60_000) / 1000)}s`;
}

/** Totals line for a list of calls: count, tokens, time, failures. */
function CallTotals({ calls }: { calls: ModelCall[] }) {
  const { t } = useI18n();
  const sum = (f: (c: ModelCall) => number) => calls.reduce((a, c) => a + f(c), 0);
  const failed = calls.filter((c) => c.outcome !== "ok").length;
  return (
    <span className="small ink-2">
      {t("detail.totals", {
        n: calls.length,
        i: sum((c) => c.input_tokens).toLocaleString(),
        o: sum((c) => c.output_tokens).toLocaleString(),
        d: ms(sum((c) => c.duration_ms)),
        f: failed,
      })}
    </span>
  );
}

/**
 * Lists model calls; a row opens the full call (prompt envelope and answer).
 * `path` is the list endpoint; `filter` narrows what is shown.
 */
export function ModelCalls({ path, filter, empty, poll, recordsHref }: { path: string; filter?: (c: ModelCall) => boolean; empty?: ReactNode; poll?: number; recordsHref?: string }) {
  const { t } = useI18n();
  const q = useApi<ModelCall[]>(path, poll);
  const [open, setOpen] = useState<number | null>(null);
  const rows = useMemo(() => (q.data ?? []).filter(filter ?? (() => true)), [q.data, filter]);
  if (q.error) return <ErrorNote error={q.error} retry={q.reload} />;
  if (q.loading && !q.data) return <SkeletonRows rows={3} cols={6} />;
  if (rows.length === 0) return <Empty title={empty ?? t("detail.calls.none")} />;
  return (
    <>
      <div className="toolbar">
        <CallTotals calls={rows} />
        {recordsHref ? (
          <a className="small" href={recordsHref} onClick={(e) => routeClick(e, recordsHref)}>
            {t("llm.openFiltered")}
          </a>
        ) : null}
      </div>
      <CallTable rows={rows} onOpen={setOpen} />
      <ModelCallDrawer id={open} onClose={() => setOpen(null)} />
    </>
  );
}

/** The call table shared by every call list; `owner` adds a run / analysis column. */
export function CallTable({ rows, onOpen, owner }: { rows: ModelCall[]; onOpen: (id: number) => void; owner?: boolean }) {
  const { t } = useI18n();
  return (
    <div className="table-wrap">
      <table className="table detail-table">
        <thead>
          <tr>
            <th>#</th>
            {owner ? <th>{t("detail.col.owner")}</th> : null}
            <th>{t("detail.col.role")}</th>
            <th>{t("detail.col.subject")}</th>
            <th>{t("detail.col.outcome")}</th>
            <th>{t("detail.col.result")}</th>
            <th className="num">{t("detail.col.tokens")}</th>
            <th className="num">{t("detail.col.time")}</th>
            <th>{t("detail.col.when")}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((c) => (
            <tr key={c.id} className="clickable" tabIndex={0} onClick={() => onOpen(c.id)} onKeyDown={(e) => (e.key === "Enter" ? onOpen(c.id) : undefined)}>
              <td className="mono small">{c.id}</td>
              {owner ? (
                <td className="small" onClick={(e) => e.stopPropagation()}>
                  <OwnerRef call={c} />
                </td>
              ) : null}
              <td>
                <RoleName role={c.role} />
              </td>
              <td className="mono small detail-subject" title={c.subject}>
                {c.subject || "—"}
              </td>
              <td>
                <CallOutcome outcome={c.outcome} />
              </td>
              <td className="small detail-result" title={c.error || c.result}>
                {c.result || c.error || "—"}
              </td>
              <td className="num small">
                {c.input_tokens.toLocaleString()} / {c.output_tokens.toLocaleString()}
              </td>
              <td className="num small">{ms(c.duration_ms)}</td>
              <td className="small">
                <Time iso={c.created_at} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** [R-n] for a run's call, [I-n] for an analysis (intake) call. */
export function OwnerRef({ call }: { call: ModelCall }) {
  if (call.run_id) return <Ref kind="R" id={call.run_id} to={`/runs/${call.run_id}`} />;
  if (call.intake_id)
    return (
      <a className="ref" href={`/analyze/${call.intake_id}`} onClick={(e) => routeClick(e, `/analyze/${call.intake_id}`)}>
        [I-{call.intake_id}]
      </a>
    );
  return <span className="muted">—</span>;
}

/** One model call in full: what was asked, what came back, what it led to. */
export function ModelCallDrawer({ id, onClose }: { id: number | null; onClose: () => void }) {
  const { t } = useI18n();
  const q = useApi<ModelCall>(id ? `/model-calls/${id}` : null);
  const c = id && q.data?.id === id ? q.data : undefined;
  return (
    <Drawer open={id !== null} onClose={onClose} wide title={c ? <>{t("detail.call.title", { id: c.id })} · <RoleName role={c.role} /></> : t("detail.call.loading")}>
      {q.error ? <ErrorNote error={q.error} retry={q.reload} /> : null}
      {!c ? (
        <SkeletonRows rows={4} cols={2} />
      ) : (
        <>
          <dl className="dkv">
            <dt>{t("detail.col.outcome")}</dt>
            <dd>
              <CallOutcome outcome={c.outcome} />
            </dd>
            <dt>{t("detail.col.result")}</dt>
            <dd>{c.result || "—"}</dd>
            {c.error ? (
              <>
                <dt>{t("detail.call.error")}</dt>
                <dd className="mono small detail-error">{c.error}</dd>
              </>
            ) : null}
            <dt>{t("detail.col.subject")}</dt>
            <dd className="mono small">{c.subject || "—"}</dd>
            <dt>{t("detail.call.links")}</dt>
            <dd className="row">
              <OwnerRef call={c} />
              {c.hypothesis_id ? <Ref kind="H" id={c.hypothesis_id} to={`/hypotheses/${c.hypothesis_id}`} /> : null}
              {c.evidence_id ? <Ref kind="E" id={c.evidence_id} /> : null}
              {c.commit_sha ? <Hash value={c.commit_sha} /> : null}
            </dd>
            <dt>{t("detail.call.model")}</dt>
            <dd className="mono small">{c.model || "—"}</dd>
            <dt>{t("detail.col.tokens")}</dt>
            <dd className="small">{t("detail.call.tokens", { i: c.input_tokens.toLocaleString(), o: c.output_tokens.toLocaleString(), max: c.max_tokens.toLocaleString() })}</dd>
            <dt>{t("detail.col.time")}</dt>
            <dd className="small">
              {ms(c.duration_ms)} · <Time iso={c.created_at} abs />
            </dd>
          </dl>
          <section className="stack" style={{ gap: 6 }}>
            <div className="row">
              <h3 className="detail-h">{t("detail.call.response")}</h3>
              <CopyButton text={c.response == null ? "" : JSON.stringify(c.response, null, 2)} />
            </div>
            {c.response == null ? <p className="small muted">{c.outcome === "running" ? t("detail.call.stillRunning") : t("detail.call.noResponse")}</p> : <pre className="detail-pre">{JSON.stringify(c.response, null, 2)}</pre>}
          </section>
          <section className="stack" style={{ gap: 6 }}>
            <div className="row">
              <h3 className="detail-h">{t("detail.call.request")}</h3>
              <CopyButton text={c.request ?? ""} />
            </div>
            <p className="small muted">{t("detail.call.requestNote")}</p>
            <pre className="detail-pre">{prettyEnvelope(c.request ?? "")}</pre>
          </section>
        </>
      )}
    </Drawer>
  );
}

/** The envelope is "<preamble>\n\n<json>"; indent the JSON part for reading. */
function prettyEnvelope(s: string): string {
  const i = s.indexOf("{");
  if (i < 0) return s;
  try {
    return s.slice(0, i) + JSON.stringify(JSON.parse(s.slice(i)), null, 2);
  } catch {
    return s;
  }
}

function CopyButton({ text }: { text: string }) {
  const { t } = useI18n();
  const [done, setDone] = useState(false);
  if (!text) return null;
  return (
    <Button
      size="sm"
      variant="ghost"
      icon={done ? "check" : "copy"}
      onClick={() => {
        void navigator.clipboard?.writeText(text).then(() => {
          setDone(true);
          setTimeout(() => setDone(false), 1500);
        });
      }}
    >
      {done ? t("common.copied") : t("common.copy")}
    </Button>
  );
}
