# Security model

## Authentication

- Every `/api/v1` route requires `Authorization: Bearer rmt_<43 chars>`. `/healthz` is the only unauthenticated route.
- Tokens are 32 random bytes. Only `SHA-256(token)` is stored (`api_tokens.token_sha256`); the plaintext is printed once by `remnantd create-user` / `create-token`.
- A token is rejected if it is revoked, expired, or its user is disabled. Revoke with `UPDATE remnant.api_tokens SET revoked_at = now() WHERE id = …`.
- Browsers: `EventSource` cannot send headers, so the UI reads the SSE stream with `fetch()` and a streaming body reader. Tokens are never put in query strings.

## Authorisation

Roles are checked per route (see `architecture.md`). Separation of duty for approvals and pattern reviews is also enforced by `CHECK` constraints, so a bug in the API layer cannot bypass it.

## Model credentials

- API keys are sealed with AES-256-GCM under `REMNANT_MASTER_KEY` with associated data `remnant/llm_profiles/api_key/v1`. A sealed value copied into another column fails to decrypt.
- The API returns only `has_api_key` and a four-character hint. No route returns key material; `store.LLMCredentials` is the only decrypting path and is used only to build a model client.
- A model client is built from its stored profile only: `ANTHROPIC_*` variables in the server environment are ignored, so they cannot redirect a profile's key to another endpoint.
- The master key is never stored in the database. Losing it makes stored keys unrecoverable; re-enter them.
- Rotation: `key_version` exists for a future re-seal command. Until then, rotate by re-creating profiles.

## Source handling

- Local snapshots are read only from `$REMNANT_DATA_DIR/snapshots/<key>`; keys match `^[a-z0-9][a-z0-9._-]{0,127}$` and cannot contain `..`. A snapshot root that is a symlink is rejected, and the tree digest never follows symlinks.
- Remote repositories must be `https://` on a host listed in `REMNANT_REPO_HOSTS`, without credentials, port, query or fragment. An empty allowlist disables them.
- The fetcher re-checks the allowlist, resolves the host and refuses it if *any* address is private, loopback, link-local or otherwise non-public, then pins git's connection to the checked address (`http.curloptResolve`), so DNS rebinding cannot redirect it inside the network. Git runs with no system or user configuration, no credential helper, no hooks, no submodules, no redirects and only the https transport; refs that could be read as options are rejected. It fetches one ref at depth 1, removes `.git`, refuses trees over 2 GiB and never replaces an existing snapshot directory.
- A git target's revision is its commit SHA; its tree digest is recorded too, and every run and worker request re-hashes the checkout against it.
- Repository text, comments and embedded prompts are untrusted data for agents (see `agent-contracts.md`).

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
