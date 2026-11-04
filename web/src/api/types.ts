// Types mirror api/openapi.yaml. Only fields the UI reads are listed.

export type Role = "admin" | "researcher" | "reviewer";

export interface Principal {
  user_id: number;
  username: string;
  role: Role;
  /** Signed in with a temporary password: only changing it is allowed. */
  must_change_password: boolean;
}

export type Profile = "personal" | "team" | "managed" | "custom";

/** How the instance is used; see store.Policy. */
export interface InstancePolicy {
  profile: Profile;
  self_approval: boolean;
  self_review: boolean;
  admin_lab_approval: boolean;
  account_requests: boolean;
  session_hours: number;
}

export type Presets = Record<Exclude<Profile, "custom">, InstancePolicy>;

export interface AuthStatus {
  /** Who the session cookie belongs to; null when nobody is signed in. */
  user: Principal | null;
  setup_required: boolean;
  account_requests: boolean;
  presets: Presets;
}

export type RunStatus =
  | "queued"
  | "collecting"
  | "indexing"
  | "surface_mapping"
  | "hypothesis_generation"
  | "static_validation"
  | "review_required"
  | "validation_approved"
  | "lab_validation"
  | "report_ready"
  | "paused"
  | "completed"
  | "cancelled"
  | "failed"
  | "timed_out";

export type HypothesisStatus =
  | "draft"
  | "triage"
  | "accepted"
  | "rejected"
  | "validation_pending"
  | "validated"
  | "not_reproducible"
  | "duplicate";

export type Confidence = "low" | "medium" | "high";

export interface Policy {
  source_mode: "read_only";
  dynamic_mode: "disabled" | "local_lab_only";
  network: "none";
}

export interface Limits {
  max_workers: number;
  wall_seconds: number;
  max_tokens: number;
}

export interface Project {
  id: number;
  name: string;
  description: string;
  status: "active" | "paused" | "archived";
  llm_profile_id: number | null;
  policy: Policy;
  target_count: number;
  active_runs: number;
  created_at: string;
  updated_at: string;
}

export interface Target {
  id: number;
  project_id: number;
  kind: "git_repository" | "local_snapshot" | "lab_fixture";
  display_name: string;
  source_uri: string;
  source_ref: string;
  snapshot_key: string;
  revision: string;
  tree_sha256: string;
  primary_language: string;
  status: "pending" | "ready" | "unavailable" | "quarantined";
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface Pattern {
  id: number;
  source_kind: "historical" | "validated_internal" | "researcher_authored" | "mined";
  source_ref: string;
  title: string;
  vulnerability_class: string;
  cwe_id: string;
  language: string;
  root_cause: string;
  preconditions: string[];
  lifecycle_points: string[];
  code_signatures: string[];
  counter_signatures: string[];
  version: number;
  validation_status: "unreviewed" | "reviewed" | "validated" | "rejected";
  created_by: number | null;
  reviewed_by: number | null;
  reviewed_at: string | null;
  created_at: string;
  self_reviewed: boolean;
  /** Where a mined pattern came from; {} for others. */
  origin: { repository?: string; commit?: string; committed_at?: string; subject?: string; signals?: string[] };
}

export interface Run {
  id: number;
  project_id: number;
  status: RunStatus;
  limits: Limits;
  policy: Policy;
  scope_hash: string;
  auto: boolean;
  auto_discover: boolean;
  llm_profile_id: number | null;
  started_by: number | null;
  error_summary: string;
  input_tokens: number;
  output_tokens: number;
  resume_stage: string;
  resume_status: string;
  attempts: number;
  active_ms: number;
  queued_at: string;
  started_at: string | null;
  completed_at: string | null;
}

export interface RunDetail extends Run {
  targets: { target_id: number; revision: string }[];
  pattern_ids: number[];
  hypothesis_counts: Partial<Record<HypothesisStatus, number>>;
}

export interface Hypothesis {
  id: number;
  auto_triaged?: boolean;
  run_id: number;
  target_id: number;
  pattern_id: number | null;
  title: string;
  claim: string;
  vulnerability_class: string;
  cwe_id: string;
  status: HypothesisStatus;
  triage_confidence: Confidence;
  skeptic_verdict: "" | "supports" | "weakens" | "refutes";
  facts: string[];
  preconditions: string[];
  unknowns: string[];
  affected_symbols: string[];
  expected_observation: string;
  counter_evidence: string[];
  reviewer_note: string;
  reviewed_by: number | null;
  reviewed_at: string | null;
  evidence_count: number;
  created_at: string;
  updated_at: string;
  project_id?: number;
}

export type EvidenceKind =
  | "source_location"
  | "call_path"
  | "data_flow"
  | "static_finding"
  | "build_result"
  | "runtime_observation"
  | "counter_evidence"
  | "external_reference";

export interface Evidence {
  id: number;
  hypothesis_id: number | null;
  target_id: number;
  evidence_kind: EvidenceKind;
  file_path: string;
  line_start: number | null;
  line_end: number | null;
  symbol: string;
  revision: string;
  content_sha256: string;
  summary: string;
  excerpt: string;
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface HypothesisDetail extends Hypothesis {
  evidence: Evidence[];
}

export interface LabLimits {
  wall_seconds: number;
  memory_mb: number;
  cpu_cores: number;
  max_processes: number;
}

export type Decision = "pending" | "approved" | "denied" | "expired" | "revoked";

export interface Approval {
  id: number;
  run_id: number;
  hypothesis_id: number;
  target_id: number;
  target_revision: string;
  scope: {
    action_class: string;
    operation: string;
    run_scope_hash: string;
    hypothesis_id: number;
    target_id: number;
    revision: string;
    network: string;
    limits: LabLimits;
  };
  scope_hash: string;
  request_summary: string;
  decision: Decision;
  requested_by: number;
  decided_by: number | null;
  reviewer_note: string;
  decided_at: string | null;
  expires_at: string;
  created_at: string;
  self_approved: boolean;
  project_id?: number;
  hypothesis_title?: string;
  requested_by_name?: string;
}

export interface Experiment {
  id: number;
  run_id: number;
  hypothesis_id: number | null;
  approval_id: number | null;
  target_id: number | null;
  operation: string;
  request_id: string;
  status: "queued" | "running" | "passed" | "finding" | "failed" | "resource_limited" | "cancelled";
  workspace_revision: string;
  result_summary: string;
  exit_code: number | null;
  started_at: string | null;
  completed_at: string | null;
  created_at: string;
}

export interface Artifact {
  id: number;
  experiment_id: number | null;
  artifact_kind: string;
  name: string;
  sha256: string;
  size_bytes: number;
  media_type: string;
  classification: string;
  created_at: string;
}

export interface RunEvent {
  id: number;
  run_id: number;
  event_type: string;
  stage: string;
  message: string;
  payload: Record<string, unknown>;
  created_at: string;
}

export interface LLMProfile {
  id: number;
  name: string;
  provider: "anthropic" | "openai";
  base_url: string;
  model: string;
  effort: "low" | "medium" | "high" | "xhigh" | "max";
  has_api_key: boolean;
  api_key_hint: string;
  options: ModelOptions;
  /** Projects and agents that use the profile; it cannot be deleted while used. */
  used_by: number;
  updated_at: string;
}

/** Per-profile request settings; zero values keep the model or SDK default. */
export interface ModelOptions {
  thinking: "" | "adaptive" | "disabled";
  timeout_seconds: number;
  max_retries: number | null;
}

export interface ProfileCheck {
  ok: boolean;
  model?: string;
  error?: string;
}

export type AgentRole = "historian" | "hypothesis_researcher" | "skeptic" | "reporter";

export interface Agent {
  role: AgentRole;
  display_name: string;
  enabled: boolean;
  llm_profile_id: number | null;
  config: { instructions: string; max_call_tokens: number };
  updated_at: string;
}

export interface LabLimits {
  wall_seconds: number;
  memory_mb: number;
  cpu_cores: number;
  max_processes: number;
}

export interface ResearchSettings {
  run_defaults: { max_workers: number; wall_seconds: number; max_tokens: number };
  candidates_per_pattern: number;
  max_candidates: number;
  max_call_tokens: number;
  lab_defaults: LabLimits;
  approval_ttl_hours: number;
  disabled_static_operations: string[];
}

export interface ResearchSettingsResponse {
  settings: ResearchSettings;
  defaults: ResearchSettings;
  static_operations: string[];
}

export interface SystemConfig {
  listen: string;
  data_dir: string;
  master_key_source: "environment" | "file";
  orchestrator: boolean;
  orchestrator_concurrency: number;
  worker: "off" | "bwrap";
  worker_rules: boolean;
  static_operations: string[];
  lab_operations: string[];
  repo_hosts: string[] | null;
  advisory_feed: string;
  auto_update: "off" | "check" | "install";
  update_interval: string;
  update_repo: string;
}

export interface Summary {
  projects: number;
  targets: number;
  active_runs: number;
  paused_runs: number;
  awaiting_triage: number;
  pending_approvals: number;
  validation_pending: number;
  validated: number;
  runs_by_status: Partial<Record<RunStatus, number>>;
  generated_at: string;
}

export interface Branding {
  name: string;
  accent: string;
}

export interface UpdateState {
  current: string;
  mode: "off" | "check" | "install";
  latest?: string;
  release_url?: string;
  available: boolean;
  status: "idle" | "checking" | "installing" | "restarting" | "disabled";
  last_checked?: string;
  last_error?: string;
}

export interface VersionInfo {
  version: string;
  commit: string;
  update?: UpdateState;
}

export interface UserRef {
  id: number;
  username: string;
  role: Role;
  disabled: boolean;
  /** Requested on the sign-in page; waits for an administrator. */
  pending: boolean;
  has_password: boolean;
}

export interface IntakeOptions {
  auto: boolean;
  auto_discover: boolean;
  mine_history: boolean;
  history_depth: number;
  max_commits: number;
  max_patterns: number;
  max_tokens: number;
  include_patterns: number[];
}

export type IntakeStatus = "fetching" | "mining" | "started" | "failed";

export interface Intake {
  id: number;
  source_uri: string;
  source_ref: string;
  project_id: number;
  project_name: string;
  target_id: number;
  target_status: "pending" | "ready" | "unavailable" | "quarantined";
  revision: string;
  run_id: number | null;
  llm_profile_id: number | null;
  status: IntakeStatus;
  options: IntakeOptions;
  commits_found: number;
  commits_read: number;
  patterns: number[];
  input_tokens: number;
  output_tokens: number;
  error_summary: string;
  created_by: number | null;
  created_at: string;
  updated_at: string;
}

export interface CommitRecord {
  id: number;
  sha: string;
  committed_at: string;
  subject: string;
  body: string;
  files: string[];
  signals: string[];
  score: number;
}

export interface IntakeDetail {
  intake: Intake;
  commits: CommitRecord[];
  repo_hosts: string[];
}
