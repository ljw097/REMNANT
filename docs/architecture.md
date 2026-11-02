# REMNANT architecture

## Design goals

1. Turn historical vulnerability reports and fixes into reusable root-cause patterns.
2. Map those patterns to source-code structures in selected repositories.
3. Produce hypotheses with source locations, call-path evidence, falsifiable preconditions and explicit uncertainty.
4. Validate hypotheses through static analysis and, only after approval, bounded local-lab tests.
5. Feed *confirmed root-cause patterns* back into the knowledge base for a new research cycle.
6. Preserve provenance: every hypothesis and report statement must point to evidence records.

## Non-goals for MVP

- Arbitrary public-host scanning or service fingerprinting.
- Remote exploit execution, reverse shells, persistence, credential harvesting or data exfiltration.
- Autonomous changes to production repositories.
- Treating an LLM confidence score as proof.

## Logical components

```text
Next.js UI
  └── /api/research/* (existing Go HTTP server + JWT + SSE)
        ├── Research API / policy gate
        ├── Research Manager
        │     ├── Run state machine
        │     ├── Bounded planner
        │     └── Worker pool
        ├── Existing LLM profile adapter (profile ID only)
        ├── Evidence/Pattern store (PostgreSQL)
        └── Worker supervisor
              ├── source indexer (read-only)
              ├── static analyzers (read-only)
              └── local lab runner (explicit approval; network=none)
```

## State machine

`queued → collecting → indexing → surface_mapping → hypothesis_generation → static_validation → review_required`

Optional local validation path:

`review_required → validation_approved → lab_validation → report_ready`

Terminal states: `completed`, `cancelled`, `failed`, `timed_out`.

Rules:

- A source-only run may progress automatically through collection, indexing, mapping and static analysis.
- A dynamic experiment cannot start unless an approval row exists for the exact run, target revision, experiment class and policy hash.
- Any target revision change invalidates an earlier approval.
- A worker timeout or resource-limit breach terminates the process and marks the experiment `resource_limited`.
- A finding may be labelled `validated` only when independent evidence supports the root cause and the expected security consequence. A crash alone is not proof of exploitability.

## Research loop

1. **Collect:** import advisory/CVE metadata and patch references; save source URI, retrieval time, version and content hash.
2. **Distill:** extract root cause, failure preconditions, lifecycle phase, affected invariant, CWE (when justified), and patch semantics.
3. **Index:** pin target to immutable Git commit; build language-aware symbol/call/reference index.
4. **Map surface:** identify input boundaries, parsers, allocators, state transitions, error paths, cleanup paths and concurrency boundaries.
5. **Generate hypotheses:** connect a pattern to concrete symbols and state an observable, falsifiable claim.
6. **Challenge:** a Skeptic agent looks for code paths or invariants that refute the claim.
7. **Static validate:** run deterministic rules and source-level checks. Keep model-generated rules separate from trusted built-in rules.
8. **Approve lab test:** require a human to approve exact commit + test harness + resource policy.
9. **Dynamic validate:** run a constrained test harness in a local no-network sandbox; store raw output as an artifact.
10. **Promote pattern:** only after human review and adequate evidence, create a new versioned pattern linked to the validated hypothesis.

## Data model boundaries

- `research_projects`: user-facing project and policy boundary.
- `research_targets`: immutable repository snapshot / local lab fixture.
- `research_patterns`: reusable historical or validated root-cause patterns.
- `research_runs`: one complete research attempt; may link to an ARTEX task for UI traceability.
- `research_hypotheses`: falsifiable candidate claims.
- `research_evidence`: source locations, call-path facts, analyzer findings and references.
- `research_experiments`: individual deterministic analyzer or lab executions.
- `research_approvals`: human approval bound to a scope hash.
- `research_artifacts`: immutable output manifest and SHA-256.
- `research_events`: append-only UI timeline.

## Ranking policy

Do not ask the model for a single opaque probability. Rank hypotheses by explainable dimensions:

- Reachability: can a relevant input reach the code path?
- Preconditions: are the failure conditions satisfiable?
- Invariant violation: is a concrete state or memory invariant broken?
- Consequence: is there an observable security impact?
- Evidence quality: direct source evidence, deterministic analyzer output, reproducible test.
- Counter-evidence: known guards, cleanup paths, bounds checks, unreachable conditions.

UI labels: `low`, `medium`, `high`; these are triage priorities, not proof. `validated` is a separate evidence-based status.
