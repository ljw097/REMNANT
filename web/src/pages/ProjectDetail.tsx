import { useState } from "react";
import { Frame } from "../App";
import { patch, post } from "../api/client";
import { useApi } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { Link } from "../router";
import { useSession } from "../session";
import type { LLMProfile, Project, Run, Target } from "../api/types";
import { Alert, Button, ConfirmButton, Drawer, Empty, ErrorNote, errorText, Field, Hash, Panel, Ref, Segmented, SkeletonRows, State, Time, useToast } from "../ui/components";
import { DocHeader, PolicyList, SectionTitle, StageLine } from "../ui/domain";
import { runLook, targetLook } from "../ui/status";
import { ProjectForm } from "./Projects";
import { Icon } from "../ui/Icon";

export function ProjectDetailPage({ id }: { id: number }) {
  const { t } = useI18n();
  const { can } = useSession();
  const toast = useToast();
  const p = useApi<Project>(`/projects/${id}`);
  const targets = useApi<Target[]>(`/projects/${id}/targets`, 20_000);
  const runs = useApi<Run[]>(`/runs?project_id=${id}`, 30_000);
  const profiles = useApi<LLMProfile[]>("/llm-profiles");
  const [editing, setEditing] = useState(false);
  const [adding, setAdding] = useState(false);

  if (p.error) return <Frame crumbs={[{ label: t("nav.projects"), to: "/projects" }, { label: `#${id}` }]}><ErrorNote error={p.error} retry={p.reload} /></Frame>;
  const project = p.data;
  const profile = profiles.data?.find((x) => x.id === project?.llm_profile_id);

  const setStatus = async (status: Project["status"]) => {
    try {
      await patch(`/projects/${id}`, { status });
      p.reload();
      toast(t("projects.saved"));
    } catch (e) {
      toast(errorText(e, t), "bad");
    }
  };

  return (
    <Frame crumbs={[{ label: t("nav.projects"), to: "/projects" }, { label: project?.name ?? `#${id}` }]}>
      {!project ? (
        <SkeletonRows rows={6} cols={3} />
      ) : (
        <div className="stack" style={{ gap: 20 }}>
          <DocHeader
            docno={`PRJ-${project.id}`}
            title={project.name}
            state={project.status !== "active" ? <span className="state" data-tone="muted" data-form="waiting">{t(`projects.status.${project.status}` as Key)}</span> : undefined}
            actions={
              can("researcher") ? (
                <>
                  <Link to={`/runs/new?project=${project.id}`} className="btn primary">
                    <Icon name="play" />
                    {t("runs.new")}
                  </Link>
                  <Button icon="settings" onClick={() => setEditing(true)}>
                    {t("projects.edit")}
                  </Button>
                  {project.status === "active" ? (
                    <ConfirmButton variant="ghost" icon="archive" confirmLabel={t("projects.archiveConfirm")} onConfirm={() => void setStatus("archived")}>
                      {t("projects.archive")}
                    </ConfirmButton>
                  ) : (
                    <Button variant="ghost" onClick={() => void setStatus("active")}>
                      {t("projects.reactivate")}
                    </Button>
                  )}
                </>
              ) : undefined
            }
            fields={[
              { label: t("projects.model"), value: profile ? `${profile.name} · ${profile.model}` : <span className="muted">{t("projects.noModel")}</span> },
              { label: t("projects.col.targets"), value: <span className="num">{project.target_count}</span> },
              { label: t("projects.col.active"), value: <span className="num">{project.active_runs}</span> },
              { label: t("projects.col.updated"), value: <Time iso={project.updated_at} abs /> },
            ]}
          />
          {project.description ? <p className="prose ink-2">{project.description}</p> : null}

          <div className="grid-12">
            <div className="span-8 stack">
              <Panel
                title={<SectionTitle no="1">{t("projects.targets")}</SectionTitle>}
                tools={
                  can("researcher") ? (
                    <Button size="sm" icon="plus" onClick={() => setAdding(true)}>
                      {t("targets.add")}
                    </Button>
                  ) : undefined
                }
              >
                {targets.loading && !targets.data ? (
                  <SkeletonRows rows={2} cols={4} />
                ) : (targets.data ?? []).length === 0 ? (
                  <Empty title={t("targets.emptyTitle")} body={t("targets.emptyBody")} action={can("researcher") ? <Button onClick={() => setAdding(true)}>{t("targets.add")}</Button> : undefined} />
                ) : (
                  <div className="table-wrap">
                    <table className="table">
                      <thead>
                        <tr>
                          <th>{t("targets.col.name")}</th>
                          <th>{t("targets.col.status")}</th>
                          <th>{t("run.field.revision")}</th>
                          <th>{t("targets.col.lang")}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {(targets.data ?? []).map((x) => (
                          <tr key={x.id}>
                            <td className="title-cell">
                              <Ref kind="T" id={x.id} /> {x.display_name}
                              <span className="sub">
                                {x.kind === "git_repository" ? `${x.source_uri}${x.source_ref ? ` @ ${x.source_ref}` : ""}` : t(`targets.kind.${x.kind}` as Key) + ` · ${x.snapshot_key}`}
                              </span>
                              {typeof x.metadata.fetch_error === "string" ? <span className="sub" style={{ color: "var(--bad)" }}>{x.metadata.fetch_error}</span> : null}
                            </td>
                            <td>
                              <State look={targetLook(x.status)} />
                            </td>
                            <td>{x.revision ? <Hash value={x.revision} /> : <span className="muted small">{t("targets.awaitingPin")}</span>}</td>
                            <td className="small">{x.primary_language || "—"}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </Panel>

              <Panel title={<SectionTitle no="2">{t("nav.runs")}</SectionTitle>} tools={<Link to={`/runs?project=${id}`} className="small">{t("common.viewAll")}</Link>}>
                {runs.loading && !runs.data ? (
                  <SkeletonRows rows={2} cols={3} />
                ) : (runs.data ?? []).length === 0 ? (
                  <Empty title={t("runs.emptyTitle")} body={t("projects.noRunsBody")} />
                ) : (
                  <div className="run-lines">
                    {(runs.data ?? []).slice(0, 8).map((r) => (
                      <Link key={r.id} to={`/runs/${r.id}`} className="run-line">
                        <span>
                          <strong>{t("runs.runN", { id: r.id })}</strong>
                          <span className="small muted" style={{ display: "block" }}>
                            <Time iso={r.queued_at} />
                          </span>
                        </span>
                        <StageLine status={r.status} resumeStage={r.resume_stage} resumeStatus={r.resume_status} mini />
                        <State look={runLook(r.status)} />
                      </Link>
                    ))}
                  </div>
                )}
              </Panel>
            </div>
            <aside className="span-4 stack">
              <Panel title={t("projects.policy")}>
                <div className="panel-body stack" style={{ gap: 10 }}>
                  <PolicyList dynamic={project.policy.dynamic_mode} />
                  <p className="tiny muted">{t("projects.policyNote")}</p>
                </div>
              </Panel>
            </aside>
          </div>
        </div>
      )}
      {project ? <ProjectForm open={editing} onClose={() => setEditing(false)} project={project} profiles={profiles.data ?? []} onSaved={() => p.reload()} /> : null}
      <TargetForm open={adding} onClose={() => setAdding(false)} projectId={id} onSaved={() => { targets.reload(); p.reload(); }} />
    </Frame>
  );
}

function TargetForm({ open, onClose, projectId, onSaved }: { open: boolean; onClose: () => void; projectId: number; onSaved: () => void }) {
  const { t } = useI18n();
  const toast = useToast();
  const [kind, setKind] = useState<Target["kind"]>("local_snapshot");
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [uri, setUri] = useState("");
  const [ref, setRef] = useState("");
  const [lang, setLang] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const keyOk = /^[a-z0-9][a-z0-9._-]{0,127}$/.test(key) && !key.includes("..");

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      await post(`/projects/${projectId}/targets`, {
        kind,
        display_name: name,
        snapshot_key: key,
        primary_language: lang,
        ...(kind === "git_repository" ? { source_uri: uri, source_ref: ref } : {}),
      });
      toast(kind === "git_repository" ? t("targets.addedGit") : t("targets.added"));
      onSaved();
      onClose();
      setName("");
      setKey("");
      setUri("");
      setRef("");
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
      title={t("targets.add")}
      foot={
        <>
          <Button onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" type="submit" form="target-form" loading={busy} disabled={!name.trim() || !keyOk || (kind === "git_repository" && !uri)}>
            {t("targets.add")}
          </Button>
        </>
      }
    >
      <form id="target-form" className="form-grid" onSubmit={submit}>
        <Segmented<Target["kind"]>
          label={t("targets.kind")}
          value={kind}
          onChange={setKind}
          options={[
            { value: "local_snapshot", label: t("targets.kind.local_snapshot") },
            { value: "git_repository", label: t("targets.kind.git_repository") },
            { value: "lab_fixture", label: t("targets.kind.lab_fixture") },
          ]}
        />
        <p className="small ink-2">{t(`targets.kindHelp.${kind}` as Key)}</p>
        <Field label={t("targets.name")}>{(id) => <input id={id} className="input" value={name} maxLength={200} onChange={(e) => setName(e.target.value)} />}</Field>
        {kind === "git_repository" ? (
          <div className="form-grid two">
            <Field label={t("targets.uri")} hint={t("targets.uriHint")}>
              {(id) => <input id={id} className="input mono" type="url" placeholder="https://github.com/org/repo" value={uri} onChange={(e) => setUri(e.target.value)} />}
            </Field>
            <Field label={t("targets.ref")} hint={t("targets.refHint")}>
              {(id) => <input id={id} className="input mono" placeholder="main" value={ref} maxLength={200} onChange={(e) => setRef(e.target.value)} />}
            </Field>
          </div>
        ) : null}
        <Field label={t("targets.key")} hint={kind === "git_repository" ? t("targets.keyHintGit") : t("targets.keyHintLocal")} error={key && !keyOk ? t("targets.keyInvalid") : undefined}>
          {(id) => <input id={id} className="input mono" value={key} maxLength={128} aria-invalid={key && !keyOk ? true : undefined} onChange={(e) => setKey(e.target.value.toLowerCase())} />}
        </Field>
        <Field label={t("targets.lang")} hint={t("targets.langHint")}>
          {(id) => <input id={id} className="input" value={lang} maxLength={40} placeholder="c" onChange={(e) => setLang(e.target.value)} />}
        </Field>
        {error ? (
          <Alert tone="bad" icon="alert">
            {error}
          </Alert>
        ) : null}
      </form>
    </Drawer>
  );
}
