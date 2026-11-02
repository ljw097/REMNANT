import { createContext, useCallback, useContext, useMemo, type ReactNode } from "react";
import ko from "./ko";
import en from "./en";

export type Lang = "ko" | "en";
export type Key = keyof typeof ko;
export type T = (key: Key, vars?: Record<string, string | number>) => string;

const dicts: Record<Lang, Record<Key, string>> = { ko, en };

export function translate(lang: Lang, key: Key, vars?: Record<string, string | number>): string {
  let s = dicts[lang][key] ?? dicts.en[key] ?? key;
  if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
  return s;
}

export function detectLang(): Lang {
  try {
    const saved = localStorage.getItem("remnant.lang");
    if (saved === "ko" || saved === "en") return saved;
  } catch {
    // ignore
  }
  return navigator.language.toLowerCase().startsWith("ko") ? "ko" : "en";
}

interface I18n {
  lang: Lang;
  t: T;
  date: (iso: string | null | undefined) => string;
  dateTime: (iso: string | null | undefined) => string;
  ago: (iso: string | null | undefined) => string;
  num: (n: number) => string;
  duration: (ms: number) => string;
  bytes: (n: number) => string;
}

const Ctx = createContext<I18n | null>(null);

export function I18nProvider({ lang, children }: { lang: Lang; children: ReactNode }) {
  const t = useCallback<T>((key, vars) => translate(lang, key, vars), [lang]);
  const value = useMemo<I18n>(() => {
    const locale = lang === "ko" ? "ko-KR" : "en-US";
    const dtf = new Intl.DateTimeFormat(locale, { dateStyle: "medium" });
    const dttf = new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" });
    const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
    const nf = new Intl.NumberFormat(locale);
    return {
      lang,
      t,
      date: (iso) => (iso ? dtf.format(new Date(iso)) : "—"),
      dateTime: (iso) => (iso ? dttf.format(new Date(iso)) : "—"),
      ago: (iso) => {
        if (!iso) return "—";
        const s = (new Date(iso).getTime() - Date.now()) / 1000;
        const abs = Math.abs(s);
        if (abs < 45) return rtf.format(Math.round(s), "second");
        if (abs < 2700) return rtf.format(Math.round(s / 60), "minute");
        if (abs < 79200) return rtf.format(Math.round(s / 3600), "hour");
        if (abs < 2592000) return rtf.format(Math.round(s / 86400), "day");
        return dtf.format(new Date(iso));
      },
      num: (n) => nf.format(n),
      duration: (ms) => {
        const s = Math.round(ms / 1000);
        if (s < 60) return t("unit.seconds", { n: s });
        const m = Math.floor(s / 60);
        if (m < 60) return t("unit.minutes", { m, s: s % 60 });
        return t("unit.hours", { h: Math.floor(m / 60), m: m % 60 });
      },
      bytes: (n) => {
        if (n < 1024) return `${nf.format(n)} B`;
        if (n < 1 << 20) return `${(n / 1024).toFixed(1)} KiB`;
        return `${(n / (1 << 20)).toFixed(1)} MiB`;
      },
    };
  }, [lang, t]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useI18n(): I18n {
  const v = useContext(Ctx);
  if (!v) throw new Error("useI18n outside I18nProvider");
  return v;
}
