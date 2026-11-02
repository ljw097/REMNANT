# REMNANT UI specification

The UI belongs inside the existing Next.js frontend and uses the existing authentication, layout, theme and SSE client conventions. Add a top-level `RESEARCH` navigation item; do not create a separate login or separate key-management system.

## Pages

### 1. Research dashboard — `/research`

Cards: active runs, targets, hypotheses awaiting review, validated findings, worker resource use and last run activity. Every number is queried from `research_*` tables; no illustrative counters in production.

### 2. Projects — `/research/projects`

Project columns: name, target count, active runs, configured LLM profile, last activity, status. Project drawer contains description, LLM profile selector and immutable policy summary:

- Source checkout: read-only.
- Repository host allowlist: administrator-configured.
- Dynamic testing: local lab only; disabled by default.
- Network from worker: disabled.

### 3. Targets — `/research/projects/{id}/targets`

Target form supports an allow-listed Git repository URL or a server-side local snapshot key. It does not accept a raw absolute filesystem path. Before save, backend resolves the repository, pins a commit SHA, calculates a tree digest and stores the snapshot under an application-managed directory. Display the canonical URL, branch/ref, commit SHA, tree digest and source status.

### 4. Knowledge base — `/research/knowledge`

Search by CVE/advisory, CWE, vulnerability class, root-cause phrase and language. Pattern details show: original source, patch reference, failure preconditions, lifecycle phase, affected invariant, code signatures, counter-signatures, version and review status.

### 5. Run wizard — `/research/runs/new`

Only source-only analysis is available in MVP. Fields: project, one or more pinned targets, selected root-cause patterns, LLM profile, worker count (1–2 default, max 4), wall-time budget, token budget. The final page shows the exact source commits and read-only/no-network policy before submission.

### 6. Run details — `/research/runs/{id}`

Tabs: Overview, Coverage, Hypotheses, Evidence, Experiments, Activity, Artifacts. Display stage progress and append-only activity from `/runs/{id}/events` SSE. A run cannot silently expand its target set or switch to network-enabled operation.

### 7. Hypothesis review — `/research/hypotheses/{id}`

Show claim, class/CWE (if supported), affected symbols, preconditions, expected observation, supporting evidence, counter-evidence, target commit and triage confidence. Actions: accept for further analysis, reject, mark duplicate. Dynamic validation is a distinct action and opens an approval summary; it never starts immediately on click.

### 8. Lab approval — approval drawer

Display exact target + commit SHA, test harness digest, expected test behavior, CPU/memory/time ceilings, network `none`, file mounts, output paths and expiry. Require explicit decision. Any mismatch in commit or policy hash invalidates approval.

### 9. Findings and reports — `/research/findings`, `/research/reports`

Separate candidate hypotheses from validated findings. Reports include evidence IDs, file/line references, commit SHA, analyzer versions, test commands (lab-only), reproduction status, counter-evidence, limitations and remediation notes. Export Markdown and JSON; PDF can be added later using the existing report subsystem.

### 10. Models — existing LLM profile settings

REMNANT links to the existing ARTEX model profile page. The research UI sees only `id`, name, provider format, model and masked key hint/`has_api_key`. Never return API key material from `/api/research/*`. Do not duplicate provider credentials in a REMNANT-specific table.
