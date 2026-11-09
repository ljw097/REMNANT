// What is running right now, from anywhere: the top bar shows a live count,
// and the panel lists model calls in flight (with their elapsed time),
// unfinished runs (stage, token budget, hypotheses so far, whether a worker
// holds them) and repository analyses still fetching or mining.

import { useEffect, useState } from "react";
import type { ModelCall, RunStatus } from "../api/types";
import { useApi } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { Link } from "../router";
import { Button, Drawer, Empty, Panel, Ref, State } from "./components";
import { ModelCallDrawer, ms, OwnerRef, RoleName } from "./ModelCalls";
import { runLook } from "./status";

interface ActiveRun {
  id: number;
  status: RunStatus;
  project: string;
  auto: boolean;
  auto_discover: boolean;
  output_tokens: number;
  input_tokens: number;
  max_tokens: number;
  working: boolean;
  hypotheses: number;
  created_at: string;
}
interface ActiveIntake {
  id: number;
  status: "fetching" | "mining";
  source_uri: string;
  commits_found: number;
  commits_read: number;
  created_at: string;
}
interface Activity {
  runs: ActiveRun[];
  intakes: ActiveIntake[];
  calls: ModelCall[];
  now: string;
}

/** A clock that ticks once a second while `on`, in the server's time. */
function useServerNow(serverNow: string | undefined, on: boolean): number {
  const [offset, setOffset] = useState(0);
  const [, tick] = useState(0);
  useEffect(() => {
    if (serverNow) setOffset(Date.parse(serverNow) - Date.now());
  }, [serverNow]);
  useEffect(() => {
    if (!on) return;
    const id = setInterval(() => tick((n) => n + 1), 1000);
    return () => clearInterval(id);
  }, [on]);
  return Date.now() + offset;
}

export function LiveActivity() {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [call, setCall] = useState<number | null>(null);
  const q = useApi<Activity>("/activity", open ? 3000 : 10_000);
  const a = q.data;
  const now = useServerNow(a?.now, open || (a?.calls.length ?? 0) > 0);
  const working = a ? a.runs.filter((r) => r.working).length : 0;
  const live = a ? a.calls.length + working + a.intakes.length : 0;

  return (
    <>
      <Button
        variant="ghost"
        size="sm"
        className="live-btn"
        data-live={live > 0 || undefined}
        aria-label={t("live.button", { n: live })}
        title={t("live.button", { n: live })}
        onClick={() => setOpen(true)}
      >
        <span className="live-pulse" aria-hidden="true" />
        {live > 0 ? t("live.count", { n: live }) : t("live.idle")}
      </Button>
      <Drawer open={open} onClose={() => setOpen(false)} wide title={t("live.title")}>
        {!a ? null : (
          <>
            <p className="small ink-2">{t("live.sub")}</p>
            <Panel title={t("live.calls")} meta={t("live.n", { n: a.calls.length })}>
              {a.calls.length === 0 ? (
                <Empty title={t("live.noCalls")} />
              ) : (
                <ul className="item-list">
                  {a.calls.map((c) => (
                    <li key={c.id}>
                      <a
                        href={`#call-${c.id}`}
                        onClick={(e) => {
                          e.preventDefault();
                          setCall(c.id);
                        }}
                      >
                        <span className="t">
                          <RoleName role={c.role} /> · <span className="mono small">{c.subject || "—"}</span>
                        </span>
                        <span className="state" data-tone="info" data-form="live">
                          {ms(Math.max(0, now - Date.parse(c.created_at)))}
                        </span>
                        <span className="m">
                          <OwnerRef call={c} />
                          <span className="mono">{c.model}</span>
                          <span>{t("live.cap", { n: c.max_tokens.toLocaleString() })}</span>
                        </span>
                      </a>
                    </li>
                  ))}
                </ul>
              )}
            </Panel>
            <Panel title={t("live.runs")} meta={t("live.n", { n: a.runs.length })}>
              {a.runs.length === 0 ? (
                <Empty title={t("live.noRuns")} />
              ) : (
                <ul className="item-list">
                  {a.runs.map((r) => {
                    const pct = r.max_tokens ? Math.min(100, Math.round((r.output_tokens / r.max_tokens) * 100)) : 0;
                    return (
                      <li key={r.id}>
                        <Link to={`/runs/${r.id}`} onClick={() => setOpen(false)}>
                          <span className="t">
                            <Ref kind="R" id={r.id} /> {r.project}
                          </span>
                          <State look={runLook(r.status)} />
                          <span className="m">
                            <span>{r.working ? t("live.working") : t("live.waiting")}</span>
                            <span>{t("live.hyps", { n: r.hypotheses })}</span>
                            <span className="live-budget" title={t("live.budget", { o: r.output_tokens.toLocaleString(), l: r.max_tokens.toLocaleString() })}>
                              <span className="live-budget-bar" aria-hidden="true">
                                <span style={{ width: `${pct}%` }} />
                              </span>
                              {t("live.budget", { o: r.output_tokens.toLocaleString(), l: r.max_tokens.toLocaleString() })}
                            </span>
                            {r.auto ? <span>{t("live.auto")}</span> : null}
                          </span>
                        </Link>
                      </li>
                    );
                  })}
                </ul>
              )}
            </Panel>
            <Panel title={t("live.intakes")} meta={t("live.n", { n: a.intakes.length })}>
              {a.intakes.length === 0 ? (
                <Empty title={t("live.noIntakes")} />
              ) : (
                <ul className="item-list">
                  {a.intakes.map((i) => (
                    <li key={i.id}>
                      <Link to={`/analyze/${i.id}`} onClick={() => setOpen(false)}>
                        <span className="t mono">{i.source_uri.replace(/^https:\/\//, "")}</span>
                        <span className="state" data-tone="info" data-form="live">
                          {t(`intake.status.${i.status}` as Key)}
                        </span>
                        <span className="m">
                          <span className="ref">[I-{i.id}]</span>
                          {i.status === "mining" ? <span>{t("live.commits", { r: i.commits_read, f: i.commits_found })}</span> : null}
                        </span>
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </Panel>
          </>
        )}
      </Drawer>
      <ModelCallDrawer id={call} onClose={() => setCall(null)} />
    </>
  );
}
