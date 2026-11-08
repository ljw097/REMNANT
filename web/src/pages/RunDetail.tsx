import { useEffect, useMemo, useRef, useState } from "react";
import { Frame } from "../App";
import { download, post } from "../api/client";
import { streamRunEvents } from "../api/sse";
import { useApi, type Query } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { Link, navigate } from "../router";
import { useSession } from "../session";
import type { Artifact, Experiment, Hypothesis, LLMProfile, Project, RunDetail, RunEvent, Target } from "../api/types";
import { Button, ConfirmButton, Empty, ErrorNote, errorText, Hash, Panel, Priority, Ref, SkeletonRows, State, Tabs, Time, useToast } from "../ui/components";
import { DocHeader, PolicyList, SectionTitle, StageLine } from "../ui/domain";
import { ReportView } from "../ui/ReportView";
import { Icon } from "../ui/Icon";
import { ColumnPicker, useColumns } from "../ui/ColumnPicker";
import { experimentLook, hypothesisLook, pausableRun, runLook, terminalRun } from "../ui/status";

type Tab = "report" | "hypotheses" | "experiments" | "activity" | "artifacts";

export function RunDetailPage({ id }: { id: number }) {
  const { t, num, duration } = useI18n();
  const { can, reloadSummary } = useSession();
  const toast = useToast();
  const run = useApi<RunDetail>(`/runs/${id}`);
  const r = run.data;
  const live = r ? !terminalRun(r.status) : false;
  const hyps = useApi<Hypothesis[]>(`/runs/${id}/hypotheses`, live ? 15_000 : undefined);
  const exps = useApi<Experiment[]>(`/runs/${id}/experiments`, live ? 15_000 : undefined);
  const arts = useApi<Artifact[]>(`/runs/${id}/artifacts`, live ? 30_000 : undefined);
  const project = useApi<Project>(r ? `/projects/${r.project_id}` : null);
  const targets = useApi<Target[]>(r ? `/projects/${r.project_id}/targets` : null);
  const profiles = useApi<LLMProfile[]>("/llm-profiles");
  const [tab, setTab] = useState<Tab>("hypotheses");
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [stream, setStream] = useState<"live" | "reconnecting" | "ended">("live");
  const seen = useRef(new Set<number>());
  const [busy, setBusy] = useState<string>();

  // Live timeline. A status change also refreshes the run and its lists.
  useEffect(() => {
    const ac = new AbortController();
    seen.current = new Set();
    setEvents([]);
    void streamRunEvents(id, ac.signal, {
      onEvent: (e) => {
        if (seen.current.has(e.id)) return;
        seen.current.add(e.id);
        setEvents((xs) => [...xs, e]);
        if (e.event_type === "run.status" || e.event_type === "hypothesis.created" || e.event_type === "experiment.finished" || e.event_type === "report.generated") {
          run.reload();
          hyps.reload();
          exps.reload();
          arts.reload();
        }
      },
      onState: setStream,
      onEnd: () => setStream("ended"),
    });
    return () => ac.abort();
  }, [id]);

  const lastStage = useMemo(() => {
    for (let i = events.length - 1; i >= 0; i--) {
      const e = events[i]!;
      if (e.event_type === "run.status" && typeof e.payload?.from === "string") return e.payload.from;
    }
    return undefined;
  }, [events]);

  // The rendered report is the Markdown artifact; when a finished run has one,
  // land on it so the write-up is the first thing seen.
  const reportArt = useMemo(() => (arts.data ?? []).find((a) => a.artifact_kind === "report" && /markdown/.test(a.media_type)), [arts.data]);
  const autoLanded = useRef(false);
  useEffect(() => {
    if (!autoLanded.current && reportArt && r && terminalRun(r.status)) {
      autoLanded.current = true;
      setTab("report");
    }
  }, [reportArt, r]);

  const act = async (name: string, path: string, done: Key) => {
    setBusy(name);
    try {
      await post(path);
      toast(t(done));
      run.reload();
      reloadSummary();
    } catch (e) {
      toast(errorText(e, t), "bad");
    } finally {
      setBusy(undefined);
    }
  };

  if (run.error) {
    return (
      <Frame crumbs={[{ label: t("nav.runs"), to: "/runs" }, { label: t("runs.runN", { id }) }]}>
        <ErrorNote error={run.error} retry={run.reload} />
      </Frame>
    );
  }
  if (!r) {
    return (
      <Frame crumbs={[{ label: t("nav.runs"), to: "/runs" }, { label: t("runs.runN", { id }) }]}>
        <SkeletonRows rows={6} cols={3} />
      </Frame>
    );
  }

  const targetName = (tid: number) => targets.data?.find((x) => x.id === tid)?.display_name ?? `#${tid}`;
  const profile = profiles.data?.find((p) => p.id === r.llm_profile_id);
  const counts = r.hypothesis_counts;
  const total = Object.values(counts).reduce((a, b) => a + (b ?? 0), 0);

  const actions = (
    <>
      {r.status === "paused" && can("researcher") ? (
        <Button icon="play" loading={busy === "resume"} onClick={() => act("resume", `/runs/${id}/resume`, "run.resumed")}>
          {t("run.resume")}
        </Button>
      ) : null}
      {pausableRun(r.status) && can("researcher") ? (
        <Button icon="pause" loading={busy === "pause"} onClick={() => act("pause", `/runs/${id}/pause`, "run.paused")}>
          {t("run.pause")}
        </Button>
      ) : null}
      {r.status === "review_required" && can("researcher", "reviewer") ? (
        <Button variant="primary" icon="file" loading={busy === "finalize"} onClick={() => act("finalize", `/runs/${id}/finalize`, "run.finalized")}>
          {t("run.finalize")}
        </Button>
      ) : null}
      {!terminalRun(r.status) && can("researcher") ? (
        <ConfirmButton icon="stop" confirmLabel={t("run.cancelConfirm")} loading={busy === "cancel"} onConfirm={() => act("cancel", `/runs/${id}/cancel`, "run.cancelled")}>
          {t("run.cancel")}
        </ConfirmButton>
      ) : null}
    </>
  );

  return (
    <Frame crumbs={[{ label: t("nav.runs"), to: "/runs" }, { label: t("runs.runN", { id }) }]}>
      <div className="stack" style={{ gap: 20 }}>
        <DocHeader
          docno={`R-${r.id}`}
          title={project.data?.name ?? t("common.project", { id: r.project_id })}
          state={<>
            <State look={runLook(r.status)} />
            {r.auto_discover ? <span className="state" data-tone="todo" data-form="solid" title={t("run.discoverNote")}>{t("run.discover")}</span> : null}
            {r.auto ? <span className="state" data-tone="info" data-form="solid" title={t("run.autoNote")}>{t("run.auto")}</span> : null}
          </>}
          actions={actions}
          fields={[
            { label: t("run.field.revision"), value: r.targets.length === 1 ? <Hash value={r.targets[0]!.revision} /> : t("newRun.nTargets", { n: r.targets.length }) },
            { label: t("run.field.scope"), value: <Hash value={r.scope_hash} /> },
            { label: t("run.field.model"), value: profile ? `${profile.name} · ${profile.model}` : "—" },
            { label: t("run.field.queued"), value: <Time iso={r.queued_at} abs /> },
            { label: t("run.field.tokens"), value: <span className="num">{t("run.tokensValue", { o: num(r.output_tokens), l: num(r.limits.max_tokens), i: num(r.input_tokens) })}</span> },
            { label: t("run.field.active"), value: <span className="num">{t("run.activeValue", { a: duration(r.active_ms), l: duration(r.limits.wall_seconds * 1000) })}</span> },
            ...(r.attempts || r.resume_stage
              ? [{ label: t("run.field.recovery"), value: t("run.recoveryValue", { n: r.attempts, stage: r.resume_stage ? t(`stage.${r.resume_stage}` as Key) : "—" }) }]
              : []),
            ...(r.completed_at ? [{ label: t("run.field.completed"), value: <Time iso={r.completed_at} abs /> }] : []),
          ]}
        />

        {r.error_summary ? (
          <div className="alert" data-tone={r.status === "timed_out" ? "warn" : "bad"} role="status">
            <p>{r.error_summary}</p>
          </div>
        ) : null}

        {hyps.data && hyps.data.length === 0 ? <WhyNoHypotheses events={events} onActivity={() => setTab("activity")} /> : null}

        <Panel title={<SectionTitle no="1">{t("run.progress")}</SectionTitle>} meta={<span className="live-dot" data-state={stream}>{t(`run.stream.${stream}` as Key)}</span>}>
          <div className="panel-body">
            <StageLine status={r.status} resumeStage={r.resume_stage} resumeStatus={r.resume_status} lastStage={lastStage} />
          </div>
        </Panel>

        <Panel title={<SectionTitle no="2">{t("run.scope")}</SectionTitle>}>
            <div className="panel-body stack" style={{ gap: 14 }}>
              <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    <th>{t("run.target")}</th>
                    <th>{t("run.field.revision")}</th>
                  </tr>
                </thead>
                <tbody>
                  {r.targets.map((x) => (
                    <tr key={x.target_id}>
                      <td>
                        <Ref kind="T" id={x.target_id} /> {targetName(x.target_id)}
                      </td>
                      <td>
                        <Hash value={x.revision} length={16} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              </div>
              <div className="row small">
                <span className="muted">{t("run.patterns")}</span>
                {r.pattern_ids.length === 0 ? <span className="muted">—</span> : r.pattern_ids.map((p) => <Ref key={p} kind="P" id={p} to={`/patterns/${p}`} />)}
              </div>
              <PolicyList dynamic={r.policy.dynamic_mode} />
            </div>
        </Panel>

        <section>
          <Tabs<Tab>
            value={tab}
            onChange={setTab}
            tabs={[
              { value: "report", label: <><span className="section-no">§3</span>{t("run.tab.report")}</> },
              { value: "hypotheses", label: <><span className="section-no">§4</span>{t("run.tab.hypotheses")}</>, count: total },
              { value: "experiments", label: <><span className="section-no">§5</span>{t("run.tab.experiments")}</>, count: exps.data?.length },
              { value: "activity", label: <><span className="section-no">§6</span>{t("run.tab.activity")}</>, count: events.length },
              { value: "artifacts", label: <><span className="section-no">§7</span>{t("run.tab.artifacts")}</>, count: arts.data?.length },
            ]}
          />
          {tab === "report" ? <ReportView runId={id} /> : null}
          {tab === "hypotheses" ? <HypothesesTable q={hyps} runId={id} /> : null}
          {tab === "experiments" ? <ExperimentsTable q={exps} /> : null}
          {tab === "activity" ? <Activity events={events} /> : null}
          {tab === "artifacts" ? <Artifacts q={arts} runId={id} /> : null}
        </section>
      </div>
    </Frame>
  );
}

function HypothesesTable({ q, runId }: { q: Query<Hypothesis[]>; runId: number }) {
  const { t } = useI18n();
  const col = useColumns("hypotheses");
  const rows = q.data ?? [];
  return (
    <Panel>
      <div className="toolbar">
        <span className="small muted num">{t("common.nRows", { n: rows.length })}</span>
        <span className="spacer" />
        {rows.some((h) => h.status === "triage" || h.status === "draft") ? (
          <Link to={`/review?run=${runId}`} className="btn sm primary">
            {t("review.start")}
          </Link>
        ) : null}
        <ColumnPicker set="hypotheses" />
      </div>
      {q.loading && !q.data ? (
        <SkeletonRows rows={4} cols={4} />
      ) : q.error ? (
        <ErrorNote error={q.error} retry={q.reload} />
      ) : rows.length === 0 ? (
        <Empty title={t("run.noHypotheses")} body={t("run.noHypothesesBody")} />
      ) : (
        <HypothesisRows rows={rows} col={col} />
      )}
    </Panel>
  );
}

export function HypothesisRows({ rows, col, queue }: { rows: Hypothesis[]; col: (c: string) => boolean; queue?: string }) {
  const { t } = useI18n();
  const href = (h: Hypothesis) => `/hypotheses/${h.id}${queue ? `?queue=${queue}` : ""}`;
  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            <th>{t("hyp.col.title")}</th>
            <th>{t("hyp.col.status")}</th>
            {col("run") ? <th>{t("columns.hypotheses.run")}</th> : null}
            {col("class") ? <th>{t("columns.hypotheses.class")}</th> : null}
            {col("priority") ? <th>{t("columns.hypotheses.priority")}</th> : null}
            {col("skeptic") ? <th>{t("columns.hypotheses.skeptic")}</th> : null}
            {col("evidence") ? <th className="right">{t("columns.hypotheses.evidence")}</th> : null}
            {col("updated") ? <th>{t("columns.hypotheses.updated")}</th> : null}
          </tr>
        </thead>
        <tbody>
          {rows.map((h) => (
            <tr key={h.id} data-href onClick={(e) => !(e.target as HTMLElement).closest("a") && navigate(href(h))}>
              <td className="title-cell">
                <Link to={href(h)}>{h.title}</Link>
                <span className="sub">
                  <span className="ref">[H-{h.id}]</span> {h.affected_symbols.slice(0, 3).join(", ")}
                </span>
              </td>
              <td>
                <State look={hypothesisLook(h.status)} />
              </td>
              {col("run") ? (
                <td>
                  <Ref kind="R" id={h.run_id} to={`/runs/${h.run_id}`} />
                </td>
              ) : null}
              {col("class") ? (
                <td className="small">
                  {h.vulnerability_class}
                  {h.cwe_id ? <span className="muted"> · {h.cwe_id}</span> : null}
                </td>
              ) : null}
              {col("priority") ? (
                <td>
                  <Priority level={h.triage_confidence} />
                </td>
              ) : null}
              {col("skeptic") ? <td className="small">{h.skeptic_verdict ? t(`skeptic.${h.skeptic_verdict}` as Key) : <span className="muted">—</span>}</td> : null}
              {col("evidence") ? <td className="right num">{h.evidence_count}</td> : null}
              {col("updated") ? (
                <td className="small ink-2">
                  <Time iso={h.updated_at} />
                </td>
              ) : null}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ExperimentsTable({ q }: { q: Query<Experiment[]> }) {
  const { t } = useI18n();
  const rows = q.data ?? [];
  return (
    <Panel>
      {q.loading && !q.data ? (
        <SkeletonRows rows={3} cols={4} />
      ) : rows.length === 0 ? (
        <Empty title={t("run.noExperiments")} body={t("run.noExperimentsBody")} />
      ) : (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>{t("exp.col.operation")}</th>
                <th>{t("exp.col.status")}</th>
                <th>{t("exp.col.subject")}</th>
                <th>{t("exp.col.summary")}</th>
                <th>{t("exp.col.when")}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((x) => (
                <tr key={x.id}>
                  <td>
                    <span className="mono small">{x.operation}</span>
                    <span className="sub" style={{ display: "block" }}>
                      <span className="ref">[X-{x.id}]</span>
                    </span>
                  </td>
                  <td>
                    <State look={experimentLook(x.status)} />
                  </td>
                  <td className="small">
                    {x.hypothesis_id ? <Ref kind="H" id={x.hypothesis_id} to={`/hypotheses/${x.hypothesis_id}`} /> : null}
                    {x.approval_id ? <Ref kind="A" id={x.approval_id} /> : null}
                    {!x.hypothesis_id && x.target_id ? <Ref kind="T" id={x.target_id} /> : null}
                  </td>
                  <td className="small" style={{ maxWidth: 420 }}>
                    {x.result_summary || <span className="muted">—</span>}
                  </td>
                  <td className="small ink-2">
                    <Time iso={x.completed_at ?? x.started_at ?? x.created_at} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Panel>
  );
}

/**
 * Explains a run that produced no hypotheses, from the events its
 * generation stages emit, so "0" is never left unexplained.
 */
function WhyNoHypotheses({ events, onActivity }: { events: RunEvent[]; onActivity: () => void }) {
  const { t } = useI18n();
  const { can } = useSession();
  const last = (type: string) => [...events].reverse().find((e) => e.event_type === type);
  const n = (e: RunEvent | undefined, k: string) => (typeof e?.payload?.[k] === "number" ? (e.payload[k] as number) : 0);
  const skipped = events.some((e) => e.event_type === "stage.skipped" && e.payload?.reason === "researcher_disabled");
  const mapped = last("stage.mapped");
  const gen = last("stage.generated");
  const firstError = events.find((e) => e.event_type === "agent.error")?.message;

  const reasons: { tone: "info" | "warn"; text: string; hint?: string; link?: { to: string; label: string } }[] = [];
  if (skipped) {
    reasons.push({ tone: "warn", text: t("run.why.researcherOff"), link: can("admin") ? { to: "/settings/agents", label: t("settings.agents") } : undefined });
  } else if (mapped && n(mapped, "candidates") === 0) {
    reasons.push({ tone: "warn", text: t("run.why.noRegions"), hint: t("run.why.noRegionsHint") });
  } else if (gen) {
    if (n(gen, "failed") > 0) {
      const truncated = /truncated at max/.test(firstError ?? "");
      reasons.push({
        tone: "warn",
        text: t("run.why.failed", { n: n(gen, "failed"), error: firstError ?? "" }),
        hint: truncated ? t("run.why.truncatedHint") : t("run.why.failedHint"),
        link: can("admin") ? (truncated ? { to: "/settings/research", label: t("settings.research") } : { to: "/settings/models", label: t("settings.models") }) : undefined,
      });
    }
    if (n(gen, "refused") > 0) reasons.push({ tone: "warn", text: t("run.why.refused", { n: n(gen, "refused") }) });
    if (n(gen, "invalid") > 0) reasons.push({ tone: "warn", text: t("run.why.invalid", { n: n(gen, "invalid") }) });
    if (gen.payload?.budget_hit === true) reasons.push({ tone: "warn", text: t("run.why.budget"), hint: t("run.why.budgetHint") });
    if (n(gen, "declined") > 0) reasons.push({ tone: "info", text: t("run.why.declined", { n: n(gen, "declined") }), hint: t("run.why.declinedHint") });
  }
  if (reasons.length === 0) return null;
  const tone = reasons.some((r) => r.tone === "warn") ? "warn" : "info";
  return (
    <div className="alert" data-tone={tone} role="status">
      <Icon name={tone === "warn" ? "alert" : "info"} />
      <div className="stack" style={{ gap: 6 }}>
        <strong>{t("run.why.title")}</strong>
        <ul className="why-list">
          {reasons.map((r, i) => (
            <li key={i}>
              {r.text}
              {r.hint ? <span className="small ink-2"> {r.hint}</span> : null}
              {r.link ? (
                <>
                  {" "}
                  <Link to={r.link.to} className="small">
                    {r.link.label}
                  </Link>
                </>
              ) : null}
            </li>
          ))}
        </ul>
        <span>
          <Button size="sm" variant="ghost" onClick={onActivity}>
            {t("run.why.activity")}
          </Button>
        </span>
      </div>
    </div>
  );
}

function Activity({ events }: { events: RunEvent[] }) {
  const { t, dateTime } = useI18n();
  const first = useRef(events.length);
  const shown = [...events].reverse();
  return (
    <Panel>
      {events.length === 0 ? (
        <Empty title={t("run.noActivity")} />
      ) : (
        <ol className="events" aria-live="polite">
          {shown.map((e, i) => (
            <li key={e.id} data-new={events.length - 1 - i >= first.current}>
              <time className="when" dateTime={e.created_at} title={dateTime(e.created_at)}>
                {new Date(e.created_at).toLocaleTimeString()}
              </time>
              <span className="type">{e.event_type}</span>
              <span>{e.message}</span>
            </li>
          ))}
        </ol>
      )}
    </Panel>
  );
}

function Artifacts({ q, runId }: { q: Query<Artifact[]>; runId: number }) {
  const { t, bytes } = useI18n();
  const toast = useToast();
  const rows = q.data ?? [];
  return (
    <Panel>
      {q.loading && !q.data ? (
        <SkeletonRows rows={3} cols={4} />
      ) : rows.length === 0 ? (
        <Empty title={t("run.noArtifacts")} body={t("run.noArtifactsBody")} />
      ) : (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>{t("art.col.name")}</th>
                <th>{t("art.col.kind")}</th>
                <th className="right">{t("art.col.size")}</th>
                <th>sha256</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {rows.map((a) => (
                <tr key={a.id}>
                  <td className="title-cell">{a.name}</td>
                  <td className="small">{t(`art.kind.${a.artifact_kind}` as Key)}</td>
                  <td className="right num small">{bytes(a.size_bytes)}</td>
                  <td>
                    <Hash value={a.sha256} />
                  </td>
                  <td className="right">
                    <Button
                      size="sm"
                      icon="download"
                      onClick={() => download(`/runs/${runId}/artifacts/${a.id}`, a.name).catch(() => toast(t("art.unavailable"), "bad"))}
                    >
                      {t("art.download")}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Panel>
  );
}
