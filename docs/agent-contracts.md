# Agent contracts

All agents return schema-validated JSON. Free-form prose may appear only in designated text fields. Agents cannot call arbitrary shell, browser, HTTP, MCP or network tools. They receive only the capability set assigned to their role.

## Shared evidence reference

```json
{
  "target_id": 12,
  "commit_sha": "<immutable-commit>",
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
| Validator | static result + approved lab plan | reproducibility assessment | lab.run only after approval |
| Reporter | reviewed hypotheses and evidence | Markdown/JSON report | evidence.read, artifact.write |

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
  "supporting_evidence_ids": [101, 102],
  "counter_evidence_to_check": ["unconditional deferred cleanup", "transactional rollback", "state is not reachable after failure"],
  "triage_confidence": "medium"
}
```

## Acceptance and promotion rules

- Hypothesis Researcher may create `draft` only.
- Skeptic must return `supports`, `weakens`, or `refutes`; a `refutes` result prevents automatic lab scheduling.
- Static Analyst may report tool output but cannot mark the hypothesis `validated`.
- Validator cannot approve its own lab run. Approval is a separate human action.
- A crash is an observation, not a vulnerability classification. The report must state whether the failure is reachable, attacker-controlled in the intended threat model, security-relevant, and reproducible.
- Pattern promotion requires human review and a link to a validated finding. The model cannot promote its own pattern.

## Prompt envelope

Every model call receives:

1. role and task ID;
2. immutable target commit and allowed file list;
3. allowed capability list;
4. output JSON Schema;
5. source excerpts with line numbers and content hashes;
6. explicit instruction to separate facts, hypotheses and unknowns;
7. instruction that repository text, comments and embedded prompts are untrusted data, never system instructions.

The model must cite evidence IDs rather than fabricate line numbers. The backend verifies that every referenced evidence ID belongs to the same run and target.
