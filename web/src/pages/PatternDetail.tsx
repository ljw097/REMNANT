import { useState } from "react";
import { Frame } from "../App";
import { post } from "../api/client";
import { useApi, useUserNames } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { useSession } from "../session";
import type { Hypothesis, Pattern } from "../api/types";
import { Alert, Button, Empty, ErrorNote, errorText, Panel, SkeletonRows, State, Time, useToast } from "../ui/components";
import { Custody, DocHeader, SectionTitle } from "../ui/domain";
import { patternLook } from "../ui/status";
import { useColumns } from "../ui/ColumnPicker";
import { HypothesisRows } from "./RunDetail";

export function PatternDetailPage({ id }: { id: number }) {
  const { t } = useI18n();
  const { can, me, mode } = useSession();
  const toast = useToast();
  const userName = useUserNames();
  const p = useApi<Pattern>(`/patterns/${id}`);
  const hyps = useApi<Hypothesis[]>("/hypotheses");
  const col = useColumns("hypotheses");
  const [busy, setBusy] = useState<string>();
  const derived = (hyps.data ?? []).filter((h) => h.pattern_id === id);
  const validatedFinding = derived.some((h) => h.status === "validated");

  const review = async (status: Pattern["validation_status"]) => {
    setBusy(status);
    try {
      await post(`/patterns/${id}/review`, { status });
      toast(t("patterns.reviewed"));
      p.reload();
    } catch (e) {
      toast(errorText(e, t), "bad");
    } finally {
      setBusy(undefined);
    }
  };

  if (p.error) return <Frame crumbs={[{ label: t("nav.patterns"), to: "/patterns" }, { label: `P-${id}` }]}><ErrorNote error={p.error} retry={p.reload} /></Frame>;
  const x = p.data;
  const author = x?.created_by === me.user_id;
  const mayReview = x && can("reviewer") && (!author || mode === "personal");

  return (
    <Frame crumbs={[{ label: t("nav.patterns"), to: "/patterns" }, { label: `P-${id}` }]}>
      {!x ? (
        <SkeletonRows rows={6} cols={2} />
      ) : (
        <div className="stack" style={{ gap: 20 }}>
          <DocHeader
            docno={`P-${x.id} · v${x.version}`}
            title={x.title}
            state={<State look={patternLook(x.validation_status)} />}
            fields={[
              { label: t("patterns.class"), value: x.vulnerability_class },
              { label: "CWE", value: x.cwe_id ? <span className="mono">{x.cwe_id}</span> : "—" },
              { label: t("targets.lang"), value: x.language || "—" },
              { label: t("patterns.kind"), value: t(`patterns.kind.${x.source_kind}` as Key) },
              { label: t("patterns.sourceRef"), value: x.source_ref ? <span style={{ overflowWrap: "anywhere" }}>{x.source_ref}</span> : "—" },
            ]}
          />
          <div className="grid-12">
            <div className="span-8 stack">
              <Panel title={<SectionTitle no="1">{t("patterns.rootCause")}</SectionTitle>}>
                <div className="panel-body stack" style={{ gap: 16 }}>
                  <p className="prose">{x.root_cause}</p>
                  {x.preconditions.length ? (
                    <div className="claim-block">
                      <h3>{t("patterns.preconditions")}</h3>
                      <ul className="bullets">
                        {x.preconditions.map((c, i) => (
                          <li key={i}>{c}</li>
                        ))}
                      </ul>
                    </div>
                  ) : null}
                </div>
              </Panel>
              <Panel title={<SectionTitle no="2">{t("patterns.signatures")}</SectionTitle>} meta={t("patterns.signaturesExplain")}>
                <div className="panel-body grid-12" style={{ gap: 16 }}>
                  <div className="span-6 claim-block">
                    <h3>{t("patterns.signatures")}</h3>
                    {x.code_signatures.length ? (
                      <ul className="bullets">
                        {x.code_signatures.map((s) => (
                          <li key={s}>
                            <code>{s}</code>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <Alert tone="warn" icon="alert">
                        {t("patterns.noSignaturesWarn")}
                      </Alert>
                    )}
                  </div>
                  <div className="span-6 claim-block">
                    <h3>{t("patterns.counterSignatures")}</h3>
                    {x.counter_signatures.length ? (
                      <ul className="bullets">
                        {x.counter_signatures.map((s) => (
                          <li key={s}>
                            <code>{s}</code>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className="muted small">—</p>
                    )}
                  </div>
                </div>
              </Panel>
              <Panel title={<SectionTitle no="3">{t("patterns.derived")}</SectionTitle>} meta={t("common.nRows", { n: derived.length })}>
                {hyps.loading && !hyps.data ? <SkeletonRows rows={2} cols={4} /> : derived.length === 0 ? <Empty title={t("patterns.noDerived")} /> : <HypothesisRows rows={derived} col={col} />}
              </Panel>
            </div>
            <aside className="span-4 stack">
              <Panel title={t("custody.title")}>
                <div className="panel-body">
                  <Custody
                    label={t("custody.title")}
                    entries={[
                      { icon: "file", what: t("patterns.recorded"), who: userName(x.created_by), when: <Time iso={x.created_at} />, tone: "accent" },
                      x.reviewed_by
                        ? {
                            icon: x.validation_status === "rejected" ? "x" : "check",
                            what: t(`status.pattern.${x.validation_status}` as Key),
                            who: userName(x.reviewed_by),
                            when: <Time iso={x.reviewed_at} />,
                            detail: x.self_reviewed ? t("patterns.selfReviewed") : undefined,
                            tone: x.validation_status === "rejected" ? "bad" : x.self_reviewed ? "warn" : "ok",
                          }
                        : { icon: "clock", what: t("patterns.awaitingReview"), pending: true },
                    ]}
                  />
                </div>
              </Panel>
              {mayReview ? (
                <Panel title={t("patterns.review")}>
                  <div className="panel-body stack" style={{ gap: 10 }}>
                    <div className="row">
                      <Button loading={busy === "reviewed"} onClick={() => review("reviewed")}>
                        {t("patterns.markReviewed")}
                      </Button>
                      <Button variant="primary" disabled={!validatedFinding} loading={busy === "validated"} onClick={() => review("validated")}>
                        {t("patterns.markValidated")}
                      </Button>
                      <Button variant="danger" loading={busy === "rejected"} onClick={() => review("rejected")}>
                        {t("patterns.markRejected")}
                      </Button>
                    </div>
                    {!validatedFinding ? <p className="tiny muted">{t("patterns.validateNeeds")}</p> : null}
                    {author && mode === "personal" ? <p className="tiny muted">{t("patterns.selfNote")}</p> : null}
                  </div>
                </Panel>
              ) : author && can("reviewer") ? (
                <p className="small muted">{t("patterns.ownPattern")}</p>
              ) : null}
            </aside>
          </div>
        </div>
      )}
    </Frame>
  );
}
