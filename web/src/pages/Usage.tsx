// Token usage at a glance: how many tokens the agents used today and over a
// period, per day, per model, per agent and per run or analysis, and what
// that costs once an administrator enters model prices. Read from the LLM
// call log, so it covers calls since that log exists.

import { useEffect, useMemo, useState } from "react";
import { Frame } from "../App";
import { put } from "../api/client";
import { useApi } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { Link } from "../router";
import { useSession } from "../session";
import { Alert, Button, Empty, ErrorNote, errorText, PageHead, Panel, Ref, Segmented, SkeletonRows, useToast } from "../ui/components";
import { ms, RoleName } from "../ui/ModelCalls";

interface Totals {
  calls: number;
  input_tokens: number;
  output_tokens: number;
  duration_ms: number;
  not_answered: number;
  cost: number | null;
}
interface Row extends Totals {
  key: string;
  label?: string;
  run_id?: number;
  intake_id?: number;
}
interface UsageData {
  totals: Totals;
  today: Totals;
  by_day: Row[];
  by_model: Row[];
  by_role: Row[];
  by_source: Row[];
  unpriced: string[];
}
type Price = { input_per_mtok: number; output_per_mtok: number };

const periods = ["1", "7", "30", "90", "0"] as const;
type Period = (typeof periods)[number];
const tz = (() => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
})();

const n = (v: number) => v.toLocaleString();
/** Compact token counts for tiles: 1.2M, 340K. */
const compact = (v: number) => (v >= 1e6 ? `${(v / 1e6).toFixed(v >= 1e7 ? 0 : 1)}M` : v >= 1e4 ? `${Math.round(v / 1e3)}K` : n(v));
const money = (v: number | null) => (v === null ? null : v < 0.01 && v > 0 ? "<$0.01" : `$${v.toFixed(2)}`);

export function UsagePage() {
  const { t } = useI18n();
  const { can } = useSession();
  const [period, setPeriod] = useState<Period>(() => {
    try {
      return (localStorage.getItem("remnant.usage.period") as Period) || "7";
    } catch {
      return "7";
    }
  });
  useEffect(() => {
    try {
      localStorage.setItem("remnant.usage.period", period);
    } catch {
      // per-viewer convenience only
    }
  }, [period]);
  const q = useApi<UsageData>(`/usage?days=${period}&tz=${encodeURIComponent(tz)}`, 30_000);
  const u = q.data;
  const costLine = (c: number | null) => money(c) ?? t("usage.noPrice");

  return (
    <Frame crumbs={[{ label: t("nav.usage") }]}>
      <PageHead
        title={t("usage.title")}
        sub={t("usage.sub")}
        actions={
          <Segmented<Period>
            label={t("usage.period")}
            value={period}
            onChange={setPeriod}
            options={periods.map((p) => ({ value: p, label: t(`usage.period.${p}` as Key) }))}
          />
        }
      />
      {q.error ? <ErrorNote error={q.error} retry={q.reload} /> : null}
      {!u ? (
        <SkeletonRows rows={4} cols={4} />
      ) : (
        <div className="stack" style={{ gap: 16 }}>
          <div className="statcards">
            <div className="statcard">
              <span className="statcard-top">{t("usage.today")}</span>
              <span className="statcard-value">{compact(u.today.output_tokens)}</span>
              <span className="statcard-sub">{t("usage.todaySub", { i: compact(u.today.input_tokens), c: n(u.today.calls) })}</span>
            </div>
            <div className="statcard">
              <span className="statcard-top">{t("usage.output")}</span>
              <span className="statcard-value">{compact(u.totals.output_tokens)}</span>
              <span className="statcard-sub">{n(u.totals.output_tokens)}</span>
            </div>
            <div className="statcard">
              <span className="statcard-top">{t("usage.input")}</span>
              <span className="statcard-value">{compact(u.totals.input_tokens)}</span>
              <span className="statcard-sub">{n(u.totals.input_tokens)}</span>
            </div>
            <Link to="/llm" className="statcard" data-tone={u.totals.not_answered > 0 ? "warn" : undefined}>
              <span className="statcard-top">{t("usage.calls")}</span>
              <span className="statcard-value">{n(u.totals.calls)}</span>
              <span className="statcard-sub">{t("usage.callsSub", { f: n(u.totals.not_answered), d: ms(u.totals.duration_ms) })}</span>
            </Link>
            <div className="statcard">
              <span className="statcard-top">{t("usage.cost")}</span>
              <span className="statcard-value">{money(u.totals.cost) ?? "—"}</span>
              <span className="statcard-sub">{u.totals.cost === null ? t("usage.costUnknown", { n: u.unpriced.length }) : t("usage.costEstimate")}</span>
            </div>
          </div>

          {u.totals.calls === 0 ? (
            <Panel>
              <Empty title={t("usage.none")} body={t("usage.noneBody")} />
            </Panel>
          ) : (
            <>
              <Panel title={t("usage.perDay")} meta={t("usage.perDaySub")}>
                <DailyBars rows={u.by_day} costLine={costLine} />
              </Panel>
              <div className="grid-12">
                <div className="span-6 stack">
                  <Panel title={t("usage.byModel")}>
                    <UsageTable rows={u.by_model} first={(r) => <span className="mono small">{r.key || "—"}</span>} costLine={costLine} />
                  </Panel>
                </div>
                <div className="span-6 stack">
                  <Panel title={t("usage.byAgent")}>
                    <UsageTable rows={u.by_role} first={(r) => <RoleName role={r.key} />} costLine={costLine} />
                  </Panel>
                </div>
              </div>
              <Panel title={t("usage.bySource")} meta={t("usage.bySourceSub")}>
                <UsageTable
                  rows={u.by_source}
                  first={(r) => (
                    <span className="row" style={{ gap: 6, flexWrap: "nowrap", minWidth: 0 }}>
                      {r.run_id ? <Ref kind="R" id={r.run_id} to={`/runs/${r.run_id}`} /> : <Link to={`/analyze/${r.intake_id}`} className="ref">[I-{r.intake_id}]</Link>}
                      <span className="detail-subject">{r.label?.replace(/^https:\/\//, "")}</span>
                    </span>
                  )}
                  costLine={costLine}
                  link={(r) => (r.run_id ? `/llm?run_id=${r.run_id}` : `/llm?intake_id=${r.intake_id}`)}
                />
              </Panel>
            </>
          )}
          {can("admin") ? <PriceEditor seen={u.by_model.map((r) => r.key)} unpriced={u.unpriced} onSaved={q.reload} /> : u.unpriced.length ? <p className="small muted">{t("usage.askAdmin")}</p> : null}
        </div>
      )}
    </Frame>
  );
}

/** Output tokens per day: one series, so no legend; hover or focus a bar for its numbers. */
function DailyBars({ rows, costLine }: { rows: Row[]; costLine: (c: number | null) => string }) {
  const { t } = useI18n();
  const [hover, setHover] = useState<number | null>(null);
  const [table, setTable] = useState(false);
  const max = Math.max(1, ...rows.map((r) => r.output_tokens));
  const W = 720, H = 180, pad = 28;
  const bw = Math.max(4, Math.min(36, (W - pad) / Math.max(rows.length, 1) - 2));
  const h = hover === null ? null : rows[hover];
  if (table) {
    return (
      <>
        <div className="toolbar">
          <Button size="sm" variant="ghost" onClick={() => setTable(false)}>{t("usage.showChart")}</Button>
        </div>
        <UsageTable rows={rows} first={(r) => <span className="mono small">{r.key}</span>} costLine={costLine} />
      </>
    );
  }
  return (
    <div className="usage-chart">
      <div className="toolbar">
        <span className="small ink-2">{h ? t("usage.tip", { d: h.key, o: n(h.output_tokens), i: n(h.input_tokens), c: n(h.calls), cost: costLine(h.cost) }) : t("usage.hoverHint")}</span>
        <Button size="sm" variant="ghost" onClick={() => setTable(true)}>{t("usage.showTable")}</Button>
      </div>
      <svg viewBox={`0 0 ${W} ${H + 22}`} role="img" aria-label={t("usage.chartLabel")} preserveAspectRatio="xMinYMid meet">
        <line x1={pad} y1={H} x2={W} y2={H} className="usage-axis" />
        <text x={pad - 6} y={12} className="usage-tick" textAnchor="end">{compact(max)}</text>
        <text x={pad - 6} y={H} className="usage-tick" textAnchor="end">0</text>
        {rows.map((r, i) => {
          const x = pad + 2 + i * (bw + 2);
          const bh = Math.max(r.output_tokens > 0 ? 2 : 0, (r.output_tokens / max) * (H - 16));
          return (
            <g key={r.key} onMouseEnter={() => setHover(i)} onMouseLeave={() => setHover(null)} onFocus={() => setHover(i)} onBlur={() => setHover(null)} tabIndex={0} aria-label={t("usage.tip", { d: r.key, o: n(r.output_tokens), i: n(r.input_tokens), c: n(r.calls), cost: costLine(r.cost) })}>
              <rect x={x - 1} y={0} width={bw + 2} height={H} className="usage-hit" />
              <path d={roundedTop(x, H - bh, bw, bh, Math.min(4, bw / 2, bh))} className="usage-bar" data-active={hover === i || undefined} />
              {rows.length <= 14 || i === 0 || i === rows.length - 1 ? (
                <text x={x + bw / 2} y={H + 15} className="usage-tick" textAnchor="middle">{r.key.slice(5)}</text>
              ) : null}
            </g>
          );
        })}
      </svg>
    </div>
  );
}

/** A bar anchored to the baseline with a rounded data end. */
function roundedTop(x: number, y: number, w: number, h: number, r: number): string {
  if (h <= 0) return "";
  return `M${x},${y + h}V${y + r}Q${x},${y} ${x + r},${y}H${x + w - r}Q${x + w},${y} ${x + w},${y + r}V${y + h}Z`;
}

function UsageTable({ rows, first, costLine, link }: { rows: Row[]; first: (r: Row) => React.ReactNode; costLine: (c: number | null) => string; link?: (r: Row) => string }) {
  const { t } = useI18n();
  if (rows.length === 0) return <Empty title={t("usage.none")} />;
  return (
    <div className="table-wrap">
      <table className="table detail-table">
        <thead>
          <tr>
            <th />
            <th className="num">{t("usage.col.calls")}</th>
            <th className="num">{t("usage.col.input")}</th>
            <th className="num">{t("usage.col.output")}</th>
            <th className="num">{t("usage.col.cost")}</th>
            {link ? <th /> : null}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.key}>
              <td>{first(r)}</td>
              <td className="num small">{n(r.calls)}</td>
              <td className="num small">{n(r.input_tokens)}</td>
              <td className="num small">{n(r.output_tokens)}</td>
              <td className="num small">{costLine(r.cost)}</td>
              {link ? (
                <td className="small">
                  <Link to={link(r)}>{t("usage.records")}</Link>
                </td>
              ) : null}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** Administrators enter each model's price per million tokens; costs are estimates from these. */
function PriceEditor({ seen, unpriced, onSaved }: { seen: string[]; unpriced: string[]; onSaved: () => void }) {
  const { t } = useI18n();
  const toast = useToast();
  const q = useApi<Record<string, Price>>("/settings/model-prices");
  const [draft, setDraft] = useState<Record<string, { i: string; o: string }>>({});
  const [extra, setExtra] = useState("");
  const [busy, setBusy] = useState(false);
  const models = useMemo(() => [...new Set([...seen, ...Object.keys(q.data ?? {}), ...Object.keys(draft)])].filter(Boolean).sort(), [seen, q.data, draft]);
  useEffect(() => {
    if (!q.data) return;
    const d: Record<string, { i: string; o: string }> = {};
    for (const [m, p] of Object.entries(q.data)) d[m] = { i: String(p.input_per_mtok), o: String(p.output_per_mtok) };
    setDraft(d);
  }, [q.data]);
  const value = (m: string) => draft[m] ?? { i: "", o: "" };
  const save = async () => {
    setBusy(true);
    try {
      const body: Record<string, Price> = {};
      for (const m of models) {
        const v = value(m);
        if (v.i.trim() === "" && v.o.trim() === "") continue;
        body[m] = { input_per_mtok: Number(v.i) || 0, output_per_mtok: Number(v.o) || 0 };
      }
      await put("/settings/model-prices", body);
      toast(t("usage.pricesSaved"));
      q.reload();
      onSaved();
    } catch (e) {
      toast(errorText(e, t), "bad");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Panel title={t("usage.prices")} meta={t("usage.pricesSub")}>
      {unpriced.length ? (
        <div className="panel-body" style={{ paddingBottom: 0 }}>
          <Alert icon="info">{t("usage.unpriced", { models: unpriced.join(", ") })}</Alert>
        </div>
      ) : null}
      <div className="table-wrap">
        <table className="table detail-table">
          <thead>
            <tr>
              <th>{t("detail.call.model")}</th>
              <th>{t("usage.priceIn")}</th>
              <th>{t("usage.priceOut")}</th>
            </tr>
          </thead>
          <tbody>
            {models.map((m) => (
              <tr key={m}>
                <td className="mono small">{m}</td>
                {(["i", "o"] as const).map((k) => (
                  <td key={k}>
                    <input
                      className="input price-input"
                      type="number"
                      min={0}
                      max={10000}
                      step="0.01"
                      inputMode="decimal"
                      aria-label={`${m} ${k === "i" ? t("usage.priceIn") : t("usage.priceOut")}`}
                      value={value(m)[k]}
                      placeholder="—"
                      onChange={(e) => setDraft((d) => ({ ...d, [m]: { ...value(m), [k]: e.target.value } }))}
                    />
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <form
        className="toolbar"
        onSubmit={(e) => {
          e.preventDefault();
          const m = extra.trim();
          if (m) setDraft((d) => ({ ...d, [m]: d[m] ?? { i: "", o: "" } }));
          setExtra("");
        }}
      >
        <input className="input mono" style={{ maxWidth: 260 }} value={extra} placeholder={t("usage.addModel")} aria-label={t("usage.addModel")} onChange={(e) => setExtra(e.target.value)} />
        <Button type="submit" size="sm" icon="plus" disabled={!extra.trim()}>{t("usage.add")}</Button>
        <span className="spacer" />
        <Button variant="primary" size="sm" loading={busy} onClick={() => void save()}>{t("common.save")}</Button>
      </form>
    </Panel>
  );
}
