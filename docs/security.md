# Security model

## Authentication

- **People** sign in with a username and password (`POST /auth/login`). Passwords are 10–256 characters and not the username, and are stored only as PBKDF2-HMAC-SHA256 (600 000 iterations, 16-byte random salt, standard library). An unknown username costs the same hash work as a wrong password, and the response does not say which it was.
- **Guessing:** password endpoints are rate-limited per client address (20 per minute), and 10 wrong passwords in a row lock the account for 15 minutes. Account requests are limited to 5 per hour per address.
- **Sessions** are 32 random bytes in an `HttpOnly`, `SameSite=Strict` cookie (`Secure` behind TLS or `X-Forwarded-Proto: https`); only `SHA-256(token)` is stored (`sessions.token_sha256`). A session ends after the instance policy's inactivity period (1 hour – 30 days), after 30 days regardless, on sign-out, when the user is disabled, and — except the one making the change — when the password changes or an administrator resets it.
- **Cross-site requests:** a state-changing request authenticated by cookie must carry `X-Remnant-Request: 1`, which a cross-site form cannot send and cross-origin scripts cannot add without a CORS preflight remnantd never grants; if the browser sends `Origin`, its host must equal `Host`. The public login, setup and account-request endpoints require the same header.
- **Temporary passwords** (16 random base32 characters) are shown once when an administrator creates or resets an account; such a session can only change the password, sign out and read who it is until then.
- **Account requests** (when the policy allows them) create a pending researcher account that cannot sign in until an administrator approves it; declining deletes it.
- **API tokens** for scripts: `Authorization: Bearer rmt_<43 chars>`, 32 random bytes, only `SHA-256(token)` stored (`api_tokens.token_sha256`), printed once by `remnantd create-user` / `create-token` or Settings → Users. A token is rejected if revoked, expired, or its user is disabled or pending. Revoke with `UPDATE remnant.api_tokens SET revoked_at = now() WHERE id = …`. Tokens never appear in URLs.
- **Public routes:** `/healthz`, `GET /api/v1/branding`, `GET /api/v1/auth/status`, `POST /api/v1/auth/login`, `POST /api/v1/auth/request-account` and `/api/v1/setup`. The web UI's static files are public; they contain no data.
- **First-run setup** (`POST /setup`) works only while there are no users and needs the one-time code `remnantd serve` prints to its log: 12 random characters, stored only as SHA-256, valid 24 hours, burned after 10 wrong attempts. Setup runs under a table lock, so two concurrent requests cannot both create an administrator. `remnantd setup` does the same from the command line.
- **Local instances:** while remnantd listens only on a loopback address (the default `127.0.0.1:8080`), it refuses requests whose `Host` is not `localhost` or a loopback address (421), so a web page cannot reach it through DNS rebinding.

## Authorisation

Roles are checked per route (see `architecture.md`). Separation of duty for approvals and pattern reviews is also enforced by `CHECK` constraints, so a bug in the API layer cannot bypass it.

The instance policy, chosen at setup (personal, team or managed, then any rule changed) and changeable by an administrator, decides how far those rules bend:

- `self_approval` / `self_review` let a requester decide their own lab approval and an author review their own pattern. The database still requires a second person unless the row is marked `self_approved` / `self_reviewed`, and the API sets that flag only when the policy allows it, so every self-decision stays visible in the record and the UI's chain of custody.
- `admin_lab_approval` reserves lab approval decisions for administrators.
- `account_requests` opens the sign-in page's account request form.

When the policy cannot be read, the API applies the team rules. The last active administrator cannot be demoted or disabled; `remnantd reset-password` recovers a forgotten administrator password on the server.

The web UI is served with a strict Content-Security-Policy (same-origin scripts, styles and connections only; no inline code; no framing).

## Model credentials

- API keys are sealed with AES-256-GCM under the master key (`REMNANT_MASTER_KEY`, or the key file `REMNANT_MASTER_KEY_FILE`, default `<data dir>/master.key`, created with mode 0600 when neither exists and refused if other users can read it) with associated data `remnant/llm_profiles/api_key/v1`. A sealed value copied into another column fails to decrypt.
- The API returns only `has_api_key` and a four-character hint. No route returns key material; `store.LLMCredentials` is the only decrypting path and is used only to build a model client.
- A model client is built from its stored profile only: `ANTHROPIC_*` variables in the server environment are ignored, so they cannot redirect a profile's key to another endpoint.
- The master key is never stored in the database. Losing it makes stored keys unrecoverable; re-enter them.
- Rotation: an administrator replaces a profile's key in Settings → Model profiles (`PATCH /llm-profiles/{id}` with `api_key`); the old sealed key is overwritten. "Test connection" (`POST /llm-profiles/{id}/test`, or `/llm-profiles/test` for unsaved settings) looks the model up at the provider with the key — it spends no tokens and returns only success or the HTTP reason, never key material. A profile cannot be deleted while a project, agent or unfinished run uses it.
- The call log (`model_calls`) keeps each agent call's task envelope and answer so people can audit them. It holds no key material (keys travel only in request headers), and no system prompt; the envelope contains the same repository excerpts and commit text the evidence and commit records already store, and it is readable by any signed-in user, like runs. The repository text inside it stays untrusted data and is shown as text, never rendered as HTML.
- Agent instructions set by administrators are appended to an agent's system prompt after REMNANT's rules, under a heading that says they cannot override them; the model's output is still schema-validated and evidence-checked as before.

## Source handling

- Local snapshots are read only from `$REMNANT_DATA_DIR/snapshots/<key>`; keys match `^[a-z0-9][a-z0-9._-]{0,127}$` and cannot contain `..`. A snapshot root that is a symlink is rejected, and the tree digest never follows symlinks.
- Remote repositories must be `https://` on a host listed in `REMNANT_REPO_HOSTS`, without credentials, port, query or fragment. An empty allowlist disables them.
- The fetcher re-checks the allowlist, resolves the host and refuses it if *any* address is private, loopback, link-local or otherwise non-public, then pins git's connection to the checked address (`http.curloptResolve`), so DNS rebinding cannot redirect it inside the network. Git runs with no system or user configuration, no credential helper, no hooks, no submodules, no redirects and only the https transport; refs that could be read as options are rejected. It fetches one ref at depth 1, removes `.git`, refuses trees over 2 GiB and never replaces an existing snapshot directory.
- A git target's revision is its commit SHA; its tree digest is recorded too, and every run and worker request re-hashes the checkout against it.
- Repository text, comments and embedded prompts are untrusted data for agents (see `agent-contracts.md`).

## Repository history (intake)

- An intake fetches only from hosts in `REMNANT_REPO_HOSTS`, through the same fetcher as other git targets: https only, no redirects, credentials, hooks or submodules, the connection pinned to a public address checked before fetching, and a size limit. With remote repositories off, the intake API refuses with `source_policy`.
- History is read inside the temporary clone, before `.git` is removed: up to `history_depth` commits (≤5000), commit messages scored for security-fix signals, and for at most 60 candidates the diff of modified source files (≤20 files, ≤16 KiB, `--no-ext-diff --no-textconv`, so repository attributes cannot run drivers). Commits at the shallow boundary are skipped. Only this text is stored (`commit_records`); the snapshot remains the tree alone.
- Commit messages and diffs are untrusted. The Historian receives them as data in its task envelope under rules that tell it to ignore instructions inside; its answer is schema-checked, and its code signatures are kept only if they appear on the pre-fix side of the diff, so a pattern cannot point at invented code.
- Mined patterns are recorded with `source_kind = mined`, the commit they came from, and `unreviewed` status; they feed a run like any other pattern, and their hypotheses go through the same triage and approval rules.

## Autopilot and reporting

- An autopilot run triages its own hypotheses and finalises a report without a person. Each machine decision is stored with `auto_triaged = true` and no `reviewed_by`, and a database CHECK forbids a person-less triage that is not marked automatic, so the record never reads as a human decision. Autopilot never runs code: lab experiments (dynamic validation) still need a person's approval, and findings it did not confirm by running code are marked **candidate**.
- The Reporter agent writes the narrative report and the disclosure draft. It receives the run's structured findings as untrusted data under rules that forbid inventing facts, writing exploit code, or claiming a person wrote the report. The factual, deterministic report (every hypothesis, evidence excerpt with content hash, and experiment) is always kept as an appendix, and a provenance line stating that the report was produced by an automated tool and is pending human review is appended by code, not by the model, so it is always present and truthful.
- REMNANT drafts the disclosure; it never sends anything. A person reviews and sends it.

## Worker isolation

Implemented by `internal/sandbox` (bwrap inside a systemd user cgroup scope); see `worker-protocol.md`. In short: read-only source mount, write-only output directory, no network at the namespace level, non-root, dropped capabilities, seccomp/AppArmor, PID/CPU/memory/time limits, no container-runtime socket, no host home directories, SSH agents, cloud credentials, browser profiles, or REMNANT configuration and secrets. Everything a worker returns is validated before it is stored.

## Releases and self-update

- Release archives are listed in `SHA256SUMS`, signed with Ed25519 over `"remnant-release-v1\n" || SHA256SUMS`. The private key exists only as the `REMNANT_RELEASE_SIGNING_KEY` Actions secret; the public key is compiled into `remnantd` (`internal/update/keys.go`).
- The updater trusts nothing that is not covered by that signature: the version comes from the signed archive name (not the release tag), so a compromised release host can neither substitute binaries nor roll an installation back. Downloads are size-capped, https-only (redirects included), and only the two expected regular files are extracted.
- Before swapping, each new binary must run and report the signed version. The previous binaries are kept as `.old`; a version that fails to start three times is rolled back automatically.
- Key rotation: generate a new key (`remnant-release keygen`), release a version whose `keys.go` lists both public keys, signed with the old key; then switch the secret to the new key. Installations that skipped that release must be updated by hand once.
- If the signing secret leaks, remove it, rotate as above, and treat releases signed after the leak as untrusted.

## Deployment hardening

- remnantd listens on `127.0.0.1:8080` by default. Put a TLS-terminating reverse proxy in front before exposing it beyond the host.
- Give the database role ownership of the `remnant` schema only.
- Supply `REMNANT_MASTER_KEY` from a secret manager or a root-readable environment file, not a shell history or unit file in version control.
- Back up PostgreSQL before upgrades; migrations run automatically on `serve` and `migrate`.
- The HTTP server sets `nosniff`, `no-store` and `no-referrer`, limits request bodies to 1 MiB and rejects unknown JSON fields.
