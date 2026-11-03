// State vocabulary: every status has a tone (colour) and a form (line
// style), so state never depends on colour alone.

import type { Key } from "../i18n";
import type { Decision, Experiment, HypothesisStatus, Pattern, RunStatus, Target } from "../api/types";

export type Tone = "muted" | "ok" | "warn" | "bad" | "info" | "todo";
export type Form = "solid" | "waiting" | "live" | "stopped";
export interface StateLook {
  tone: Tone;
  form: Form;
  label: Key;
}

const run: Record<RunStatus, [Tone, Form]> = {
  queued: ["muted", "waiting"],
  collecting: ["info", "live"],
  indexing: ["info", "live"],
  surface_mapping: ["info", "live"],
  hypothesis_generation: ["info", "live"],
  static_validation: ["info", "live"],
  review_required: ["todo", "waiting"],
  validation_approved: ["info", "waiting"],
  lab_validation: ["info", "live"],
  report_ready: ["info", "waiting"],
  paused: ["warn", "waiting"],
  completed: ["ok", "solid"],
  cancelled: ["muted", "stopped"],
  failed: ["bad", "stopped"],
  timed_out: ["warn", "stopped"],
};

const hypothesis: Record<HypothesisStatus, [Tone, Form]> = {
  draft: ["muted", "waiting"],
  triage: ["todo", "waiting"],
  accepted: ["info", "solid"],
  rejected: ["muted", "stopped"],
  validation_pending: ["warn", "waiting"],
  validated: ["ok", "solid"],
  not_reproducible: ["muted", "stopped"],
  duplicate: ["muted", "stopped"],
};

const decision: Record<Decision, [Tone, Form]> = {
  pending: ["warn", "waiting"],
  approved: ["ok", "solid"],
  denied: ["bad", "stopped"],
  expired: ["muted", "stopped"],
  revoked: ["muted", "stopped"],
};

const experiment: Record<Experiment["status"], [Tone, Form]> = {
  queued: ["muted", "waiting"],
  running: ["info", "live"],
  passed: ["ok", "solid"],
  finding: ["bad", "solid"],
  failed: ["bad", "stopped"],
  resource_limited: ["warn", "stopped"],
  cancelled: ["muted", "stopped"],
};

const target: Record<Target["status"], [Tone, Form]> = {
  pending: ["muted", "waiting"],
  ready: ["ok", "solid"],
  unavailable: ["bad", "stopped"],
  quarantined: ["warn", "stopped"],
};

const pattern: Record<Pattern["validation_status"], [Tone, Form]> = {
  unreviewed: ["warn", "waiting"],
  reviewed: ["info", "solid"],
  validated: ["ok", "solid"],
  rejected: ["muted", "stopped"],
};

const look = (kind: string, s: string, m: Record<string, [Tone, Form]>): StateLook => {
  const [tone, form] = m[s] ?? ["muted", "solid"];
  return { tone, form, label: `status.${kind}.${s}` as Key };
};

export const runLook = (s: RunStatus) => look("run", s, run);
export const hypothesisLook = (s: HypothesisStatus) => look("hypothesis", s, hypothesis);
export const decisionLook = (s: Decision) => look("approval", s, decision);
export const experimentLook = (s: Experiment["status"]) => look("experiment", s, experiment);
export const targetLook = (s: Target["status"]) => look("target", s, target);
export const patternLook = (s: Pattern["validation_status"]) => look("pattern", s, pattern);

export const terminalRun = (s: RunStatus) => s === "completed" || s === "cancelled" || s === "failed" || s === "timed_out";
export const automaticRun = (s: RunStatus) =>
  ["collecting", "indexing", "surface_mapping", "hypothesis_generation", "static_validation", "lab_validation"].includes(s);
export const pausableRun = (s: RunStatus) => automaticRun(s) || s === "queued" || s === "validation_approved" || s === "report_ready";
