// One repository intake's progress: its steps, the patterns the Historian
// mined, every Historian call, and the security-fix candidates found in the
// history (each opens to its message, diff and the Historian's verdict).

import { useState } from "react";
import { Frame } from "../App";
import type { Intake, IntakeDetail, ModelCall, Pattern } from "../api/types";
import { useApi } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { Link, navigate } from "../router";
import { Alert, Button, Empty, errorText, Hash, PageHead, Panel, SkeletonRows, Time } from "../ui/components";
import { CommitDrawer } from "../ui/Details";
import { Icon, type IconName } from "../ui/Icon";
import { CallOutcome, ModelCallDrawer, ModelCalls } from "../ui/ModelCalls";

export function IntakeState({ intake }: { intake: Intake }) {
  const { t } = useI18n();
  const look: Record<Intake["status"], [string, string]> = {
    fetching: ["info", "live"],
    mining: ["info", "live"],
    started: ["ok", "solid"],
    failed: ["bad", "stopped"],
  };
  const [tone, form] = look[intake.status];
  return (
    <span className="state" data-tone={tone} data-form={form}>
      {t(`intake.status.${intake.status}` as Key)}
    </span>
  );
}

export function IntakePage({ id }: { id: number }) {
  const { t } = useI18n();
  const q = useApi<IntakeDetail>(`/intakes/${id}`, 2000);
  const patterns = useApi<Pattern[]>("/patterns");
  const d = q.data;
  const i = d?.intake;
  const busy = !i || i.status === "fetching" || i.status === "mining";
  const calls = useApi<ModelCall[]>(`/intakes/${id}/model-calls`, busy ? 2000 : undefined);
  const callFor = (sha: string) => (calls.data ?? []).find((c) => c.commit_sha === sha);
  const [commit, setCommit] = useState<string | null>(null);
  const [openCall, setOpenCall] = useState<number | null>(null);
  const jump = (target: string) => document.getElementById(target)?.scrollIntoView({ behavior: "smooth", block: "start" });
  const stepTarget: Record<string, string> = { history: "intake-commits", patterns: "intake-calls", hunt: "" , fetch: "intake-commits" };
  const steps: { key: string; icon: IconName; state: "done" | "active" | "todo" | "failed" | "skipped"; detail: React.ReactNode }[] = i
    ? [
        {
          key: "fetch",
          icon: "download",
          state: i.target_status === "pending" ? (i.status === "failed" ? "failed" : "active") : i.target_status === "ready" ? "done" : "failed",
          detail: i.revision ? <Hash value={i.revision} /> : t("intake.fetching"),
        },
        {
          key: "history",
          icon: "clock",
          state: !i.options.mine_history ? "skipped" : i.target_status === "ready" ? "done" : i.status === "failed" ? "failed" : "todo",
          detail: i.options.mine_history ? t("intake.found", { n: i.commits_found, depth: i.options.history_depth }) : t("intake.skipped"),
        },
        {
          key: "patterns",
          icon: "patterns",
          state: !i.options.mine_history ? "skipped" : i.status === "mining" ? "active" : i.status === "started" ? "done" : i.status === "failed" && i.target_status === "ready" ? "failed" : "todo",
          detail: i.options.mine_history ? t("intake.drafted", { read: i.commits_read, n: i.patterns.length, tokens: i.output_tokens.toLocaleString() }) : t("intake.skipped"),
        },
        {
          key: "hunt",
          icon: "runs",
          state: i.run_id ? "done" : i.status === "failed" ? "failed" : "todo",
          detail: i.run_id ? <Link to={`/runs/${i.run_id}`}>{t("intake.openRun", { id: i.run_id })}</Link> : t("intake.waiting"),
        },
      ]
    : [];
  const mined = (patterns.data ?? []).filter((p) => i?.patterns.includes(p.id));

  return (
    <Frame crumbs={[{ label: t("nav.analyze"), to: "/analyze" }, { label: `I-${id}` }]}>
      {!d || !i ? (
        q.error ? (
          <Alert tone="bad">{errorText(q.error, t)}</Alert>
        ) : (
          <SkeletonRows rows={4} cols={2} />
        )
      ) : (
        <div className="stack" style={{ gap: 16 }}>
          <PageHead
            title={<span className="mono">{i.source_uri.replace(/^https:\/\//, "")}{i.source_ref ? `@${i.source_ref}` : ""}</span>}
            sub={
              <>
                <IntakeState intake={i} /> <Link to={`/projects/${i.project_id}`}>{i.project_name}</Link> · <Time iso={i.created_at} />
              </>
            }
            actions={i.run_id ? <Button variant="primary" icon="arrowRight" onClick={() => navigate(`/runs/${i.run_id}`)}>{t("intake.openRun", { id: i.run_id })}</Button> : undefined}
          />
          {i.status === "failed" ? (
            <Alert tone="bad" icon="alert">
              {i.error_summary}
            </Alert>
          ) : null}
          <Panel title={t("intake.progress")}>
            <ol className="intake-steps">
              {steps.map((s) => (
                <li key={s.key} data-state={s.state}>
                  <span className="seal">
                    <Icon name={s.state === "done" ? "check" : s.state === "failed" ? "x" : s.icon} size={14} />
                  </span>
                  <span className="stack" style={{ gap: 2 }}>
                    {stepTarget[s.key] ? (
                      <button type="button" className="linkish" title={t("detail.intake.jump")} onClick={() => jump(stepTarget[s.key]!)}>
                        <strong>{t(`analyze.step.${s.key}` as Key)}</strong>
                      </button>
                    ) : (
                      <strong>{t(`analyze.step.${s.key}` as Key)}</strong>
                    )}
                    <span className="small ink-2">{s.detail}</span>
                  </span>
                </li>
              ))}
            </ol>
          </Panel>
          {i.options.mine_history ? (
            <Panel title={t("intake.patterns")} meta={t("intake.patternsSub")}>
              {mined.length === 0 ? (
                <Empty title={i.status === "mining" || i.status === "fetching" ? t("intake.patternsPending") : t("intake.noPatterns")} />
              ) : (
                <ul className="item-list">
                  {mined.map((p) => (
                    <li key={p.id}>
                      <Link to={`/patterns/${p.id}`}>
                        <span className="t">{p.title}</span>
                        <span className="state" data-tone="muted" data-form="waiting">
                          {t("patterns.kind.mined")}
                        </span>
                        <span className="m">
                          <span className="ref">[P-{p.id}]</span>
                          <span>
                            {p.vulnerability_class}
                            {p.cwe_id ? ` · ${p.cwe_id}` : ""}
                          </span>
                          {p.origin?.commit ? <span className="mono">{p.origin.commit.slice(0, 12)}</span> : null}
                        </span>
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </Panel>
          ) : null}
          <section id="intake-calls">
            <Panel title={t("detail.intake.calls")} meta={t("detail.intake.callsSub")}>
              <ModelCalls path={`/intakes/${id}/model-calls`} poll={busy ? 3000 : undefined} empty={t("detail.intake.noCalls")} recordsHref={`/llm?intake_id=${id}`} />
            </Panel>
          </section>
          {d.commits.length > 0 ? (
            <section id="intake-commits">
              <Panel title={t("intake.commits")} meta={t("intake.commitsSub", { n: d.commits.length })}>
                <p className="small muted" style={{ padding: "10px var(--pad) 0" }}>{t("detail.intake.commitHint")}</p>
                <ul className="item-list">
                  {d.commits.map((c) => {
                    const call = callFor(c.sha);
                    return (
                      <li key={c.id}>
                        <a
                          href={`#${c.sha}`}
                          onClick={(e) => {
                            e.preventDefault();
                            setCommit(c.sha);
                          }}
                        >
                          <span className="t" style={{ whiteSpace: "normal" }}>
                            {c.subject}
                          </span>
                          {call ? <CallOutcome outcome={call.outcome} /> : <span className="small muted">{t("detail.commit.notRead")}</span>}
                          <span className="m">
                            <span className="mono">{c.sha.slice(0, 12)}</span>
                            <Time iso={c.committed_at} />
                            {c.signals.map((sg) => (
                              <span key={sg} className="detail-tag">
                                {t(`signal.${sg}` as Key)}
                              </span>
                            ))}
                            <span className="mono small">{c.files.slice(0, 3).join(", ")}{c.files.length > 3 ? ` +${c.files.length - 3}` : ""}</span>
                            {call?.result ? <span className="small">→ {call.result}</span> : null}
                          </span>
                        </a>
                      </li>
                    );
                  })}
                </ul>
              </Panel>
            </section>
          ) : null}
          <CommitDrawer intakeId={id} sha={commit} calls={calls.data ?? []} onClose={() => setCommit(null)} onOpenCall={(cid) => { setCommit(null); setOpenCall(cid); }} />
          <ModelCallDrawer id={openCall} onClose={() => setOpenCall(null)} />
        </div>
      )}
    </Frame>
  );
}
