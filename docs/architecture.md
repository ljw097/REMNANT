# REMNANT architecture

## Design goals

1. Turn historical vulnerability reports and fixes into reusable root-cause patterns.
2. Map those patterns onto source structures in pinned snapshots.
3. Produce hypotheses with source locations, call-path evidence, falsifiable preconditions and explicit uncertainty.
4. Validate hypotheses through static analysis and, only after a second person approves, bounded local-lab tests.
5. Feed *confirmed* root-cause patterns back into the knowledge base.
6. Preserve provenance: every hypothesis and report statement points to evidence records.

## Non-goals

- Arbitrary public-host scanning or service fingerprinting.
- Remote exploit execution, reverse shells, persistence, credential harvesting or data exfiltration.
- Autonomous changes to production repositories.
- Treating a model's confidence as proof.

## Components

```text
clients (web UI, curl, scripts)
  └── remnantd  ── /api/v1 (bearer tokens) + SSE
        ├── api        handlers, input validation, role checks
        ├── auth       users · roles · token digests
        ├── policy     project policy · run scope hash · lab scope hash
        ├── research   state machine · triage rules · limits
        ├── source     repo allowlist · snapshot keys · tree digests
        ├── store      PostgreSQL schema "remnant" (embedded migrations)
        ├── secrets    AES-256-GCM sealing of model keys
        ├── llm        model client: profile → schema-constrained JSON
        ├── orchestrator  claims runs by lease and drives them through the state machine
        └── worker runtime  [planned; the orchestrator already speaks its protocol]
              ├── source indexer (read-only)
              ├── static analyzers (read-only)
              └── local lab runner (approval required; network none)
```

remnantd has no runtime dependency on any other service besides PostgreSQL and, when a run calls a model, the configured model provider.

## Identity and roles

| Role | May |
|---|---|
| `researcher` | create projects, targets, patterns, runs; request lab approvals; cancel runs |
| `reviewer` | triage hypotheses; review patterns; decide lab approvals |
| `admin` | everything, plus model profiles |

Separation of duty is enforced in the database, not only the API:

- `approvals.decided_by <> approvals.requested_by`
- `patterns.reviewed_by <> patterns.created_by`

## Revisions and scope hashes

A target's `revision` is a Git commit SHA or `sha256:<tree digest>` for a local snapshot. The digest covers every regular file's relative path, executable bit and content, plus symlink text (symlinks are never followed); `.git/` is excluded.

When a run is created, remnantd pins each target's current revision in `run_targets` and stores

```text
run.scope_hash = SHA-256(policy, sorted (target, revision), sorted pattern ids, limits)
```

A lab approval stores the exact scope a human sees and its hash:

```text
approval.scope = {action_class, run_scope_hash, hypothesis_id, target_id, revision, network: none, limits}
approval.scope_hash = SHA-256(approval.scope)
```

When the approval is decided, remnantd recomputes the scope from current state. If the target revision moved, the project stopped allowing dynamic testing, or the approval expired (24 h), the approval becomes `expired` and the decision is refused with 409.

## Run state machine

```text
queued → collecting → indexing → surface_mapping → hypothesis_generation → static_validation → review_required
review_required → validation_approved → lab_validation → (review_required | report_ready)
review_required → report_ready → completed
any non-terminal → cancelled | failed | timed_out
```

`research.CanTransition` is the single definition of allowed moves; `store.TransitionRun` applies it with a compare-and-set on the current status and appends a `run.status` event in the same transaction.

Rules:

- A source-only run may progress automatically through collection, indexing, mapping and static analysis.
- `validation_approved` is entered only by `DecideApproval` with an approval for that run.
- A worker timeout or resource-limit breach terminates the process and records the experiment as `resource_limited`.
- `validated` requires independent evidence for the root cause and the expected security consequence. A crash alone is not proof of exploitability.

## Research loop

1. **Collect:** record advisory/CVE metadata and patch references with source URI and content hash.
2. **Distill:** extract root cause, preconditions, lifecycle phase, affected invariant, CWE (when justified) and patch semantics into a pattern.
3. **Index:** build a symbol/call/reference index of the pinned snapshot.
4. **Map surface:** identify input boundaries, parsers, allocators, state transitions, error and cleanup paths, concurrency boundaries.
5. **Hypothesise:** connect a pattern to concrete symbols with an observable, falsifiable claim.
6. **Challenge:** a Skeptic agent looks for guards and invariants that refute the claim.
7. **Static validate:** deterministic rules; model-written rules stay separate from trusted built-in rules.
8. **Approve:** a reviewer other than the requester approves the exact lab scope.
9. **Dynamic validate:** run the harness in a local, network-less sandbox; store raw output as an artifact.
10. **Promote:** a reviewer marks a pattern `validated` only once a validated hypothesis derives from it.

## Data model (`remnant` schema)

| Table | Purpose |
|---|---|
| `users`, `api_tokens` | identity; tokens stored as SHA-256 digests |
| `llm_profiles` | model provider settings; API key sealed with the master key |
| `projects` | policy boundary and default model profile |
| `targets` | source snapshots with immutable revision |
| `patterns` | historical, researcher-authored or validated root-cause patterns |
| `runs`, `run_targets`, `run_patterns` | one research attempt and its pinned scope |
| `agent_profiles` | per-role agent configuration |
| `hypotheses`, `evidence` | falsifiable claims and their supporting/counter evidence |
| `approvals` | human approval bound to a scope hash |
| `experiments`, `artifacts` | analyzer and lab executions, content-addressed outputs |
| `events` | append-only run timeline (SSE source) |
| `schema_migrations` | applied migrations with file digests |

Migrations are embedded in the binary and applied in order under an advisory lock. Editing an applied migration is detected by digest and stops startup; schema changes always go in a new file.

## Ranking policy

Do not ask the model for a single opaque probability. Rank hypotheses by explainable dimensions:

- Reachability: can a relevant input reach the code path?
- Preconditions: are the failure conditions satisfiable?
- Invariant violation: is a concrete state or memory invariant broken?
- Consequence: is there an observable security impact?
- Evidence quality: direct source evidence, deterministic analyzer output, reproducible test.
- Counter-evidence: guards, cleanup paths, bounds checks, unreachable conditions.

`low` / `medium` / `high` are triage priorities, not proof. `validated` is a separate evidence-based status.

## Orchestrator

`internal/orchestrator` runs inside `remnantd serve` (disable with `REMNANT_ORCHESTRATOR=off`).

### Claiming and liveness

- Every poll (2 s) it claims the oldest run in `queued`, `validation_approved` or `report_ready` whose lease is free, with `FOR UPDATE SKIP LOCKED`, so several instances can share one database.
- A claim is a 30 s lease (`runs.lease_owner`, `lease_expires_at`). A heartbeat renews it every 10 s and reads the run's status; a `cancelled` status or a lost lease stops the work within one heartbeat.
- A run left in an automatic stage with an expired lease (its instance died) is failed by the reaper with `orchestrator lease expired`. Stages are not resumed.
- On shutdown, runs in progress are failed with `interrupted: remnantd shut down …`.
- A source run that exceeds `limits.wall_seconds` ends `timed_out`.

### Source pipeline (`queued` → `review_required`)

| Stage | What happens | Model |
|---|---|---|
| `collecting` | Re-hash each snapshot; the run fails if it no longer matches the pinned revision. | — |
| `indexing` | Walk each snapshot (regular files of known languages, ≤ 1 MiB, ≤ 20 000 files, no symlinks, no `.git`), split files into symbol regions with a language-aware heuristic, store the symbol index as a `source_index` artifact. | — |
| `surface_mapping` | Score every region against every pattern: +3 per matched `code_signatures` entry, −2 per `counter_signatures` entry (identifiers match on word boundaries). Keep the best 3 per pattern and target, at most 24 per run, and record each as `source_location` evidence with a line-numbered excerpt and its SHA-256. | — |
| `hypothesis_generation` | For each candidate, `limits.max_workers` at a time: the **Researcher** decides whether the region carries the pattern's root cause and drafts a hypothesis citing evidence IDs; the **Skeptic** then tries to refute it. | yes |
| `static_validation` | Each configured analyzer operation (default `static.semgrep`) per target through the worker protocol. Skipped with an event when no worker runtime is configured. | — |

Model answers are constrained by JSON Schema and then validated again: every cited evidence ID must be one the agent was given, every Skeptic line range must lie inside the cited excerpt, and text fields have length limits. Invalid answers and refusals skip the candidate with an event; the run fails only if every model call errored (for example, a wrong API key). `limits.max_tokens` is the output-token budget (thinking included) across all calls; each call reserves up to 16 000 and returns what it did not use, and generation stops when fewer than 2 048 remain. Token usage is recorded on the run.

The Skeptic's verdict sets the hypothesis's triage priority: `refutes` forces `low`, `weakens` can only lower it. Every hypothesis then waits in `triage` for a human.

### Lab phase (`validation_approved` → `lab_validation` → `review_required`)

For every approved approval of the run that has no experiment yet, the orchestrator recomputes the approval's scope hash; if anything moved, the approval is `revoked`. Otherwise it sends the approved `lab.*` operation with the approved limits to the worker, records the experiment (one per approval, enforced by a unique index) and its observations as `runtime_observation` evidence. Without a worker runtime the experiment is recorded as `failed: not run` and the hypothesis returns to `accepted`.

A reviewer then records `validated` (requires an experiment with status `finding`) or `not_reproducible`.

### Report (`report_ready` → `completed`)

`POST /runs/{id}/finalize` (no pending approvals allowed) moves a reviewed run to `report_ready`. The orchestrator renders a JSON and a Markdown report from the database, stores both content-addressed under `$REMNANT_DATA_DIR/artifacts`, and completes the run. Model-written text is flattened to single lines in Markdown so it cannot inject headings or fences.
