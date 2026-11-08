// The note a run shows when it produced no hypotheses: why, read from the
// events its generation stages emit, with links to the regions, the LLM
// records and the activity log.

import type { RunEvent } from "../api/types";
import { useI18n } from "../i18n";
import { Link } from "../router";
import { useSession } from "../session";
import { Button } from "./components";
import { Icon } from "./Icon";

/**
 * Explains a run that produced no hypotheses, from the events its
 * generation stages emit, so "0" is never left unexplained.
 */
export function WhyNoHypotheses({ events, onTab }: { events: RunEvent[]; onTab: (t: "regions" | "calls" | "activity") => void }) {
  const { t } = useI18n();
  const { can } = useSession();
  const last = (type: string) => [...events].reverse().find((e) => e.event_type === type);
  const n = (e: RunEvent | undefined, k: string) => (typeof e?.payload?.[k] === "number" ? (e.payload[k] as number) : 0);
  const skipped = events.some((e) => e.event_type === "stage.skipped" && e.payload?.reason === "researcher_disabled");
  const mapped = last("stage.mapped");
  const gen = last("stage.generated");
  const firstError = events.find((e) => e.event_type === "agent.error")?.message;

  const reasons: { tone: "info" | "warn"; text: string; hint?: string; link?: { to: string; label: string } }[] = [];
  if (skipped) {
    reasons.push({ tone: "warn", text: t("run.why.researcherOff"), link: can("admin") ? { to: "/settings/agents", label: t("settings.agents") } : undefined });
  } else if (mapped && n(mapped, "candidates") === 0) {
    reasons.push({ tone: "warn", text: t("run.why.noRegions"), hint: t("run.why.noRegionsHint") });
  } else if (gen) {
    if (n(gen, "failed") > 0) {
      const truncated = /truncated at max/.test(firstError ?? "");
      reasons.push({
        tone: "warn",
        text: t("run.why.failed", { n: n(gen, "failed"), error: firstError ?? "" }),
        hint: truncated ? t("run.why.truncatedHint") : t("run.why.failedHint"),
        link: can("admin") ? (truncated ? { to: "/settings/research", label: t("settings.research") } : { to: "/settings/models", label: t("settings.models") }) : undefined,
      });
    }
    if (n(gen, "refused") > 0) reasons.push({ tone: "warn", text: t("run.why.refused", { n: n(gen, "refused") }) });
    if (n(gen, "invalid") > 0) reasons.push({ tone: "warn", text: t("run.why.invalid", { n: n(gen, "invalid") }) });
    if (gen.payload?.budget_hit === true) reasons.push({ tone: "warn", text: t("run.why.budget"), hint: t("run.why.budgetHint") });
    if (n(gen, "declined") > 0) reasons.push({ tone: "info", text: t("run.why.declined", { n: n(gen, "declined") }), hint: t("run.why.declinedHint") });
  }
  if (reasons.length === 0) return null;
  const tone = reasons.some((r) => r.tone === "warn") ? "warn" : "info";
  return (
    <div className="alert" data-tone={tone} role="status">
      <Icon name={tone === "warn" ? "alert" : "info"} />
      <div className="stack" style={{ gap: 6 }}>
        <strong>{t("run.why.title")}</strong>
        <ul className="why-list">
          {reasons.map((r, i) => (
            <li key={i}>
              {r.text}
              {r.hint ? <span className="small ink-2"> {r.hint}</span> : null}
              {r.link ? (
                <>
                  {" "}
                  <Link to={r.link.to} className="small">
                    {r.link.label}
                  </Link>
                </>
              ) : null}
            </li>
          ))}
        </ul>
        <span>
          <span className="row">
            <Button size="sm" variant="ghost" onClick={() => onTab("regions")}>
              {t("detail.open.regions")}
            </Button>
            <Button size="sm" variant="ghost" onClick={() => onTab("calls")}>
              {t("detail.open.calls")}
            </Button>
            <Button size="sm" variant="ghost" onClick={() => onTab("activity")}>
              {t("run.why.activity")}
            </Button>
          </span>
        </span>
      </div>
    </div>
  );
}
