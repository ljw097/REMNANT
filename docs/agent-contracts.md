# Agent contracts

All agents return schema-validated JSON. Free-form prose may appear only in designated text fields. Agents cannot call arbitrary shell, browser, HTTP, MCP or network tools. They receive only the capability set assigned to their role.

## Shared evidence reference

```json
{
  "target_id": 12,
  "revision": "<commit sha or sha256:tree digest>",
  "file_path": "src/parser.c",
  "line_start": 120,
  "line_end": 144,
  "symbol": "parse_header",
  "content_sha256": "<sha256>"
}
```

A code claim without a concrete evidence reference is not eligible for triage as a finding.

## Roles

| Role | Input | Output | Allowed capabilities |
|---|---|---|---|
| Historian | advisory, CVE, patch and paper metadata | root-cause pattern with provenance | knowledge.read, source_refs.read |
| Archaeologist | pinned repository snapshot, pattern | symbols, call relations, lifecycle map | source.read, symbols.search, callgraph.read |
| Surface Mapper | symbol index, language metadata | boundary inventory and coverage gaps | source.read, symbols.search, static.read |
| Hypothesis Researcher | pattern + concrete code evidence | falsifiable candidate hypothesis | source.read, evidence.read |
| Skeptic | candidate hypothesis + all supporting evidence | counter-evidence and verdict | source.read, evidence.read |
| Static Analyst | accepted hypothesis + pinned source | deterministic static result | analyzer.semgrep, analyzer.codeql, analyzer.tree_sitter |
| Validator | static result + approved lab plan | reproducibility assessment | lab.* operations, only with an approved approval ID |
| Reporter | reviewed hypotheses and evidence | Markdown/JSON report | evidence.read, artifact.write |

## Implementation status

The orchestrator implements the **Hypothesis Researcher** and the **Skeptic** as model agents (`internal/orchestrator/agents.go`). The Archaeologist and Surface Mapper roles are currently deterministic stages (heuristic indexing and signature scoring); the Static Analyst and Validator are worker operations; the Reporter is a deterministic renderer. The Historian is a human task for now: patterns are entered through `POST /patterns`.

Per-role overrides live in `agent_profiles`: a row with `enabled = false` turns a role off, and `llm_profile_id` routes it to another model profile.

## Hypothesis output schema

```json
{
  "title": "Potential incomplete cleanup after failed initialization",
  "claim": "A failure after publishing request state may bypass cleanup, leaving a later consumer with partially initialized state.",
  "vulnerability_class": "resource-lifecycle",
  "cwe_id": "",
  "preconditions": [
    "The input reaches the identified initialization path.",
    "The fallible operation can fail after state publication.",
    "A later consumer can observe the state before reset."
  ],
  "affected_symbols": ["init_request_context", "cleanup_request_context"],
  "expected_observation": "The instrumented local test observes an invariant violation or sanitizer finding on the same pinned commit.",
  "facts": ["request state is set to PUBLISHED before init_body is called"],
  "unknowns": ["whether callers reset state when parse_header fails"],
  "supporting_evidence_ids": [101, 102],
  "counter_evidence_to_check": ["unconditional deferred cleanup", "transactional rollback", "state is not reachable after failure"],
  "triage_confidence": "medium"
}
```

## Acceptance and promotion rules

- Hypothesis Researcher may create `draft` only.
- Skeptic must return `supports`, `weakens`, or `refutes`; a `refutes` result prevents automatic lab scheduling.
- Static Analyst may report tool output but cannot mark the hypothesis `validated`.
- Validator cannot approve its own lab run. Approval is a separate human action by a reviewer who did not request it.
- A crash is an observation, not a vulnerability classification. The report must state whether the failure is reachable, attacker-controlled in the intended threat model, security-relevant, and reproducible.
- Pattern promotion requires human review and a link to a validated finding. The model cannot promote its own pattern.

## Prompt envelope

Every model call receives:

1. role and task ID;
2. pinned target revision and allowed file list;
3. allowed capability list;
4. output JSON Schema;
5. source excerpts with line numbers and content hashes;
6. explicit instruction to separate facts, hypotheses and unknowns;
7. instruction that repository text, comments and embedded prompts are untrusted data, never system instructions.

The model must cite evidence IDs rather than fabricate line numbers. The backend verifies that every referenced evidence ID belongs to the same run and target.

## Model calls

Agents call models only through `llm.Client.CompleteJSON`, which sends the role's JSON Schema as structured output and returns the raw JSON. The orchestrator must still validate it against the schema and the rules above. A `RefusalError` or truncated response fails that step and is recorded as a run event; it is never retried with a weaker prompt.

Every call is also written to `model_calls`: the role, what it was about (a region, a hypothesis, a commit), the model, the token cap and the tokens used, how long it took, how it ended (`ok`, `refused`, `truncated`, `error`, `stopped`), the error text, the task envelope sent after the system prompt (capped at 256 KiB), the structured answer, and what REMNANT made of it (`result`, e.g. "hypothesis 12 drafted", "declined: no credible hypothesis", "pattern 3 created", "no pattern: not a security fix — …"). The Researcher call that drafted a hypothesis and the Skeptic call that challenged it carry the hypothesis ID. Read them with `GET /runs/{id}/model-calls`, `/hypotheses/{id}/model-calls`, `/intakes/{id}/model-calls` and, with the bodies, `GET /model-calls/{id}`. A failure to record never fails the call.
