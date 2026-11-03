# REMNANT — running and setup guide

REMNANT is a source-first vulnerability research tool. You give it source
code and root-cause patterns (or a repository URL); it turns past
vulnerabilities into evidence-backed hypotheses about the current code,
gates any code execution behind an approval, and writes a report. This
guide takes you from nothing to a finished report.

- [1. What you need](#1-what-you-need)
- [2. Run it](#2-run-it)
- [3. First-run setup](#3-first-run-setup)
- [4. Signing in](#4-signing-in)
- [5. Your first analysis](#5-your-first-analysis)
- [6. Reviewing and finishing](#6-reviewing-and-finishing)
- [7. Administration](#7-administration)
- [8. Configuration reference](#8-configuration-reference)
- [9. Updating](#9-updating)

---

## 1. What you need

- **Go ≥ 1.25** and **Node.js ≥ 20.19** to build (the web UI is built once and
  embedded in the `remnantd` binary).
- **PostgreSQL ≥ 14.** Running locally, `remnantd local` can start one for you
  with **Docker**; as a server you point REMNANT at your own PostgreSQL.
- An **Anthropic API key** for real analysis. Without one you can still run the
  synthetic demo (`scripts/dev-demo.sh`), where the model is faked.
- For dynamic (lab) validation only: Linux with unprivileged user namespaces,
  `bwrap` (bubblewrap), and a systemd user session.

---

## 2. Run it

### Locally, one command

On your own machine, with Docker installed:

```sh
scripts/local.sh
```

This builds the web UI and `remnantd`, then runs `remnantd local`, which:

- creates a per-user data directory and a master key file (0600),
- listens on `127.0.0.1:8080` only and turns automatic updates off,
- starts a PostgreSQL in Docker when `REMNANT_DATABASE_URL` is not set
  (container `remnant-db`, a persistent volume, port 5433),
- applies migrations, serves, and **opens the first-run setup page in your
  browser**.

Without Docker, start any PostgreSQL yourself and set `REMNANT_DATABASE_URL`;
the same command uses it.

### As a server

```sh
# 1. Build (web UI first, then the binaries)
(cd web && npm ci && npm run build)
go build -o remnantd ./cmd/remnantd
go build -o remnant-worker ./cmd/remnant-worker   # only needed for lab validation

# 2. Point it at PostgreSQL and a data directory
export REMNANT_DATABASE_URL='postgres://remnant@localhost/remnant'
export REMNANT_DATA_DIR=/var/lib/remnant
# REMNANT_MASTER_KEY is optional; without it a key file is created in the data dir.

# 3. Migrate and serve
./remnantd migrate
./remnantd serve     # the log prints a one-time setup code
```

Open `http://<host>:8080/`. Put it behind TLS (a reverse proxy) for anything
but local use.

---

## 3. First-run setup

While no users exist, the log prints a **one-time setup code** (valid 24 h).
The setup page (opened automatically for `remnantd local`, otherwise just open
the URL) asks you to:

1. **Choose how the instance is used** — this presets the rules, and you can
   change any of them:

   | Type | For | Rules |
   |---|---|---|
   | **Personal** | one researcher on their own machine | you may approve your own lab requests and review your own patterns (recorded as such); 30-day sessions |
   | **Team** | researchers who cross-check each other | nobody decides their own requests; 7-day sessions |
   | **Managed** | an organization with administrators as gatekeepers | only administrators approve lab experiments; people may request an account; 12-hour sessions |

2. **Create the administrator** — a username, a password (10+ characters), an
   optional instance name, and the setup code.

Setup signs you straight in. Without the browser:
`remnantd setup -profile personal -username me` (prints a temporary password;
add `-password-stdin` to set your own).

---

## 4. Signing in

- People sign in with a **username and password**. "Keep me signed in" extends
  the session to its 30-day maximum; otherwise it idles out per the policy.
- **Forgot a password?** An administrator resets it in Settings → Users, or on
  the server: `remnantd reset-password -username NAME` (works even for the last
  administrator).
- **Scripts and the CLI** use API tokens (`Authorization: Bearer rmt_…`):
  Settings → Users → "API token", or `remnantd create-token -username NAME`.
- Press **⌘K** (or Ctrl-K) anywhere for the command palette — jump to any page,
  run, pattern or project.

---

## 5. Your first analysis

### From a repository URL (the fast path)

1. Enable a repository host once, on the server:
   `REMNANT_REPO_HOSTS=github.com` (and restart). An administrator sees this on
   the Analyze page when it is off.
2. **Analyze a repository** → paste a URL (`github.com/org/repo`, a `.git` URL,
   or `…/tree/branch`). Leave **Autopilot** and **Find security fixes in the
   history** on.
3. REMNANT fetches the repository with its history, finds commits that look
   like security fixes, has the **Historian** draft root-cause patterns from
   them, and starts a run that hunts for the same mistakes in the current code.
   With autopilot it triages the hypotheses and writes the report by itself.

### Manually

1. **Settings → Model profiles** (admin): add a model profile with your
   Anthropic API key. Test the connection (spends no tokens).
2. **Projects** → create a project (read-only source, no network, dynamic
   testing off by default).
3. **Targets** (inside a project) → add a local snapshot (a source tree under
   `$REMNANT_DATA_DIR/snapshots/<key>`) or a pinned Git repository.
4. **Patterns** → add root-cause patterns with `code_signatures`, or reuse
   mined ones.
5. **New run** → pick the project, targets and patterns; optionally turn on
   **Autopilot**.

---

## 6. Reviewing and finishing

- **Review** queue: hypotheses waiting for a decision, highest priority first.
  Keyboard: `J`/`K` move, `A` accept, `R` reject, `D` duplicate, `V`/`N` record
  validated / not reproducible. Each hypothesis shows its claim, evidence
  excerpts with content hashes, the Skeptic's counter-evidence and the chain of
  custody.
- **Lab approvals**: dynamic validation runs code, so it needs an approval of
  the exact scope (target revision, operation, limits, network none). In team
  and managed instances a second person approves; autopilot never runs code.
- **Autopilot** (set on a run or on a repository analysis): triages the
  hypotheses and finalises the report without a person. Findings it did not
  confirm by running code are marked **candidate**.
- **Reports**: every finished run produces a Markdown and JSON report. When the
  **Reporter** agent is on, it also writes a natural-language narrative and a
  **responsible-disclosure draft** — which you review and send; REMNANT never
  sends anything. Reports carry a provenance line (produced by an automated
  tool, pending human review).

---

## 7. Administration

All under **Settings** (admin):

- **Operating mode** — the instance policy: self-approval, self-review,
  administrator-only lab approval, account requests, and the inactivity
  sign-out. Changing it applies to later decisions.
- **Users** — add users (with a temporary password shown once, or one you set),
  reset passwords, approve or decline account requests, change roles, disable.
- **Model profiles** — register, test, edit and re-key API keys (sealed at
  rest), set thinking/timeout/retries, delete when unused.
- **Agents** — turn the Historian, Hypothesis Researcher, Skeptic or Reporter
  on or off, give each its own model profile, a per-call token cap and extra
  instructions.
- **Expert settings** — run defaults, candidates per pattern and per run, the
  per-call token cap, lab defaults, approval lifetime, and which static
  analyzers run.
- **Organization branding** — instance name and accent colour.
- **System and updates** — the running version, signed self-update, and the
  read-only server configuration.

---

## 8. Configuration reference

Server settings come from the environment (a restart applies them):

| Variable | Default | Meaning |
|---|---|---|
| `REMNANT_DATABASE_URL` | — (required) | PostgreSQL connection string |
| `REMNANT_MASTER_KEY` | read from a key file | base64 of 32 bytes; encrypts model API keys. Without it, `<data dir>/master.key` is created (0600) |
| `REMNANT_DATA_DIR` | `data` | snapshots, artifacts, update staging, the key file |
| `REMNANT_LISTEN` | `127.0.0.1:8080` | address to serve on |
| `REMNANT_REPO_HOSTS` | — (off) | comma-separated hosts allowed for repository fetch/analysis |
| `REMNANT_WORKER` | `off` | `bwrap` enables the sandboxed worker (static analysis + lab) |
| `REMNANT_AUTO_UPDATE` | `check` | `off`, `check`, or `install` |

See `README.md` for the full list.

Instance behaviour (profiles, agents, run limits, branding) is set at runtime
in the UI, not by environment variables.

---

## 9. Updating

- **Signed self-update**: an administrator installs the latest signed release
  from Settings → System and updates, or the server does it on a schedule
  (`REMNANT_AUTO_UPDATE=install`). A release that fails to start is rolled back.
- **From source**: pull, rebuild (`npm run build` in `web/`, then `go build`),
  and restart; `remnantd` applies new migrations on start.
- **Local Docker database**: `remnantd local` reuses the `remnant-db` container
  and its volume across restarts; remove them with
  `docker rm -f remnant-db && docker volume rm remnant-db-data` to start clean.

---

More detail: `docs/ui.md` (the web UI and modes), `docs/security.md` (the
security model), `docs/architecture.md`, and `api/openapi.yaml` (the API).
