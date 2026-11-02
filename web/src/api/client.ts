// HTTP client for /api/v1. The bearer token lives in memory (mirrored to
// sessionStorage so a reload keeps the session) and is only ever sent in
// the Authorization header.

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

const TOKEN_KEY = "remnant.token";
let token: string | null = readStoredToken();
const unauthorized = new Set<() => void>();

function readStoredToken(): string | null {
  try {
    return sessionStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

export function getToken(): string | null {
  return token;
}

export function setToken(t: string | null): void {
  token = t;
  try {
    if (t) sessionStorage.setItem(TOKEN_KEY, t);
    else sessionStorage.removeItem(TOKEN_KEY);
  } catch {
    // Storage may be unavailable (private mode); the in-memory token still works.
  }
}

/** Registers a callback for 401 responses; returns an unsubscribe function. */
export function onUnauthorized(fn: () => void): () => void {
  unauthorized.add(fn);
  return () => unauthorized.delete(fn);
}

export function authHeaders(): Record<string, string> {
  return token ? { Authorization: `Bearer ${token}` } : {};
}

export async function api<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json", ...authHeaders() };
  if (body !== undefined) headers["Content-Type"] = "application/json";
  let res: Response;
  try {
    res = await fetch(`/api/v1${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal,
      credentials: "omit",
    });
  } catch (e) {
    if ((e as Error).name === "AbortError") throw e;
    throw new ApiError(0, "network", "network");
  }
  if (res.status === 401) {
    unauthorized.forEach((fn) => fn());
  }
  const text = await res.text();
  let data: unknown = undefined;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = undefined;
    }
  }
  if (!res.ok) {
    const err = (data ?? {}) as { error?: string; code?: string };
    throw new ApiError(res.status, err.code ?? "http_" + res.status, err.error ?? res.statusText);
  }
  return data as T;
}

export const get = <T,>(path: string, signal?: AbortSignal) => api<T>("GET", path, undefined, signal);
export const post = <T,>(path: string, body?: unknown) => api<T>("POST", path, body ?? {});
export const patch = <T,>(path: string, body: unknown) => api<T>("PATCH", path, body);
export const put = <T,>(path: string, body: unknown) => api<T>("PUT", path, body);

/** Downloads an authenticated artifact and hands it to the browser as a file. */
export async function download(path: string, filename: string): Promise<void> {
  const res = await fetch(`/api/v1${path}`, { headers: authHeaders(), credentials: "omit" });
  if (!res.ok) throw new ApiError(res.status, "download", res.statusText);
  const url = URL.createObjectURL(await res.blob());
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
}
