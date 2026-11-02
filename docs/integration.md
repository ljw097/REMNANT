# ARTEX integration notes

## Verified baseline

The current public `jiwoochris/artex-global` README describes a Go single-binary backend, embedded Next.js frontend, PostgreSQL, JWT-authenticated REST API, SSE event stream, a task engine with one planner loop and N worker goroutines, and a dual-graph design: a global asset graph plus a per-task exploration graph. The repository schema includes `tasks`, `llm_profiles`, `agents`, `tools`, and activity/usage tables. The README documents the model settings page and configurable worker concurrency.

## Integration decision

Add REMNANT as a **separate research subsystem**, not as a new mode of the generic autonomous penetration-testing worker.

- Reuse `llm_profiles` through `research_projects.llm_profile_id` and optional `research_agent_profiles.llm_profile_id`.
- Reuse the existing task record only as a visible parent/trace link (`research_runs.task_id`); do not send REMNANT work through generic offensive tool permissions.
- Reuse the existing auth middleware and SSE infrastructure, but namespace routes and event names under `/api/research`.
- Keep source-code research entities in their own namespaced tables. Do not overload existing asset graph nodes with file paths, AST symbols, or code evidence.
- Optionally anchor a research hypothesis to an existing exploration node only when a task is explicitly created for a controlled lab; this is a later integration, not an MVP dependency.

## Expected source locations

The exact tree may move between commits. In the checked public layout, inspect these before editing:

- `db/schema.sql` — additive schema location and startup execution.
- `server/` — HTTP handlers, middleware and manager lifecycle.
- `engine/` — task scheduling, cancellation and worker lifecycle.
- `agent/` — planner/worker/tool registration and prompts.
- `web/` — Next.js application and UI components.

Do not assume file names or function signatures from this document. Bind to the actual checked-out revision and add compile-time tests around the integration seam.

## API-key handling

The public schema already has `llm_profiles` with provider/model/base URL/proxy and `api_key`/`api_key_hint` columns. REMNANT should select an existing profile by ID and never return the key through research endpoints. Before production use, inspect the current read/write path to establish whether the key is encrypted at rest. A `TEXT` column declaration alone does not prove encryption. If it is not encrypted, add encryption at rest using an instance secret supplied outside the database, rotate existing keys, and return only `has_api_key` plus a masked hint to the UI.
