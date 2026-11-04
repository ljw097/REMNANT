// HTTP client for /api/v1. The browser session is an HttpOnly cookie the
// server sets at sign-in; scripts never see it. Every request carries the
// same-origin header the server requires before it acts on a cookie.

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

const unauthorized = new Set<() => void>();

/** Registers a callback for 401 responses; returns an unsubscribe function. */
export function onUnauthorized(fn: () => void): () => void {
  unauthorized.add(fn);
  return () => unauthorized.delete(fn);
}

export function authHeaders(): Record<string, string> {
  return { "X-Remnant-Request": "1" };
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
      credentials: "same-origin",
    });
  } catch (e) {
    if ((e as Error).name === "AbortError") throw e;
    throw new ApiError(0, "network", "network");
  }
  if (res.status === 401 && !path.startsWith("/auth/")) {
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
export const del = <T,>(path: string) => api<T>("DELETE", path);

/** Fetches an authenticated resource as plain text (e.g. a Markdown report). */
export async function getText(path: string, signal?: AbortSignal): Promise<string> {
  let res: Response;
  try {
    res = await fetch(`/api/v1${path}`, { headers: authHeaders(), credentials: "same-origin", signal });
  } catch (e) {
    if ((e as Error).name === "AbortError") throw e;
    throw new ApiError(0, "network", "network");
  }
  if (res.status === 401 && !path.startsWith("/auth/")) unauthorized.forEach((fn) => fn());
  const text = await res.text();
  if (!res.ok) throw new ApiError(res.status, "http_" + res.status, res.statusText);
  return text;
}

/** Downloads an authenticated artifact and hands it to the browser as a file. */
export async function download(path: string, filename: string): Promise<void> {
  const res = await fetch(`/api/v1${path}`, { headers: authHeaders(), credentials: "same-origin" });
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
