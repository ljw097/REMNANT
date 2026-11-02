# REMNANT for ARTEX — implementation specification

Version: 0.1.0  
Status: implementation-ready design package; not yet applied to the upstream repository.

## Scope

REMNANT is a source-first vulnerability-research subsystem for ARTEX. It correlates historical vulnerability root causes with source-code structures, creates evidence-backed hypotheses, runs static analysis, and can request bounded dynamic validation inside an isolated local lab.

The initial release intentionally does **not** provide arbitrary-target scanning, remote exploitation, shell access, credential collection, or data-exfiltration workflows. Targets are source repositories or local intentionally vulnerable fixtures owned by the operator or explicitly authorized for review. Dynamic validation is restricted to a local sandbox with network disabled.

## Repository assumptions verified against `jiwoochris/artex-global` (2026-11-02)

- Backend: Go `net/http`, single binary, JWT auth and SSE.
- Frontend: Next.js, statically built and embedded in the Go binary.
- Database: PostgreSQL via `pgx`; `db/schema.sql` is idempotently executed at startup.
- Existing architecture: task engine, planner/workers, `tasks`, `llm_profiles`, tool/agent configuration, asset graph and per-task exploration graph.
- Existing LLM profile schema includes provider format, endpoint, model, API key and key hint fields. REMNANT references a profile ID; it must not introduce a second independent API-key store.

See `docs/integration.md` for integration points and caveats.

## Files

- `db/001_remnant.sql` — PostgreSQL additive schema.
- `api/openapi.yaml` — REST and SSE contract.
- `docs/architecture.md` — component and lifecycle design.
- `docs/ui.md` — concrete Next.js page and control specification.
- `docs/agent-contracts.md` — agent roles, inputs, outputs and acceptance rules.
- `worker/protocol.md` — versioned JSONL worker protocol and isolation contract.
- `internal/research/contracts.go` — Go interfaces and typed DTO skeleton.
- `examples/` — example run request and worker messages.

## Apply order

1. Back up PostgreSQL.
2. Add the SQL block from `db/001_remnant.sql` to the end of ARTEX's `db/schema.sql` (the repository currently re-runs that schema idempotently at startup), or wire it into the same startup migration loader.
3. Add the Go package under `internal/research/` and register the API routes behind ARTEX's existing JWT middleware.
4. Add the dedicated worker runner. Do not expose the existing generic pentest tool set to REMNANT agents.
5. Add the Next.js pages under the existing application route conventions.
6. Build and test before enabling the feature on a network-accessible instance.

This package is a specification and schema starter, not a claim that the GitHub repository has been modified or that the code compiles against the current private/local checkout. Route registration and store construction must be adapted to the exact checked-out commit.
