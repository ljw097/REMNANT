import { useState } from "react";
import { Frame } from "../App";
import { del, patch, post } from "../api/client";
import { useApi, useUserNames } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { useSession } from "../session";
import type { Hypothesis, Pattern } from "../api/types";
import { Alert, Button, ConfirmButton, Drawer, Empty, ErrorNote, errorText, Field, Hash, Panel, SkeletonRows, State, Time, useToast } from "../ui/components";
import { navigate } from "../router";
import { Custody, DocHeader, SectionTitle } from "../ui/domain";
import { patternLook } from "../ui/status";
import { useColumns } from "../ui/ColumnPicker";
import { HypothesisRows } from "./RunDetail";

export function PatternDetailPage({ id }: { id: number }) {
  const { t } = useI18n();
  const { can, me, policy } = useSession();
  const toast = useToast();
  const userName = useUserNames();
  const p = useApi<Pattern>(`/patterns/${id}`);
  const hyps = useApi<Hypothesis[]>("/hypotheses");
  const col = useColumns("hypotheses");
  const [busy, setBusy] = useState<string>();
  const [editing, setEditing] = useState(false);
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

  // Only an unreviewed pattern is editable, and only one no run used can be
  // deleted (the server enforces both and explains a refusal).
  const remove = async () => {
    setBusy("delete");
    try {
      await del(`/patterns/${id}`);
      toast(t("patterns.deleted"));
      navigate("/patterns");
    } catch (e) {
      toast(errorText(e, t), "bad");
      setBusy(undefined);
    }
  };

  if (p.error) return <Frame crumbs={[{ label: t("nav.patterns"), to: "/patterns" }, { label: `P-${id}` }]}><ErrorNote error={p.error} retry={p.reload} /></Frame>;
  const x = p.data;
  const author = x?.created_by === me.user_id;
  const mayReview = x && can("reviewer") && (!author || policy.self_review);

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
            actions={
              can("researcher") ? (
                <>
                  {x.validation_status === "unreviewed" ? (
                    <Button icon="sliders" onClick={() => setEditing(true)}>
                      {t("common.edit")}
                    </Button>
                  ) : null}
                  {derived.length === 0 ? (
                    <ConfirmButton variant="ghost" icon="x" confirmLabel={t("patterns.deleteConfirm")} loading={busy === "delete"} onConfirm={() => void remove()}>
                      {t("common.delete")}
                    </ConfirmButton>
                  ) : null}
                </>
              ) : undefined
            }
            fields={[
              { label: t("patterns.class"), value: x.vulnerability_class },
              { label: "CWE", value: x.cwe_id ? <span className="mono">{x.cwe_id}</span> : "—" },
              { label: t("targets.lang"), value: x.language || "—" },
              { label: t("patterns.kind"), value: t(`patterns.kind.${x.source_kind}` as Key) },
              { label: t("patterns.sourceRef"), value: x.source_ref ? <span style={{ overflowWrap: "anywhere" }}>{x.source_ref}</span> : "—" },
              ...(x.origin?.commit
                ? [
                    {
                      label: t("patterns.origin"),
                      value: (
                        <span className="stack" style={{ gap: 2 }}>
                          <Hash value={x.origin.commit} />
                          {x.origin.repository ? <span className="small mono">{x.origin.repository.replace(/^https:\/\//, "")}</span> : null}
                        </span>
                      ),
                    },
                  ]
                : []),
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
                    {author && policy.self_review ? <p className="tiny muted">{t("patterns.selfNote")}</p> : null}
                  </div>
                </Panel>
              ) : author && can("reviewer") ? (
                <p className="small muted">{t("patterns.ownPattern")}</p>
              ) : null}
            </aside>
          </div>
        </div>
      )}
      {x && editing ? <EditPattern pattern={x} onClose={() => setEditing(false)} onSaved={() => p.reload()} /> : null}
    </Frame>
  );
}

const lines = (v: string) => v.split(/[\n,]/).map((x) => x.trim()).filter(Boolean);

/** Edits an unreviewed pattern. Lists take one item per line (signatures may also be comma-separated). */
function EditPattern({ pattern, onClose, onSaved }: { pattern: Pattern; onClose: () => void; onSaved: () => void }) {
  const { t } = useI18n();
  const toast = useToast();
  const [f, setF] = useState({
    title: pattern.title,
    vulnerability_class: pattern.vulnerability_class,
    cwe_id: pattern.cwe_id,
    language: pattern.language,
    root_cause: pattern.root_cause,
    preconditions: (pattern.preconditions ?? []).join("\n"),
    code_signatures: (pattern.code_signatures ?? []).join("\n"),
    counter_signatures: (pattern.counter_signatures ?? []).join("\n"),
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setF((x) => ({ ...x, [k]: e.target.value }));
  const ok = f.title.trim() && f.vulnerability_class.trim() && f.root_cause.trim();
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      await patch(`/patterns/${pattern.id}`, {
        title: f.title.trim(),
        vulnerability_class: f.vulnerability_class.trim(),
        cwe_id: f.cwe_id.trim(),
        language: f.language.trim(),
        root_cause: f.root_cause.trim(),
        preconditions: f.preconditions.split("\n").map((x) => x.trim()).filter(Boolean),
        code_signatures: lines(f.code_signatures),
        counter_signatures: lines(f.counter_signatures),
      });
      toast(t("patterns.saved"));
      onSaved();
      onClose();
    } catch (err) {
      setError(errorText(err, t));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Drawer
      open
      onClose={onClose}
      title={t("patterns.editTitle", { id: pattern.id })}
      foot={
        <>
          <Button onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" type="submit" form="edit-pattern" loading={busy} disabled={!ok}>
            {t("common.save")}
          </Button>
        </>
      }
    >
      <form id="edit-pattern" className="form-grid" onSubmit={submit}>
        <Alert icon="info">{t("patterns.editNote")}</Alert>
        <Field label={t("patterns.col.title")}>{(id) => <input id={id} className="input" value={f.title} maxLength={300} onChange={set("title")} />}</Field>
        <div className="form-grid two">
          <Field label={t("patterns.class")}>{(id) => <input id={id} className="input" value={f.vulnerability_class} maxLength={100} onChange={set("vulnerability_class")} />}</Field>
          <Field label="CWE">{(id) => <input id={id} className="input mono" value={f.cwe_id} maxLength={20} placeholder="CWE-787" onChange={set("cwe_id")} />}</Field>
        </div>
        <Field label={t("targets.lang")}>{(id) => <input id={id} className="input" value={f.language} maxLength={40} onChange={set("language")} />}</Field>
        <Field label={t("patterns.rootCause")}>{(id) => <textarea id={id} className="textarea" value={f.root_cause} onChange={set("root_cause")} />}</Field>
        <Field label={t("patterns.preconditions")} hint={t("patterns.onePerLine")}>{(id) => <textarea id={id} className="textarea" value={f.preconditions} onChange={set("preconditions")} />}</Field>
        <Field label={t("patterns.signatures")} hint={t("patterns.signaturesHint")}>{(id) => <textarea id={id} className="textarea mono" value={f.code_signatures} onChange={set("code_signatures")} />}</Field>
        <Field label={t("patterns.counterSignatures")} hint={t("patterns.onePerLine")}>{(id) => <textarea id={id} className="textarea mono" value={f.counter_signatures} onChange={set("counter_signatures")} />}</Field>
        {error ? (
          <Alert tone="bad" icon="alert">
            {error}
          </Alert>
        ) : null}
      </form>
    </Drawer>
  );
}
