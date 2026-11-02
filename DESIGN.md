---
name: REMNANT
description: Source-first vulnerability research console; evidence and provenance over model confidence.
colors:
  accent: "#1f3fbf"
  accent-ink: "#ffffff"
  paper: "#f6f7f9"
  surface: "#ffffff"
  panel: "#eef0f4"
  panel-hover: "#e4e7ed"
  ink: "#14171c"
  ink-2: "#474e5c"
  ink-3: "#626a7a"
  rule: "#e1e4ea"
  rule-strong: "#c9ced7"
  ok: "#157f3d"
  warn: "#9a5800"
  bad: "#b42318"
  info: "#1d5fbf"
  muted: "#626a7a"
  code-bg: "#f3f5f8"
  code-mark: "#fff3c4"
  accent-ink-dark: "#0b0d12"
  paper-dark: "#0e1015"
  surface-dark: "#14171d"
  panel-dark: "#101318"
  panel-hover-dark: "#1b1f27"
  ink-dark: "#e8ebf1"
  ink-2-dark: "#b2b9c6"
  ink-3-dark: "#8e96a5"
  rule-dark: "#252a33"
  rule-strong-dark: "#343a45"
  ok-dark: "#56c785"
  warn-dark: "#e6a640"
  bad-dark: "#f2756d"
  info-dark: "#72a8ff"
  code-bg-dark: "#10131a"
  code-mark-dark: "#3a3214"
typography:
  headline:
    fontFamily: "system-ui, -apple-system, Segoe UI, Noto Sans KR, Apple SD Gothic Neo, Malgun Gothic, Roboto, sans-serif"
    fontSize: "22px"
    fontWeight: 600
    lineHeight: 1.25
    letterSpacing: "-0.01em"
  title:
    fontFamily: "system-ui, -apple-system, Segoe UI, Noto Sans KR, Apple SD Gothic Neo, Malgun Gothic, Roboto, sans-serif"
    fontSize: "18px"
    fontWeight: 600
    lineHeight: 1.3
    letterSpacing: "-0.01em"
  subtitle:
    fontFamily: "system-ui, -apple-system, Segoe UI, Noto Sans KR, Apple SD Gothic Neo, Malgun Gothic, Roboto, sans-serif"
    fontSize: "16px"
    fontWeight: 600
    lineHeight: 1.35
    letterSpacing: "-0.01em"
  body:
    fontFamily: "system-ui, -apple-system, Segoe UI, Noto Sans KR, Apple SD Gothic Neo, Malgun Gothic, Roboto, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.5
  body-small:
    fontFamily: "system-ui, -apple-system, Segoe UI, Noto Sans KR, Apple SD Gothic Neo, Malgun Gothic, Roboto, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.5
  label:
    fontFamily: "system-ui, -apple-system, Segoe UI, Noto Sans KR, Apple SD Gothic Neo, Malgun Gothic, Roboto, sans-serif"
    fontSize: "12px"
    fontWeight: 600
    lineHeight: 1.4
  control:
    fontFamily: "system-ui, -apple-system, Segoe UI, Noto Sans KR, Apple SD Gothic Neo, Malgun Gothic, Roboto, sans-serif"
    fontSize: "13px"
    fontWeight: 550
    lineHeight: 1
  mono:
    fontFamily: "ui-monospace, SFMono-Regular, Cascadia Mono, JetBrains Mono, Noto Sans Mono, Menlo, Consolas, monospace"
    fontSize: "12.5px"
    fontWeight: 400
    lineHeight: 1.6
rounded:
  hairline: "3px"
  control: "4px"
  container: "6px"
  bar: "8px"
  pill: "11px"
  round: "50%"
spacing:
  gap-compact: "8px"
  gap: "12px"
  pad: "16px"
  page: "24px"
  row-compact: "32px"
  row: "40px"
  sidebar: "240px"
  content-max: "1440px"
components:
  button:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.control}"
    rounded: "{rounded.control}"
    padding: "0 12px"
    height: "32px"
  button-hover:
    backgroundColor: "{colors.panel-hover}"
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.accent-ink}"
    typography: "{typography.control}"
    rounded: "{rounded.control}"
    padding: "0 12px"
    height: "32px"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.ink-2}"
    rounded: "{rounded.control}"
    height: "32px"
  button-danger:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.bad}"
    rounded: "{rounded.control}"
    height: "32px"
  button-sm:
    padding: "0 8px"
    height: "26px"
  button-lg:
    padding: "0 16px"
    height: "38px"
  input:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "6px 10px"
    height: "34px"
  state-chip:
    typography: "{typography.label}"
    rounded: "{rounded.pill}"
    padding: "0 8px"
    height: "22px"
  panel:
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.container}"
    padding: "16px"
  table-row:
    typography: "{typography.body}"
    padding: "6px 12px"
    height: "40px"
  nav-item:
    textColor: "{colors.ink-2}"
    rounded: "{rounded.control}"
    padding: "4px 10px"
    height: "32px"
  nav-item-active:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
  sidebar:
    backgroundColor: "{colors.panel}"
    width: "240px"
  drawer:
    backgroundColor: "{colors.surface}"
    width: "560px"
---

# Design System: REMNANT

## Overview

**Creative North Star: "The Case File"**

REMNANT is the category standard for security and developer tools (left sidebar, tables, detail panels) executed precisely, with one specialised instrument per situation: a transit line for run progress, an RFC-style document header on runs and reports, a chain-of-custody trail on hypotheses and approvals, and line-numbered evidence excerpts. Every surface reads like a page of a well-kept case file: cool neutral paper and ink, quiet rules, numbered sections, references in brackets, hashes beside every claim.

The world is cool neutral paper and ink with a second, cooler panel layer for chrome (sidebar, sign-in side). Density is product-grade and user-switchable (comfortable or compact). Colour is scarce and functional: the brandable accent marks what you can act on, and semantic state colours always arrive paired with a line form, so nothing depends on colour alone. Motion is calm and short; nothing animates for decoration. Light and dark are equal citizens, both shipped and both derived from the same tokens.

Confirmed rejections: the generic SaaS card wall, and hacker-film styling (black screens, neon green, terminal pastiche).

**Key Characteristics:**
- System sans for everything; monospace only for hashes, revisions, references, paths and code.
- One brandable accent, reserved for actionable controls; progress and state use the info blue.
- State = tone + form: solid (settled), dashed (waiting on a person or queue), pulsing dot (live), struck through (stopped).
- 1px rules and tonal layers carry structure; shadows are reserved for things that float.
- One 12-column grid; panels span 12, 8/4, 6/6 or 4/4/4 and collapse to full width below 1100px.

## Colors

A cool, near-achromatic paper-and-ink palette with one brandable blue accent and four semantic tones; every value has a tuned dark twin.

### Primary
- **Case Blue** (accent): the brandable accent, overridden per organization through `--accent-base`. Fills primary buttons, focus rings, focused-input borders, selected mode cards, checkbox fills and in-text action links. In dark theme it is lifted toward white (`color-mix(in oklch, accent-base 70%, white)`) with dark ink on top. Hover darkens in light theme and lightens in dark.
- **Accent Ink** (accent-ink / accent-ink-dark): text and icons on accent fills.

### Neutral
- **Cool Paper** (paper): the page ground behind panels.
- **Sheet White** (surface): panels, tables, inputs, drawers, the active nav item.
- **Ledger Grey** (panel): the cooler chrome layer: sidebar, sign-in side, segmented-control tracks, skeleton base. Panel-hover is its pressed/hover step.
- **Ink, Ink 2, Ink 3** (ink, ink-2, ink-3): primary text; secondary text and default nav/ghost text; metadata, hints, table headers, line numbers.
- **Rule and Strong Rule** (rule, rule-strong): 1px dividers and panel borders; control borders, inactive transit track, scrollbar thumb.
- **Code Ground and Marker** (code-bg, code-mark): evidence excerpt ground and the highlighter-yellow band on marked lines; code-bg also backs bracketed references.

### Semantic tones
- **Settled Green** (ok): completed, validated, approved, ready; the safety-boundary icons.
- **Amber** (warn): paused, pending approval, validating, timed out, quarantined.
- **Signal Red** (bad): failed, denied, findings, destructive buttons, field errors.
- **Progress Blue** (info): run progress, the transit line, in-flight states, attention counts in the sidebar, the default alert tone.
- **Muted** (muted): draft, rejected, cancelled, duplicate, expired.

### Named Rules
**The Actionable Accent Rule.** The accent appears only on things you can act on (primary buttons, links that do something, focus, the selected choice in a picker). Progress, state and attention counts use Progress Blue (info), never the accent, so an organization's brand colour never reads as a status.

**The Two-Signal Rule.** No state is expressed by colour alone. Every status comes from the status vocabulary as a tone plus a form, and the form must be legible in greyscale.

**The Neutral Translucency Rule.** Translucent bars and overlays (top bar, decision bar, focus halos, selection, tinted borders) mix in `srgb` against `transparent`, never `oklch`, so they stay neutral instead of drifting in hue. Tinted fills against an opaque surface (state chip ground, alert ground, row hover, danger hover) mix in `oklch`.

## Typography

**Body Font:** system-ui (with -apple-system, Segoe UI, Noto Sans KR, Apple SD Gothic Neo, Malgun Gothic, Roboto, sans-serif)
**Label/Mono Font:** ui-monospace (with SFMono-Regular, Cascadia Mono, JetBrains Mono, Noto Sans Mono, Menlo, Consolas, monospace)

**Character:** One native sans in every weight the product needs, Korean-capable from the stack down, with monospace as a precise instrument for anything that is an identifier rather than prose.

### Hierarchy
- **Headline** (600, 22px, 1.25, -0.01em): page titles and document-header titles; the dashboard waiting counts use the same size at 650 with tabular figures.
- **Title** (600, 18px, 1.3): section headings, including numbered sections (`§1 Evidence`, section number in mono Ink 3).
- **Subtitle** (600, 16px, 1.35): sub-sections, empty-state titles, the review claim at body weight with 1.6 leading.
- **Body** (400, 14px, 1.5): default text and table cells; prose capped at 72ch.
- **Body Small** (13px): metadata lines, alerts, panel feet, custody detail.
- **Label** (600, 12px): field labels inside document headers, table headers, nav group headings, state chips. Sentence case; no uppercase tracking.
- **Control** (550, 13px, line-height 1): buttons, tabs (14px), segmented controls.
- **Mono** (400, 12.5px, 1.6): evidence excerpts; hashes, refs and section numbers at 0.9-0.92em of their context.

Compact density shifts the whole ramp down one step (11/12/13/15/17/20px) and tightens rows and padding.

### Named Rules
**The Identifier Rule.** Monospace marks identity, not mood: hashes, revisions, `[H-4]`-style references, file paths, operation names, document numbers and code. Never headings or prose.

**The Tabular Rule.** Tables, counts, timestamps and the waiting-count numerals use tabular figures.

## Layout

A fixed 240px sidebar beside a fluid main column; content padding 24px, max width 1440px. A sticky 52px top bar holds breadcrumbs and the theme/language/update controls. Pages compose on one 12-column grid with a 16px gap; dashboard panels are user-sized as wide (12), half (6) or third (4), and detail pages use a main column plus a 340px side rail (custody, safety boundary). Everything collapses to one column below 1100px (grid) or 1180px (review rail).

Below 860px the sidebar becomes an off-canvas sheet (min(300px, 86vw)) over a scrim, settings navigation becomes a horizontally scrolling strip, and the sign-in side panel is hidden. Below 640px the transit line names only the current station and two-column forms stack.

Spacing rhythm: 4px base; 8 / 12 / 16 / 24px are the working steps. `--pad` (16, compact 12) is the panel inset; `--gap` (12, compact 8) the in-component gap; `--row` (40, compact 32) the table row height.

## Elevation & Depth

Structure is flat and tonal: panels sit on paper with a 1px rule, chrome sits on the cooler panel layer. Shadows exist only for surfaces that float above the page: the sticky decision bar, popover menus, the drawer, toasts, and the off-canvas sidebar. Sticky bars are translucent with a background blur rather than shadowed.

### Shadow Vocabulary
- **Float** (`--shadow`; light `0 1px 2px rgb(16 20 28 / 0.06), 0 4px 16px rgb(16 20 28 / 0.08)`): decision bar, column-picker menu.
- **Lift** (`--shadow-lift`; light `0 2px 6px rgb(16 20 28 / 0.08), 0 16px 40px rgb(16 20 28 / 0.14)`): drawer, toasts, mobile sidebar. Dark theme deepens both to black-based values.

### Named Rules
**The Float-Only Shadow Rule.** If it does not overlap other content, it gets a rule, not a shadow.

## Shapes

Small, even corners: 4px for controls and nav items, 6px for containers (panels, document headers, excerpts, alerts, toasts), 8px for the floating decision bar, 3px for refs, kbd and hash buttons, and a full pill (11px) only for state chips. Borders are 1px everywhere; the transit track is 3px (2px mini). Line style is semantic: dashed means waiting, struck means stopped. Circles are stations and seals; the one square-ish station (4px radius) marks the lab-validation interchange.

## Components

### Buttons
Restrained and exact; colour only when the button is the main act.
- **Shape:** 4px corners, 32px tall (26 small, 38 large), 1px strong-rule border.
- **Default:** Sheet White ground, Ink text, Control type; hover steps to panel-hover; active nudges down 0.5px.
- **Primary:** accent ground and border, accent-ink text; one per decision point.
- **Danger:** red text with a red-tinted border on white; arms in place (ConfirmButton) and becomes the destructive variant for 4 seconds before it commits.
- **Ghost:** transparent, Ink 2; icon-only buttons are square at the button height.
- **Focus:** 2px accent outline, 2px offset. Shortcut hints sit inside the button as kbd keys.

### State chips
- **Style:** 22px pill, Label type, 6px leading dot, 1px border at 55% tone, 8% tone ground.
- **Forms:** solid (filled dot); waiting (dashed border, hollow ring dot, no ground) for anything waiting on a person or the queue: to triage, needs review, pending approval, queued; live (pulsing dot) for active stages; stopped (strike-through, dash marker, no ground) for terminal non-success.
- Status mapping lives in one vocabulary (run, hypothesis, approval, experiment, target, pattern); new statuses get a tone and a form there, never ad-hoc colours.

### Cards / Containers
- **Corner Style:** 6px.
- **Background:** Sheet White on Cool Paper.
- **Shadow Strategy:** none (see Elevation).
- **Border:** 1px rule; header and footer separated by 1px rules.
- **Internal Padding:** 12px x `--pad` header, `--pad` body.

### Inputs / Fields
- **Style:** 34px min height, 1px strong-rule border, white ground, 4px corners; label above in 13px/600, hint or error below in 12px.
- **Focus:** accent border plus a 3px accent halo at 22% (srgb).
- **Error / Disabled:** red border via aria-invalid; disabled controls drop to 50% opacity.

### Navigation
Sidebar on the panel layer, grouped (Work, Library, Manage) under 12px/600 Ink 3 headings. Items are 32px, Ink 2 with a 16px icon; hover fills panel-hover; the current page is a Sheet White tile with an inset 1px rule. Counts are small tabular pills; counts that need attention fill with Progress Blue. Tabs underline the selected tab in Ink (2px), not the accent.

### Transit Stage Line (signature)
A run is a line of stations from queued to completed. Reached track and stations fill Progress Blue; the current station is hollow with a soft halo; a station awaiting review is dashed; a paused segment is dashed amber; a failed segment and station are red. Lab validation is an interchange (rounded square). A mini variant (9px stations, 2px track, no names) sits in run lists and dashboard rows.

### Document Header (signature)
RFC-style: a masthead row with the mono document number (`[R-2]`, `RUN-…`), the title, its state chip and actions, then a ruled definition grid of fields (label over value) that auto-fits at 180px minimum.

### Evidence Excerpt (signature)
A figure with a white caption bar (mono path and line range, symbol, truncated sha256 with copy) over a code-ground block with a right-aligned 6ch line-number gutter split by a 1px rule; marked lines carry the highlighter band and bold numbers.

### Custody Trail (signature)
A vertical chain of 19px circular seals joined by a 1px strong rule. Seals take ok, warn, bad or Progress Blue tones; a pending link is dashed in both seal and rule.

### Drawer
A right side sheet on a native dialog, 560px wide, Lift shadow over a 32% scrim, head / scrolling body / right-aligned action foot.

## Do's and Don'ts

### Do:
- **Do** pick every status look from the status vocabulary (tone + form); use the dashed waiting form for anything waiting on a human.
- **Do** keep the accent on actionable controls and use Progress Blue (`--info`) for progress, live state and attention counts.
- **Do** mix translucent bars and halos in `srgb` against transparent; mix opaque tints in `oklch`.
- **Do** show identifiers (hashes, revisions, refs, paths) in monospace with a copy affordance for hashes.
- **Do** separate structure with 1px rules and the panel layer; keep containers at 6px and controls at 4px.
- **Do** verify both themes and both densities; every colour token has a dark twin.

### Don't:
- **Don't** fill state, progress or counts with the accent; a brand colour must never read as a status.
- **Don't** express a state by colour alone or invent per-page status colours.
- **Don't** add shadows to panels or anything that does not float.
- **Don't** use monospace or uppercase-tracked labels for headings or decoration.
- **Don't** reach for hacker-film styling: black-and-neon screens, terminal pastiche, decorative animation.
