import { useEffect, useState } from "react";
import { Frame } from "../App";
import { patch, post } from "../api/client";
import { useApi } from "../hooks";
import { useI18n } from "../i18n";
import { Link, navigate, useLocation } from "../router";
import { useSession } from "../session";
import type { LLMProfile, Project } from "../api/types";
import { Alert, Button, Drawer, Empty, ErrorNote, errorText, Field, PageHead, Panel, SkeletonRows, Time, useToast } from "../ui/components";
import { Icon } from "../ui/Icon";

export function ProjectsPage() {
  const { t } = useI18n();
  const { can } = useSession();
  const { query } = useLocation();
  const q = useApi<Project[]>("/projects");
  const profiles = useApi<LLMProfile[]>("/llm-profiles");
  const [creating, setCreating] = useState(query.get("new") === "1");
  const [showArchived, setShowArchived] = useState(false);
  const rows = (q.data ?? []).filter((p) => showArchived || p.status !== "archived");
  const profileName = (id: number | null) => profiles.data?.find((p) => p.id === id)?.name ?? "—";

  return (
    <Frame crumbs={[{ label: t("nav.projects") }]}>
      <PageHead
        title={t("nav.projects")}
        sub={t("projects.sub")}
        actions={
          can("researcher") ? (
            <Button variant="primary" icon="plus" onClick={() => setCreating(true)}>
              {t("projects.new")}
            </Button>
          ) : null
        }
      />
      <Panel>
        <div className="toolbar">
          <label className="check small">
            <input type="checkbox" checked={showArchived} onChange={(e) => setShowArchived(e.target.checked)} />
            {t("projects.showArchived")}
          </label>
        </div>
        {q.loading && !q.data ? (
          <SkeletonRows rows={4} cols={5} />
        ) : q.error ? (
          <ErrorNote error={q.error} retry={q.reload} />
        ) : rows.length === 0 ? (
          <Empty title={t("projects.emptyTitle")} body={t("projects.emptyBody")} action={can("researcher") ? <Button onClick={() => setCreating(true)}>{t("projects.new")}</Button> : undefined} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t("projects.col.name")}</th>
                  <th className="right">{t("projects.col.targets")}</th>
                  <th className="right">{t("projects.col.active")}</th>
                  <th>{t("projects.col.model")}</th>
                  <th>{t("projects.col.lab")}</th>
                  <th>{t("projects.col.updated")}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((p) => (
                  <tr key={p.id} data-href onClick={(e) => !(e.target as HTMLElement).closest("a") && navigate(`/projects/${p.id}`)}>
                    <td className="title-cell">
                      <Link to={`/projects/${p.id}`}>{p.name}</Link>
                      {p.description ? <span className="sub">{p.description.slice(0, 120)}</span> : null}
                    </td>
                    <td className="right num">{p.target_count}</td>
                    <td className="right num">{p.active_runs || "—"}</td>
                    <td className="small">{profileName(p.llm_profile_id)}</td>
                    <td className="small">
                      {p.policy.dynamic_mode === "local_lab_only" ? (
                        <span className="row" style={{ gap: 4, color: "var(--warn)" }}>
                          <Icon name="flask" size={14} />
                          {t("policy.labAllowedShort")}
                        </span>
                      ) : (
                        <span className="muted">{t("policy.labDisabledShort")}</span>
                      )}
                    </td>
                    <td className="small ink-2">
                      <Time iso={p.updated_at} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Panel>
      <ProjectForm open={creating} onClose={() => setCreating(false)} profiles={profiles.data ?? []} onSaved={(p) => navigate(`/projects/${p.id}`)} />
    </Frame>
  );
}

export function ProjectForm({ open, onClose, profiles, project, onSaved }: { open: boolean; onClose: () => void; profiles: LLMProfile[]; project?: Project; onSaved: (p: Project) => void }) {
  const { t } = useI18n();
  const toast = useToast();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [profile, setProfile] = useState<number | "">("");
  const [lab, setLab] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  useEffect(() => {
    if (!open) return;
    setName(project?.name ?? "");
    setDescription(project?.description ?? "");
    setProfile(project?.llm_profile_id ?? (profiles.length === 1 ? profiles[0]!.id : ""));
    setLab(project?.policy.dynamic_mode === "local_lab_only");
    setError(undefined);
  }, [open, project, profiles]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(undefined);
    const policy = { source_mode: "read_only", dynamic_mode: lab ? "local_lab_only" : "disabled", network: "none" };
    try {
      const saved = project
        ? await patch<Project>(`/projects/${project.id}`, {
            name,
            description,
            policy,
            ...(profile === "" ? { clear_llm_profile: true } : { llm_profile_id: profile }),
          })
        : await post<Project>("/projects", { name, description, policy, ...(profile === "" ? {} : { llm_profile_id: profile }) });
      toast(project ? t("projects.saved") : t("projects.created"));
      onSaved(saved);
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
      title={project ? t("projects.edit") : t("projects.new")}
      foot={
        <>
          <Button onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" type="submit" form="project-form" loading={busy} disabled={!name.trim()}>
            {project ? t("common.save") : t("projects.create")}
          </Button>
        </>
      }
    >
      <form id="project-form" className="form-grid" onSubmit={submit}>
        <Field label={t("projects.name")}>{(id) => <input id={id} className="input" value={name} maxLength={120} required onChange={(e) => setName(e.target.value)} autoFocus />}</Field>
        <Field label={t("projects.description")} hint={t("projects.descriptionHint")}>
          {(id) => <textarea id={id} className="textarea" value={description} maxLength={8000} onChange={(e) => setDescription(e.target.value)} />}
        </Field>
        <Field label={t("projects.model")} hint={profiles.length ? t("projects.modelHint") : t("projects.noModels")}>
          {(id) => (
            <select id={id} className="select" value={profile} onChange={(e) => setProfile(e.target.value ? Number(e.target.value) : "")}>
              <option value="">{t("projects.noModel")}</option>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} · {p.model}
                </option>
              ))}
            </select>
          )}
        </Field>
        <fieldset className="field" style={{ border: 0, padding: 0, margin: 0 }}>
          <legend className="label" style={{ fontSize: "var(--fs-1)", fontWeight: 600, marginBottom: 6 }}>
            {t("projects.policy")}
          </legend>
          <ul className="policy">
            <li>
              <Icon name="lock" />
              {t("policy.readOnly")}
            </li>
            <li>
              <Icon name="noNetwork" />
              {t("policy.noNetwork")}
            </li>
          </ul>
          <label className="check" style={{ marginTop: 8 }}>
            <input type="checkbox" checked={lab} onChange={(e) => setLab(e.target.checked)} />
            {t("projects.allowLab")}
          </label>
          {lab ? (
            <Alert tone="warn" icon="flask">
              {t("projects.allowLabWarn")}
            </Alert>
          ) : (
            <span className="hint">{t("projects.allowLabHint")}</span>
          )}
        </fieldset>
        {error ? (
          <Alert tone="bad" icon="alert">
            {error}
          </Alert>
        ) : null}
      </form>
    </Drawer>
  );
}
