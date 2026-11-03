import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { ApiError, get, onUnauthorized, put, setToken, getToken } from "./api/client";
import type { Branding, Principal, Role, Summary, VersionInfo } from "./api/types";
import { detectLang, I18nProvider, type Lang } from "./i18n";
import { applyAppearance, defaultPrefs, normalizePrefs, type Prefs } from "./prefs";

interface Session {
  me: Principal;
  branding: Branding;
  setBranding: (b: Branding) => void;
  prefs: Prefs;
  savePrefs: (p: Partial<Prefs>) => Promise<void>;
  version: VersionInfo | null;
  reloadVersion: () => void;
  summary: Summary | null;
  /** "personal": one person may approve their own requests (recorded); "team": two-person rules. */
  mode: "team" | "personal";
  setMode: (m: "team" | "personal") => void;
  reloadSummary: () => void;
  signOut: () => void;
  can: (...roles: Role[]) => boolean;
}

const Ctx = createContext<Session | null>(null);

export function useSession(): Session {
  const s = useContext(Ctx);
  if (!s) throw new Error("useSession outside SessionProvider");
  return s;
}

export const appName = (b: Branding) => b.name.trim() || "REMNANT";

type Phase = { kind: "loading" } | { kind: "signedOut"; error?: string } | { kind: "ready"; me: Principal; prefs: Prefs };

/**
 * Loads the public branding, then the session for the stored token. Renders
 * `signIn` until a token is accepted, `children` after.
 */
type Gate = { onToken: (t: string) => Promise<void>; branding: Branding; lang: Lang; setLang: (l: Lang) => void; error?: string };

export function SessionProvider({ children, signIn, setup }: { children: ReactNode; signIn: (p: Gate) => ReactNode; setup: (p: Gate) => ReactNode }) {
  const [branding, setBranding] = useState<Branding>({ name: "", accent: "" });
  const [lang, setLang] = useState<Lang>(detectLang());
  const [phase, setPhase] = useState<Phase>({ kind: getToken() ? "loading" : "signedOut" });
  const [version, setVersion] = useState<VersionInfo | null>(null);
  const [summary, setSummary] = useState<Summary | null>(null);
  const [tick, setTick] = useState(0);

  const [setupRequired, setSetupRequired] = useState(false);
  const [mode, setMode] = useState<"team" | "personal">("team");
  useEffect(() => {
    get<{ required: boolean }>("/setup")
      .then((s) => setSetupRequired(s.required))
      .catch(() => undefined);
  }, []);
  useEffect(() => {
    if (phase.kind === "ready") get<{ mode: "team" | "personal" }>("/instance").then((i) => setMode(i.mode)).catch(() => undefined);
  }, [phase.kind]);

  useEffect(() => {
    get<Branding>("/branding").then(setBranding).catch(() => undefined);
  }, []);

  const start = useCallback(async () => {
    try {
      const me = await get<Principal>("/me");
      const raw = await get<unknown>("/me/preferences").catch(() => ({}));
      setPhase({ kind: "ready", me, prefs: normalizePrefs(raw, me.role, detectLang()) });
    } catch (e) {
      setToken(null);
      const msg = e instanceof ApiError && e.status === 401 ? "signin.error.invalid" : "signin.error.unreachable";
      setPhase({ kind: "signedOut", error: msg });
      throw e;
    }
  }, []);

  useEffect(() => {
    if (phase.kind === "loading") void start().catch(() => undefined);
    // Only on first mount.
  }, []);

  useEffect(
    () =>
      onUnauthorized(() => {
        setToken(null);
        setPhase({ kind: "signedOut", error: "signin.error.expired" });
      }),
    [],
  );

  const ready = phase.kind === "ready" ? phase : null;
  const effectiveLang = ready ? ready.prefs.language : lang;

  useEffect(() => {
    const p = ready ? ready.prefs : { ...defaultPrefs("researcher", lang), language: lang };
    applyAppearance(p, branding.accent);
    if (p.theme !== "system") return;
    const mq = matchMedia("(prefers-color-scheme: dark)");
    const on = () => applyAppearance(p, branding.accent);
    mq.addEventListener("change", on);
    return () => mq.removeEventListener("change", on);
  }, [ready, lang, branding.accent]);

  useEffect(() => {
    document.title = appName(branding);
  }, [branding]);

  // Version and the counts behind navigation badges refresh in the background.
  useEffect(() => {
    if (!ready) return;
    const ac = new AbortController();
    get<VersionInfo>("/system/version", ac.signal).then(setVersion).catch(() => undefined);
    get<Summary>("/summary", ac.signal).then(setSummary).catch(() => undefined);
    const id = setInterval(() => {
      if (document.visibilityState === "visible") get<Summary>("/summary").then(setSummary).catch(() => undefined);
    }, 30_000);
    return () => {
      ac.abort();
      clearInterval(id);
    };
  }, [ready?.me.user_id, tick]);

  const value = useMemo<Session | null>(() => {
    if (!ready) return null;
    return {
      me: ready.me,
      branding,
      setBranding,
      prefs: ready.prefs,
      savePrefs: async (p) => {
        const next = { ...ready.prefs, ...p };
        setPhase({ ...ready, prefs: next });
        await put("/me/preferences", next);
      },
      version,
      reloadVersion: () => get<VersionInfo>("/system/version").then(setVersion).catch(() => undefined),
      summary,
      reloadSummary: () => setTick((n) => n + 1),
      signOut: () => {
        setToken(null);
        setPhase({ kind: "signedOut" });
      },
      can: (...roles) => ready.me.role === "admin" || roles.includes(ready.me.role),
      mode,
      setMode,
    };
  }, [ready, branding, version, summary, mode]);

  return (
    <I18nProvider lang={effectiveLang}>
      {phase.kind === "loading" ? (
        <div className="empty center" aria-busy="true" />
      ) : value ? (
        <Ctx.Provider value={value}>{children}</Ctx.Provider>
      ) : (
        (setupRequired && phase.kind === "signedOut" ? setup : signIn)({
          branding,
          lang,
          setLang,
          error: phase.kind === "signedOut" ? phase.error : undefined,
          onToken: async (t) => {
            setToken(t);
            await start();
            setSetupRequired(false);
            get<Branding>("/branding").then(setBranding).catch(() => undefined);
          },
        })
      )}
    </I18nProvider>
  );
}
