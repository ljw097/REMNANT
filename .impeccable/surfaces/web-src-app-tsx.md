---
version: 1
slug: "web-src-app-tsx"
primary_target: "web/src/App.tsx"
related_targets: []
---

# REMNANT web UI

Scope: the whole authenticated web app (sign-in, dashboard, projects/targets, knowledge base, runs, hypothesis review, approvals, findings, settings). Mode: Operate.

Audience and task: researchers queue and watch runs; reviewers triage hypotheses and decide lab approvals; admins manage models, branding and updates. Desktop and laptop for review, phone for approvals and run status. Quality bar: Linear, GitHub, Sentry.

Constraints: React + Vite embedded in remnantd; strict CSP (self only, no inline script); untrusted text rendered as text; Korean/English; role-aware, per-user preferences and dashboard, admin branding.

## Direction contract

THESIS: The category standard done precisely, plus one specialised instrument per situation; it refuses the generic SaaS card wall and the hacker-terminal pastiche.

OWN-WORLD: Cool neutral paper and ink with a second, cooler panel layer; one brandable accent appearing only on actionable controls; semantic state colours always paired with line form (solid live, dashed waiting, struck stopped); system sans for everything, monospace only for hashes, revisions and code; 4px corner radius, 1px rules, one 12-column grid.

STORY: A reviewer opens the app, sees what waits for them (triage, approvals), reads a hypothesis with its evidence and custody trail, decides in one keystroke, and moves to the next; a researcher sees every run as a line of stations and knows where each stands.

FIRST VIEWPORT: Left sidebar with role-aware sections and counts; dashboard of user-arranged panels led by "Waiting for you" (triage and approvals) and "Runs in motion" drawn as transit lines; version/update and instance name in the sidebar footer and header.

FORM: Category standard (canon), chosen by the user, with situational modules from the round; seed 7757c538.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
