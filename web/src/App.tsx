import { useEffect, useState, type ReactNode } from "react";
import { appName, SessionProvider, useSession } from "./session";
import { useI18n, type Key } from "./i18n";
import { Link, match, navigate, useLocation } from "./router";
import { Icon, type IconName } from "./ui/Icon";
import { Button, ToastProvider } from "./ui/components";
import { SignIn } from "./pages/SignIn";
import { Setup } from "./pages/Setup";
import { ForcedPasswordChange } from "./pages/Password";
import { Dashboard } from "./pages/Dashboard";
import { RunsPage } from "./pages/Runs";
import { AnalyzePage, IntakePage } from "./pages/Analyze";
import { NewRunPage } from "./pages/NewRun";
import { RunDetailPage } from "./pages/RunDetail";
import { HypothesisPage } from "./pages/Hypothesis";
import { ReviewQueuePage } from "./pages/Review";
import { ApprovalsPage } from "./pages/Approvals";
import { FindingsPage } from "./pages/Findings";
import { ProjectsPage } from "./pages/Projects";
import { ProjectDetailPage } from "./pages/ProjectDetail";
import { PatternsPage } from "./pages/Patterns";
import { PatternDetailPage } from "./pages/PatternDetail";
import { SettingsPage } from "./pages/Settings";
import type { Role, Summary } from "./api/types";

export function App() {
  return (
    <SessionProvider signIn={(p) => <SignIn {...p} />} setup={(p) => <Setup {...p} />} changePassword={<ForcedPasswordChange />}>
      <ToastProvider>
        <Shell />
      </ToastProvider>
    </SessionProvider>
  );
}

interface NavItem {
  to: string;
  icon: IconName;
  label: Key;
  roles?: Role[];
  count?: (s: Summary) => number;
  attention?: boolean;
}

const navGroups: { label: Key; items: NavItem[] }[] = [
  {
    label: "nav.group.work",
    items: [
      { to: "/", icon: "dashboard", label: "nav.dashboard" },
      { to: "/review", icon: "review", label: "nav.review", roles: ["reviewer"], count: (s) => s.awaiting_triage, attention: true },
      { to: "/approvals", icon: "approvals", label: "nav.approvals", count: (s) => s.pending_approvals, attention: true },
      { to: "/analyze", icon: "branch", label: "nav.analyze", roles: ["researcher"] },
      { to: "/runs", icon: "runs", label: "nav.runs", count: (s) => s.active_runs },
      { to: "/findings", icon: "findings", label: "nav.findings", count: (s) => s.validated },
    ],
  },
  {
    label: "nav.group.library",
    items: [
      { to: "/projects", icon: "projects", label: "nav.projects" },
      { to: "/patterns", icon: "patterns", label: "nav.patterns" },
    ],
  },
];

function Shell() {
  const { me, branding, summary, version, prefs, can, signOut, policy } = useSession();
  const { t } = useI18n();
  const { path } = useLocation();
  const [navOpen, setNavOpen] = useState(false);

  // First load lands on the user's start page.
  useEffect(() => {
    if (path === "/" && prefs.startPage !== "/" && sessionStorage.getItem("remnant.landed") !== "1") {
      sessionStorage.setItem("remnant.landed", "1");
      navigate(prefs.startPage, { replace: true });
    } else {
      sessionStorage.setItem("remnant.landed", "1");
    }
  }, []);
  useEffect(() => setNavOpen(false), [path]);

  const active = (to: string) => (to === "/" ? path === "/" : path === to || path.startsWith(to + "/"));
  const update = version?.update;

  return (
    <div className="shell" data-nav={navOpen ? "open" : "closed"}>
      <aside className="sidebar" aria-label={t("nav.label")}>
        <Link to="/" className="brand">
          <span className="brand-mark" aria-hidden="true">
            <Icon name="shield" size={14} />
          </span>
          {appName(branding)}
        </Link>
        <nav className="nav">
          {navGroups.map((g) => (
            <div className="nav-group" key={g.label}>
              <h2>{t(g.label)}</h2>
              {g.items
                .filter((i) => !i.roles || can(...i.roles))
                .map((i) => {
                  const n = summary && i.count ? i.count(summary) : 0;
                  return (
                    <Link key={i.to} to={i.to} aria-current={active(i.to) ? "page" : undefined}>
                      <Icon name={i.icon} />
                      {t(i.label)}
                      {n > 0 ? (
                        <span className={i.attention ? "count attention" : "count"} aria-label={t("nav.countLabel", { n })}>
                          {n}
                        </span>
                      ) : null}
                    </Link>
                  );
                })}
            </div>
          ))}
          <div className="nav-group">
            <h2>{t("nav.group.settings")}</h2>
            <Link to="/settings" aria-current={active("/settings") ? "page" : undefined}>
              <Icon name="settings" />
              {t("nav.settings")}
              {update?.available && can("admin") ? <span className="count attention">1</span> : null}
            </Link>
          </div>
        </nav>
        <div className="sidebar-foot">
          <div className="whoami">
            <span className="avatar" aria-hidden="true">
              {me.username.slice(0, 2)}
            </span>
            <span style={{ minWidth: 0, flex: 1 }}>
              <strong style={{ display: "block", color: "var(--ink)" }}>{me.username}</strong>
              {policy.profile === "personal" ? t("profile.personal") : t(`role.${me.role}` as Key)}
            </span>
            <Button variant="ghost" size="sm" iconOnly icon="logout" aria-label={t("nav.signOut")} title={t("nav.signOut")} onClick={signOut} />
          </div>
          <div className="version-line">
            <Icon name="update" size={12} />
            {version ? version.version : "…"}
            {update?.available ? (
              <Link to="/settings/system" style={{ color: "var(--accent)" }}>
                {t("nav.updateAvailable", { v: update.latest ?? "" })}
              </Link>
            ) : null}
          </div>
        </div>
      </aside>
      <div className="scrim" onClick={() => setNavOpen(false)} />
      <div className="main">
        <Routes onMenu={() => setNavOpen((o) => !o)} />
      </div>
    </div>
  );
}

/** Top bar with breadcrumbs; pages provide their own crumbs through Frame. */
export function Frame({ crumbs, children, actions }: { crumbs: { label: ReactNode; to?: string }[]; children: ReactNode; actions?: ReactNode }) {
  const { t } = useI18n();
  const { prefs, savePrefs } = useSession();
  const nextTheme = prefs.theme === "dark" ? "light" : prefs.theme === "light" ? "system" : "dark";
  return (
    <>
      <header className="topbar">
        <Button className="menu-btn" variant="ghost" iconOnly icon="menu" aria-label={t("nav.menu")} onClick={() => window.dispatchEvent(new Event("remnant:menu"))} />
        <nav className="crumbs" aria-label={t("nav.breadcrumbs")}>
          {crumbs.map((c, i) => (
            <span key={i} className="row" style={{ gap: 6, flexWrap: "nowrap", minWidth: 0 }}>
              {i > 0 ? <Icon name="chevronRight" size={12} /> : null}
              {c.to && i < crumbs.length - 1 ? <Link to={c.to}>{c.label}</Link> : <span className="here">{c.label}</span>}
            </span>
          ))}
        </nav>
        <div className="topbar-actions">
          {actions}
          <Button
            variant="ghost"
            iconOnly
            icon={prefs.theme === "dark" ? "moon" : prefs.theme === "light" ? "sun" : "monitor"}
            aria-label={t("prefs.theme.cycle", { theme: t(`prefs.theme.${prefs.theme}` as Key) })}
            title={t("prefs.theme.cycle", { theme: t(`prefs.theme.${prefs.theme}` as Key) })}
            onClick={() => void savePrefs({ theme: nextTheme })}
          />
          <Button
            variant="ghost"
            size="sm"
            icon="globe"
            aria-label={t("prefs.language.switch")}
            onClick={() => void savePrefs({ language: prefs.language === "ko" ? "en" : "ko" })}
          >
            {prefs.language === "ko" ? "EN" : "한국어"}
          </Button>
        </div>
      </header>
      <main className="content" id="main">
        {children}
      </main>
    </>
  );
}

function Routes({ onMenu }: { onMenu: () => void }) {
  const { path } = useLocation();
  const { t } = useI18n();
  useEffect(() => {
    window.addEventListener("remnant:menu", onMenu);
    return () => window.removeEventListener("remnant:menu", onMenu);
  }, [onMenu]);

  let m: Record<string, string> | null;
  if (path === "/") return <Dashboard />;
  if (path === "/runs") return <RunsPage />;
  if (path === "/analyze") return <AnalyzePage />;
  if ((m = match("/analyze/:id", path))) return <IntakePage id={Number(m.id)} />;
  if (path === "/runs/new") return <NewRunPage />;
  if ((m = match("/runs/:id", path))) return <RunDetailPage id={Number(m.id)} />;
  if (path === "/review") return <ReviewQueuePage />;
  if ((m = match("/hypotheses/:id", path))) return <HypothesisPage id={Number(m.id)} />;
  if (path === "/approvals") return <ApprovalsPage />;
  if (path === "/findings") return <FindingsPage />;
  if (path === "/projects") return <ProjectsPage />;
  if ((m = match("/projects/:id", path))) return <ProjectDetailPage id={Number(m.id)} />;
  if (path === "/patterns") return <PatternsPage />;
  if ((m = match("/patterns/:id", path))) return <PatternDetailPage id={Number(m.id)} />;
  if (path === "/settings") return <SettingsPage section="preferences" />;
  if ((m = match("/settings/:section", path))) return <SettingsPage section={m.section ?? "preferences"} />;
  return (
    <Frame crumbs={[{ label: t("notFound.title") }]}>
      <div className="empty">
        <h3>{t("notFound.title")}</h3>
        <p>{t("notFound.body")}</p>
        <Link to="/">{t("notFound.home")}</Link>
      </div>
    </Frame>
  );
}
