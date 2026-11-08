// Global command palette (⌘K / ⌘J): jump to any page, run, pattern or
// project from anywhere, keyboard-first. Opened by the shortcut or the
// topbar Search button (which dispatches "remnant:command").

import { useEffect, useMemo, useRef, useState } from "react";
import { get } from "../api/client";
import type { LLMProfile, Pattern, Project, Run } from "../api/types";
import { useI18n, type Key } from "../i18n";
import { navigate } from "../router";
import { useSession } from "../session";
import { Icon, type IconName } from "./Icon";

interface Command {
  id: string;
  label: string;
  sub?: string;
  icon: IconName;
  to: string;
  group: Key;
  keywords: string;
}

const pages: { label: Key; icon: IconName; to: string; admin?: boolean }[] = [
  { label: "nav.dashboard", icon: "dashboard", to: "/" },
  { label: "nav.review", icon: "review", to: "/review" },
  { label: "nav.approvals", icon: "approvals", to: "/approvals" },
  { label: "nav.analyze", icon: "branch", to: "/analyze" },
  { label: "nav.runs", icon: "runs", to: "/runs" },
  { label: "runs.new", icon: "plus", to: "/runs/new" },
  { label: "nav.reports", icon: "file", to: "/reports" },
  { label: "nav.findings", icon: "findings", to: "/findings" },
  { label: "nav.llm", icon: "cpu", to: "/llm" },
  { label: "nav.projects", icon: "projects", to: "/projects" },
  { label: "nav.patterns", icon: "patterns", to: "/patterns" },
  { label: "settings.models", icon: "key", to: "/settings/models", admin: true },
  { label: "settings.agents", icon: "cpu", to: "/settings/agents", admin: true },
  { label: "settings.research", icon: "sliders", to: "/settings/research", admin: true },
  { label: "nav.settings", icon: "settings", to: "/settings" },
];

export function CommandPalette() {
  const { t } = useI18n();
  const { can } = useSession();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [sel, setSel] = useState(0);
  const [loaded, setLoaded] = useState(false);
  const [entities, setEntities] = useState<Command[]>([]);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  // Open on ⌘K / ⌘J / Ctrl+K, and on the topbar Search button's event.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === "k" || e.key === "j")) {
        e.preventDefault();
        setOpen((o) => !o);
      } else if (e.key === "Escape") {
        setOpen(false);
      }
    };
    const onCmd = () => setOpen(true);
    window.addEventListener("keydown", onKey);
    window.addEventListener("remnant:command", onCmd);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("remnant:command", onCmd);
    };
  }, []);

  // Load the searchable entities the first time the palette opens.
  useEffect(() => {
    if (!open) {
      setSel(0);
      return;
    }
    // Keep the last query so reopening continues it; select it so typing replaces.
    inputRef.current?.focus();
    inputRef.current?.select();
    if (loaded) return;
    setLoaded(true);
    void Promise.allSettled([
      get<Run[]>("/runs"),
      get<Pattern[]>("/patterns"),
      get<Project[]>("/projects"),
      can("admin") ? get<LLMProfile[]>("/llm-profiles") : Promise.resolve([]),
    ]).then(([runs, patterns, projects, profiles]) => {
      const out: Command[] = [];
      const projName = new Map<number, string>();
      if (projects.status === "fulfilled") {
        for (const p of projects.value) {
          projName.set(p.id, p.name);
          out.push({ id: `project-${p.id}`, label: p.name, icon: "projects", to: `/projects/${p.id}`, group: "cmd.group.projects", keywords: `project ${p.name}` });
        }
      }
      if (runs.status === "fulfilled") {
        for (const r of runs.value.slice(0, 40)) {
          out.push({
            id: `run-${r.id}`,
            label: `R-${r.id} · ${projName.get(r.project_id) ?? t("common.project", { id: r.project_id })}`,
            sub: t(`status.run.${r.status}` as Key),
            icon: "runs",
            to: `/runs/${r.id}`,
            group: "cmd.group.runs",
            keywords: `run r-${r.id} ${r.status} ${projName.get(r.project_id) ?? ""}`,
          });
        }
      }
      if (patterns.status === "fulfilled") {
        for (const p of patterns.value.slice(0, 60)) {
          out.push({
            id: `pattern-${p.id}`,
            label: p.title,
            sub: `P-${p.id} · ${p.vulnerability_class}`,
            icon: "patterns",
            to: `/patterns/${p.id}`,
            group: "cmd.group.patterns",
            keywords: `pattern p-${p.id} ${p.title} ${p.vulnerability_class} ${p.cwe_id}`,
          });
        }
      }
      if (profiles.status === "fulfilled" && Array.isArray(profiles.value)) {
        for (const p of profiles.value) {
          out.push({ id: `model-${p.id}`, label: p.name, sub: p.model, icon: "key", to: "/settings/models", group: "cmd.group.models", keywords: `model profile ${p.name} ${p.model}` });
        }
      }
      setEntities(out);
    });
  }, [open]);

  const navCommands = useMemo<Command[]>(
    () => pages.filter((p) => !p.admin || can("admin")).map((p) => ({ id: `page-${p.to}`, label: t(p.label), icon: p.icon, to: p.to, group: "cmd.group.go", keywords: `${t(p.label)} ${p.to}` })),
    [t, can],
  );

  const results = useMemo(() => {
    const needle = q.trim().toLowerCase();
    const all = [...navCommands, ...entities];
    const matched = needle === "" ? navCommands : all.filter((c) => c.keywords.toLowerCase().includes(needle) || c.label.toLowerCase().includes(needle));
    return matched.slice(0, 50);
  }, [q, navCommands, entities]);

  useEffect(() => {
    if (sel >= results.length) setSel(Math.max(0, results.length - 1));
  }, [results.length]);

  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>(`[data-i="${sel}"]`)?.scrollIntoView({ block: "nearest" });
  }, [sel]);

  if (!open) return null;

  const choose = (c: Command | undefined) => {
    if (!c) return;
    setOpen(false);
    navigate(c.to);
  };

  // Group the results in order, keeping the flat index for keyboard nav.
  let idx = -1;
  const groups: { group: Key; items: { c: Command; i: number }[] }[] = [];
  for (const c of results) {
    idx++;
    const g = groups.find((x) => x.group === c.group);
    const entry = { c, i: idx };
    if (g) g.items.push(entry);
    else groups.push({ group: c.group, items: [entry] });
  }

  return (
    <div className="cmdk-backdrop" role="presentation" onMouseDown={() => setOpen(false)}>
      <div className="cmdk" role="dialog" aria-modal="true" aria-label={t("cmd.title")} onMouseDown={(e) => e.stopPropagation()}>
        <div className="cmdk-input">
          <Icon name="search" size={18} />
          <input
            ref={inputRef}
            value={q}
            placeholder={t("cmd.placeholder")}
            aria-label={t("cmd.title")}
            spellCheck={false}
            autoComplete="off"
            onChange={(e) => {
              setQ(e.target.value);
              setSel(0);
            }}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown") {
                e.preventDefault();
                setSel((s) => Math.min(results.length - 1, s + 1));
              } else if (e.key === "ArrowUp") {
                e.preventDefault();
                setSel((s) => Math.max(0, s - 1));
              } else if (e.key === "Enter") {
                e.preventDefault();
                choose(results[sel]);
              } else if (e.key === "Escape") {
                // Close without the browser clearing the input first.
                e.preventDefault();
                setOpen(false);
              }
            }}
          />
          <kbd>Esc</kbd>
        </div>
        <div className="cmdk-list" ref={listRef}>
          {results.length === 0 ? (
            <p className="cmdk-empty">{t("cmd.empty")}</p>
          ) : (
            groups.map((g) => (
              <div className="cmdk-group" key={g.group}>
                <div className="cmdk-group-label">{t(g.group)}</div>
                {g.items.map(({ c, i }) => (
                  <button key={c.id} type="button" data-i={i} className="cmdk-item" aria-selected={i === sel} onMouseEnter={() => setSel(i)} onClick={() => choose(c)}>
                    <Icon name={c.icon} size={16} />
                    <span className="cmdk-label">{c.label}</span>
                    {c.sub ? <span className="cmdk-sub">{c.sub}</span> : null}
                  </button>
                ))}
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
