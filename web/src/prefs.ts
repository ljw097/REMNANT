// Per-user preferences, stored server-side (PUT /me/preferences) so they
// follow the user across devices. Unknown or invalid values fall back to
// role-aware defaults, so older or hand-edited preferences never break the UI.

import type { Role } from "./api/types";
import type { Lang } from "./i18n";

export type PanelId = "waiting" | "runs" | "findings" | "recentRuns" | "projects" | "system";

export const allPanels: PanelId[] = ["waiting", "runs", "findings", "recentRuns", "projects", "system"];

export interface PanelPref {
  id: PanelId;
  visible: boolean;
}

export type ColumnSet = "runs" | "hypotheses" | "patterns";

export const columnChoices: Record<ColumnSet, string[]> = {
  runs: ["project", "progress", "tokens", "queued", "attempts"],
  hypotheses: ["run", "class", "priority", "skeptic", "evidence", "updated"],
  patterns: ["class", "cwe", "language", "source", "status"],
};

export interface Prefs {
  theme: "system" | "light" | "dark";
  density: "comfortable" | "compact";
  language: Lang;
  startPage: string;
  dashboard: PanelPref[];
  columns: Record<ColumnSet, string[]>;
}

export const startPages = ["/", "/review", "/runs", "/approvals", "/projects"] as const;

/** Reviewers see what waits for them first; researchers see runs first. */
export function defaultDashboard(role: Role): PanelPref[] {
  const order: PanelId[] =
    role === "reviewer"
      ? ["waiting", "findings", "runs", "recentRuns", "projects", "system"]
      : role === "admin"
        ? ["waiting", "runs", "system", "findings", "recentRuns", "projects"]
        : ["runs", "waiting", "recentRuns", "findings", "projects", "system"];
  return order.map((id) => ({ id, visible: id !== "system" || role === "admin" }));
}

export function defaultPrefs(role: Role, lang: Lang): Prefs {
  return {
    theme: "system",
    density: "comfortable",
    language: lang,
    startPage: role === "reviewer" ? "/review" : "/",
    dashboard: defaultDashboard(role),
    columns: { runs: [...columnChoices.runs], hypotheses: [...columnChoices.hypotheses], patterns: [...columnChoices.patterns] },
  };
}

/** Merges stored preferences over defaults, dropping anything invalid. */
export function normalizePrefs(raw: unknown, role: Role, lang: Lang): Prefs {
  const d = defaultPrefs(role, lang);
  if (!raw || typeof raw !== "object") return d;
  const r = raw as Record<string, unknown>;
  const pick = <V extends string>(v: unknown, ok: readonly V[], dflt: V): V => (ok.includes(v as V) ? (v as V) : dflt);
  const dash: PanelPref[] = [];
  if (Array.isArray(r.dashboard)) {
    for (const p of r.dashboard) {
      const id = (p as PanelPref)?.id;
      if (allPanels.includes(id) && !dash.some((x) => x.id === id)) dash.push({ id, visible: (p as PanelPref).visible !== false });
    }
  }
  for (const p of d.dashboard) if (!dash.some((x) => x.id === p.id)) dash.push(p);
  const columns = { ...d.columns };
  if (r.columns && typeof r.columns === "object") {
    for (const set of Object.keys(columnChoices) as ColumnSet[]) {
      const v = (r.columns as Record<string, unknown>)[set];
      if (Array.isArray(v)) columns[set] = v.filter((c): c is string => typeof c === "string" && columnChoices[set].includes(c));
    }
  }
  return {
    theme: pick(r.theme, ["system", "light", "dark"] as const, d.theme),
    density: pick(r.density, ["comfortable", "compact"] as const, d.density),
    language: pick(r.language, ["ko", "en"] as const, d.language),
    startPage: pick(r.startPage, startPages, d.startPage as (typeof startPages)[number]),
    dashboard: dash,
    columns,
  };
}

/** Applies theme, density, language and accent to the document. */
export function applyAppearance(p: Pick<Prefs, "theme" | "density" | "language">, accent: string): void {
  const root = document.documentElement;
  const dark = p.theme === "dark" || (p.theme === "system" && matchMedia("(prefers-color-scheme: dark)").matches);
  root.dataset.theme = dark ? "dark" : "light";
  root.dataset.density = p.density;
  root.lang = p.language;
  if (/^#[0-9a-fA-F]{6}$/.test(accent)) root.style.setProperty("--accent-base", accent);
  else root.style.removeProperty("--accent-base");
  try {
    localStorage.setItem("remnant.lang", p.language);
  } catch {
    // ignore
  }
}

/** WCAG contrast ratio of a colour against white; used to warn about unreadable accents. */
export function contrastOnWhite(hex: string): number {
  const c = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255).map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
  const l = 0.2126 * c[0]! + 0.7152 * c[1]! + 0.0722 * c[2]!;
  return 1.05 / (l + 0.05);
}
