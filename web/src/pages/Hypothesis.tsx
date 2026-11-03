import { useEffect, useMemo, useState } from "react";
import { Frame } from "../App";
import { patch, post } from "../api/client";
import { useApi, useHotkeys, useUserNames } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { Link, navigate, useLocation } from "../router";
import { useSession } from "../session";
import type { Approval, Evidence, Experiment, Hypothesis, HypothesisDetail, LabLimits, Project, Run } from "../api/types";
import { Alert, Button, Drawer, Empty, ErrorNote, errorText, Field, Hash, Panel, Priority, Ref, SkeletonRows, State, Time, useToast } from "../ui/components";
import { Custody, Excerpt, PolicyList, SectionTitle, type CustodyEntry } from "../ui/domain";
import { decisionLook, experimentLook, hypothesisLook } from "../ui/status";

export function HypothesisPage({ id }: { id: number }) {
  const { t } = useI18n();
  const { query } = useLocation();
  const queueName = query.get("queue");
  const queueRun = query.get("run");
  const h = useApi<HypothesisDetail>(`/hypotheses/${id}`);
  const run = useApi<Run>(h.data ? `/runs/${h.data.run_id}` : null);
  const project = useApi<Project>(run.data ? `/projects/${run.data.project_id}` : null);
  const exps = useApi<Experiment[]>(h.data ? `/runs/${h.data.run_id}/experiments` : null);
  const approvals = useApi<Approval[]>("/approvals");
  const queue = useApi<Hypothesis[]>(queueName === "triage" ? "/hypotheses?status=draft,triage" : null);

  const queueIds = useMemo(() => {
    let rows = queue.data ?? [];
    if (queueRun) rows = rows.filter((x) => x.run_id === Number(queueRun));
    return rows.map((x) => x.id);
  }, [queue.data, queueRun]);
  const pos = queueIds.indexOf(id);
  const suffix = queueName ? `?queue=${queueName}${queueRun ? `&run=${queueRun}` : ""}` : "";
  const go = (delta: number) => {
    const next = queueIds[pos + delta];
    if (next !== undefined) navigate(`/hypotheses/${next}${suffix}`);
  };
  useHotkeys({ j: () => go(1), k: () => go(-1) }, queueName !== null);

  const crumbs = queueName
    ? [{ label: t("nav.review"), to: "/review" }, { label: `H-${id}` }]
    : [{ label: t("nav.runs"), to: "/runs" }, ...(h.data ? [{ label: t("runs.runN", { id: h.data.run_id }), to: `/runs/${h.data.run_id}` }] : []), { label: `H-${id}` }];

  return (
    <Frame
      crumbs={crumbs}
      actions={
        queueName && queueIds.length > 0 ? (
          <span className="queue-nav">
            <Button size="sm" variant="ghost" iconOnly icon="chevronLeft" aria-label={t("review.prev")} title={t("review.prev")} disabled={pos <= 0} onClick={() => go(-1)} />
            <span className="num">{pos >= 0 ? t("review.position", { i: pos + 1, n: queueIds.length }) : t("review.done")}</span>
            <Button size="sm" variant="ghost" iconOnly icon="chevronRight" aria-label={t("review.next")} title={t("review.next")} disabled={pos < 0 || pos >= queueIds.length - 1} onClick={() => go(1)} />
          </span>
        ) : null
      }
    >
      {h.error ? (
        <ErrorNote error={h.error} retry={h.reload} />
      ) : !h.data ? (
        <SkeletonRows rows={8} cols={2} />
      ) : (
        <Review
          key={h.data.id}
          h={h.data}
          run={run.data}
          project={project.data}
          experiments={(exps.data ?? []).filter((x) => x.hypothesis_id === id)}
          approvals={(approvals.data ?? []).filter((a) => a.hypothesis_id === id)}
          onChanged={() => {
            h.reload();
            approvals.reload();
            exps.reload();
          }}
          onDecided={() => {
            // In a queue, move on to the next item that still waits.
            if (!queueName) return;
            const rest = queueIds.filter((x) => x !== id);
            const next = rest[Math.min(Math.max(pos, 0), rest.length - 1)];
            queue.reload();
            if (next !== undefined) navigate(`/hypotheses/${next}${suffix}`);
            else navigate("/review");
          }}
        />
      )}
    </Frame>
  );
}

function Review({
  h,
  run,
  project,
  experiments,
  approvals,
  onChanged,
  onDecided,
}: {
  h: HypothesisDetail;
  run?: Run;
  project?: Project;
  experiments: Experiment[];
  approvals: Approval[];
  onChanged: () => void;
  onDecided: () => void;
}) {
  const { t } = useI18n();
  const { can, reloadSummary } = useSession();
  const userName = useUserNames();
  const toast = useToast();
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState<string>();
  const [labOpen, setLabOpen] = useState(false);

  const sources = h.evidence.filter((e) => e.evidence_kind === "source_location");
  const counters = h.evidence.filter((e) => e.evidence_kind === "counter_evidence");
  const observations = h.evidence.filter((e) => !["source_location", "counter_evidence"].includes(e.evidence_kind));
  const marksFor = (ev: Evidence): [number, number][] =>
    counters.filter((c) => c.file_path === ev.file_path && c.line_start).map((c) => [c.line_start!, c.line_end ?? c.line_start!]);

  const triageable = h.status === "draft" || h.status === "triage";
  const finished = experiments.some((x) => ["passed", "finding", "failed", "resource_limited"].includes(x.status));
  const hasFinding = experiments.some((x) => x.status === "finding");
  const concludable = h.status === "validation_pending" && finished;
  const reviewer = can("reviewer");

  const decide = async (decision: string) => {
    setBusy(decision);
    try {
      await patch(`/hypotheses/${h.id}`, { decision, reviewer_note: note });
      toast(t("hyp.decided", { decision: t(`decision.${decision}` as Key) }));
      setNote("");
      reloadSummary();
      onChanged();
      onDecided();
    } catch (e) {
      toast(errorText(e, t), "bad");
    } finally {
      setBusy(undefined);
    }
  };

  useHotkeys(
    {
      a: () => triageable && reviewer && void decide("accepted"),
      r: () => triageable && reviewer && void decide("rejected"),
      d: () => triageable && reviewer && void decide("duplicate"),
      v: () => concludable && hasFinding && reviewer && void decide("validated"),
      n: () => concludable && reviewer && void decide("not_reproducible"),
    },
    !busy,
  );

  const custody: CustodyEntry[] = [
    {
      icon: "file",
      what: t("custody.drafted"),
      who: t("custody.researcherAgent"),
      when: <Time iso={h.created_at} />,
      detail: h.pattern_id ? (
        <>
          {t("custody.fromPattern")} <Ref kind="P" id={h.pattern_id} to={`/patterns/${h.pattern_id}`} />
        </>
      ) : undefined,
      tone: "accent",
    },
    h.skeptic_verdict
      ? {
          icon: h.skeptic_verdict === "refutes" ? "x" : h.skeptic_verdict === "weakens" ? "alert" : "check",
          what: t(`skeptic.${h.skeptic_verdict}` as Key),
          who: t("custody.skepticAgent"),
          detail: t("custody.counterN", { n: counters.length }),
          tone: h.skeptic_verdict === "refutes" ? "bad" : h.skeptic_verdict === "weakens" ? "warn" : "ok",
        }
      : { icon: "clock", what: t("custody.notChallenged"), who: t("custody.skepticAgent"), pending: true },
    h.auto_triaged
      ? {
          icon: h.status === "rejected" || h.status === "duplicate" ? "x" : "check",
          what: t(`custody.triage.${h.status === "rejected" || h.status === "duplicate" ? h.status : "accepted"}` as Key),
          who: t("custody.autopilot"),
          when: <Time iso={h.reviewed_at} />,
          detail: t("custody.autoNote"),
          tone: h.status === "rejected" || h.status === "duplicate" ? "warn" : "ok",
        }
      : h.reviewed_by && !triageable
        ? {
            icon: h.status === "rejected" || h.status === "duplicate" ? "x" : "check",
            what: t(`custody.triage.${h.status === "rejected" || h.status === "duplicate" ? h.status : "accepted"}` as Key),
            who: userName(h.reviewed_by),
            when: <Time iso={h.reviewed_at} />,
            detail: h.reviewer_note || undefined,
            tone: h.status === "rejected" || h.status === "duplicate" ? "bad" : "ok",
          }
        : { icon: "user", what: t("custody.awaitingTriage"), pending: true },
    ...approvals
      .slice()
      .reverse()
      .map<CustodyEntry>((a) => ({
        icon: "approvals",
        what: (
          <span className="row" style={{ gap: 6 }}>
            {t("custody.lab", { op: a.scope.operation })} <State look={decisionLook(a.decision)} />
          </span>
        ),
        who: a.decided_by ? t("custody.requestedDecided", { r: userName(a.requested_by), d: userName(a.decided_by) }) : t("custody.requestedBy", { r: userName(a.requested_by) }),
        when: <Time iso={a.decided_at ?? a.created_at} />,
        detail: (
          <>
            <Ref kind="A" id={a.id} to={`/approvals?open=${a.id}`} /> <Hash value={a.scope_hash} label={t("approvals.scopeHash")} />
            {a.self_approved ? <> · {t("approvals.selfApproved")}</> : null}
          </>
        ),
        tone: a.decision === "approved" ? "ok" : a.decision === "pending" ? "warn" : "bad",
        pending: a.decision === "pending",
      })),
    ...experiments.map<CustodyEntry>((x) => ({
      icon: "flask",
      what: (
        <span className="row" style={{ gap: 6 }}>
          <span className="mono">{x.operation}</span> <State look={experimentLook(x.status)} />
        </span>
      ),
      when: <Time iso={x.completed_at ?? x.started_at} />,
      detail: x.result_summary,
      tone: x.status === "finding" ? "bad" : x.status === "passed" ? "ok" : "warn",
    })),
    ...(h.status === "validated" || h.status === "not_reproducible"
      ? [
          {
            icon: h.status === "validated" ? "check" : "x",
            what: t(`status.hypothesis.${h.status}` as Key),
            who: userName(h.reviewed_by),
            when: <Time iso={h.reviewed_at} />,
            tone: h.status === "validated" ? "ok" : undefined,
          } as CustodyEntry,
        ]
      : []),
  ];

  return (
    <div className="stack" style={{ gap: 18 }}>
      <header className="stack" style={{ gap: 10 }}>
        <div className="row" style={{ gap: 10 }}>
          <span className="ref">[H-{h.id}]</span>
          <State look={hypothesisLook(h.status)} />
          <Priority level={h.triage_confidence} />
          <span className="small ink-2">
            {h.vulnerability_class}
            {h.cwe_id ? ` · ${h.cwe_id}` : ""}
          </span>
          <span className="spacer" />
          <span className="small muted row" style={{ gap: 6 }}>
            <Ref kind="R" id={h.run_id} to={`/runs/${h.run_id}`} />
            <Ref kind="T" id={h.target_id} />
            {project ? <Link to={`/projects/${project.id}`}>{project.name}</Link> : null}
          </span>
        </div>
        <h1 style={{ maxWidth: "48ch" }}>{h.title}</h1>
      </header>

      <div className="review">
        <div className="stack" style={{ gap: 18, minWidth: 0 }}>
          <Panel>
            <div className="panel-body stack" style={{ gap: 18 }}>
              <div className="claim-block">
                <h3>{t("hyp.claim")}</h3>
                <p className="review-claim">{h.claim}</p>
              </div>
              <div className="grid-12" style={{ gap: 20 }}>
                <ListBlock span="span-4" title={t("hyp.facts")} hint={t("hyp.factsHint")} items={h.facts} />
                <ListBlock span="span-4" title={t("hyp.preconditions")} hint={t("hyp.preconditionsHint")} items={h.preconditions} />
                <ListBlock span="span-4" title={t("hyp.unknowns")} hint={t("hyp.unknownsHint")} items={h.unknowns} />
              </div>
              <div className="claim-block">
                <h3>{t("hyp.expected")}</h3>
                <p className="prose">{h.expected_observation || <span className="muted">—</span>}</p>
              </div>
              {h.affected_symbols.length ? (
                <div className="row small">
                  <span className="muted">{t("hyp.symbols")}</span>
                  {h.affected_symbols.map((s) => (
                    <code key={s}>{s}</code>
                  ))}
                </div>
              ) : null}
            </div>
          </Panel>

          <section className="stack" style={{ gap: 10 }}>
            <SectionTitle no="1">{t("hyp.evidence")}</SectionTitle>
            {sources.length === 0 ? (
              <Panel>
                <Empty title={t("hyp.noEvidence")} />
              </Panel>
            ) : (
              sources.map((ev) => <Excerpt key={ev.id} ev={ev} marks={marksFor(ev)} refNode={<Ref kind="E" id={ev.id} />} />)
            )}
            {observations.length ? (
              <Panel title={t("hyp.observations")}>
                <ul className="item-list">
                  {observations.map((o) => (
                    <li key={o.id}>
                      <div>
                        <span className="t" style={{ whiteSpace: "normal" }}>
                          {o.summary}
                        </span>
                        <span className="small muted">{t(`evidence.kind.${o.evidence_kind}` as Key)}</span>
                        <span className="m">
                          <Ref kind="E" id={o.id} />
                          {o.file_path ? (
                            <span className="mono">
                              {o.file_path}
                              {o.line_start ? `:${o.line_start}` : ""}
                            </span>
                          ) : null}
                          {typeof o.metadata.rule_id === "string" && o.metadata.rule_id ? <span className="mono">{o.metadata.rule_id}</span> : null}
                        </span>
                      </div>
                    </li>
                  ))}
                </ul>
              </Panel>
            ) : null}
          </section>

          <section className="stack" style={{ gap: 10 }}>
            <SectionTitle no="2">{t("hyp.counter")}</SectionTitle>
            <Panel>
              {counters.length === 0 && h.counter_evidence.length === 0 ? (
                <Empty title={t("hyp.noCounter")} body={h.skeptic_verdict ? undefined : t("hyp.noCounterBody")} />
              ) : (
                <ul className="item-list">
                  {(counters.length ? counters : h.counter_evidence.map((c, i) => ({ id: -i, summary: c }) as Evidence)).map((c) => (
                    <li key={c.id}>
                      <div>
                        <CounterText summary={c.summary} />
                        <span />
                        {c.id > 0 && (c.file_path || c.line_start) ? (
                          <span className="m">
                            <Ref kind="E" id={c.id} />
                            <span className="mono">
                              {c.file_path}
                              {c.line_start ? `:${c.line_start}${c.line_end && c.line_end !== c.line_start ? `–${c.line_end}` : ""}` : ""}
                            </span>
                          </span>
                        ) : null}
                      </div>
                    </li>
                  ))}
                </ul>
              )}
            </Panel>
          </section>
        </div>

        <aside className="stack" style={{ gap: 16, position: "sticky", top: 68 }}>
          <Panel title={t("custody.title")}>
            <div className="panel-body">
              <Custody entries={custody} label={t("custody.title")} />
            </div>
          </Panel>
          {project ? (
            <Panel title={t("hyp.boundary")}>
              <div className="panel-body">
                <PolicyList dynamic={project.policy.dynamic_mode} />
              </div>
            </Panel>
          ) : null}
        </aside>
      </div>

      <DecisionBar
        h={h}
        note={note}
        setNote={setNote}
        busy={busy}
        decide={decide}
        reviewer={reviewer}
        triageable={triageable}
        concludable={concludable}
        hasFinding={hasFinding}
        canRequestLab={h.status === "accepted" && can("researcher") && run?.status === "review_required"}
        labAllowed={project?.policy.dynamic_mode === "local_lab_only"}
        onLab={() => setLabOpen(true)}
      />
      <LabRequest open={labOpen} onClose={() => setLabOpen(false)} h={h} onDone={onChanged} />
    </div>
  );
}

function ListBlock({ title, hint, items, span }: { title: string; hint: string; items: string[]; span: string }) {
  return (
    <div className={`claim-block ${span}`}>
      <h3 title={hint}>{title}</h3>
      {items.length ? (
        <ul className="bullets">
          {items.map((x, i) => (
            <li key={i}>{x}</li>
          ))}
        </ul>
      ) : (
        <p className="muted small">—</p>
      )}
    </div>
  );
}

function DecisionBar(p: {
  h: Hypothesis;
  note: string;
  setNote: (s: string) => void;
  busy?: string;
  decide: (d: string) => void;
  reviewer: boolean;
  triageable: boolean;
  concludable: boolean;
  hasFinding: boolean;
  canRequestLab: boolean;
  labAllowed: boolean;
  onLab: () => void;
}) {
  const { t } = useI18n();
  const showTriage = p.reviewer && p.triageable;
  const showConclude = p.reviewer && p.concludable;
  if (!showTriage && !showConclude && !p.canRequestLab) {
    if (p.h.status === "validation_pending")
      return (
        <div className="decision-bar">
          <span className="small ink-2">{t("hyp.waitingForLab")}</span>
        </div>
      );
    return null;
  }
  return (
    <div className="decision-bar" role="group" aria-label={t("hyp.decision")}>
      {showTriage || showConclude ? (
        <input className="input note" placeholder={t("hyp.notePlaceholder")} aria-label={t("hyp.note")} value={p.note} maxLength={8000} onChange={(e) => p.setNote(e.target.value)} />
      ) : null}
      {showTriage ? (
        <>
          <Button variant="primary" icon="check" kbd="A" loading={p.busy === "accepted"} onClick={() => p.decide("accepted")}>
            {t("decision.accepted")}
          </Button>
          <Button icon="x" kbd="R" loading={p.busy === "rejected"} onClick={() => p.decide("rejected")}>
            {t("decision.rejected")}
          </Button>
          <Button variant="ghost" kbd="D" loading={p.busy === "duplicate"} onClick={() => p.decide("duplicate")}>
            {t("decision.duplicate")}
          </Button>
        </>
      ) : null}
      {showConclude ? (
        <>
          <Button variant="primary" icon="check" kbd="V" disabled={!p.hasFinding} title={p.hasFinding ? undefined : t("hyp.validatedNeedsFinding")} loading={p.busy === "validated"} onClick={() => p.decide("validated")}>
            {t("decision.validated")}
          </Button>
          <Button icon="x" kbd="N" loading={p.busy === "not_reproducible"} onClick={() => p.decide("not_reproducible")}>
            {t("decision.not_reproducible")}
          </Button>
        </>
      ) : null}
      {p.canRequestLab ? (
        p.labAllowed ? (
          <Button icon="flask" onClick={p.onLab}>
            {t("lab.request")}
          </Button>
        ) : (
          <span className="small muted">{t("lab.disabledByPolicy")}</span>
        )
      ) : null}
    </div>
  );
}

const labOps = ["lab.asan_test", "lab.ubsan_test", "lab.unit_test", "lab.libfuzzer"] as const;

function LabRequest({ open, onClose, h, onDone }: { open: boolean; onClose: () => void; h: Hypothesis; onDone: () => void }) {
  const { t } = useI18n();
  const toast = useToast();
  const [op, setOp] = useState<(typeof labOps)[number]>("lab.asan_test");
  const [summary, setSummary] = useState("");
  const [limits, setLimits] = useState<LabLimits>({ wall_seconds: 300, memory_mb: 2048, cpu_cores: 2, max_processes: 32 });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  useEffect(() => {
    if (open) setSummary(h.expected_observation ? t("lab.summaryDefault", { obs: h.expected_observation }) : "");
  }, [open, h, t]);
  const submit = async () => {
    setBusy(true);
    setError(undefined);
    try {
      await post(`/hypotheses/${h.id}/lab-approval`, { operation: op, summary, limits });
      toast(t("lab.requested"));
      onDone();
      onClose();
    } catch (e) {
      setError(errorText(e, t));
    } finally {
      setBusy(false);
    }
  };
  const num = (k: keyof LabLimits, min: number, max: number) => (
    <Field label={t(`lab.limit.${k}` as Key)}>
      {(id) => <input id={id} className="input" type="number" min={min} max={max} value={limits[k]} onChange={(e) => setLimits({ ...limits, [k]: Math.min(max, Math.max(min, Number(e.target.value) || min)) })} />}
    </Field>
  );
  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={t("lab.request")}
      foot={
        <>
          <Button onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" loading={busy} disabled={!summary.trim()} onClick={submit}>
            {t("lab.submit")}
          </Button>
        </>
      }
    >
      <Alert icon="shield">{t("lab.explain")}</Alert>
      <Field label={t("lab.operation")} hint={t("lab.harnessHint", { name: op.slice(4) })}>
        {(id) => (
          <select id={id} className="select" value={op} onChange={(e) => setOp(e.target.value as (typeof labOps)[number])}>
            {labOps.map((o) => (
              <option key={o} value={o}>
                {o} — {t(`lab.op.${o.slice(4)}` as Key)}
              </option>
            ))}
          </select>
        )}
      </Field>
      <Field label={t("lab.summary")} hint={t("lab.summaryHint")}>
        {(id) => <textarea id={id} className="textarea" value={summary} maxLength={8000} onChange={(e) => setSummary(e.target.value)} />}
      </Field>
      <div className="form-grid two">
        {num("wall_seconds", 1, 3600)}
        {num("memory_mb", 64, 16384)}
        {num("cpu_cores", 1, 8)}
        {num("max_processes", 1, 256)}
      </div>
      {error ? (
        <Alert tone="bad" icon="alert">
          {error}
        </Alert>
      ) : null}
    </Drawer>
  );
}

// The orchestrator records the skeptic as "Skeptic rationale (<verdict>): <text>".
function CounterText({ summary }: { summary: string }) {
  const { t } = useI18n();
  const m = /^Skeptic rationale \((supports|weakens|refutes)\): ([\s\S]*)$/.exec(summary);
  if (!m) return <span className="counter-text">{summary}</span>;
  const verdict = m[1] as "supports" | "weakens" | "refutes";
  const tone = verdict === "refutes" ? "bad" : verdict === "weakens" ? "warn" : "muted";
  return (
    <span className="counter-text">
      <span className="row" style={{ gap: 8 }}>
        <span className="small muted">{t("custody.skepticAgent")}</span>
        <span className="state" data-tone={tone} data-form="solid">{t(`skeptic.${verdict}` as Key)}</span>
      </span>
      {m[2]}
    </span>
  );
}
