// Drill-down views behind the numbers the UI shows: every model call with
// its prompt and answer, the candidate regions a run examined, run events
// with their data, and the commits an intake read. Everything here is
// read-only and rendered as text (repository and model content is never
// HTML).

import { useMemo, useState, type ReactNode } from "react";
import type { CommitRecord, Evidence, ModelCall, RunEvent } from "../api/types";
import { useApi } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { navigate } from "../router";
import { Button, Drawer, Empty, ErrorNote, Hash, Panel, Ref, SkeletonRows, Time } from "./components";

const roleKey = (r: string) => (["hypothesis_researcher", "skeptic", "reporter", "historian"].includes(r) ? (`detail.role.${r}` as Key) : null);

function RoleName({ role }: { role: string }) {
  const { t } = useI18n();
  const k = roleKey(role);
  return <>{k ? t(k) : role}</>;
}

const outcomeTone: Record<ModelCall["outcome"], [string, string]> = {
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

function routeClick(e: React.MouseEvent, to: string) {
  if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  e.preventDefault();
  navigate(to);
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

export { RoleName };

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
            {c.response == null ? <p className="small muted">{t("detail.call.noResponse")}</p> : <pre className="detail-pre">{JSON.stringify(c.response, null, 2)}</pre>}
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

/* ---------- Candidate regions ---------- */

type CandMeta = { pattern_id?: number; score?: number; matched?: string[]; countered?: string[]; outcome?: string; language?: string };

/**
 * The source regions a run selected for review, best first, with why each
 * was picked and what came of it. A row expands to the excerpt and the
 * model calls about it.
 */
export function Candidates({ runId, poll }: { runId: number; poll?: number }) {
  const { t } = useI18n();
  const q = useApi<Evidence[]>(`/runs/${runId}/candidates`, poll);
  const calls = useApi<ModelCall[]>(`/runs/${runId}/model-calls`, poll);
  const [openCall, setOpenCall] = useState<number | null>(null);
  const [expanded, setExpanded] = useState<number | null>(null);
  const byEvidence = useMemo(() => {
    const m = new Map<number, ModelCall[]>();
    for (const c of calls.data ?? []) if (c.evidence_id) m.set(c.evidence_id, [...(m.get(c.evidence_id) ?? []), c]);
    return m;
  }, [calls.data]);
  if (q.error) return <ErrorNote error={q.error} retry={q.reload} />;
  if (q.loading && !q.data) return <SkeletonRows rows={3} cols={5} />;
  const rows = q.data ?? [];
  if (rows.length === 0) return <Empty title={t("detail.regions.none")} body={t("detail.regions.noneBody")} />;
  const counts = rows.reduce<Record<string, number>>((a, e) => {
    const o = regionOutcome(e, byEvidence.get(e.id));
    a[o] = (a[o] ?? 0) + 1;
    return a;
  }, {});
  return (
    <>
      <div className="toolbar">
        <span className="small ink-2">
          {t("detail.regions.count", { n: rows.length })}
          {Object.entries(counts).map(([o, n]) => ` · ${t(`detail.region.${o}` as Key)} ${n}`)}
        </span>
      </div>
      <div className="table-wrap">
        <table className="table detail-table">
          <thead>
            <tr>
              <th>{t("detail.col.region")}</th>
              <th>{t("detail.col.pattern")}</th>
              <th className="num">{t("detail.col.score")}</th>
              <th>{t("detail.col.matched")}</th>
              <th>{t("detail.col.outcome")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((e) => {
              const m = (e.metadata ?? {}) as CandMeta;
              const cs = byEvidence.get(e.id) ?? [];
              const hyp = cs.find((c) => c.hypothesis_id)?.hypothesis_id;
              const outcome = regionOutcome(e, cs);
              const isOpen = expanded === e.id;
              return [
                <tr key={e.id} className="clickable" tabIndex={0} aria-expanded={isOpen} onClick={() => setExpanded(isOpen ? null : e.id)} onKeyDown={(k) => (k.key === "Enter" ? setExpanded(isOpen ? null : e.id) : undefined)}>
                  <td className="mono small detail-subject" title={e.file_path}>
                    <Ref kind="E" id={e.id} /> {e.file_path}:{e.line_start}-{e.line_end}
                    {e.symbol ? <span className="muted"> {e.symbol}</span> : null}
                  </td>
                  <td className="small">{m.pattern_id ? <Ref kind="P" id={m.pattern_id} to={`/patterns/${m.pattern_id}`} /> : t("detail.region.general")}</td>
                  <td className="num small">{m.score ?? "—"}</td>
                  <td className="mono small detail-result">
                    {(m.matched ?? []).join(", ")}
                    {m.countered?.length ? <span className="muted"> − {m.countered.join(", ")}</span> : null}
                  </td>
                  <td className="small">
                    {hyp ? <Ref kind="H" id={hyp} to={`/hypotheses/${hyp}`} /> : null} {t(`detail.region.${outcome}` as Key)}
                  </td>
                </tr>,
                isOpen ? (
                  <tr key={`${e.id}-x`} className="detail-expand">
                    <td colSpan={5}>
                      <div className="stack" style={{ gap: 8 }}>
                        <span className="small ink-2">{e.summary}</span>
                        <span className="small">
                          {t("detail.region.hash")} <Hash value={e.content_sha256} /> · {t("detail.region.revision")} <Hash value={e.revision} />
                        </span>
                        <pre className="detail-pre">{e.excerpt}</pre>
                        {cs.length ? (
                          <div className="row">
                            <span className="small muted">{t("detail.region.calls")}</span>
                            {cs.map((c) => (
                              <Button key={c.id} size="sm" onClick={() => setOpenCall(c.id)}>
                                #{c.id} <RoleName role={c.role} /> · {t(`detail.outcome.${c.outcome}` as Key)}
                              </Button>
                            ))}
                          </div>
                        ) : (
                          <span className="small muted">{t("detail.region.notAsked")}</span>
                        )}
                      </div>
                    </td>
                  </tr>
                ) : null,
              ];
            })}
          </tbody>
        </table>
      </div>
      <ModelCallDrawer id={openCall} onClose={() => setOpenCall(null)} />
    </>
  );
}

function regionOutcome(e: Evidence, calls: ModelCall[] | undefined): string {
  const o = (e.metadata as CandMeta | null)?.outcome;
  if (o && ["declined", "invalid", "duplicate", "refused"].includes(o)) return o;
  if (calls?.some((c) => c.hypothesis_id)) return "hypothesis";
  if (calls?.some((c) => c.outcome === "error" || c.outcome === "truncated")) return "failed";
  if (o) return "hypothesis"; // marked in the same transaction as the hypothesis
  return "pending";
}

/* ---------- Events ---------- */

/** Run events, newest first; each row opens to its stage and data. */
export function EventLog({ events, stage, live }: { events: RunEvent[]; stage?: string; live?: boolean }) {
  const { t, dateTime } = useI18n();
  const shown = [...events].filter((e) => !stage || e.stage === stage).reverse();
  if (shown.length === 0) return <Empty title={t("run.noActivity")} />;
  return (
    <ol className="events" aria-live={live ? "polite" : undefined}>
      {shown.map((e) => (
        <li key={e.id} className="event-row">
          <details>
            <summary>
              <time className="when" dateTime={e.created_at} title={dateTime(e.created_at)}>
                {new Date(e.created_at).toLocaleTimeString()}
              </time>
              <span className="type">{e.event_type}</span>
              <span>{e.message}</span>
            </summary>
            <dl className="dkv event-data">
              <dt>{t("detail.event.id")}</dt>
              <dd className="mono small">{e.id}</dd>
              <dt>{t("detail.event.stage")}</dt>
              <dd className="mono small">{e.stage ? t(`stage.${e.stage}` as Key) : "—"} {e.stage ? <span className="muted">({e.stage})</span> : null}</dd>
              <dt>{t("detail.event.time")}</dt>
              <dd className="small">{dateTime(e.created_at)}</dd>
              <dt>{t("detail.event.data")}</dt>
              <dd>{e.payload && Object.keys(e.payload).length ? <pre className="detail-pre">{JSON.stringify(e.payload, null, 2)}</pre> : <span className="muted small">—</span>}</dd>
            </dl>
          </details>
        </li>
      ))}
    </ol>
  );
}

/* ---------- Stage drawer ---------- */

/**
 * Everything recorded for one station of the stage line: its events, and
 * for the stages that do model work, the regions and calls behind them.
 */
export function StageDrawer({ stage, onClose, events, runId }: { stage: string | null; onClose: () => void; events: RunEvent[]; runId: number }) {
  const { t } = useI18n();
  const evs = stage ? events.filter((e) => e.stage === stage) : [];
  const first = evs[0]?.created_at;
  const last = evs[evs.length - 1]?.created_at;
  const span = first && last ? new Date(last).getTime() - new Date(first).getTime() : 0;
  return (
    <Drawer open={stage !== null} onClose={onClose} wide title={stage ? t("detail.stage.title", { stage: t(`stage.${stage}` as Key) }) : ""}>
      {stage ? (
        <>
          <p className="small ink-2">{t(`detail.stage.what.${stage}` as Key)}</p>
          <p className="small">{evs.length ? t("detail.stage.span", { n: evs.length, d: ms(span) }) : t("detail.stage.noEvents")}</p>
          <Panel title={t("run.tab.activity")}>
            <EventLog events={events} stage={stage} />
          </Panel>
          {stage === "surface_mapping" ? (
            <Panel title={t("detail.tab.regions")}>
              <Candidates runId={runId} />
            </Panel>
          ) : null}
          {stage === "hypothesis_generation" ? (
            <Panel title={t("detail.tab.calls")}>
              <ModelCalls path={`/runs/${runId}/model-calls`} filter={(c) => c.role === "hypothesis_researcher" || c.role === "skeptic"} />
            </Panel>
          ) : null}
          {stage === "report_ready" || stage === "completed" ? (
            <Panel title={t("detail.tab.calls")}>
              <ModelCalls path={`/runs/${runId}/model-calls`} filter={(c) => c.role === "reporter"} empty={t("detail.calls.noReporter")} />
            </Panel>
          ) : null}
        </>
      ) : null}
    </Drawer>
  );
}

/* ---------- Commits (repository intake) ---------- */

/** One commit an intake read: message, files, signals, diff and the Historian's verdict. */
export function CommitDrawer({ intakeId, sha, calls, onClose, onOpenCall }: { intakeId: number; sha: string | null; calls: ModelCall[]; onClose: () => void; onOpenCall: (id: number) => void }) {
  const { t, dateTime } = useI18n();
  const q = useApi<CommitRecord>(sha ? `/intakes/${intakeId}/commits/${sha}` : null);
  const c = sha && q.data?.sha === sha ? q.data : undefined;
  const call = calls.find((x) => x.commit_sha === sha);
  return (
    <Drawer open={sha !== null} onClose={onClose} wide title={sha ? <span className="mono">{sha.slice(0, 12)}</span> : ""}>
      {q.error ? <ErrorNote error={q.error} retry={q.reload} /> : null}
      {!c ? (
        <SkeletonRows rows={4} cols={2} />
      ) : (
        <>
          <h3>{c.subject}</h3>
          <dl className="dkv">
            <dt>{t("detail.commit.sha")}</dt>
            <dd>
              <Hash value={c.sha} length={40} />
            </dd>
            <dt>{t("detail.commit.date")}</dt>
            <dd className="small">{dateTime(c.committed_at)}</dd>
            <dt>{t("detail.commit.signals")}</dt>
            <dd className="row">
              {c.signals.length ? c.signals.map((s) => <span key={s} className="detail-tag">{t(`signal.${s}` as Key)}</span>) : <span className="muted">—</span>}
              <span className="small muted">{t("detail.commit.score", { n: c.score })}</span>
            </dd>
            <dt>{t("detail.commit.historian")}</dt>
            <dd>
              {call ? (
                <span className="row">
                  <CallOutcome outcome={call.outcome} /> <span className="small">{call.result || call.error}</span>
                  <Button size="sm" onClick={() => onOpenCall(call.id)}>
                    {t("detail.commit.openCall", { id: call.id })}
                  </Button>
                </span>
              ) : (
                <span className="small muted">{t("detail.commit.notRead")}</span>
              )}
            </dd>
            <dt>{t("detail.commit.files", { n: c.files.length })}</dt>
            <dd className="mono small">{c.files.join("\n") || "—"}</dd>
          </dl>
          {c.body ? (
            <section className="stack" style={{ gap: 6 }}>
              <h3 className="detail-h">{t("detail.commit.message")}</h3>
              <pre className="detail-pre">{c.body}</pre>
            </section>
          ) : null}
          <section className="stack" style={{ gap: 6 }}>
            <h3 className="detail-h">{t("detail.commit.diff")}</h3>
            {c.diff ? <pre className="detail-pre detail-diff">{c.diff}</pre> : <p className="small muted">{t("detail.commit.noDiff")}</p>}
          </section>
        </>
      )}
    </Drawer>
  );
}
