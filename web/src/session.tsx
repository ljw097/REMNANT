import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { get, onUnauthorized, post, put } from "./api/client";
import type { AuthStatus, Branding, InstancePolicy, Presets, Principal, Role, Summary, VersionInfo } from "./api/types";
import { detectLang, I18nProvider, type Lang } from "./i18n";
import { navigate } from "./router";
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
  /** How the instance is used: self-approval, admin-only lab approval, account requests… */
  policy: InstancePolicy;
  presets: Presets | null;
  setPolicy: (p: InstancePolicy) => void;
  reloadSummary: () => void;
  signOut: () => void;
  /** Re-reads the principal, e.g. after a temporary password was changed. */
  refreshMe: () => Promise<void>;
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

/** What the sign-in and setup pages get before anyone is signed in. */
export type Gate = {
  /** Called once the server has set the session cookie. */
  onSignedIn: () => Promise<void>;
  branding: Branding;
  lang: Lang;
  setLang: (l: Lang) => void;
  error?: string;
  status: AuthStatus | null;
};

const teamPolicy: InstancePolicy = { profile: "team", self_approval: false, self_review: false, admin_lab_approval: false, account_requests: false, session_hours: 168 };

/**
 * Loads the public branding, then the session behind the browser's cookie.
 * Renders `setup` on a new instance, `signIn` until someone signs in,
 * `changePassword` while they hold a temporary password, `children` after.
 */
export function SessionProvider({
  children,
  signIn,
  setup,
  changePassword,
}: {
  children: ReactNode;
  signIn: (p: Gate) => ReactNode;
  setup: (p: Gate) => ReactNode;
  changePassword: ReactNode;
}) {
  const [branding, setBranding] = useState<Branding>({ name: "", accent: "" });
  const [lang, setLang] = useState<Lang>(detectLang());
  const [phase, setPhase] = useState<Phase>({ kind: "loading" });
  const [version, setVersion] = useState<VersionInfo | null>(null);
  const [summary, setSummary] = useState<Summary | null>(null);
  const [tick, setTick] = useState(0);

  const [status, setStatus] = useState<AuthStatus | null>(null);
  const [policy, setPolicy] = useState<InstancePolicy>(teamPolicy);
  const [presets, setPresets] = useState<Presets | null>(null);
  useEffect(() => {
    if (phase.kind === "signedOut" && status === null) get<AuthStatus>("/auth/status").then(setStatus).catch(() => undefined);
  }, [phase.kind, status]);
  useEffect(() => {
    if (phase.kind === "ready")
      get<{ policy: InstancePolicy; presets: Presets }>("/instance")
        .then((i) => {
          setPolicy(i.policy);
          setPresets(i.presets);
        })
        .catch(() => undefined);
  }, [phase.kind]);

  useEffect(() => {
    get<Branding>("/branding").then(setBranding).catch(() => undefined);
  }, []);

  const start = useCallback(async () => {
    try {
      const st = await get<AuthStatus>("/auth/status");
      setStatus(st);
      if (!st.user) {
        setPhase({ kind: "signedOut" });
        return;
      }
      const me = st.user;
      const raw = await get<unknown>("/me/preferences").catch(() => ({}));
      setPhase({ kind: "ready", me, prefs: normalizePrefs(raw, me.role, detectLang()) });
    } catch (e) {
      setPhase({ kind: "signedOut", error: "signin.error.unreachable" });
      throw e;
    }
  }, []);

  useEffect(() => {
    void start().catch(() => undefined);
  }, []);

  useEffect(
    () =>
      onUnauthorized(() => {
        setPhase((p) => (p.kind === "ready" ? { kind: "signedOut", error: "signin.error.expired" } : p));
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
    // A session holding a temporary password may only change it.
    if (!ready || ready.me.must_change_password) return;
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
  }, [ready?.me.user_id, ready?.me.must_change_password, tick]);

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
        void post("/auth/logout").catch(() => undefined);
        // The next person to sign in starts from the top, not on this user's page.
        navigate("/", { replace: true });
        setPhase({ kind: "signedOut" });
      },
      refreshMe: async () => {
        const me = await get<Principal>("/me");
        setPhase({ ...ready, me });
      },
      can: (...roles) => ready.me.role === "admin" || roles.includes(ready.me.role),
      policy,
      presets,
      setPolicy,
    };
  }, [ready, branding, version, summary, policy, presets]);

  return (
    <I18nProvider lang={effectiveLang}>
      {phase.kind === "loading" ? (
        <div className="empty center" aria-busy="true" />
      ) : value ? (
        <Ctx.Provider value={value}>{value.me.must_change_password ? changePassword : children}</Ctx.Provider>
      ) : (
        (status?.setup_required ? setup : signIn)({
          branding,
          lang,
          setLang,
          status,
          error: phase.kind === "signedOut" ? phase.error : undefined,
          onSignedIn: async () => {
            await start();
            get<Branding>("/branding").then(setBranding).catch(() => undefined);
          },
        })
      )}
    </I18nProvider>
  );
}
