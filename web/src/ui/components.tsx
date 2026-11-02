import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type ReactNode,
} from "react";
import { useI18n, type Key } from "../i18n";
import { Icon, type IconName } from "./Icon";
import type { StateLook } from "./status";
import { ApiError } from "../api/client";
import { navigate } from "../router";

/* ---------- Buttons ---------- */

type BtnProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "danger" | "ghost" | "default";
  size?: "sm" | "lg";
  icon?: IconName;
  loading?: boolean;
  iconOnly?: boolean;
  kbd?: string;
};

export function Button({ variant = "default", size, icon, loading, iconOnly, kbd, children, className, disabled, ...rest }: BtnProps) {
  const cls = ["btn", variant !== "default" && variant, size, iconOnly && "icon", className].filter(Boolean).join(" ");
  return (
    <button type="button" className={cls} disabled={disabled || loading} aria-busy={loading || undefined} {...rest}>
      {loading ? <Icon name="refresh" className="spin" /> : icon ? <Icon name={icon} /> : null}
      {children}
      {kbd ? <kbd>{kbd}</kbd> : null}
    </button>
  );
}

/* ---------- State and small data ---------- */

export function State({ look, title }: { look: StateLook; title?: string }) {
  const { t } = useI18n();
  return (
    <span className="state" data-tone={look.tone} data-form={look.form} title={title}>
      {t(look.label)}
    </span>
  );
}

export function Priority({ level }: { level: "low" | "medium" | "high" }) {
  const { t } = useI18n();
  return (
    <span className="prio" data-level={level} title={t("hyp.priority.hint")}>
      <i />
      <i />
      <i />
      {t(`priority.${level}` as Key)}
    </span>
  );
}

export function Hash({ value, length = 12, label }: { value: string; length?: number; label?: string }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  if (!value) return <span className="muted">—</span>;
  const shown = value.startsWith("sha256:") ? "sha256:" + value.slice(7, 7 + length) : value.slice(0, length);
  return (
    <span className="hash" title={value}>
      <span>
        {label ? <span className="visually-hidden">{label} </span> : null}
        {shown}
        {value.length > shown.length ? "…" : ""}
      </span>
      <button
        type="button"
        aria-label={copied ? t("common.copied") : t("common.copy")}
        onClick={() => {
          void navigator.clipboard?.writeText(value).then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
          });
        }}
      >
        <Icon name={copied ? "check" : "copy"} size={13} />
      </button>
    </span>
  );
}

export function Time({ iso, abs }: { iso: string | null | undefined; abs?: boolean }) {
  const { ago, dateTime } = useI18n();
  if (!iso) return <span className="muted">—</span>;
  return (
    <time dateTime={iso} title={dateTime(iso)}>
      {abs ? dateTime(iso) : ago(iso)}
    </time>
  );
}

export function Ref({ kind, id, to }: { kind: "H" | "E" | "R" | "A" | "P" | "T" | "X"; id: number; to?: string }) {
  const text = `[${kind}-${id}]`;
  return to ? (
    <a className="ref" href={to} onClick={(e) => routeClick(e, to)}>
      {text}
    </a>
  ) : (
    <span className="ref">{text}</span>
  );
}

function routeClick(e: React.MouseEvent, to: string) {
  if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey) return;
  e.preventDefault();
  navigate(to);
}

/* ---------- Layout pieces ---------- */

export function PageHead({ title, sub, actions }: { title: ReactNode; sub?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="page-head">
      <div className="titles">
        <h1>{title}</h1>
        {sub ? <p className="sub">{sub}</p> : null}
      </div>
      {actions ? <div className="actions">{actions}</div> : null}
    </div>
  );
}

export function Panel({ title, meta, tools, children, foot, id, size }: { title?: ReactNode; meta?: ReactNode; tools?: ReactNode; children: ReactNode; foot?: ReactNode; id?: string; size?: string }) {
  const hid = useId();
  return (
    <section className="panel" aria-labelledby={title ? hid : undefined} id={id} data-size={size}>
      {title ? (
        <header className="panel-head">
          <h2 id={hid}>{title}</h2>
          {meta ? <span className="meta">{meta}</span> : null}
          {tools ? <div className="tools">{tools}</div> : null}
        </header>
      ) : null}
      {children}
      {foot ? <footer className="panel-foot">{foot}</footer> : null}
    </section>
  );
}

export function Empty({ title, body, action, center }: { title: ReactNode; body?: ReactNode; action?: ReactNode; center?: boolean }) {
  return (
    <div className={center ? "empty center" : "empty"}>
      <h3>{title}</h3>
      {body ? <p>{body}</p> : null}
      {action}
    </div>
  );
}

export function SkeletonRows({ rows = 5, cols = 4 }: { rows?: number; cols?: number }) {
  return (
    <div aria-busy="true" style={{ padding: "8px var(--pad)" }}>
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} style={{ display: "grid", gridTemplateColumns: `2fr repeat(${cols - 1}, 1fr)`, gap: 16, padding: "10px 0" }}>
          {Array.from({ length: cols }, (_, j) => (
            <span key={j} className="skeleton" style={{ width: `${60 + ((i * 7 + j * 13) % 35)}%` }} />
          ))}
        </div>
      ))}
    </div>
  );
}

export function ErrorNote({ error, retry }: { error: unknown; retry?: () => void }) {
  const { t } = useI18n();
  return (
    <div className="alert" data-tone="bad" role="alert" style={{ margin: "var(--pad)" }}>
      <Icon name="alert" />
      <p style={{ flex: 1 }}>{errorText(error, t)}</p>
      {retry ? (
        <Button size="sm" onClick={retry}>
          {t("common.retry")}
        </Button>
      ) : null}
    </div>
  );
}

export function errorText(e: unknown, t: (k: Key, v?: Record<string, string | number>) => string): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return t("error.network");
    if (e.status === 403) return t("error.forbidden", { detail: e.message });
    if (e.status === 404) return t("error.notFound");
    if (e.status >= 500) return t("error.server");
    return e.message;
  }
  return String((e as Error)?.message ?? e);
}

export function Alert({ tone = "info", icon = "info", children }: { tone?: "info" | "warn" | "bad" | "ok"; icon?: IconName; children: ReactNode }) {
  return (
    <div className="alert" data-tone={tone}>
      <Icon name={icon} />
      <div>{children}</div>
    </div>
  );
}

/* ---------- Form fields ---------- */

export function Field({ label, hint, error, children, id }: { label: ReactNode; hint?: ReactNode; error?: ReactNode; children: (id: string) => ReactNode; id?: string }) {
  const auto = useId();
  const fid = id ?? auto;
  return (
    <div className="field">
      <label htmlFor={fid}>{label}</label>
      {children(fid)}
      {error ? <span className="error">{error}</span> : hint ? <span className="hint">{hint}</span> : null}
    </div>
  );
}

export function Segmented<V extends string>({ value, options, onChange, label }: { value: V; options: { value: V; label: ReactNode; icon?: IconName }[]; onChange: (v: V) => void; label: string }) {
  return (
    <div className="segmented" role="group" aria-label={label}>
      {options.map((o) => (
        <button key={o.value} type="button" aria-pressed={o.value === value} onClick={() => onChange(o.value)}>
          {o.icon ? <Icon name={o.icon} size={14} /> : null}
          {o.label}
        </button>
      ))}
    </div>
  );
}

/* ---------- Tabs ---------- */

export function Tabs<V extends string>({ value, onChange, tabs }: { value: V; onChange: (v: V) => void; tabs: { value: V; label: ReactNode; count?: number }[] }) {
  return (
    <div className="tabs" role="tablist">
      {tabs.map((tab) => (
        <button key={tab.value} role="tab" type="button" aria-selected={tab.value === value} onClick={() => onChange(tab.value)}>
          {tab.label}
          {tab.count !== undefined ? <span className="count">{tab.count}</span> : null}
        </button>
      ))}
    </div>
  );
}

/* ---------- Drawer (side sheet on <dialog>) ---------- */

export function Drawer({ open, onClose, title, children, foot }: { open: boolean; onClose: () => void; title: ReactNode; children: ReactNode; foot?: ReactNode }) {
  const ref = useRef<HTMLDialogElement>(null);
  const { t } = useI18n();
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);
  return (
    <dialog
      ref={ref}
      className="drawer"
      onClose={onClose}
      onClick={(e) => {
        if (e.target === ref.current) onClose();
      }}
    >
      {open ? (
        <>
          <header className="drawer-head">
            <h2>{title}</h2>
            <Button variant="ghost" iconOnly icon="x" aria-label={t("common.close")} onClick={onClose} />
          </header>
          <div className="drawer-body">{children}</div>
          {foot ? <footer className="drawer-foot">{foot}</footer> : null}
        </>
      ) : null}
    </dialog>
  );
}

/* ---------- Toasts ---------- */

interface ToastMsg {
  id: number;
  text: string;
  tone?: "bad";
}
const ToastCtx = createContext<(text: string, tone?: "bad") => void>(() => undefined);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastMsg[]>([]);
  const { t } = useI18n();
  const push = useCallback((text: string, tone?: "bad") => {
    const id = Date.now() + Math.random();
    setItems((xs) => [...xs.slice(-3), { id, text, tone }]);
    setTimeout(() => setItems((xs) => xs.filter((x) => x.id !== id)), tone === "bad" ? 8000 : 4000);
  }, []);
  return (
    <ToastCtx.Provider value={push}>
      {children}
      <div className="toasts" role="status" aria-live="polite">
        {items.map((x) => (
          <div key={x.id} className="toast" data-tone={x.tone}>
            <p>{x.text}</p>
            <button type="button" aria-label={t("common.close")} onClick={() => setItems((xs) => xs.filter((y) => y.id !== x.id))}>
              <Icon name="x" size={14} />
            </button>
          </div>
        ))}
      </div>
    </ToastCtx.Provider>
  );
}

export const useToast = () => useContext(ToastCtx);

/* ---------- Confirm-in-place button for irreversible actions ---------- */

export function ConfirmButton({ children, confirmLabel, onConfirm, ...rest }: BtnProps & { confirmLabel: ReactNode; onConfirm: () => void }) {
  const [armed, setArmed] = useState(false);
  useEffect(() => {
    if (!armed) return;
    const id = setTimeout(() => setArmed(false), 4000);
    return () => clearTimeout(id);
  }, [armed]);
  return (
    <Button
      {...rest}
      variant={armed ? "danger" : rest.variant}
      aria-live="polite"
      onClick={() => {
        if (armed) {
          setArmed(false);
          onConfirm();
        } else setArmed(true);
      }}
    >
      {armed ? confirmLabel : children}
    </Button>
  );
}
