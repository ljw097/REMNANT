// LLM records: every model call any agent made, across runs and repository
// analyses, newest first. Filters live in the URL so other screens can link
// here already narrowed (e.g. one run's calls, or only failures).

import { useEffect, useState } from "react";
import { Frame } from "../App";
import { get } from "../api/client";
import type { ModelCall } from "../api/types";
import { useApi } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { navigate, useLocation } from "../router";
import { Button, Empty, ErrorNote, PageHead, Panel, SkeletonRows } from "../ui/components";
import { CallTable, ModelCallDrawer, ms } from "../ui/ModelCalls";

interface Page {
  calls: ModelCall[];
  next_before: number;
  stats: {
    calls: number;
    input_tokens: number;
    output_tokens: number;
    duration_ms: number;
    by_outcome: Record<string, number>;
    by_role: Record<string, number>;
  };
}

const roles = ["hypothesis_researcher", "skeptic", "historian", "reporter"] as const;
const outcomes = ["running", "ok", "refused", "truncated", "error", "stopped"] as const;
const filterKeys = ["q", "role", "outcome", "owner", "run_id", "intake_id"] as const;

export function LlmRecordsPage() {
  const { t } = useI18n();
  const { query } = useLocation();
  const f = Object.fromEntries(filterKeys.map((k) => [k, query.get(k) ?? ""])) as Record<(typeof filterKeys)[number], string>;
  const params = new URLSearchParams(Object.entries(f).filter(([, v]) => v));
  const qs = params.toString();
  const first = useApi<Page>(`/model-calls?${qs}${qs ? "&" : ""}limit=100`, 10_000);
  const [older, setOlder] = useState<ModelCall[]>([]);
  const [next, setNext] = useState(0);
  const [loadingMore, setLoadingMore] = useState(false);
  const [open, setOpen] = useState<number | null>(null);
  const [search, setSearch] = useState(f.q);

  // A new filter starts from the newest page again.
  useEffect(() => {
    setOlder([]);
    setNext(0);
    setSearch(f.q);
  }, [qs]);
  useEffect(() => {
    if (first.data && older.length === 0) setNext(first.data.next_before);
  }, [first.data]);

  const set = (k: (typeof filterKeys)[number], v: string) => {
    const p = new URLSearchParams(params);
    if (v) p.set(k, v);
    else p.delete(k);
    const s = p.toString();
    navigate(`/llm${s ? `?${s}` : ""}`, { replace: true });
  };
  const more = async () => {
    setLoadingMore(true);
    try {
      const pg = await get<Page>(`/model-calls?${qs}${qs ? "&" : ""}limit=100&before_id=${next}`);
      setOlder((o) => [...o, ...pg.calls]);
      setNext(pg.next_before);
    } finally {
      setLoadingMore(false);
    }
  };

  const st = first.data?.stats;
  const rows = [...(first.data?.calls ?? []), ...older.filter((o) => !(first.data?.calls ?? []).some((c) => c.id === o.id))];
  const notOk = st ? st.calls - (st.by_outcome.ok ?? 0) - (st.by_outcome.running ?? 0) : 0;
  const firstNotOk = outcomes.find((o) => o !== "ok" && o !== "running" && (st?.by_outcome[o] ?? 0) > 0) ?? "";
  const filtered = qs !== "";

  return (
    <Frame crumbs={[{ label: t("nav.llm") }]}>
      <PageHead title={t("llm.title")} sub={t("llm.sub")} />
      <div className="statcards llm-stats">
        <button type="button" className="statcard" onClick={() => set("outcome", "")}>
          <span className="statcard-top">{t("llm.stat.calls")}</span>
          <span className="statcard-value">{st ? st.calls.toLocaleString() : "—"}</span>
          <span className="statcard-sub">{st ? t("llm.stat.time", { d: ms(st.duration_ms) }) : " "}</span>
        </button>
        <div className="statcard">
          <span className="statcard-top">{t("llm.stat.tokens")}</span>
          <span className="statcard-value">{st ? st.output_tokens.toLocaleString() : "—"}</span>
          <span className="statcard-sub">{st ? t("llm.stat.input", { n: st.input_tokens.toLocaleString() }) : " "}</span>
        </div>
        <button type="button" className="statcard" data-tone={notOk > 0 ? "warn" : undefined} aria-pressed={f.outcome !== "" && f.outcome !== "ok"} onClick={() => set("outcome", f.outcome && f.outcome !== "ok" ? "" : firstNotOk)}>
          <span className="statcard-top">{t("llm.stat.notOk")}</span>
          <span className="statcard-value">{st ? notOk.toLocaleString() : "—"}</span>
          <span className="statcard-sub">
            {st
              ? outcomes
                  .filter((o) => o !== "ok" && o !== "running" && (st.by_outcome[o] ?? 0) > 0)
                  .map((o) => `${t(`detail.outcome.${o}` as Key)} ${st.by_outcome[o]}`)
                  .join(" · ") || " "
              : " "}
          </span>
        </button>
        {roles.map((r) => (
          <button key={r} type="button" className="statcard" aria-pressed={f.role === r} onClick={() => set("role", f.role === r ? "" : r)}>
            <span className="statcard-top">{t(`detail.role.${r}` as Key)}</span>
            <span className="statcard-value">{st ? (st.by_role[r] ?? 0).toLocaleString() : "—"}</span>
            <span className="statcard-sub">{" "}</span>
          </button>
        ))}
      </div>

      <Panel>
        <form
          className="toolbar llm-filters"
          onSubmit={(e) => {
            e.preventDefault();
            set("q", search.trim());
          }}
        >
          <input className="input" type="search" value={search} placeholder={t("llm.search")} aria-label={t("llm.search")} onChange={(e) => setSearch(e.target.value)} />
          <select className="select" value={f.role} aria-label={t("detail.col.role")} onChange={(e) => set("role", e.target.value)}>
            <option value="">{t("llm.allRoles")}</option>
            {roles.map((r) => (
              <option key={r} value={r}>
                {t(`detail.role.${r}` as Key)}
              </option>
            ))}
          </select>
          <select className="select" value={f.outcome} aria-label={t("detail.col.outcome")} onChange={(e) => set("outcome", e.target.value)}>
            <option value="">{t("llm.allOutcomes")}</option>
            {outcomes.map((o) => (
              <option key={o} value={o}>
                {t(`detail.outcome.${o}` as Key)}
              </option>
            ))}
          </select>
          <select className="select" value={f.owner} aria-label={t("llm.owner")} onChange={(e) => set("owner", e.target.value)}>
            <option value="">{t("llm.owner.all")}</option>
            <option value="run">{t("llm.owner.run")}</option>
            <option value="intake">{t("llm.owner.intake")}</option>
          </select>
          {f.run_id ? (
            <Button size="sm" icon="x" onClick={() => set("run_id", "")}>
              {t("llm.onlyRun", { id: f.run_id })}
            </Button>
          ) : null}
          {f.intake_id ? (
            <Button size="sm" icon="x" onClick={() => set("intake_id", "")}>
              {t("llm.onlyIntake", { id: f.intake_id })}
            </Button>
          ) : null}
          {filtered ? (
            <Button size="sm" variant="ghost" onClick={() => navigate("/llm", { replace: true })}>
              {t("llm.clear")}
            </Button>
          ) : null}
        </form>
        {first.error ? (
          <ErrorNote error={first.error} retry={first.reload} />
        ) : first.loading && !first.data ? (
          <SkeletonRows rows={5} cols={7} />
        ) : rows.length === 0 ? (
          <Empty title={filtered ? t("llm.noneFiltered") : t("llm.none")} body={filtered ? undefined : t("llm.noneBody")} />
        ) : (
          <>
            <CallTable rows={rows} onOpen={setOpen} owner />
            <div className="toolbar">
              <span className="small ink-2">{t("llm.shown", { n: rows.length, total: st?.calls ?? rows.length })}</span>
              {next ? (
                <Button size="sm" loading={loadingMore} onClick={() => void more()}>
                  {t("llm.older")}
                </Button>
              ) : null}
            </div>
          </>
        )}
      </Panel>
      <ModelCallDrawer id={open} onClose={() => setOpen(null)} />
    </Frame>
  );
}
