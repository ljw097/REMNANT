import type { ReactNode } from "react";
import { useI18n, type Key } from "../i18n";
import type { Evidence, Hypothesis, RunStatus } from "../api/types";
import { Icon, type IconName } from "./Icon";
import { Hash } from "./components";

/* ---------- Transit stage line ----------
   A run is a line of stations. Lab validation is an interchange: runs only
   pass through it when a reviewer approves a lab experiment. */

export const stations: RunStatus[] = [
  "queued",
  "collecting",
  "indexing",
  "surface_mapping",
  "hypothesis_generation",
  "static_validation",
  "review_required",
  "lab_validation",
  "report_ready",
  "completed",
];

function stationIndex(s: string): number {
  if (s === "validation_approved") return stations.indexOf("lab_validation") - 1;
  return stations.indexOf(s as RunStatus);
}

export function StageLine({
  status,
  resumeStage,
  resumeStatus,
  lastStage,
  mini,
  onSelect,
}: {
  status: RunStatus;
  resumeStage?: string;
  resumeStatus?: string;
  lastStage?: string;
  mini?: boolean;
  /** Makes each station a button that opens that stage's details. */
  onSelect?: (stage: RunStatus) => void;
}) {
  const { t } = useI18n();
  let mode: "run" | "waiting" | "paused" | "failed" | "done" = "run";
  let at = stationIndex(status);
  if (status === "paused") {
    mode = "paused";
    at = stationIndex(resumeStage || resumeStatus || "queued");
  } else if (status === "failed" || status === "timed_out" || status === "cancelled") {
    mode = "failed";
    at = lastStage ? stationIndex(lastStage) : -1;
  } else if (status === "completed") {
    mode = "done";
  } else if (status === "review_required") {
    mode = "waiting";
  }
  const label = t("stage.aria", { status: t(`status.run.${status}` as Key) });
  return (
    <div className={mini ? "stageline mini" : "stageline"} role={onSelect ? "group" : "img"} aria-label={label}>
      {stations.map((s, i) => {
        const reached = mode === "done" || i <= at;
        const current = i === at && mode !== "done";
        const segment = i === at && mode === "paused" ? "paused" : i === at && mode === "failed" ? "failed" : undefined;
        const attrs = {
          "data-reached": reached,
          "data-current": current,
          "data-mode": current ? mode : undefined,
          "data-segment": segment,
          "data-interchange": s === "lab_validation",
          title: t(`stage.${s}` as Key),
        };
        const inner = (
          <>
            <span className="dot" />
            {mini ? null : <span className="name">{t(`stage.${s}` as Key)}</span>}
          </>
        );
        return onSelect ? (
          <button key={s} type="button" className="stop selectable" {...attrs} aria-label={t("detail.stage.open", { stage: t(`stage.${s}` as Key) })} onClick={() => onSelect(s)}>
            {inner}
          </button>
        ) : (
          <div key={s} className="stop" {...attrs}>
            {inner}
          </div>
        );
      })}
    </div>
  );
}

/* ---------- Hypothesis lifecycle flow (one "intent") ---------- */

type FlowState = "done" | "current" | "pending" | "skipped" | "bad";

// hypothesisFlow derives the lifecycle of one hypothesis: how it travelled from
// a matched candidate, through the Researcher and Skeptic, to triage, optional
// lab validation, and an outcome. Each node's state drives the same stage-line
// visual the run uses, so the path never depends on colour alone.
function hypothesisFlow(h: Hypothesis): { key: Key; state: FlowState; note: Key }[] {
  const decided = !["draft", "triage"].includes(h.status);
  const terminal = ["accepted", "rejected", "validated", "not_reproducible", "duplicate"].includes(h.status);
  const bad = ["rejected", "not_reproducible", "duplicate"].includes(h.status);
  const inLab = ["validation_pending", "validated", "not_reproducible"].includes(h.status);

  const skeptic: FlowState = h.skeptic_verdict === "" ? "skipped" : h.skeptic_verdict === "refutes" ? "bad" : "done";
  const triage: FlowState = decided ? "done" : "current";
  const lab: FlowState = h.status === "validation_pending" ? "current" : inLab ? "done" : "skipped";
  const outcome: FlowState = terminal ? (bad ? "bad" : "done") : "pending";

  return [
    { key: "flow.candidate", state: "done", note: "flow.candidate.note" },
    { key: "flow.researcher", state: "done", note: "flow.researcher.note" },
    { key: "flow.skeptic", state: skeptic, note: h.skeptic_verdict ? (`flow.skeptic.${h.skeptic_verdict}` as Key) : "flow.skeptic.skipped" },
    { key: "flow.triage", state: triage, note: h.auto_triaged ? "flow.triage.auto" : decided ? "flow.triage.human" : "flow.triage.waiting" },
    { key: "flow.lab", state: lab, note: lab === "skipped" ? "flow.lab.skipped" : "flow.lab.note" },
    { key: "flow.outcome", state: outcome, note: `status.hypothesis.${h.status}` as Key },
  ];
}

export function HypothesisFlow({ h }: { h: Hypothesis }) {
  const { t } = useI18n();
  return (
    <div className="stageline" role="img" aria-label={t("flow.aria")}>
      {hypothesisFlow(h).map((n) => {
        const reached = n.state === "done" || n.state === "current" || n.state === "bad";
        const segment = n.state === "bad" ? "failed" : n.state === "skipped" ? "paused" : undefined;
        return (
          <div key={n.key} className="stop" data-reached={reached} data-current={n.state === "current"} data-segment={segment} data-interchange={n.key === "flow.lab"} title={t(n.note)}>
            <span className="dot" />
            <span className="name">{t(n.key)}</span>
          </div>
        );
      })}
    </div>
  );
}

/* ---------- RFC-style document header ---------- */

export function DocHeader({ docno, title, state, fields, actions }: { docno: string; title: ReactNode; state?: ReactNode; fields: { label: ReactNode; value: ReactNode }[]; actions?: ReactNode }) {
  return (
    <section className="docheader">
      <div className="masthead">
        <span className="docno">{docno}</span>
        <h1 style={{ flex: 1, minWidth: 0 }}>{title}</h1>
        {state}
        {actions ? <div className="row">{actions}</div> : null}
      </div>
      <dl>
        {fields.map((f, i) => (
          <div key={i}>
            <dt>{f.label}</dt>
            <dd>{f.value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

export function SectionTitle({ no, children, id }: { no: string; children: ReactNode; id?: string }) {
  return (
    <h2 id={id}>
      <span className="section-no">§{no}</span>
      {children}
    </h2>
  );
}

/* ---------- Evidence excerpt with an aligned line-number gutter ---------- */

export interface ExcerptLine {
  no: number | null;
  src: string;
}

/** Parses "   120 | code" lines written by the orchestrator. */
export function parseExcerpt(excerpt: string): { lines: ExcerptLine[]; note?: string } {
  const lines: ExcerptLine[] = [];
  let note: string | undefined;
  for (const raw of excerpt.replace(/\n$/, "").split("\n")) {
    const m = /^\s*(\d+)?\s\|\s?(.*)$/.exec(raw);
    if (!m) {
      lines.push({ no: null, src: raw });
      continue;
    }
    if (!m[1]) {
      note = m[2];
      continue;
    }
    lines.push({ no: Number(m[1]), src: m[2] ?? "" });
  }
  return { lines, note };
}

export function Excerpt({ ev, marks, refNode }: { ev: Evidence; marks?: [number, number][]; refNode?: ReactNode }) {
  const { t } = useI18n();
  const { lines, note } = parseExcerpt(ev.excerpt);
  const marked = (n: number | null) => n !== null && (marks ?? []).some(([a, b]) => n >= a && n <= b);
  return (
    <figure className="excerpt" style={{ margin: 0 }}>
      <figcaption className="excerpt-head">
        {refNode}
        <span className="path">
          {ev.file_path}
          {ev.line_start ? `:${ev.line_start}${ev.line_end && ev.line_end !== ev.line_start ? `–${ev.line_end}` : ""}` : ""}
        </span>
        {ev.symbol ? <span className="muted">{t("evidence.in", { symbol: ev.symbol })}</span> : null}
        <span className="spacer" />
        {ev.content_sha256 ? <Hash value={ev.content_sha256} label="sha256" /> : null}
      </figcaption>
      {lines.length > 0 ? (
        <pre tabIndex={0} aria-label={t("evidence.excerptOf", { path: ev.file_path })}>
          {lines.map((l, i) => (
            <div key={i} className="ln" data-mark={marked(l.no)}>
              <span className="no">{l.no ?? ""}</span>
              <span className="src">{l.src || " "}</span>
            </div>
          ))}
        </pre>
      ) : null}
      {note ? <div className="note">{note}</div> : null}
    </figure>
  );
}

/* ---------- Chain of custody ---------- */

export interface CustodyEntry {
  icon: IconName;
  what: ReactNode;
  who?: ReactNode;
  when?: ReactNode;
  detail?: ReactNode;
  tone?: "ok" | "bad" | "warn" | "accent";
  pending?: boolean;
}

export function Custody({ entries, label }: { entries: CustodyEntry[]; label: string }) {
  return (
    <ol className="custody" aria-label={label}>
      {entries.map((e, i) => (
        <li key={i} data-tone={e.tone} data-pending={e.pending}>
          <span className="seal">
            <Icon name={e.icon} size={11} />
          </span>
          <div>
            <div className="what">{e.what}</div>
            {e.who || e.when ? (
              <div className="who">
                {e.who}
                {e.who && e.when ? " · " : null}
                {e.when}
              </div>
            ) : null}
            {e.detail ? <div className="detail">{e.detail}</div> : null}
          </div>
        </li>
      ))}
    </ol>
  );
}

/* ---------- Safety boundary ---------- */

export function PolicyList({ dynamic }: { dynamic: "disabled" | "local_lab_only" }) {
  const { t } = useI18n();
  return (
    <ul className="policy">
      <li>
        <Icon name="lock" />
        {t("policy.readOnly")}
      </li>
      <li>
        <Icon name="noNetwork" />
        {t("policy.noNetwork")}
      </li>
      <li data-open={dynamic === "local_lab_only"}>
        <Icon name="flask" />
        {dynamic === "local_lab_only" ? t("policy.labAllowed") : t("policy.labDisabled")}
      </li>
    </ul>
  );
}
