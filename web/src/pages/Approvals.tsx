import { useState } from "react";
import { Frame } from "../App";
import { post } from "../api/client";
import { useApi, useUserNames } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { Link, navigate, useLocation } from "../router";
import { useSession } from "../session";
import type { Approval } from "../api/types";
import { Alert, Button, ConfirmButton, Drawer, Empty, ErrorNote, errorText, Field, Hash, PageHead, Panel, Ref, SkeletonRows, State, Tabs, Time, useToast } from "../ui/components";
import { Custody } from "../ui/domain";
import { decisionLook } from "../ui/status";

type Tab = "pending" | "decided";

export function ApprovalsPage() {
  const { t } = useI18n();
  const { query } = useLocation();
  const [tab, setTab] = useState<Tab>("pending");
  const all = useApi<Approval[]>("/approvals", 30_000);
  const openId = query.get("open") ? Number(query.get("open")) : null;
  const pending = (all.data ?? []).filter((a) => a.decision === "pending");
  const decided = (all.data ?? []).filter((a) => a.decision !== "pending");
  const rows = tab === "pending" ? pending : decided;
  const open = (all.data ?? []).find((a) => a.id === openId);

  return (
    <Frame crumbs={[{ label: t("nav.approvals") }]}>
      <PageHead title={t("nav.approvals")} sub={t("approvals.sub")} />
      <Tabs<Tab>
        value={tab}
        onChange={setTab}
        tabs={[
          { value: "pending", label: t("approvals.tab.pending"), count: pending.length },
          { value: "decided", label: t("approvals.tab.decided"), count: decided.length },
        ]}
      />
      <Panel>
        {all.loading && !all.data ? (
          <SkeletonRows rows={4} cols={3} />
        ) : all.error ? (
          <ErrorNote error={all.error} retry={all.reload} />
        ) : rows.length === 0 ? (
          <Empty title={tab === "pending" ? t("approvals.emptyPending") : t("approvals.emptyDecided")} body={tab === "pending" ? t("approvals.emptyPendingBody") : undefined} />
        ) : (
          <ul className="item-list">
            {rows.map((a) => (
              <li key={a.id}>
                <Link to={`/approvals?open=${a.id}`}>
                  <span className="t">{a.hypothesis_title}</span>
                  <State look={decisionLook(a.decision)} />
                  <span className="m">
                    <span className="ref">[A-{a.id}]</span>
                    <span className="mono">{a.scope.operation}</span>
                    <span>{t("approvals.requestedBy", { name: a.requested_by_name ?? "" })}</span>
                    {a.decision === "pending" ? (
                      <span>
                        {t("approvals.expires")} <Time iso={a.expires_at} />
                      </span>
                    ) : (
                      <Time iso={a.decided_at} />
                    )}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </Panel>
      <ApprovalDrawer approval={open} onClose={() => navigate("/approvals", { replace: true })} onDone={all.reload} />
    </Frame>
  );
}

function ApprovalDrawer({ approval: a, onClose, onDone }: { approval?: Approval; onClose: () => void; onDone: () => void }) {
  const { t, dateTime } = useI18n();
  const { me, can, reloadSummary, mode } = useSession();
  const userName = useUserNames();
  const toast = useToast();
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState<string>();
  const [error, setError] = useState<string>();

  const decide = async (decision: "approved" | "denied") => {
    if (!a) return;
    setBusy(decision);
    setError(undefined);
    try {
      await post(`/approvals/${a.id}`, { decision, reviewer_note: note });
      toast(t(decision === "approved" ? "approvals.approvedToast" : "approvals.deniedToast"));
      setNote("");
      reloadSummary();
      onDone();
      onClose();
    } catch (e) {
      setError(errorText(e, t));
      onDone();
    } finally {
      setBusy(undefined);
    }
  };

  const own = a?.requested_by === me.user_id;
  const selfAllowed = own && mode === "personal";
  const mayDecide = a?.decision === "pending" && can("reviewer") && (!own || selfAllowed);
  const expired = a ? new Date(a.expires_at).getTime() < Date.now() : false;

  return (
    <Drawer
      open={!!a}
      onClose={onClose}
      title={a ? <>{t("approvals.detailTitle")} <span className="ref">[A-{a.id}]</span></> : ""}
      foot={
        a?.decision === "pending" ? (
          mayDecide ? (
            <>
              <Button variant="danger" icon="x" loading={busy === "denied"} onClick={() => decide("denied")}>
                {t("approvals.deny")}
              </Button>
              {selfAllowed ? (
                <ConfirmButton variant="primary" icon="check" loading={busy === "approved"} disabled={expired} confirmLabel={t("approvals.selfConfirm")} onConfirm={() => decide("approved")}>
                  {t("approvals.selfApprove")}
                </ConfirmButton>
              ) : (
                <Button variant="primary" icon="check" loading={busy === "approved"} disabled={expired} onClick={() => decide("approved")}>
                  {t("approvals.approve")}
                </Button>
              )}
            </>
          ) : (
            <span className="small muted">{own ? t("approvals.ownRequest") : t("approvals.reviewerOnly")}</span>
          )
        ) : undefined
      }
    >
      {a ? (
        <>
          <div className="stack" style={{ gap: 6 }}>
            <div className="row">
              <State look={decisionLook(a.decision)} />
              <Link to={`/hypotheses/${a.hypothesis_id}`}>{a.hypothesis_title}</Link>
            </div>
            <p className="small ink-2">{a.request_summary}</p>
          </div>
          <section className="stack" style={{ gap: 8 }}>
            <h3 className="small">{t("approvals.scope")}</h3>
            <div className="docheader">
              <dl>
                <Item label={t("approvals.operation")} value={<span className="mono">{a.scope.operation}</span>} />
                <Item label={t("run.target")} value={<Ref kind="T" id={a.target_id} />} />
                <Item label={t("run.field.revision")} value={<Hash value={a.target_revision} length={16} />} />
                <Item label={t("approvals.network")} value={<strong>{t("approvals.networkNone")}</strong>} />
                <Item label={t("lab.limit.wall_seconds")} value={<span className="num">{a.scope.limits.wall_seconds}s</span>} />
                <Item label={t("lab.limit.memory_mb")} value={<span className="num">{a.scope.limits.memory_mb} MB</span>} />
                <Item label={t("lab.limit.cpu_cores")} value={<span className="num">{a.scope.limits.cpu_cores}</span>} />
                <Item label={t("lab.limit.max_processes")} value={<span className="num">{a.scope.limits.max_processes}</span>} />
                <Item label={t("approvals.runScope")} value={<Hash value={a.scope.run_scope_hash} />} />
                <Item label={t("approvals.scopeHash")} value={<Hash value={a.scope_hash} />} />
              </dl>
            </div>
            <p className="tiny muted">{t("approvals.staleNote")}</p>
          </section>
          <section className="stack" style={{ gap: 8 }}>
            <h3 className="small">{t("custody.title")}</h3>
            <Custody
              label={t("custody.title")}
              entries={[
                { icon: "user", what: t("approvals.requested"), who: userName(a.requested_by), when: <time title={dateTime(a.created_at)}>{dateTime(a.created_at)}</time>, tone: "accent" },
                a.decision === "pending"
                  ? { icon: "clock", what: t("approvals.awaitingSecond"), detail: <>{t("approvals.expires")} <Time iso={a.expires_at} /></>, pending: true, tone: expired ? "bad" : "warn" }
                  : {
                      icon: a.decision === "approved" ? "check" : "x",
                      what: t(`status.approval.${a.decision}` as Key),
                      who: a.decided_by ? userName(a.decided_by) : undefined,
                      when: <Time iso={a.decided_at} />,
                      detail: [a.self_approved ? t("approvals.selfApproved") : "", a.reviewer_note].filter(Boolean).join(" · ") || undefined,
                      tone: a.decision === "approved" ? (a.self_approved ? "warn" : "ok") : "bad",
                    },
              ]}
            />
          </section>
          {selfAllowed && a.decision === "pending" ? (
            <Alert tone="warn" icon="user">
              {t("approvals.selfExplain")}
            </Alert>
          ) : null}
          {mayDecide ? (
            <Field label={t("hyp.note")} hint={t("approvals.noteHint")}>
              {(id) => <textarea id={id} className="textarea" value={note} maxLength={8000} onChange={(e) => setNote(e.target.value)} />}
            </Field>
          ) : null}
          {expired && a.decision === "pending" ? (
            <Alert tone="warn" icon="clock">
              {t("approvals.expiredNote")}
            </Alert>
          ) : null}
          {error ? (
            <Alert tone="bad" icon="alert">
              {error}
            </Alert>
          ) : null}
        </>
      ) : null}
    </Drawer>
  );
}

function Item({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}
