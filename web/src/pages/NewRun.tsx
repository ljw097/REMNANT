import { useEffect, useMemo, useState } from "react";
import { Frame } from "../App";
import { post } from "../api/client";
import { useApi } from "../hooks";
import { useI18n } from "../i18n";
import { Link, navigate, useLocation } from "../router";
import type { LLMProfile, Pattern, Project, ResearchSettingsResponse, RunDetail, Target } from "../api/types";
import { Alert, Button, Empty, errorText, Field, PageHead, Panel, SkeletonRows, State } from "../ui/components";
import { PolicyList, SectionTitle } from "../ui/domain";
import { Hash } from "../ui/components";
import { patternLook } from "../ui/status";

export function NewRunPage() {
  const { t, num } = useI18n();
  const { query } = useLocation();
  const projects = useApi<Project[]>("/projects");
  const profiles = useApi<LLMProfile[]>("/llm-profiles");
  const patterns = useApi<Pattern[]>("/patterns");
  const [projectId, setProjectId] = useState<number | null>(query.get("project") ? Number(query.get("project")) : null);
  const targets = useApi<Target[]>(projectId ? `/projects/${projectId}/targets` : null);
  const [targetIds, setTargetIds] = useState<number[]>([]);
  const [patternIds, setPatternIds] = useState<number[]>([]);
  const [patternQuery, setPatternQuery] = useState("");
  const [profileId, setProfileId] = useState<number | "">("");
  const [workers, setWorkers] = useState(2);
  const [wall, setWall] = useState(900);
  const [tokens, setTokens] = useState(200000);
  // Start from the instance's run defaults (Settings → Expert settings).
  const research = useApi<ResearchSettingsResponse>("/settings/research");
  useEffect(() => {
    const d = research.data?.settings.run_defaults;
    if (!d) return;
    setWorkers(d.max_workers);
    setWall(d.wall_seconds);
    setTokens(d.max_tokens);
  }, [research.data]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  const active = (projects.data ?? []).filter((p) => p.status === "active");
  const project = active.find((p) => p.id === projectId);
  useEffect(() => {
    if (projectId === null && active.length === 1) setProjectId(active[0]!.id);
  }, [active, projectId]);
  useEffect(() => {
    setTargetIds([]);
    setProfileId("");
  }, [projectId]);

  const ready = (targets.data ?? []).filter((x) => x.status === "ready");
  const usable = (patterns.data ?? []).filter((p) => p.validation_status !== "rejected");
  const shownPatterns = usable.filter((p) => !patternQuery || `${p.title} ${p.vulnerability_class} ${p.cwe_id} ${p.language}`.toLowerCase().includes(patternQuery.toLowerCase()));
  const chosenPatterns = usable.filter((p) => patternIds.includes(p.id));
  const noSignatures = chosenPatterns.length > 0 && chosenPatterns.every((p) => p.code_signatures.length === 0);
  const effectiveProfile = profileId === "" ? project?.llm_profile_id ?? null : profileId;
  const profile = (profiles.data ?? []).find((p) => p.id === effectiveProfile);

  const problems = useMemo(() => {
    const out: string[] = [];
    if (!project) out.push(t("newRun.need.project"));
    if (targetIds.length === 0) out.push(t("newRun.need.target"));
    if (!profile) out.push(t("newRun.need.profile"));
    else if (!profile.has_api_key) out.push(t("newRun.need.key"));
    return out;
  }, [project, targetIds, profile, t]);

  const submit = async () => {
    if (!project || problems.length) return;
    setBusy(true);
    setError(undefined);
    try {
      const run = await post<RunDetail>("/runs", {
        project_id: project.id,
        target_ids: targetIds,
        pattern_ids: patternIds,
        llm_profile_id: profileId === "" ? undefined : profileId,
        limits: { max_workers: workers, wall_seconds: wall, max_tokens: tokens },
      });
      navigate(`/runs/${run.id}`);
    } catch (e) {
      setError(errorText(e, t));
      setBusy(false);
    }
  };

  const toggle = (list: number[], set: (v: number[]) => void, id: number, on: boolean) => set(on ? [...list, id] : list.filter((x) => x !== id));

  return (
    <Frame crumbs={[{ label: t("nav.runs"), to: "/runs" }, { label: t("runs.new") }]}>
      <PageHead title={t("runs.new")} sub={t("newRun.sub")} />
      <div className="grid-12">
        <div className="span-8 stack">
          <Panel title={<SectionTitle no="1">{t("newRun.project")}</SectionTitle>}>
            <div className="panel-body">
              {projects.loading && !projects.data ? (
                <SkeletonRows rows={1} cols={1} />
              ) : active.length === 0 ? (
                <Empty title={t("projects.emptyTitle")} body={t("projects.emptyBody")} action={<Link to="/projects?new=1">{t("projects.new")}</Link>} />
              ) : (
                <Field label={t("newRun.project")}>
                  {(id) => (
                    <select id={id} className="select" value={projectId ?? ""} onChange={(e) => setProjectId(e.target.value ? Number(e.target.value) : null)}>
                      <option value="">{t("newRun.chooseProject")}</option>
                      {active.map((p) => (
                        <option key={p.id} value={p.id}>
                          {p.name}
                        </option>
                      ))}
                    </select>
                  )}
                </Field>
              )}
            </div>
          </Panel>

          <Panel title={<SectionTitle no="2">{t("newRun.targets")}</SectionTitle>} meta={t("newRun.targetsMeta")}>
            {!project ? (
              <p className="panel-body muted small">{t("newRun.pickProjectFirst")}</p>
            ) : targets.loading && !targets.data ? (
              <SkeletonRows rows={2} cols={3} />
            ) : ready.length === 0 ? (
              <Empty title={t("newRun.noReadyTargets")} body={t("newRun.noReadyTargetsBody")} action={<Link to={`/projects/${project.id}`}>{t("newRun.addTarget")}</Link>} />
            ) : (
              <div className="table-wrap">
              <table className="table">
                <tbody>
                  {ready.map((x) => (
                    <tr key={x.id}>
                      <td style={{ width: 36 }}>
                        <input
                          type="checkbox"
                          aria-label={x.display_name}
                          checked={targetIds.includes(x.id)}
                          disabled={!targetIds.includes(x.id) && targetIds.length >= 10}
                          onChange={(e) => toggle(targetIds, setTargetIds, x.id, e.target.checked)}
                        />
                      </td>
                      <td className="title-cell">
                        {x.display_name}
                        <span className="sub">{x.kind === "git_repository" ? x.source_uri : x.snapshot_key}</span>
                      </td>
                      <td>
                        <Hash value={x.revision} />
                      </td>
                      <td className="small muted">{x.primary_language}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              </div>
            )}
          </Panel>

          <Panel title={<SectionTitle no="3">{t("newRun.patterns")}</SectionTitle>} meta={t("newRun.patternsMeta", { n: patternIds.length })}>
            <div className="toolbar">
              <input className="input search" type="search" placeholder={t("patterns.search")} aria-label={t("patterns.search")} value={patternQuery} onChange={(e) => setPatternQuery(e.target.value)} />
            </div>
            {patterns.loading && !patterns.data ? (
              <SkeletonRows rows={3} cols={3} />
            ) : usable.length === 0 ? (
              <Empty title={t("patterns.emptyTitle")} body={t("newRun.noPatternsBody")} action={<Link to="/patterns?new=1">{t("patterns.new")}</Link>} />
            ) : (
              <div className="table-wrap" style={{ maxHeight: 340, overflowY: "auto" }}>
                <table className="table">
                  <tbody>
                    {shownPatterns.map((p) => (
                      <tr key={p.id}>
                        <td style={{ width: 36 }}>
                          <input type="checkbox" aria-label={p.title} checked={patternIds.includes(p.id)} onChange={(e) => toggle(patternIds, setPatternIds, p.id, e.target.checked)} />
                        </td>
                        <td className="title-cell">
                          {p.title}
                          <span className="sub">
                            {p.vulnerability_class}
                            {p.cwe_id ? ` · ${p.cwe_id}` : ""} · {t("patterns.signaturesN", { n: p.code_signatures.length })}
                          </span>
                        </td>
                        <td>
                          <State look={patternLook(p.validation_status)} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            {noSignatures ? (
              <div className="panel-body">
                <Alert tone="warn" icon="alert">
                  {t("newRun.noSignatures")}
                </Alert>
              </div>
            ) : null}
          </Panel>

          <Panel title={<SectionTitle no="4">{t("newRun.model")}</SectionTitle>}>
            <div className="panel-body form-grid two">
              <Field label={t("newRun.profile")} hint={project?.llm_profile_id ? t("newRun.profileDefault") : undefined}>
                {(id) => (
                  <select id={id} className="select" value={profileId} onChange={(e) => setProfileId(e.target.value ? Number(e.target.value) : "")}>
                    <option value="">{project?.llm_profile_id ? t("newRun.useProjectProfile") : t("newRun.chooseProfile")}</option>
                    {(profiles.data ?? []).map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name} · {p.model}
                        {p.has_api_key ? "" : ` (${t("models.noKey")})`}
                      </option>
                    ))}
                  </select>
                )}
              </Field>
              <Field label={t("newRun.workers")} hint={t("newRun.workersHint")}>
                {(id) => <input id={id} className="input" type="number" min={1} max={4} value={workers} onChange={(e) => setWorkers(Math.min(4, Math.max(1, Number(e.target.value) || 1)))} />}
              </Field>
              <Field label={t("newRun.wall")} hint={t("newRun.wallHint")}>
                {(id) => <input id={id} className="input" type="number" min={60} max={3600} step={60} value={wall} onChange={(e) => setWall(Number(e.target.value) || 900)} />}
              </Field>
              <Field label={t("newRun.tokens")} hint={t("newRun.tokensHint")}>
                {(id) => <input id={id} className="input" type="number" min={4000} max={2000000} step={10000} value={tokens} onChange={(e) => setTokens(Number(e.target.value) || 200000)} />}
              </Field>
            </div>
          </Panel>
        </div>

        <aside className="span-4 stack" style={{ position: "sticky", top: 68 }}>
          <Panel title={<SectionTitle no="5">{t("newRun.confirm")}</SectionTitle>}>
            <div className="panel-body stack" style={{ gap: 14 }}>
              <dl className="stack" style={{ gap: 8, margin: 0 }}>
                <Row label={t("newRun.project")} value={project?.name ?? "—"} />
                <Row label={t("newRun.targets")} value={t("newRun.nTargets", { n: targetIds.length })} />
                <Row label={t("newRun.patterns")} value={t("newRun.nPatterns", { n: patternIds.length })} />
                <Row label={t("newRun.profile")} value={profile ? `${profile.name} · ${profile.model}` : "—"} />
                <Row label={t("newRun.limits")} value={t("newRun.limitsValue", { w: workers, s: wall, k: num(tokens) })} />
              </dl>
              <div>
                <h4 className="small" style={{ marginBottom: 6 }}>
                  {t("newRun.boundary")}
                </h4>
                <PolicyList dynamic={project?.policy.dynamic_mode ?? "disabled"} />
                <p className="tiny muted" style={{ marginTop: 6 }}>
                  {t("newRun.pinNote")}
                </p>
              </div>
              {problems.length > 0 ? (
                <ul className="bullets small muted">
                  {problems.map((p) => (
                    <li key={p}>{p}</li>
                  ))}
                </ul>
              ) : null}
              {error ? (
                <Alert tone="bad" icon="alert">
                  {error}
                </Alert>
              ) : null}
              <Button variant="primary" size="lg" icon="play" loading={busy} disabled={problems.length > 0} onClick={submit}>
                {t("newRun.submit")}
              </Button>
            </div>
          </Panel>
        </aside>
      </div>
    </Frame>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ display: "grid", gridTemplateColumns: "110px minmax(0,1fr)", gap: 8 }}>
      <dt className="small muted">{label}</dt>
      <dd className="small" style={{ margin: 0, overflowWrap: "anywhere" }}>
        {value}
      </dd>
    </div>
  );
}
