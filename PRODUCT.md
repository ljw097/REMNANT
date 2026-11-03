# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

React + Vite (user's choice), built into static assets and embedded in the `remnantd` Go binary with `go:embed`, so the UI ships and self-updates with the server. The Go module stays the only runtime; Node is a build-time dependency of CI and release builds.

## Users

REMNANT is deployed at different scales and the interface must fit each without a separate edition (confirmed: "customizable"):

- a solo researcher running it on their own machine, who approves their own lab tests (recorded as self-approval) — the **personal** type;
- a small security research team where researchers queue runs and separate reviewers triage hypotheses and approve lab experiments — the **team** type;
- an internal service shared by several teams, where administrators gate lab approvals and accounts and manage model profiles, branding and updates — the **managed** type.

The type is chosen at first-run setup and presets the instance policy (self-approval, self-review, administrator-only lab approval, account requests, inactivity sign-out); each rule can be changed. Roles are `researcher`, `reviewer` and `admin` (admin holds every role). People sign in with a username and password; API tokens remain for scripts.

## Product Purpose

Source-first vulnerability research: turn historical root-cause patterns into falsifiable hypotheses about pinned source snapshots, back every claim with evidence, and gate any dynamic test behind an approval of an exact scope — by a second person unless the instance policy allows recorded self-approval. Success is a reviewer reaching a confident validate/reject decision quickly, with the evidence and its provenance in front of them.

## Positioning

Evidence and provenance over model confidence: every hypothesis points at hashed source excerpts of an immutable revision; a model's "confidence" is shown as a triage priority, never as proof; dynamic validation is local, network-less and requires approval (by someone other than the requester unless the policy records a self-approval), bound to a scope hash that goes stale when anything changes.

## Operating Context

- Long-running runs move through a state machine (queued → collecting → indexing → surface_mapping → hypothesis_generation → static_validation → review_required → …) and stream a live event timeline over SSE. Runs can be paused, resumed and survive restarts.
- Review is the core loop: read claim, preconditions, expected observation, evidence excerpts with line numbers, Skeptic counter-evidence; decide.
- Lab approvals show the exact scope (target revision, operation, limits, network none) and expire or go stale.
- Reports are downloadable Markdown/JSON artifacts.
- Administrators manage model profiles (keys are write-only), see the running version and install signed updates.

## Capabilities and Constraints

- The UI talks only to `/api/v1` (see `api/openapi.yaml`); it holds no credentials of its own.
- Browser sessions are HttpOnly SameSite=Strict cookies the page never reads; state-changing requests also send `X-Remnant-Request: 1`. SSE is read with `fetch()` streaming.
- Strict Content-Security-Policy (no inline script, no third-party origins); untrusted repository text and model output are rendered as text, never as HTML.
- Interface language: Korean and English, switchable by the user. API terms (`revision`, run statuses, operation names) stay recognisable in both.
- Customization (confirmed): role-aware navigation and actions; per-user preferences (theme, density, language, start page, list columns); per-user dashboard composition (show/hide/reorder cards); organization branding set by an administrator (instance name, accent colour). Preferences and branding are stored server-side.
- Administrators manage users in the UI: add (temporary password shown once), reset passwords, approve account requests, issue API tokens, change roles, disable.

## Brand Commitments

Product name REMNANT. No logo exists yet; organization branding may override the instance name and accent.

Visual direction chosen by the user (2026-11-03): the category standard for security and developer tools (sidebar, tables, detail panels), executed at the craft level of Linear, GitHub and Sentry, with situational modules applied where they fit the screen: transit-line run progress, RFC-style document headers and section/reference notation on run and report pages, chain-of-custody trails on hypotheses and approvals, one-at-a-time focused review, line-number-aligned evidence excerpts, state shown by line form as well as colour, and the accent colour reserved for actionable elements. Not wanted: hacker-film styling (black screens, neon green, terminal pastiche) and decorative animation.

## Evidence on Hand

Real API, schema and data model in the repository. No customers, testimonials, benchmarks or screenshots exist; none may be invented.

## Product Principles

1. Evidence first: a claim is only as good as the excerpt, revision and hash behind it — show them, always.
2. Make the safety boundary visible: read-only source, no network, who requested and who approved, what is stale.
3. Never present model output as proof; label it as hypothesis, priority or verdict.
4. Fit the deployment: the same build serves one researcher or many teams through roles and preferences, not editions.
5. Long work is calm: progress, pauses and recoveries are legible without babysitting.

## Accessibility & Inclusion

No product-specific requirement was established; WCAG 2.2 AA is the working baseline (keyboard operation, visible focus, contrast, reduced motion).
