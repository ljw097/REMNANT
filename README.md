# REMNANT

A source-first vulnerability research server.

REMNANT takes the root cause of a past vulnerability, turns it into a reusable
pattern, and looks for the same mistake in source code you give it. Every
finding is a falsifiable hypothesis backed by the exact lines of a pinned
revision, not a model guess you have to trust. If you want to confirm a finding
by actually running code, that step is local, has no network, is off by
default, and needs a person to approve the exact scope first.

It is a standalone service. One Go binary (`remnantd`) and one PostgreSQL
database. It owns its own users, tokens, model credentials, schema, and API,
and ships its web UI inside the same binary.

It does not scan arbitrary hosts, run remote exploits, open shells, collect
credentials, or move data out. The things it looks at are source snapshots that
you own or are allowed to review.

New here? Read [docs/GUIDE.md](docs/GUIDE.md). It covers running it, the
first-run setup, and a full walkthrough from nothing to a finished report.

## What it does

- Finds variants of known bugs. You give it a root-cause pattern (or let it mine
  one from a repository's history of security fixes), and it hunts for the same
  shape elsewhere in the current code.
- Finds bugs with no history too. General discovery mode audits the source
  directly for problems of any class, so pasting a repository URL still turns up
  something even when there is no past fix to learn from.
- Pulls in known advisories. During a repository analysis it asks the OSV feed
  for the project's published CVEs and folds in the commits that fixed them,
  including silent fixes whose message never mentioned a CVE.
- Keeps every claim honest. A finding points at hashed excerpts of an immutable
  revision, shows the Skeptic's counter-arguments, and is marked a candidate
  until a lab experiment confirms it. A model's confidence is shown as a triage
  priority, never as proof.
- Writes the report for you. Each run produces a Markdown and JSON report, and,
  with autopilot on, a responsible-disclosure draft that a person reviews and
  sends. REMNANT never sends anything itself.

Models can be Anthropic (Claude) or OpenAI, chosen per profile. The web UI is a
React app built once and embedded in the binary, so the server and its
interface update together.

## Run it on your own machine (one command)

With Docker installed:

```sh
scripts/local.sh
```

That builds the web UI and `remnantd`, then runs `remnantd local`. The local
command picks sensible defaults for a single machine: a per-user data directory,
a master key file, loopback-only listening, automatic updates off, and the
public code hosts (github.com, gitlab.com, codeberg.org, bitbucket.org) allowed
for repository analysis. When
`REMNANT_DATABASE_URL` is not set it starts a PostgreSQL in Docker for you (the
container `remnant-db`, a persistent volume, port 5433). It then applies
migrations, serves on `127.0.0.1:8080`, and opens the first-run setup page in
your browser, where you pick the personal profile and set an administrator
password. The dashboard then asks for one API key and a repository URL, and
that is enough for a first report. You do not have to administer a server. If you would rather bring your
own database, set `REMNANT_DATABASE_URL` to any PostgreSQL and the same command
uses it.

## Build and run as a server

You need Go 1.25 or newer, Node.js 20.19 or newer to build the web UI, and
PostgreSQL 14 or newer. The sandboxed worker (for dynamic validation) also needs
Linux with unprivileged user namespaces, bubblewrap (`bwrap`), and a systemd
user session for resource limits; lab harnesses use the host toolchain, for
example `cc` with sanitizers.

```sh
(cd web && npm ci && npm run build)               # web UI, embedded into remnantd
go build -o remnantd ./cmd/remnantd
go build -o remnant-worker ./cmd/remnant-worker   # only for REMNANT_WORKER=bwrap

export REMNANT_DATABASE_URL='postgres://remnant@localhost/remnant'
export REMNANT_DATA_DIR=/var/lib/remnant
# Without REMNANT_MASTER_KEY, a key is created at $REMNANT_DATA_DIR/master.key
# (mode 0600) on first start. Keep it safe: it decrypts your model API keys.

./remnantd migrate
./remnantd serve
```

Open http://localhost:8080. On first start the log prints a one-time setup code.
The setup page asks for it and lets you choose how the instance is used:
personal (one researcher on their own machine, where approving your own requests
is allowed and recorded), team (researchers cross-check each other), or managed
(administrators approve lab experiments and accounts). You can then change any
individual rule. After that, create the administrator with a password. Everyone
signs in with a username and password from then on.

Without a browser you can run `./remnantd setup -profile personal -username me`,
which prints a temporary password; add `-password-stdin` to set your own.
Scripts and the CLI use API tokens from `./remnantd create-token -username me`.

## Using it from scripts

```sh
TOKEN=$(./remnantd create-token -username me)
curl -H "Authorization: Bearer $TOKEN" localhost:8080/api/v1/me

# register a model profile (admin). provider is "anthropic" or "openai".
curl -H "Authorization: Bearer $TOKEN" \
  -d '{"name":"default","provider":"anthropic","model":"claude-opus-5-5","api_key":"sk-ant-..."}' \
  localhost:8080/api/v1/llm-profiles

# put a source tree under $REMNANT_DATA_DIR/snapshots/<key> and register it
curl -H "Authorization: Bearer $TOKEN" -d '{"name":"libdemo","llm_profile_id":1}' \
  localhost:8080/api/v1/projects
curl -H "Authorization: Bearer $TOKEN" \
  -d '{"kind":"local_snapshot","display_name":"libdemo 1.2","snapshot_key":"libdemo-1.2"}' \
  localhost:8080/api/v1/projects/1/targets
```

To start from a repository instead, allow its host with
`REMNANT_REPO_HOSTS=github.com` and paste the URL on the "Analyze a repository"
page (or `POST /intakes`). REMNANT fetches it with its history, drafts
root-cause patterns from past security fixes, asks OSV for known advisories, and
starts a run that hunts for the same mistakes in the current code. With general
discovery on, it also audits the code directly, so it finds something even when
the history is clean.

For a manual run, add patterns with `code_signatures` (`POST /patterns`) and
queue a run (`POST /runs`, see `examples/create_run.json`). The orchestrator
inside `remnantd serve` picks it up and drives it to `review_required`; follow it
with `GET /runs/{id}/events`. Runs can be paused and resumed, and after a
shutdown or crash they continue where they left off. After triage,
`POST /runs/{id}/finalize` writes the report. The rest of the API is in
`api/openapi.yaml`.

## Configuration

Server settings come from the environment. A restart applies them.

| Variable | Default | Meaning |
|---|---|---|
| `REMNANT_DATABASE_URL` | none, required | PostgreSQL connection string |
| `REMNANT_MASTER_KEY` | read from the key file | base64 of 32 random bytes; encrypts model API keys |
| `REMNANT_MASTER_KEY_FILE` | `<data dir>/master.key` | used when `REMNANT_MASTER_KEY` is unset; created (mode 0600) on first start, refused if other users can read it |
| `REMNANT_LISTEN` | `127.0.0.1:8080` | HTTP listen address |
| `REMNANT_DATA_DIR` | `./data` | snapshots live in `<dir>/snapshots/<key>` |
| `REMNANT_REPO_HOSTS` | empty | comma-separated https hosts allowed for repository targets; empty turns repository analysis off |
| `REMNANT_ADVISORY_FEED` | `osv` when repo hosts are set, else `off` | external vulnerability feed used during intake: `off` or `osv` |
| `REMNANT_ADVISORY_API` | `https://api.osv.dev` | base URL of the advisory feed |
| `REMNANT_ORCHESTRATOR` | `on` | `off` serves the API without working runs |
| `REMNANT_ORCHESTRATOR_CONCURRENCY` | `2` | runs worked on at once per instance (1 to 16) |
| `REMNANT_WORKER` | `off` | `bwrap` enables the sandboxed worker |
| `REMNANT_WORKER_BIN` | `remnant-worker` next to `remnantd` | worker executable mounted into the sandbox |
| `REMNANT_WORKER_RULES` | empty | directory of semgrep rule packs, mounted read-only |
| `REMNANT_WORKER_ALLOW_NO_CGROUP` | unset | `1` runs without a systemd user scope, so memory, task, and CPU limits are not enforced |
| `REMNANT_STATIC_OPERATIONS` | `static.patterns,static.semgrep` | analyzers run during static validation; ones the worker cannot run are skipped |
| `REMNANT_AUTO_UPDATE` | `check` | `off`, `check` (report new releases, install on request), or `install` (install and restart automatically) |
| `REMNANT_UPDATE_INTERVAL` | `6h` | how often to check, at least `10m` |
| `REMNANT_UPDATE_REPO` | `ljw097/REMNANT` | GitHub repository releases come from |
| `REMNANT_UPDATE_API` | `https://api.github.com` | GitHub API base, for mirrors |

## Layout

```text
cmd/remnantd/          server entry point and admin subcommands
cmd/remnant-worker/    worker program that runs inside the sandbox
cmd/remnant-release/   release signing: keygen, sign, verify
internal/config/       environment configuration
internal/auth/         roles, bearer tokens, middleware
internal/secrets/      AES-256-GCM sealing of stored credentials
internal/policy/        project policy, run and lab scope hashes
internal/research/      run state machine, triage rules, run limits
internal/source/        repository allowlist, snapshot keys, tree digests
internal/worker/        JSONL worker protocol and result validation
internal/workerexec/    worker operations: index, built-in rules, semgrep, lab harnesses
internal/sandbox/       supervisor: bwrap plus systemd scope, artifact verification
internal/sourceindex/   heuristic source chunker shared by orchestrator and worker
internal/artifact/      content-addressed artifact storage
internal/fetch/         git fetcher for repository targets
internal/advisory/      OSV advisory lookup used during intake
internal/update/        signed release lookup, verification, install, rollback
internal/buildinfo/     version stamped into release builds
internal/llm/           model clients (Anthropic SDK and OpenAI), schema-constrained JSON
internal/orchestrator/  run pipeline: leases, indexing, agents, lab dispatch, reports
internal/store/         PostgreSQL access; migrations/ is embedded
internal/api/           HTTP handlers, SSE, and the embedded web UI
web/                    web UI (React and Vite); web/dist is embedded by web/embed.go
api/openapi.yaml        API contract
docs/                   architecture, agent contracts, worker protocol, security, UI
scripts/                build, demo, smoke, update, and release scripts
```

## Dependencies

The dependency list is kept short on purpose:

- `github.com/jackc/pgx/v5` for PostgreSQL.
- `github.com/anthropics/anthropic-sdk-go/v2` for the Anthropic model client.

The OpenAI client, HTTP routing, auth, crypto, SSE, and migrations all use the
Go standard library.

## Testing

```sh
go test ./...                                                              # unit tests, no database needed
REMNANT_TEST_DATABASE_URL=postgres://.../scratch go test ./internal/orchestrator/   # pipeline against PostgreSQL
REMNANT_SMOKE_DATABASE_URL=postgres://.../scratch scripts/smoke.sh                 # binary, API, orchestrator, worker
REMNANT_SMOKE_DATABASE_URL=postgres://.../scratch scripts/update-e2e.sh            # self-update, rollback, tampering
(cd web && npm test)                                                      # web UI unit tests
```

Both database-backed suites drop the `remnant` schema in the database you give
them, so point them at a scratch database. Neither needs network access or a
real model key.

## Releases and updates

To cut a release, push a `vX.Y.Z` tag that points at a commit on `main`. The
release workflow tests, builds static linux/amd64 and linux/arm64 archives
reproducibly, writes `SHA256SUMS`, signs it with the Ed25519 key held only in
the `REMNANT_RELEASE_SIGNING_KEY` secret, verifies the signature against the key
compiled into `remnantd`, and publishes the GitHub release. Every pull request
runs the same build and signing as a dry run.

To install a release by hand:

```sh
curl -LO https://github.com/ljw097/REMNANT/releases/download/vX.Y.Z/{remnant_vX.Y.Z_linux_amd64.tar.gz,SHA256SUMS,SHA256SUMS.sig}
go run ./cmd/remnant-release verify SHA256SUMS   # from a checkout; or compare with the public key in internal/update/keys.go
sha256sum -c --ignore-missing SHA256SUMS
tar xzf remnant_vX.Y.Z_linux_amd64.tar.gz
```

Release builds can also update themselves. They check `REMNANT_UPDATE_REPO`
every `REMNANT_UPDATE_INTERVAL` (development builds never do).
`GET /api/v1/system/version` reports the running and latest versions. An admin
can install now with `POST /api/v1/system/update` or `remnantd update`, and
`REMNANT_AUTO_UPDATE=install` does it automatically. An update only accepts a
release whose `SHA256SUMS` verifies against the key compiled into the running
binary, never installs an older or equal version, checks the archive against the
signed checksum, extracts only `remnantd` and `remnant-worker` and runs each to
confirm it, swaps both binaries atomically while keeping `<name>.old`, restarts
in place with the same arguments and environment, and is confirmed only after
the new version has served for a minute. If it fails to start three times under
a supervisor such as systemd with `Restart=always`, the previous binaries are
restored. The directory holding the binaries has to be writable by the service
user for this; otherwise use `check` mode and install releases through your own
deployment.

## More documents

- [docs/GUIDE.md](docs/GUIDE.md): running, setup, and a full walkthrough.
- `docs/architecture.md`: components, run lifecycle, data model.
- `docs/security.md`: authentication, secrets, isolation, deployment hardening.
- `docs/agent-contracts.md`: agent roles, outputs, promotion rules.
- `docs/worker-protocol.md`: the worker protocol and isolation contract.
- `docs/ui.md`: the web UI.
