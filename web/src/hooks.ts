import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, get } from "./api/client";

export interface Query<T> {
  data: T | undefined;
  error: ApiError | undefined;
  loading: boolean;
  reload: () => void;
  setData: (d: T) => void;
}

/** Fetches path (null skips) and refetches when it changes or every pollMs. */
export function useApi<T>(path: string | null, pollMs?: number): Query<T> {
  const [data, setData] = useState<T>();
  const [error, setError] = useState<ApiError>();
  const [loading, setLoading] = useState(path !== null);
  const [tick, setTick] = useState(0);
  const reload = useCallback(() => setTick((n) => n + 1), []);

  useEffect(() => {
    if (path === null) {
      setLoading(false);
      return;
    }
    const ac = new AbortController();
    setLoading(true);
    get<T>(path, ac.signal)
      .then((d) => {
        setData(d);
        setError(undefined);
      })
      .catch((e: unknown) => {
        if ((e as Error).name !== "AbortError") setError(e instanceof ApiError ? e : new ApiError(0, "unknown", String(e)));
      })
      .finally(() => {
        if (!ac.signal.aborted) setLoading(false);
      });
    return () => ac.abort();
  }, [path, tick]);

  useEffect(() => {
    if (!pollMs || path === null) return;
    const id = setInterval(() => {
      if (document.visibilityState === "visible") reload();
    }, pollMs);
    return () => clearInterval(id);
  }, [pollMs, path, reload]);

  return { data, error, loading, reload, setData };
}

/** Runs an async action, tracking pending state; errors are rethrown to the caller. */
export function useAction<A extends unknown[], R>(fn: (...args: A) => Promise<R>) {
  const [pending, setPending] = useState(false);
  const mounted = useRef(true);
  useEffect(() => () => void (mounted.current = false), []);
  const run = useCallback(
    async (...args: A): Promise<R> => {
      setPending(true);
      try {
        return await fn(...args);
      } finally {
        if (mounted.current) setPending(false);
      }
    },
    [fn],
  );
  return [run, pending] as const;
}

/** Registers keyboard shortcuts while mounted; ignored while typing in a field. */
export function useHotkeys(map: Record<string, () => void>, enabled = true) {
  const ref = useRef(map);
  ref.current = map;
  useEffect(() => {
    if (!enabled) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const el = e.target as HTMLElement | null;
      if (el && (el.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName))) return;
      const fn = ref.current[e.key];
      if (fn) {
        e.preventDefault();
        fn();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [enabled]);
}

import type { UserRef } from "./api/types";

/** id → username for showing who did what. */
export function useUserNames(): (id: number | null | undefined) => string {
  const users = useApi<UserRef[]>("/users");
  return useCallback(
    (id) => {
      if (id === null || id === undefined) return "—";
      return users.data?.find((u) => u.id === id)?.username ?? `#${id}`;
    },
    [users.data],
  );
}
