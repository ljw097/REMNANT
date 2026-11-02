# REMNANT web UI specification

The UI is a separate static single-page application that talks only to `remnantd`'s `/api/v1`. It can be served by any static host or reverse proxy alongside remnantd; it has no server-side component and no credentials of its own.

## Sign-in

The user pastes a bearer token issued by `remnantd create-user` / `create-token`. The UI calls `GET /me`, shows the username and role, and keeps the token in memory (optionally `sessionStorage`). Actions the role cannot perform are hidden; the server enforces them regardless.

Run activity uses `GET /runs/{id}/events` read with `fetch()` and a streaming body reader, so the token stays in the `Authorization` header. On reconnect, send the last seen event ID as `Last-Event-ID`.

## Pages

### 1. Dashboard — `/`

Cards: active runs, targets, hypotheses awaiting triage, validated findings, pending approvals, recent activity. Every number comes from the API; no placeholder counters.

### 2. Projects — `/projects`

Columns: name, target count, active runs, model profile, updated, status. The project drawer shows description, model profile selector and the policy:

- Source access: read-only (fixed).
- Worker network: none (fixed).
- Dynamic testing: `disabled` (default) or `local_lab_only`. Switching to `local_lab_only` shows a confirmation explaining that it allows approved local experiments.

### 3. Targets — `/projects/{id}/targets`

Form fields: kind, display name, snapshot key, primary language; for `git_repository`, the https URL and ref. The form never accepts a filesystem path. Show revision, tree digest and status; a `pending` Git target explains that it awaits pinning.

### 4. Knowledge base — `/patterns`

Search by text, vulnerability class and language. Detail: source reference, root cause, preconditions, lifecycle points, code signatures, counter-signatures, patch semantics, version, review status, author and reviewer. Reviewers get Review / Validate / Reject; Validate is disabled until a validated hypothesis derives from the pattern, and hidden for the pattern's author.

### 5. New run — `/runs/new`

Fields: project, 1–10 ready targets, patterns, model profile (defaults to the project's), workers (1–4, default 2), wall time (60–3600 s, default 900), output-token budget (4 000–2 000 000, default 200 000). The confirmation step lists every target with its revision and states the read-only/no-network policy.

### 6. Run detail — `/runs/{id}`

Tabs: Overview (status, pinned targets and revisions, scope hash, hypothesis counts, token usage), Hypotheses, Evidence, Experiments (`GET /runs/{id}/experiments`), Activity (live SSE), Artifacts (download through `GET /runs/{id}/artifacts/{artifactId}`). A run can never change its targets or policy after creation.

When the run is `review_required`, show **Finalize report**; it is disabled while lab approvals are pending. While a run is waiting or in an automatic stage, show **Pause**; a paused run shows **Resume** and its resume stage. Show `attempts` when a run was recovered after a crash.

### 7. Hypothesis — `/hypotheses/{id}`

Show claim, class/CWE, affected symbols, preconditions, expected observation, evidence with file/line and revision, counter-evidence and triage confidence. Show the Skeptic's verdict next to the triage priority, and facts and unknowns as separate lists. Reviewer actions: Accept, Reject, Duplicate, with a note; on a `validation_pending` hypothesis with a finished lab experiment, Validated or Not reproducible. Researcher action on an accepted hypothesis: Request lab validation, which opens the approval request form (lab operation, limits, summary); it never starts anything.

### 8. Approvals — `/approvals`

List pending approvals. The drawer shows the full `scope` object (target, revision, run scope hash, limits, network `none`), the summary, requester and expiry. Reviewers who are not the requester can Approve or Deny with a note. A 409 response means the approval went stale; show why and offer to request a new one.

### 9. Findings and reports — `/findings`

Separate candidate hypotheses from validated findings. Exports (Markdown/JSON) include evidence IDs, file/line references, revisions, analyzer versions, lab commands, reproduction status, counter-evidence, limitations and remediation notes.

### 10. Model profiles — `/settings/models` (admin)

List profiles with provider, model, effort and key hint. Create form takes the API key once; it is never displayed again.
