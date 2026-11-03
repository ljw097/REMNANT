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
| Pause/resume and recovery after shutdown or crash, without repeating recorded work | implemented |
| Git fetcher: pins `git_repository` targets to a commit (allowlisted https hosts, public addresses only) | implemented |
| Signed releases (GitHub Actions) and self-update with rollback | implemented |
| Sandboxed worker runtime (`remnant-worker` in bwrap + user cgroup scope): built-in source rules, semgrep (if installed), lab harnesses with sanitizer detection | implemented (opt-in, `REMNANT_WORKER=bwrap`) |
| Web UI | **not yet**: see `docs/ui.md` |

## Quick start

Requirements: Go ≥ 1.25, Node.js ≥ 20.19 (to build the web UI) and PostgreSQL ≥ 14. The worker runtime additionally needs Linux with unprivileged user namespaces, bubblewrap (`bwrap`), and a systemd user session for cgroup limits; lab harnesses use the host's `/usr` toolchain (e.g. `cc` with sanitizers).

```sh
(cd web && npm ci && npm run build)               # web UI, embedded into remnantd
go build -o remnantd ./cmd/remnantd
go build -o remnant-worker ./cmd/remnant-worker   # needed for REMNANT_WORKER=bwrap

export REMNANT_DATABASE_URL='postgres://remnant@localhost/remnant'
export REMNANT_DATA_DIR=/var/lib/remnant
# Optional: without it, the master key is created in $REMNANT_DATA_DIR/master.key (mode 0600).
# export REMNANT_MASTER_KEY="$(head -c32 /dev/urandom | base64)"   # keep it safe: it decrypts model keys

./remnantd migrate
./remnantd serve
```

Open http://localhost:8080. On first start the log prints a one-time setup code. The setup page asks for it, lets you pick how the instance is used — **personal** (one researcher on their own machine; approving your own requests is allowed and recorded), **team** (researchers cross-check each other) or **managed** (administrators approve lab experiments and accounts), then adjust any rule — and creates the administrator with a password. Everyone then signs in with a username and password. Without the browser: `./remnantd setup -profile personal -username me` (prints a temporary password; add `-password-stdin` to set your own). API tokens for scripts: `./remnantd create-token -username me`. See `docs/ui.md`.

Scripts use an API token:

```sh
TOKEN=$(./remnantd create-token -username me)
curl -H "Authorization: Bearer $TOKEN" localhost:8080/api/v1/me

# register a model profile (admin)
curl -H "Authorization: Bearer $TOKEN" -d '{"name":"default","provider":"anthropic","model":"claude-opus-5-5","api_key":"sk-ant-..."}' \
  localhost:8080/api/v1/llm-profiles

# put a source tree under $REMNANT_DATA_DIR/snapshots/<key> and register it
curl -H "Authorization: Bearer $TOKEN" -d '{"name":"libdemo","llm_profile_id":1}' localhost:8080/api/v1/projects
curl -H "Authorization: Bearer $TOKEN" -d '{"kind":"local_snapshot","display_name":"libdemo 1.2","snapshot_key":"libdemo-1.2"}' \
  localhost:8080/api/v1/projects/1/targets
```

To start from a repository instead, allow its host (`REMNANT_REPO_HOSTS=github.com`) and paste the URL on the **Analyze a repository** page (or `POST /intakes`): REMNANT fetches it with its history, drafts root-cause patterns from past security fixes, and starts a run that hunts for the same mistakes in the current code.

Then add patterns with `code_signatures` (`POST /patterns`) and queue a run (`POST /runs`, see `examples/create_run.json`). The orchestrator inside `remnantd serve` picks it up and drives it to `review_required`; follow it with `GET /runs/{id}/events`. Runs can be paused and resumed (`POST /runs/{id}/pause`, `/resume`); after a shutdown or crash they continue where they stopped. After triage, `POST /runs/{id}/finalize` produces the report. See `api/openapi.yaml` for the rest.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `REMNANT_DATABASE_URL` | — (required) | PostgreSQL connection string |
| `REMNANT_MASTER_KEY` | read from `REMNANT_MASTER_KEY_FILE` | base64 of 32 random bytes; encrypts model API keys |
| `REMNANT_MASTER_KEY_FILE` | `<data dir>/master.key` | used when `REMNANT_MASTER_KEY` is unset; created (mode 0600) on first start, refused if other users can read it |
| `REMNANT_LISTEN` | `127.0.0.1:8080` | HTTP listen address |
| `REMNANT_DATA_DIR` | `./data` | snapshots live in `<dir>/snapshots/<key>` |
| `REMNANT_REPO_HOSTS` | empty | comma-separated https hosts allowed for `git_repository` targets; empty disables them. The orchestrator fetches pending targets from these hosts |
| `REMNANT_ORCHESTRATOR` | `on` | `off` serves the API without working runs (e.g. for a separate worker instance later) |
| `REMNANT_ORCHESTRATOR_CONCURRENCY` | `2` | runs worked on at once per instance (1–16) |
| `REMNANT_WORKER` | `off` | `bwrap` enables the sandboxed worker runtime |
| `REMNANT_WORKER_BIN` | `remnant-worker` next to `remnantd` | worker executable mounted into the sandbox |
| `REMNANT_WORKER_RULES` | empty | directory of semgrep rule packs (`<dir>/<pack>`), mounted read-only |
| `REMNANT_WORKER_ALLOW_NO_CGROUP` | unset | `1` runs without a systemd user scope; memory, task and CPU limits are then **not** enforced |
| `REMNANT_STATIC_OPERATIONS` | `static.patterns,static.semgrep` | analyzers run in `static_validation`; ones the worker cannot run are skipped |
| `REMNANT_AUTO_UPDATE` | `check` | `off`, `check` (report new releases; install on request) or `install` (install and restart automatically) |
| `REMNANT_UPDATE_INTERVAL` | `6h` | how often to check (at least `10m`) |
| `REMNANT_UPDATE_REPO` | `ljw097/REMNANT` | GitHub repository releases come from |
| `REMNANT_UPDATE_API` | `https://api.github.com` | GitHub API base (for mirrors) |

## Layout

```text
cmd/remnantd/          server entry point and admin subcommands
cmd/remnant-worker/    worker program that runs inside the sandbox
cmd/remnant-release/   release signing: keygen, sign, verify
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
internal/fetch/        git fetcher for git_repository targets
internal/update/       signed release lookup, verification, install, rollback
internal/buildinfo/    version stamped into release builds
internal/llm/          model client (Anthropic SDK, schema-constrained JSON)
internal/orchestrator/ run pipeline: leases, indexing, agents, lab dispatch, reports
internal/store/        PostgreSQL access; migrations/ is embedded
internal/api/          HTTP handlers, SSE, and the embedded web UI
web/                   web UI (React + Vite); web/dist is embedded by web/embed.go
api/openapi.yaml       API contract
docs/                  architecture, agent contracts, worker protocol, security, UI
scripts/smoke.sh       end-to-end test against a disposable database
scripts/fake_anthropic.py  local model-API stand-in used by smoke.sh
scripts/dev-demo.sh        web UI with synthetic demo data
scripts/release-build.sh   reproducible release archives and SHA256SUMS
scripts/update-e2e.sh      self-update test with real binaries
.github/workflows/         ci (tests, smoke, update e2e, signed dry-run) and release
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
REMNANT_SMOKE_DATABASE_URL=postgres://.../scratch scripts/update-e2e.sh           # self-update, rollback, tampering
(cd web && npm test)                                                      # web UI unit tests
```

Both database-backed suites DROP the `remnant` schema in the database they are given. Neither needs network access or a real model key.

## Releases and updates

**Cutting a release.** Push a `vX.Y.Z` tag that points at a commit on `main`. The `release` workflow tests, builds static linux/amd64 and linux/arm64 archives reproducibly (`scripts/release-build.sh`), writes `SHA256SUMS`, signs it with the Ed25519 key held only in the `REMNANT_RELEASE_SIGNING_KEY` secret, verifies the signature against the key compiled into `remnantd`, and publishes the GitHub release. Every pull request runs the same build and signing as a dry run.

**Installing a release by hand.**

```sh
curl -LO https://github.com/ljw097/REMNANT/releases/download/vX.Y.Z/{remnant_vX.Y.Z_linux_amd64.tar.gz,SHA256SUMS,SHA256SUMS.sig}
go run ./cmd/remnant-release verify SHA256SUMS   # from a checkout; or compare with the public key in internal/update/keys.go
sha256sum -c --ignore-missing SHA256SUMS
tar xzf remnant_vX.Y.Z_linux_amd64.tar.gz
```

**Self-update.** Release builds check `REMNANT_UPDATE_REPO` every `REMNANT_UPDATE_INTERVAL` (development builds never do). `GET /api/v1/system/version` reports the running and latest versions; `POST /api/v1/system/update` (admin) or `remnantd update` installs now; `REMNANT_AUTO_UPDATE=install` does it automatically. An update:

1. accepts only releases whose `SHA256SUMS` verifies against a key compiled into the running binary, takes the version from the signed archive name and never installs an older or equal version;
2. checks the archive against the signed checksum, extracts only `remnantd` and `remnant-worker`, and runs each with `version` to confirm it;
3. swaps both binaries atomically next to the installed ones, keeping `<name>.old`;
4. shuts down gracefully (runs in progress are requeued and resume) and re-executes itself with the same PID, arguments and environment;
5. is confirmed after the new version has served for a minute. If it fails to start three times (under a supervisor such as systemd with `Restart=always`), the previous binaries are restored and started.

The directory holding the binaries must be writable by the service user for self-update; otherwise use `check` mode and install releases through your own deployment.

## Documents

- `docs/architecture.md`: components, run lifecycle, data model
- `docs/security.md`: authentication, secrets, isolation, deployment hardening
- `docs/agent-contracts.md`: agent roles, outputs, promotion rules
- `docs/worker-protocol.md`: worker JSONL protocol v1 and isolation contract
- `docs/ui.md`: web UI specification
