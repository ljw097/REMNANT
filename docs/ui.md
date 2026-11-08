# REMNANT web UI

The web UI is a single-page app (React + Vite, TypeScript) in `web/`. `npm run build` writes `web/dist`, which is embedded into `remnantd` with `go:embed`, so the same binary serves the UI at `/` and the API at `/api/v1`. It has no server-side component and no credentials of its own. A binary built without `web/dist` serves a short page that says how to build it.

```sh
cd web && npm ci && npm run build   # then go build ./cmd/remnantd
npm run dev                         # Vite dev server; proxies /api to $REMNANT_DEV_API (default 127.0.0.1:8080)
npm test                            # unit tests (vitest)
REMNANT_DEMO_DATABASE_URL=postgres://.../scratch scripts/dev-demo.sh   # UI with synthetic data
```

## Serving

- Unknown paths that are not files get `index.html`, so deep links work. Files under `/assets/` are content-hashed and cached for a year.
- Every UI response carries a strict CSP (`default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; …`), `X-Frame-Options: DENY` and `Cross-Origin-Opener-Policy: same-origin`. The app uses no inline scripts or styles and never sets `innerHTML`; the only runtime styling is the branding accent, set through the CSSOM.
- The UI build is reproducible; release archives embed it.

## First run: type of use and the administrator

While no users exist, `remnantd serve` prints a one-time setup code to its log (valid 24 hours, burned after 10 wrong attempts). Opening the UI shows the setup page. Step 1 picks how the instance is used; each type presets the rules, and any rule can then be changed (the type becomes "custom"):

| Type | For | Rules it sets |
|---|---|---|
| **Personal** | One researcher on their own machine | May approve their own lab requests and review their own patterns, each recorded as `self_approved` / `self_reviewed`; sessions last 30 days of inactivity |
| **Team** | Researchers who cross-check each other | Nobody decides their own requests; administrators add accounts; 7-day sessions |
| **Managed** | An organization where administrators hold the gates | Only administrators decide lab approvals; people may request an account on the sign-in page, which an administrator approves; 12-hour sessions |

The rules are: self-approval of lab requests, self-review of patterns, administrator-only lab approval, account requests, and the inactivity sign-out (1 hour – 30 days). Step 2 creates the administrator: username, password (10+ characters, not the username), an optional instance name, and the setup code. Setup signs the administrator straight in.

`remnantd setup -profile personal|team|managed -username NAME [-name INSTANCE] [-password-stdin]` does the same from the command line; without `-password-stdin` it prints a temporary password that must be changed at the first sign-in. Administrators change the rules later under Settings → Operating mode; changes apply to decisions made afterwards.

### Working alone on your own machine

`scripts/local.sh` (or `remnantd local`) does all of this in one command: local defaults, a database in Docker when none is configured, migrations, serve, and it opens the setup page in your browser.


For the personal type nothing else is needed: `remnantd serve` listens on `127.0.0.1:8080` by default, and without `REMNANT_MASTER_KEY` it creates `<data dir>/master.key` (mode 0600) on first start and reuses it. While it listens only on loopback, remnantd answers only requests addressed to `localhost`/`127.0.0.1`/`::1`, so a web page elsewhere cannot reach it by DNS rebinding.

## Sign-in

People sign in with their username and password. The server sets an HttpOnly, SameSite=Strict session cookie; the page never sees it, and every state-changing request also carries `X-Remnant-Request: 1`. Sessions end after the policy's inactivity period, after 30 days at most, on sign-out, when the password changes (other sessions), or when the account is disabled.

- **Temporary passwords:** an administrator who adds a user without choosing a password, or resets one (Settings → Users), gets a temporary password shown once. The user must replace it before anything else.
- **Forgotten passwords:** an administrator resets it; on the server, `remnantd reset-password -username NAME` works even for the last administrator.
- **Account requests** (when the policy takes them): "Request an account" on the sign-in page creates a pending researcher account that appears at the top of Settings → Users for an administrator to approve or decline.
- **API tokens** remain for scripts and the CLI (`Authorization: Bearer rmt_…`); administrators issue them under Settings → Users or with `remnantd create-token`.

Run activity uses `GET /runs/{id}/events` read with `fetch()` and a streaming body reader; reconnects send `Last-Event-ID`.

## Customisation

- **Per user** (`/me/preferences`, follows the user to other devices): theme (system/light/dark), density, language (Korean/English), start page, dashboard panels and their order, and table columns.
- **Role-aware defaults**: reviewers start on Review with "Waiting for you" first; researchers see runs first; administrators also get the System panel.
- **Organization** (admin, `/branding`): instance name and accent colour. The accent is used only for actionable controls; the UI warns when it is unreadable on white.

## Screens

| Path | Purpose |
|---|---|
| `/` | Dashboard. While the instance has no projects it shows only the start form (connect a model with one API key if none exists, then paste a repository URL). Afterwards: waiting for you (triage and lab approvals), runs in motion as transit lines, recent findings, recent runs, projects, system. |
| `/review` | Hypotheses waiting for a decision, highest priority first. In a review session `J`/`K` move, `A`/`R`/`D` accept, reject or mark duplicate, `V`/`N` record validated / not reproducible. |
| `/hypotheses/{id}` | Claim, facts, preconditions, unknowns, expected observation; line-numbered evidence with revision digests; counter-evidence with the skeptic's verdict; chain of custody; the decision bar. Researchers request lab validation from here. |
| `/approvals` | Pending and decided lab approvals. The drawer shows the exact scope (operation, target, revision, limits, network none, scope hashes) and its custody. A 409 means the request went stale. |
| `/analyze`, `/analyze/{id}` | Repository analysis (sidebar: **New analysis**): paste a repository URL and press Start; every other choice has a default and sits under **Options** (on a host in `REMNANT_REPO_HOSTS`). REMNANT fetches it with its recent history, lists the commits whose messages look like security fixes (CVE/CWE, overflow, use-after-free, sanitizer and fuzzer reports…), has the Historian agent draft root-cause patterns from their diffs, and starts a run that hunts for the same mistakes in the current code. The progress page shows each step, the fix candidates and the mined patterns. With **autopilot** (on by default) the run triages its own hypotheses and writes the report and a disclosure draft with no person in the loop; lab experiments and sending the disclosure still need a person. |
| `/runs`, `/runs/{id}` | Runs list; run detail with an RFC-style header (revision, scope hash, model, tokens, time), the stage line, scope and policy, and tabs for hypotheses, experiments, live activity and artifacts. Pause, resume, cancel and finalize. |
| `/runs/new` | Project, ready targets, patterns, limits; the confirmation lists every target with its revision and the read-only/no-network policy. |
| `/findings` | Validated findings and those awaiting lab results. |
| `/projects`, `/projects/{id}` | Projects, policy, and targets (local snapshots or pinned Git repositories). |
| `/patterns`, `/patterns/{id}` | Root-cause pattern library with review state and custody. |
| `/settings/*` | Preferences, dashboard, account (password). For administrators: operating mode (instance policy); users; **model profiles** (register an API key, test the connection without spending tokens, edit, replace the key, set thinking/timeout/retries, delete when unused); **agents** (turn the Hypothesis Researcher or Skeptic on or off, give each its own model profile, a per-call token cap and additional instructions); **expert settings** (run defaults, candidates per pattern and per run, tokens per model call, lab defaults, approval lifetime, which static analyzers run); branding; system and updates with the read-only server configuration. |

Desktop and laptop get the sidebar; on phones the sidebar becomes a menu, tables scroll inside their panel, and approvals and run status are fully usable.

## State vocabulary

Every state has a tone and a line form, so it never depends on colour alone: solid (settled), dashed with a hollow dot (waiting — on a person or on a queue), live (an automatic stage is working), struck (stopped). The design system is recorded in `DESIGN.md`.
