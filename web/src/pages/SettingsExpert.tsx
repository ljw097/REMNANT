// Administrator settings for the research engine: model profiles (API keys),
// the agents that use them, research settings, and the read-only server
// configuration.

import { useEffect, useState } from "react";
import { del, patch, post, put } from "../api/client";
import type { Agent, LabLimits, LLMProfile, ModelOptions, ProfileCheck, ResearchSettings, ResearchSettingsResponse, SystemConfig } from "../api/types";
import { useApi } from "../hooks";
import { useI18n, type Key } from "../i18n";
import { Alert, Button, ConfirmButton, Drawer, Empty, errorText, Field, Panel, SkeletonRows, Time, useToast } from "../ui/components";

export type Provider = "anthropic" | "openai";
export const providers: Provider[] = ["anthropic", "openai"];

// Per-provider model suggestions, a sensible default model, and the base URL
// and key-hint shown as placeholders. The model field stays free text.
export const providerInfo: Record<Provider, { models: string[]; baseUrl: string; keyHint: string }> = {
  anthropic: {
    models: ["claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5-1"],
    baseUrl: "https://api.anthropic.com",
    keyHint: "sk-ant-…",
  },
  openai: {
    models: ["gpt-5", "gpt-5-mini", "gpt-4o", "gpt-4o-mini", "o3", "o4-mini"],
    baseUrl: "https://api.openai.com/v1",
    keyHint: "sk-…",
  },
};
const efforts = ["low", "medium", "high", "xhigh", "max"] as const;

/* ---------- Model profiles ---------- */

export function ModelsSection() {
  const { t } = useI18n();
  const toast = useToast();
  const q = useApi<LLMProfile[]>("/llm-profiles");
  const [editing, setEditing] = useState<LLMProfile | "new" | null>(null);
  const [checks, setChecks] = useState<Record<number, ProfileCheck | "busy">>({});

  const test = async (p: LLMProfile) => {
    setChecks((c) => ({ ...c, [p.id]: "busy" }));
    try {
      const r = await post<ProfileCheck>(`/llm-profiles/${p.id}/test`);
      setChecks((c) => ({ ...c, [p.id]: r }));
    } catch (e) {
      setChecks((c) => ({ ...c, [p.id]: { ok: false, error: errorText(e, t) } }));
    }
  };
  const remove = async (p: LLMProfile) => {
    try {
      await del(`/llm-profiles/${p.id}`);
      toast(t("models.deleted", { name: p.name }));
      q.reload();
    } catch (e) {
      toast(errorText(e, t), "bad");
    }
  };

  return (
    <Panel
      title={t("settings.models")}
      meta={t("models.sub")}
      tools={
        <Button size="sm" icon="plus" onClick={() => setEditing("new")}>
          {t("models.new")}
        </Button>
      }
    >
      {q.loading && !q.data ? (
        <SkeletonRows rows={2} cols={4} />
      ) : (q.data ?? []).length === 0 ? (
        <Empty title={t("models.emptyTitle")} body={t("models.emptyBody")} action={<Button onClick={() => setEditing("new")}>{t("models.new")}</Button>} />
      ) : (
        <ul className="profile-list">
          {(q.data ?? []).map((p) => {
            const c = checks[p.id];
            return (
              <li key={p.id}>
                <div className="profile-head">
                  <div className="stack" style={{ gap: 2, minWidth: 0 }}>
                    <strong>{p.name}</strong>
                    <span className="small ink-2">
                      {t(`models.provider.${p.provider}` as Key)} · <span className="mono">{p.model}</span> · {t("models.effort")} {p.effort}
                      {p.options?.thinking ? ` · ${t(`models.thinking.${p.options.thinking}` as Key)}` : ""}
                      {p.base_url ? (
                        <>
                          {" · "}
                          <span className="mono">{p.base_url}</span>
                        </>
                      ) : null}
                    </span>
                    <span className="small muted">
                      {p.has_api_key ? (
                        <>
                          {t("models.key")} <span className="mono">{p.api_key_hint}</span>
                        </>
                      ) : (
                        <span style={{ color: "var(--bad)" }}>{t("models.noKey")}</span>
                      )}
                      {" · "}
                      {p.used_by > 0 ? t("models.usedBy", { n: p.used_by }) : t("models.unused")}
                      {" · "}
                      <Time iso={p.updated_at} />
                    </span>
                  </div>
                  <div className="row" style={{ flexWrap: "nowrap" }}>
                    <Button size="sm" icon="refresh" loading={c === "busy"} disabled={!p.has_api_key} onClick={() => void test(p)}>
                      {t("models.test")}
                    </Button>
                    <Button size="sm" icon="sliders" onClick={() => setEditing(p)}>
                      {t("common.edit")}
                    </Button>
                    {p.used_by === 0 ? (
                      <ConfirmButton size="sm" variant="ghost" confirmLabel={t("models.deleteConfirm")} onConfirm={() => void remove(p)}>
                        {t("common.delete")}
                      </ConfirmButton>
                    ) : null}
                  </div>
                </div>
                {c && c !== "busy" ? <CheckResult check={c} /> : null}
              </li>
            );
          })}
        </ul>
      )}
      <ModelForm
        profile={editing === "new" ? null : editing}
        open={editing !== null}
        onClose={() => setEditing(null)}
        onSaved={() => {
          q.reload();
          setChecks({});
        }}
      />
    </Panel>
  );
}

function CheckResult({ check }: { check: ProfileCheck }) {
  const { t } = useI18n();
  return check.ok ? (
    <Alert tone="ok" icon="check">
      {t("models.testOk", { model: check.model ?? "" })}
    </Alert>
  ) : (
    <Alert tone="bad" icon="alert">
      {t("models.testFailed", { error: check.error ?? "" })}
    </Alert>
  );
}

type ModelDraft = { name: string; provider: Provider; model: string; effort: string; base_url: string; api_key: string; thinking: ModelOptions["thinking"]; timeout: string; retries: string };

const blankModel: ModelDraft = { name: "", provider: "anthropic", model: "claude-opus-5-5", effort: "high", base_url: "", api_key: "", thinking: "adaptive", timeout: "", retries: "" };

function draftOf(p: LLMProfile | null): ModelDraft {
  if (!p) return blankModel;
  return {
    name: p.name,
    provider: p.provider,
    model: p.model,
    effort: p.effort,
    base_url: p.base_url,
    api_key: "",
    thinking: p.options?.thinking ?? "",
    timeout: p.options?.timeout_seconds ? String(p.options.timeout_seconds) : "",
    retries: p.options?.max_retries != null ? String(p.options.max_retries) : "",
  };
}

function optionsOf(f: ModelDraft): ModelOptions {
  return { thinking: f.thinking, timeout_seconds: Number(f.timeout) || 0, max_retries: f.retries === "" ? null : Number(f.retries) };
}

/** Creates a profile, or edits one (the key is replaced only if a new one is typed). */
function ModelForm({ profile, open, onClose, onSaved }: { profile: LLMProfile | null; open: boolean; onClose: () => void; onSaved: () => void }) {
  const { t } = useI18n();
  const toast = useToast();
  const [f, setF] = useState<ModelDraft>(blankModel);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [check, setCheck] = useState<ProfileCheck | "busy" | null>(null);
  useEffect(() => {
    if (open) {
      setF(draftOf(profile));
      setError(undefined);
      setCheck(null);
    }
  }, [open, profile]);
  const set = (p: Partial<ModelDraft>) => {
    setF((x) => ({ ...x, ...p }));
    setCheck(null);
  };
  const timeoutBad = f.timeout !== "" && (Number(f.timeout) < 10 || Number(f.timeout) > 900);
  const retriesBad = f.retries !== "" && (Number(f.retries) < 0 || Number(f.retries) > 10 || !Number.isInteger(Number(f.retries)));
  const ok = f.name.trim() && f.model.trim() && (profile || f.api_key) && !timeoutBad && !retriesBad;

  const test = async () => {
    setCheck("busy");
    try {
      setCheck(await post<ProfileCheck>("/llm-profiles/test", { provider: f.provider, model: f.model, base_url: f.base_url, api_key: f.api_key, options: optionsOf(f) }));
    } catch (e) {
      setCheck({ ok: false, error: errorText(e, t) });
    }
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      const body = { name: f.name, model: f.model, effort: f.effort, base_url: f.base_url, options: optionsOf(f) };
      if (profile) {
        await patch(`/llm-profiles/${profile.id}`, f.api_key ? { ...body, api_key: f.api_key } : body);
        toast(f.api_key ? t("models.savedKey") : t("models.saved"));
      } else {
        await post("/llm-profiles", { ...body, provider: f.provider, api_key: f.api_key });
        toast(t("models.created"));
      }
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
      open={open}
      onClose={onClose}
      title={profile ? t("models.edit", { name: profile.name }) : t("models.new")}
      foot={
        <>
          <Button onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="primary" type="submit" form="model-form" loading={busy} disabled={!ok}>
            {profile ? t("common.save") : t("models.create")}
          </Button>
        </>
      }
    >
      <form id="model-form" className="form-grid" onSubmit={submit}>
        <Alert icon="lock">{t("models.keyNote")}</Alert>
        <Field label={t("models.name")}>{(id) => <input id={id} className="input" value={f.name} maxLength={120} onChange={(e) => set({ name: e.target.value })} />}</Field>
        <Field label={t("models.provider")} hint={t("models.providerHint")}>
          {(id) => (
            <select
              id={id}
              className="select"
              value={f.provider}
              onChange={(e) => {
                const provider = e.target.value as Provider;
                // Swap the default model when it is still the other provider's
                // default, so the field stays sensible; keep a custom model.
                const wasDefault = providers.some((p) => p !== provider && providerInfo[p].models[0] === f.model);
                set(wasDefault ? { provider, model: providerInfo[provider].models[0] } : { provider });
              }}
            >
              {providers.map((p) => (
                <option key={p} value={p}>
                  {t(`models.provider.${p}` as Key)}
                </option>
              ))}
            </select>
          )}
        </Field>
        <div className="form-grid two">
          <Field label={t("models.model")} hint={t(`models.modelHint.${f.provider}` as Key)}>
            {(id) => (
              <>
                <input id={id} className="input mono" list="model-suggestions" value={f.model} maxLength={200} spellCheck={false} onChange={(e) => set({ model: e.target.value })} />
                <datalist id="model-suggestions">
                  {providerInfo[f.provider].models.map((m) => (
                    <option key={m} value={m} />
                  ))}
                </datalist>
              </>
            )}
          </Field>
          <Field label={t("models.effort")} hint={t("models.effortHint")}>
            {(id) => (
              <select id={id} className="select" value={f.effort} onChange={(e) => set({ effort: e.target.value })}>
                {efforts.map((x) => (
                  <option key={x} value={x}>
                    {t(`models.effort.${x}` as Key)}
                  </option>
                ))}
              </select>
            )}
          </Field>
        </div>
        <Field label={profile ? t("models.replaceKey") : t("models.key")} hint={profile ? t("models.replaceKeyHint", { hint: profile.api_key_hint || "—" }) : t("models.keyHint")}>
          {(id) => <input id={id} className="input mono" type="password" autoComplete="off" spellCheck={false} value={f.api_key} placeholder={profile ? profile.api_key_hint : providerInfo[f.provider].keyHint} onChange={(e) => set({ api_key: e.target.value })} />}
        </Field>
        <Field label={t("models.baseUrl")} hint={t("models.baseUrlHint")}>
          {(id) => <input id={id} className="input mono" type="url" value={f.base_url} placeholder={providerInfo[f.provider].baseUrl} onChange={(e) => set({ base_url: e.target.value })} />}
        </Field>

        <fieldset className="rules">
          <legend>{t("models.advanced")}</legend>
          <div className="advanced-grid">
            <Field label={t("models.thinking")} hint={t("models.thinkingHint")}>
              {(id) => (
                <select id={id} className="select" value={f.thinking} onChange={(e) => set({ thinking: e.target.value as ModelOptions["thinking"] })}>
                  <option value="adaptive">{t("models.thinking.adaptive")}</option>
                  <option value="">{t("models.thinking.")}</option>
                  <option value="disabled">{t("models.thinking.disabled")}</option>
                </select>
              )}
            </Field>
            <Field label={t("models.timeout")} hint={t("models.timeoutHint")} error={timeoutBad ? t("models.timeoutRange") : undefined}>
              {(id) => <input id={id} className="input" type="number" min={10} max={900} placeholder={t("common.default")} value={f.timeout} onChange={(e) => set({ timeout: e.target.value })} />}
            </Field>
            <Field label={t("models.retries")} hint={t("models.retriesHint")} error={retriesBad ? t("models.retriesRange") : undefined}>
              {(id) => <input id={id} className="input" type="number" min={0} max={10} placeholder={t("common.default")} value={f.retries} onChange={(e) => set({ retries: e.target.value })} />}
            </Field>
          </div>
        </fieldset>

        <div className="row">
          <Button icon="refresh" loading={check === "busy"} disabled={!f.api_key || !f.model.trim()} onClick={() => void test()}>
            {t("models.testBeforeSave")}
          </Button>
          {!f.api_key && profile ? <span className="small muted">{t("models.testStoredHint")}</span> : null}
        </div>
        {check && check !== "busy" ? <CheckResult check={check} /> : null}
        {error ? (
          <Alert tone="bad" icon="alert">
            {error}
          </Alert>
        ) : null}
      </form>
    </Drawer>
  );
}

/* ---------- Agents ---------- */

export function AgentsSection() {
  const { t } = useI18n();
  const agents = useApi<Agent[]>("/agents");
  const profiles = useApi<LLMProfile[]>("/llm-profiles");
  return (
    <Panel title={t("settings.agents")} meta={t("agents.sub")}>
      {agents.loading && !agents.data ? (
        <SkeletonRows rows={2} cols={3} />
      ) : (
        <div className="panel-body stack" style={{ gap: 16 }}>
          {(agents.data ?? []).map((a) => (
            <AgentCard key={a.role} agent={a} profiles={profiles.data ?? []} onSaved={agents.reload} />
          ))}
        </div>
      )}
    </Panel>
  );
}

function AgentCard({ agent, profiles, onSaved }: { agent: Agent; profiles: LLMProfile[]; onSaved: () => void }) {
  const { t } = useI18n();
  const toast = useToast();
  const [enabled, setEnabled] = useState(agent.enabled);
  const [profileID, setProfileID] = useState<number>(agent.llm_profile_id ?? 0);
  const [instructions, setInstructions] = useState(agent.config.instructions ?? "");
  const [cap, setCap] = useState(agent.config.max_call_tokens ? String(agent.config.max_call_tokens) : "");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    setEnabled(agent.enabled);
    setProfileID(agent.llm_profile_id ?? 0);
    setInstructions(agent.config.instructions ?? "");
    setCap(agent.config.max_call_tokens ? String(agent.config.max_call_tokens) : "");
  }, [agent]);
  const capBad = cap !== "" && (Number(cap) < 2048 || Number(cap) > 32000);
  const dirty =
    enabled !== agent.enabled || profileID !== (agent.llm_profile_id ?? 0) || instructions !== (agent.config.instructions ?? "") || (Number(cap) || 0) !== (agent.config.max_call_tokens || 0);
  const bytes = new TextEncoder().encode(instructions).length;

  const save = async () => {
    setBusy(true);
    try {
      await patch(`/agents/${agent.role}`, { enabled, llm_profile_id: profileID, config: { instructions: instructions.trim(), max_call_tokens: Number(cap) || 0 } });
      toast(t("agents.saved", { name: t(`agents.${agent.role}` as Key) }));
      onSaved();
    } catch (e) {
      toast(errorText(e, t), "bad");
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="agent-card" aria-labelledby={`agent-${agent.role}`}>
      <header className="row" style={{ justifyContent: "space-between" }}>
        <div className="stack" style={{ gap: 2 }}>
          <h3 id={`agent-${agent.role}`}>{t(`agents.${agent.role}` as Key)}</h3>
          <span className="small ink-2">{t(`agents.${agent.role}.what` as Key)}</span>
        </div>
        <label className="check">
          <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
          {t("agents.enabled")}
        </label>
      </header>
      {!enabled ? (
        <Alert tone="warn" icon="alert">
          {t(`agents.${agent.role}.off` as Key)}
        </Alert>
      ) : null}
      <div className="form-grid two">
        <Field label={t("agents.profile")} hint={t("agents.profileHint")}>
          {(id) => (
            <select id={id} className="select" value={profileID} disabled={!enabled} onChange={(e) => setProfileID(Number(e.target.value))}>
              <option value={0}>{t("agents.runProfile")}</option>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} ({p.model})
                </option>
              ))}
            </select>
          )}
        </Field>
        <Field label={t("agents.cap")} hint={t("agents.capHint")} error={capBad ? t("agents.capRange") : undefined}>
          {(id) => <input id={id} className="input" type="number" min={2048} max={32000} step={1024} placeholder={t("agents.capDefault")} disabled={!enabled} value={cap} onChange={(e) => setCap(e.target.value)} />}
        </Field>
      </div>
      <Field label={t("agents.instructions")} hint={t("agents.instructionsHint", { n: bytes })} error={bytes > 4000 ? t("agents.instructionsLong") : undefined}>
        {(id) => <textarea id={id} className="textarea" rows={4} disabled={!enabled} value={instructions} placeholder={t(`agents.${agent.role}.example` as Key)} onChange={(e) => setInstructions(e.target.value)} />}
      </Field>
      <div className="row">
        <Button variant="primary" loading={busy} disabled={!dirty || capBad || bytes > 4000} onClick={() => void save()}>
          {t("common.save")}
        </Button>
        {agent.updated_at ? (
          <span className="small muted">
            {t("agents.updated")} <Time iso={agent.updated_at} />
          </span>
        ) : null}
      </div>
    </section>
  );
}

/* ---------- Research settings ---------- */

export function ResearchSection() {
  const { t } = useI18n();
  const toast = useToast();
  const q = useApi<ResearchSettingsResponse>("/settings/research");
  const [draft, setDraft] = useState<ResearchSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  useEffect(() => {
    if (q.data) setDraft(q.data.settings);
  }, [q.data]);
  if (!q.data || !draft) return <Panel title={t("settings.research")}>{q.error ? <Alert tone="bad">{errorText(q.error, t)}</Alert> : <SkeletonRows rows={4} cols={2} />}</Panel>;
  const d = q.data.defaults;
  const changed = JSON.stringify(draft) !== JSON.stringify(q.data.settings);
  const set = (p: Partial<ResearchSettings>) => setDraft({ ...draft, ...p });
  const setRun = (p: Partial<ResearchSettings["run_defaults"]>) => set({ run_defaults: { ...draft.run_defaults, ...p } });
  const setLab = (p: Partial<LabLimits>) => set({ lab_defaults: { ...draft.lab_defaults, ...p } });

  const save = async () => {
    setBusy(true);
    setError(undefined);
    try {
      await put("/settings/research", draft);
      toast(t("research.saved"));
      q.reload();
    } catch (e) {
      setError(errorText(e, t));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Panel title={t("settings.research")} meta={t("research.sub")}>
      <div className="panel-body stack" style={{ gap: 20 }}>
        <SettingGroup title={t("research.runs")} hint={t("research.runsHint")}>
          <Num label={t("newRun.workers")} value={draft.run_defaults.max_workers} min={1} max={4} def={d.run_defaults.max_workers} onChange={(v) => setRun({ max_workers: v })} />
          <Num label={t("research.wall")} value={draft.run_defaults.wall_seconds} min={60} max={3600} step={60} unit="s" def={d.run_defaults.wall_seconds} onChange={(v) => setRun({ wall_seconds: v })} />
          <Num label={t("research.tokens")} value={draft.run_defaults.max_tokens} min={4000} max={2000000} step={10000} def={d.run_defaults.max_tokens} onChange={(v) => setRun({ max_tokens: v })} />
        </SettingGroup>
        <SettingGroup title={t("research.mapping")} hint={t("research.mappingHint")}>
          <Num label={t("research.perPattern")} value={draft.candidates_per_pattern} min={1} max={10} def={d.candidates_per_pattern} onChange={(v) => set({ candidates_per_pattern: v })} />
          <Num label={t("research.maxCandidates")} value={draft.max_candidates} min={1} max={100} def={d.max_candidates} onChange={(v) => set({ max_candidates: v })} />
          <Num label={t("research.callTokens")} value={draft.max_call_tokens} min={2048} max={32000} step={1024} def={d.max_call_tokens} onChange={(v) => set({ max_call_tokens: v })} />
        </SettingGroup>
        <SettingGroup title={t("research.lab")} hint={t("research.labHint")}>
          <Num label={t("research.labWall")} value={draft.lab_defaults.wall_seconds} min={1} max={3600} unit="s" def={d.lab_defaults.wall_seconds} onChange={(v) => setLab({ wall_seconds: v })} />
          <Num label={t("research.labMemory")} value={draft.lab_defaults.memory_mb} min={64} max={16384} step={64} unit="MB" def={d.lab_defaults.memory_mb} onChange={(v) => setLab({ memory_mb: v })} />
          <Num label={t("research.labCpu")} value={draft.lab_defaults.cpu_cores} min={1} max={8} def={d.lab_defaults.cpu_cores} onChange={(v) => setLab({ cpu_cores: v })} />
          <Num label={t("research.labProcs")} value={draft.lab_defaults.max_processes} min={1} max={256} def={d.lab_defaults.max_processes} onChange={(v) => setLab({ max_processes: v })} />
          <Num label={t("research.ttl")} value={draft.approval_ttl_hours} min={1} max={168} unit="h" def={d.approval_ttl_hours} onChange={(v) => set({ approval_ttl_hours: v })} />
        </SettingGroup>
        <SettingGroup title={t("research.static")} hint={t("research.staticHint")}>
          {q.data.static_operations.length === 0 ? (
            <p className="small muted">{t("research.noStatic")}</p>
          ) : (
            q.data.static_operations.map((op) => (
              <label key={op} className="check">
                <input
                  type="checkbox"
                  checked={!draft.disabled_static_operations.includes(op)}
                  onChange={(e) =>
                    set({
                      disabled_static_operations: e.target.checked ? draft.disabled_static_operations.filter((x) => x !== op) : [...draft.disabled_static_operations, op],
                    })
                  }
                />
                <span className="mono">{op}</span>
              </label>
            ))
          )}
        </SettingGroup>
        <p className="small muted prose">{t("research.applies")}</p>
        {error ? (
          <Alert tone="bad" icon="alert">
            {error}
          </Alert>
        ) : null}
        <div className="row">
          <Button variant="primary" loading={busy} disabled={!changed} onClick={() => void save()}>
            {t("common.save")}
          </Button>
          {changed ? (
            <Button variant="ghost" onClick={() => setDraft(q.data!.settings)}>
              {t("common.cancel")}
            </Button>
          ) : null}
          <Button variant="ghost" icon="refresh" disabled={JSON.stringify(draft) === JSON.stringify(d)} onClick={() => setDraft(d)}>
            {t("research.reset")}
          </Button>
        </div>
      </div>
    </Panel>
  );
}

function SettingGroup({ title, hint, children }: { title: string; hint: string; children: React.ReactNode }) {
  return (
    <fieldset className="setting-group">
      <legend>{title}</legend>
      <p className="small ink-2">{hint}</p>
      <div className="advanced-grid">{children}</div>
    </fieldset>
  );
}

/** A bounded number input that shows the default and the range. */
function Num({ label, value, min, max, step = 1, unit, def, onChange }: { label: string; value: number; min: number; max: number; step?: number; unit?: string; def: number; onChange: (v: number) => void }) {
  const { t } = useI18n();
  const bad = !Number.isFinite(value) || value < min || value > max;
  return (
    <Field label={label} hint={t("research.range", { min: min.toLocaleString(), max: max.toLocaleString(), def: def.toLocaleString(), unit: unit ?? "" })} error={bad ? t("research.outOfRange", { min, max }) : undefined}>
      {(id) => <input id={id} className="input" type="number" min={min} max={max} step={step} value={Number.isFinite(value) ? value : ""} aria-invalid={bad || undefined} onChange={(e) => onChange(e.target.value === "" ? NaN : Number(e.target.value))} />}
    </Field>
  );
}

/* ---------- Server configuration (read-only) ---------- */

export function ServerConfigPanel() {
  const { t } = useI18n();
  const q = useApi<SystemConfig>("/system/config");
  const c = q.data;
  const rows: [Key, React.ReactNode][] = c
    ? [
        ["server.listen", <span className="mono">{c.listen}</span>],
        ["server.dataDir", <span className="mono">{c.data_dir}</span>],
        ["server.masterKey", t(`server.masterKey.${c.master_key_source}` as Key)],
        ["server.orchestrator", c.orchestrator ? t("server.runsAtOnce", { n: c.orchestrator_concurrency }) : t("server.off")],
        ["server.worker", c.worker === "bwrap" ? t("server.workerOn") : t("server.workerOff")],
        ["server.static", c.static_operations.length ? <span className="mono">{c.static_operations.join(", ")}</span> : t("server.none")],
        ["server.lab", <span className="mono">{c.lab_operations.join(", ")}</span>],
        ["server.repoHosts", c.repo_hosts?.length ? <span className="mono">{c.repo_hosts.join(", ")}</span> : t("server.repoHostsNone")],
        ["server.advisoryFeed", c.advisory_feed && c.advisory_feed !== "off" ? <span className="mono">{c.advisory_feed.toUpperCase()}</span> : t("server.off")],
        ["server.updates", `${t(`server.update.${c.auto_update}` as Key)} · ${c.update_interval} · ${c.update_repo}`],
      ]
    : [];
  return (
    <Panel title={t("server.title")} meta={t("server.sub")}>
      {!c ? (
        <SkeletonRows rows={5} cols={2} />
      ) : (
        <dl className="kv">
          {rows.map(([k, v]) => (
            <div key={k}>
              <dt>{t(k)}</dt>
              <dd>{v}</dd>
            </div>
          ))}
        </dl>
      )}
    </Panel>
  );
}
