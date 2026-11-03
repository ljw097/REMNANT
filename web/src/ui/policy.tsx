// The instance policy editor shared by first-run setup and Settings: three
// profiles that preset the rules, then each rule on its own. Changing a rule
// away from its profile's preset makes the policy "custom".

import type { InstancePolicy, Presets, Profile } from "../api/types";
import { useI18n, type Key } from "../i18n";
import { Icon, type IconName } from "./Icon";

export const fallbackPresets: Presets = {
  personal: { profile: "personal", self_approval: true, self_review: true, admin_lab_approval: false, account_requests: false, session_hours: 720 },
  team: { profile: "team", self_approval: false, self_review: false, admin_lab_approval: false, account_requests: false, session_hours: 168 },
  managed: { profile: "managed", self_approval: false, self_review: false, admin_lab_approval: true, account_requests: true, session_hours: 12 },
};

const profiles: { id: Exclude<Profile, "custom">; icon: IconName }[] = [
  { id: "personal", icon: "user" },
  { id: "team", icon: "users" },
  { id: "managed", icon: "shield" },
];

type Rule = "self_approval" | "self_review" | "admin_lab_approval" | "account_requests";
const rules: Rule[] = ["self_approval", "self_review", "admin_lab_approval", "account_requests"];
const sessionChoices = [8, 12, 24, 72, 168, 720];

/** Names the profile a set of rules matches, as the server does. */
export function classify(p: InstancePolicy, presets: Presets): InstancePolicy {
  for (const id of ["personal", "team", "managed"] as const) {
    const q = presets[id];
    if (rules.every((r) => q[r] === p[r]) && q.session_hours === p.session_hours) return { ...p, profile: id };
  }
  return { ...p, profile: "custom" };
}

/** value null shows only the profiles, for a first choice. */
export function PolicyEditor({ value, onChange, presets = fallbackPresets }: { value: InstancePolicy | null; onChange: (p: InstancePolicy) => void; presets?: Presets }) {
  const { t } = useI18n();
  return (
    <div className="stack" style={{ gap: 16 }}>
      <div className="mode-cards three" role="radiogroup" aria-label={t("policy.profile")}>
        {profiles.map(({ id, icon }) => (
          <button key={id} type="button" role="radio" aria-checked={value?.profile === id} className="mode-card" onClick={() => onChange({ ...presets[id] })}>
            <span className="row" style={{ gap: 10 }}>
              <Icon name={icon} size={18} />
              <strong>{t(`profile.${id}` as Key)}</strong>
            </span>
            <span className="small ink-2">{t(`profile.${id}.for` as Key)}</span>
          </button>
        ))}
      </div>
      {value ? <Rules value={value} onChange={(p) => onChange(classify(p, presets))} /> : null}
    </div>
  );
}

function Rules({ value, onChange }: { value: InstancePolicy; onChange: (p: InstancePolicy) => void }) {
  const { t } = useI18n();
  const set = (patch: Partial<InstancePolicy>) => onChange({ ...value, ...patch });
  return (
    <>
      <fieldset className="rules">
        <legend>
          {t("policy.rules")}
          {value.profile === "custom" ? (
            <span className="state" data-tone="info" data-form="solid">
              {t("profile.custom")}
            </span>
          ) : null}
        </legend>
        {rules.map((r) => (
          <label key={r} className="rule">
            <input type="checkbox" checked={value[r]} onChange={(e) => set({ [r]: e.target.checked })} />
            <span>
              <strong>{t(`policy.${r}` as Key)}</strong>
              <span className="small ink-2">{t(`policy.${r}.explain` as Key)}</span>
            </span>
          </label>
        ))}
        <label className="rule">
          <Icon name="clock" />
          <span>
            <strong>{t("policy.session_hours")}</strong>
            <span className="small ink-2">{t("policy.session_hours.explain")}</span>
          </span>
          <select className="select" style={{ width: "auto" }} value={value.session_hours} onChange={(e) => set({ session_hours: Number(e.target.value) })}>
            {[...new Set([...sessionChoices, value.session_hours])]
              .sort((a, b) => a - b)
              .map((h) => (
                <option key={h} value={h}>
                  {h % 24 === 0 ? t("policy.days", { n: h / 24 }) : t("policy.hours", { n: h })}
                </option>
              ))}
          </select>
        </label>
      </fieldset>
      {value.self_approval && value.admin_lab_approval ? <p className="small muted">{t("policy.selfAndAdmin")}</p> : null}
    </>
  );
}
