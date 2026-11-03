import { useState } from "react";
import { Frame } from "../App";
import { post } from "../api/client";
import { useApi } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { Link, navigate, useLocation } from "../router";
import { useSession } from "../session";
import type { Pattern } from "../api/types";
import { Alert, Button, Drawer, Empty, ErrorNote, errorText, Field, PageHead, Panel, Segmented, SkeletonRows, State, useToast } from "../ui/components";
import { ColumnPicker, useColumns } from "../ui/ColumnPicker";
import { patternLook } from "../ui/status";

export function PatternsPage() {
  const { t } = useI18n();
  const { can } = useSession();
  const { query } = useLocation();
  const [q, setQ] = useState("");
  const [cls, setCls] = useState("");
  const [status, setStatus] = useState<"all" | "validated" | "reviewed" | "unreviewed" | "mined">("all");
  const [creating, setCreating] = useState(query.get("new") === "1");
  const params = new URLSearchParams();
  if (q.trim()) params.set("q", q.trim());
  if (cls) params.set("vulnerability_class", cls);
  const list = useApi<Pattern[]>(`/patterns${params.size ? `?${params}` : ""}`);
  const all = useApi<Pattern[]>("/patterns");
  const classes = [...new Set((all.data ?? []).map((p) => p.vulnerability_class))].sort();
  const col = useColumns("patterns");
  const rows = (list.data ?? []).filter((p) =>
    status === "all" ? true : status === "mined" ? p.source_kind === "mined" : p.validation_status === status,
  );
  const chips: typeof status[] = ["all", "validated", "reviewed", "unreviewed", "mined"];

  return (
    <Frame crumbs={[{ label: t("nav.patterns") }]}>
      <PageHead
        title={t("nav.patterns")}
        sub={t("patterns.sub")}
        actions={
          can("researcher") ? (
            <Button variant="primary" icon="plus" onClick={() => setCreating(true)}>
              {t("patterns.new")}
            </Button>
          ) : null
        }
      />
      <Panel>
        <div className="toolbar">
          <div className="chips" role="group" aria-label={t("patterns.filter")}>
            {chips.map((c) => (
              <button key={c} type="button" className="chip" aria-pressed={status === c} onClick={() => setStatus(c)}>
                {t(`patterns.filter.${c}` as Key)}
              </button>
            ))}
          </div>
          <input className="input search" type="search" placeholder={t("patterns.search")} aria-label={t("patterns.search")} value={q} onChange={(e) => setQ(e.target.value)} />
          <select className="select" aria-label={t("patterns.class")} value={cls} onChange={(e) => setCls(e.target.value)}>
            <option value="">{t("patterns.allClasses")}</option>
            {classes.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
          <span className="spacer" />
          <span className="small muted num">{t("common.nRows", { n: rows.length })}</span>
          <ColumnPicker set="patterns" />
        </div>
        {list.loading && !list.data ? (
          <SkeletonRows rows={5} cols={5} />
        ) : list.error ? (
          <ErrorNote error={list.error} retry={list.reload} />
        ) : rows.length === 0 ? (
          <Empty title={t("patterns.emptyTitle")} body={q || cls ? t("patterns.emptyFiltered") : t("patterns.emptyBody")} action={can("researcher") && !q ? <Button onClick={() => setCreating(true)}>{t("patterns.new")}</Button> : undefined} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t("patterns.col.title")}</th>
                  {col("class") ? <th>{t("columns.patterns.class")}</th> : null}
                  {col("cwe") ? <th>{t("columns.patterns.cwe")}</th> : null}
                  {col("language") ? <th>{t("columns.patterns.language")}</th> : null}
                  {col("source") ? <th>{t("columns.patterns.source")}</th> : null}
                  {col("status") ? <th>{t("columns.patterns.status")}</th> : null}
                </tr>
              </thead>
              <tbody>
                {rows.map((p) => (
                  <tr key={p.id} data-href onClick={(e) => !(e.target as HTMLElement).closest("a") && navigate(`/patterns/${p.id}`)}>
                    <td className="title-cell">
                      <Link to={`/patterns/${p.id}`}>{p.title}</Link>
                      <span className="sub">
                        <span className="ref">[P-{p.id}]</span> {t("patterns.signaturesN", { n: p.code_signatures.length })}
                      </span>
                    </td>
                    {col("class") ? <td className="small">{p.vulnerability_class}</td> : null}
                    {col("cwe") ? <td className="small mono">{p.cwe_id || "—"}</td> : null}
                    {col("language") ? <td className="small">{p.language || "—"}</td> : null}
                    {col("source") ? <td className="small" style={{ maxWidth: 260, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{p.source_ref || t(`patterns.kind.${p.source_kind}` as Key)}</td> : null}
                    {col("status") ? (
                      <td>
                        <State look={patternLook(p.validation_status)} />
                      </td>
                    ) : null}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Panel>
      <PatternForm
        open={creating}
        onClose={() => setCreating(false)}
        onSaved={(p) => {
          list.reload();
          all.reload();
          navigate(`/patterns/${p.id}`);
        }}
      />
    </Frame>
  );
}

const lines = (s: string) =>
  s
    .split("\n")
    .map((x) => x.trim())
    .filter(Boolean);

function PatternForm({ open, onClose, onSaved }: { open: boolean; onClose: () => void; onSaved: (p: Pattern) => void }) {
  const { t } = useI18n();
  const toast = useToast();
  const [f, setF] = useState({ source_kind: "researcher_authored", source_ref: "", title: "", vulnerability_class: "", cwe_id: "", language: "", root_cause: "", preconditions: "", code_signatures: "", counter_signatures: "" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setF({ ...f, [k]: e.target.value });
  const cweOk = !f.cwe_id || /^CWE-[1-9]\d{0,4}$/.test(f.cwe_id);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      const p = await post<Pattern>("/patterns", {
        ...f,
        preconditions: lines(f.preconditions),
        code_signatures: lines(f.code_signatures),
        counter_signatures: lines(f.counter_signatures),
      });
      toast(t("patterns.created"));
      onSaved(p);
      onClose();
    } catch (err) {
      setError(errorText(err, t));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={t("patterns.new")}
      foot={
        <>
          <Button onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" type="submit" form="pattern-form" loading={busy} disabled={!f.title.trim() || !f.vulnerability_class.trim() || !f.root_cause.trim() || !cweOk}>
            {t("patterns.create")}
          </Button>
        </>
      }
    >
      <form id="pattern-form" className="form-grid" onSubmit={submit}>
        <Segmented
          label={t("patterns.kind")}
          value={f.source_kind}
          onChange={(v) => setF({ ...f, source_kind: v })}
          options={[
            { value: "researcher_authored", label: t("patterns.kind.researcher_authored") },
            { value: "historical", label: t("patterns.kind.historical") },
          ]}
        />
        <Field label={t("patterns.title")}>{(id) => <input id={id} className="input" value={f.title} maxLength={300} onChange={set("title")} />}</Field>
        <div className="form-grid two">
          <Field label={t("patterns.class")} hint={t("patterns.classHint")}>
            {(id) => <input id={id} className="input" value={f.vulnerability_class} maxLength={100} list="pattern-classes" onChange={set("vulnerability_class")} />}
          </Field>
          <Field label="CWE" hint={t("patterns.cweHint")} error={cweOk ? undefined : t("patterns.cweInvalid")}>
            {(id) => <input id={id} className="input mono" value={f.cwe_id} maxLength={20} placeholder="CWE-787" onChange={set("cwe_id")} />}
          </Field>
          <Field label={t("targets.lang")}>{(id) => <input id={id} className="input" value={f.language} maxLength={40} placeholder="c" onChange={set("language")} />}</Field>
          <Field label={t("patterns.sourceRef")} hint={t("patterns.sourceRefHint")}>
            {(id) => <input id={id} className="input" value={f.source_ref} maxLength={2048} onChange={set("source_ref")} />}
          </Field>
        </div>
        <Field label={t("patterns.rootCause")}>{(id) => <textarea id={id} className="textarea" value={f.root_cause} maxLength={20000} onChange={set("root_cause")} />}</Field>
        <Field label={t("patterns.preconditions")} hint={t("patterns.oneLine")}>
          {(id) => <textarea id={id} className="textarea" value={f.preconditions} onChange={set("preconditions")} />}
        </Field>
        <div className="form-grid two">
          <Field label={t("patterns.signatures")} hint={t("patterns.signaturesHint")}>
            {(id) => <textarea id={id} className="textarea mono" value={f.code_signatures} placeholder={"memcpy\nlen -"} onChange={set("code_signatures")} />}
          </Field>
          <Field label={t("patterns.counterSignatures")} hint={t("patterns.counterHint")}>
            {(id) => <textarea id={id} className="textarea mono" value={f.counter_signatures} placeholder="bounds_check" onChange={set("counter_signatures")} />}
          </Field>
        </div>
        {lines(f.code_signatures).length === 0 ? (
          <Alert tone="warn" icon="alert">
            {t("patterns.noSignaturesWarn")}
          </Alert>
        ) : null}
        {error ? (
          <Alert tone="bad" icon="alert">
            {error}
          </Alert>
        ) : null}
      </form>
    </Drawer>
  );
}
