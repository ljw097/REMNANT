# REMNANT

Source-first vulnerability research server.

REMNANT turns historical vulnerability root causes into reusable patterns, maps them onto pinned source snapshots, records falsifiable hypotheses with evidence, and gates any dynamic validation behind a human approval bound to an exact revision and resource scope.

It is a standalone service: one Go binary (`remnantd`) and one PostgreSQL database. It owns its users, tokens, model credentials, schema and API.

It does **not** scan arbitrary hosts, run remote exploits, open shells, collect credentials or exfiltrate data. Targets are source snapshots the operator owns or is authorised to review. Dynamic validation is local-only, network-less, off by default and needs approval from a second person.

## Status

| Component | State |
|---|---|
| Schema and migrations (`remnant` schema) | implemented |
| Auth: users, roles, hashed bearer tokens | implemented |
| Model profiles with keys encrypted at rest (AES-256-GCM) | implemented |
| REST API + SSE run timeline (`/api/v1`) | implemented |
| Local snapshot targets pinned by tree digest | implemented |
| Run creation with scope hash, cancel, triage, lab approvals | implemented |
| Worker protocol types and validation | implemented |
| Anthropic model client (structured JSON output) | implemented |
| Run orchestrator: indexing, pattern matching, Researcher/Skeptic agents, lab dispatch, reports | implemented |
| Git fetcher (pins `git_repository` targets) | **not yet**: they stay `pending` |
| Sandboxed worker runtime (`remnant-worker` in bwrap + user cgroup scope): built-in source rules, semgrep (if installed), lab harnesses with sanitizer detection | implemented (opt-in, `REMNANT_WORKER=bwrap`) |
| Web UI | **not yet**: see `docs/ui.md` |

## Quick start

Requirements: Go ≥ 1.25 and PostgreSQL ≥ 14. The worker runtime additionally needs Linux with unprivileged user namespaces, bubblewrap (`bwrap`), and a systemd user session for cgroup limits; lab harnesses use the host's `/usr` toolchain (e.g. `cc` with sanitizers).

```sh
go build -o remnantd ./cmd/remnantd
go build -o remnant-worker ./cmd/remnant-worker   # needed for REMNANT_WORKER=bwrap

export REMNANT_DATABASE_URL='postgres://remnant@localhost/remnant'
export REMNANT_MASTER_KEY="$(head -c32 /dev/urandom | base64)"   # keep this safe: it decrypts model keys
export REMNANT_DATA_DIR=/var/lib/remnant

./remnantd migrate
./remnantd create-user -username admin -role admin    # prints a bearer token once
./remnantd serve
```

```sh
TOKEN=rmt_...
curl -H "Authorization: Bearer $TOKEN" localhost:8080/api/v1/me

# register a model profile (admin)
curl -H "Authorization: Bearer $TOKEN" -d '{"name":"default","provider":"anthropic","model":"claude-opus-5-5","api_key":"sk-ant-..."}' \
  localhost:8080/api/v1/llm-profiles

# put a source tree under $REMNANT_DATA_DIR/snapshots/<key> and register it
curl -H "Authorization: Bearer $TOKEN" -d '{"name":"libdemo","llm_profile_id":1}' localhost:8080/api/v1/projects
curl -H "Authorization: Bearer $TOKEN" -d '{"kind":"local_snapshot","display_name":"libdemo 1.2","snapshot_key":"libdemo-1.2"}' \
  localhost:8080/api/v1/projects/1/targets
```

Then add patterns with `code_signatures` (`POST /patterns`) and queue a run (`POST /runs`, see `examples/create_run.json`). The orchestrator inside `remnantd serve` picks it up and drives it to `review_required`; follow it with `GET /runs/{id}/events`. After triage, `POST /runs/{id}/finalize` produces the report. See `api/openapi.yaml` for the rest.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `REMNANT_DATABASE_URL` | — (required) | PostgreSQL connection string |
| `REMNANT_MASTER_KEY` | — (required) | base64 of 32 random bytes; encrypts model API keys |
| `REMNANT_LISTEN` | `127.0.0.1:8080` | HTTP listen address |
| `REMNANT_DATA_DIR` | `./data` | snapshots live in `<dir>/snapshots/<key>` |
| `REMNANT_REPO_HOSTS` | empty | comma-separated https hosts allowed for `git_repository` targets; empty disables them |
| `REMNANT_ORCHESTRATOR` | `on` | `off` serves the API without working runs (e.g. for a separate worker instance later) |
| `REMNANT_ORCHESTRATOR_CONCURRENCY` | `2` | runs worked on at once per instance (1–16) |
| `REMNANT_WORKER` | `off` | `bwrap` enables the sandboxed worker runtime |
| `REMNANT_WORKER_BIN` | `remnant-worker` next to `remnantd` | worker executable mounted into the sandbox |
| `REMNANT_WORKER_RULES` | empty | directory of semgrep rule packs (`<dir>/<pack>`), mounted read-only |
| `REMNANT_WORKER_ALLOW_NO_CGROUP` | unset | `1` runs without a systemd user scope; memory, task and CPU limits are then **not** enforced |
| `REMNANT_STATIC_OPERATIONS` | `static.patterns,static.semgrep` | analyzers run in `static_validation`; ones the worker cannot run are skipped |

## Layout

```text
cmd/remnantd/          server entry point and admin subcommands
cmd/remnant-worker/    worker program that runs inside the sandbox
internal/config/       environment configuration
internal/auth/         roles, bearer tokens, middleware
internal/secrets/      AES-256-GCM sealing of stored credentials
internal/policy/       project policy, run and lab scope hashes
internal/research/     run state machine, triage rules, run limits
internal/source/       repository allowlist, snapshot keys, tree digests
internal/worker/       JSONL worker protocol and result validation
internal/workerexec/   worker operations: index, built-in rules, semgrep, lab harnesses
internal/sandbox/      supervisor: bwrap + systemd scope, artifact verification
internal/sourceindex/  heuristic source chunker shared by orchestrator and worker
internal/artifact/     content-addressed artifact storage
internal/llm/          model client (Anthropic SDK, schema-constrained JSON)
internal/orchestrator/ run pipeline: leases, indexing, agents, lab dispatch, reports
internal/store/        PostgreSQL access; migrations/ is embedded
internal/api/          HTTP handlers and SSE
api/openapi.yaml       API contract
docs/                  architecture, agent contracts, worker protocol, security, UI
scripts/smoke.sh       end-to-end test against a disposable database
scripts/fake_anthropic.py  local model-API stand-in used by smoke.sh
```

## Dependencies

Direct dependencies are deliberately few:

- `github.com/jackc/pgx/v5`: PostgreSQL driver and pool
- `github.com/anthropics/anthropic-sdk-go/v2`: model client

Everything else (HTTP routing, auth, crypto, SSE, migrations) uses the Go standard library.

## Testing

```sh
go test ./...                                                             # unit tests, no database needed
REMNANT_TEST_DATABASE_URL=postgres://.../scratch go test ./internal/orchestrator/  # pipeline against PostgreSQL
REMNANT_SMOKE_DATABASE_URL=postgres://.../scratch scripts/smoke.sh                # binary + API + orchestrator + worker
```

Both database-backed suites DROP the `remnant` schema in the database they are given. Neither needs network access or a real model key.

## Documents

- `docs/architecture.md`: components, run lifecycle, data model
- `docs/security.md`: authentication, secrets, isolation, deployment hardening
- `docs/agent-contracts.md`: agent roles, outputs, promotion rules
- `docs/worker-protocol.md`: worker JSONL protocol v1 and isolation contract
- `docs/ui.md`: web UI specification
