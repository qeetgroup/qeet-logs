// Thin HTTP client around the qeet-logs query/API server (cmd/query).
//
// - Base URL comes from VITE_API_URL (defaults to http://localhost:8100).
// - Auth in dev is an API key: it is persisted under
//   localStorage["qeet-logs.api-key"] and attached as the `X-Qeet-Api-Key`
//   header on every non-anonymous call (the backend resolves it to a tenant +
//   scopes — see platform/api/middleware/auth.go).
// - The query API answers with the `{columns, count, rows}` envelope; errors
//   across the API use a flat `{error: string}` envelope. Both are decoded here
//   and normalised into a typed `ApiError` so React Query / form handlers can
//   switch on `err.status` and surface `err.message`.

import { API_BASE_URL } from "@/env";

const KEY_STORAGE = "qeet-logs.api-key";

export const keyStore = {
  get: (): string | null =>
    typeof window !== "undefined" ? window.localStorage.getItem(KEY_STORAGE) : null,
  set: (k: string) => window.localStorage.setItem(KEY_STORAGE, k),
  clear: () => window.localStorage.removeItem(KEY_STORAGE),
};

/** Whether an API key is stored. Read synchronously for route guards. */
export function isAuthenticated(): boolean {
  return Boolean(keyStore.get());
}

/** A short, safe label for the stored key (never the full secret). */
export function keyPrefix(): string | null {
  const k = keyStore.get();
  if (!k) return null;
  return k.length > 12 ? `${k.slice(0, 12)}…` : k;
}

export class ApiError extends Error {
  status: number;
  code: string;
  details?: unknown;

  constructor(status: number, code: string, message: string, details?: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

type QueryValue = string | number | boolean | undefined | null;

type RequestOpts = {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  body?: unknown;
  query?: Record<string, QueryValue>;
  signal?: AbortSignal;
  /** Skip the auth header (used before a key is stored). */
  anonymous?: boolean;
};

function buildURL(path: string, query?: RequestOpts["query"]): URL {
  const base = API_BASE_URL.endsWith("/") ? API_BASE_URL : `${API_BASE_URL}/`;
  const url = new URL(path.startsWith("/") ? path.slice(1) : path, base);
  if (query) {
    for (const [k, v] of Object.entries(query)) {
      if (v !== undefined && v !== null && v !== "") url.searchParams.set(k, String(v));
    }
  }
  return url;
}

function onAuthLost() {
  keyStore.clear();
  if (typeof window !== "undefined" && window.location.pathname !== "/sign-in") {
    window.location.assign("/sign-in");
  }
}

async function doFetch(
  url: URL,
  method: string,
  body: unknown,
  signal: AbortSignal | undefined,
  anonymous: boolean,
): Promise<Response> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (!anonymous) {
    const k = keyStore.get();
    if (k) headers["X-Qeet-Api-Key"] = k;
  }
  return fetch(url, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
    signal,
  });
}

function safeParse(s: string): unknown {
  try {
    return JSON.parse(s);
  } catch {
    return s;
  }
}

export async function api<T = unknown>(path: string, opts: RequestOpts = {}): Promise<T> {
  const { method = "GET", body, query, signal, anonymous = false } = opts;

  const url = buildURL(path, query);
  const res = await doFetch(url, method, body, signal, anonymous);

  // Unauthenticated (bad/expired key) → clear + bounce to sign-in, unless the
  // call was explicitly anonymous (e.g. validating a candidate key).
  if (res.status === 401 && !anonymous) {
    onAuthLost();
  }

  if (res.status === 204) return undefined as T;

  const text = await res.text();
  const data = text ? safeParse(text) : null;

  if (!res.ok) {
    // Flat `{error: string}` envelope, or the richer `{error:{code,message}}`
    // shape used by some Qeet services — accept both.
    const raw = (data as { error?: unknown } | null)?.error;
    let code = `http_${res.status}`;
    let message = res.statusText || "Request failed";
    let details: unknown;
    if (typeof raw === "string") {
      message = raw;
    } else if (raw && typeof raw === "object") {
      const e = raw as { code?: string; message?: string; details?: unknown };
      code = e.code ?? code;
      message = e.message ?? message;
      details = e.details;
    }
    throw new ApiError(res.status, code, message, details);
  }

  return data as T;
}

/** The standard query response envelope returned by GET /v1/query. */
export type QueryEnvelope = {
  columns: string[];
  count: number;
  rows: Array<Record<string, unknown>>;
};

/**
 * Build a ws:// or wss:// URL against the same API host — used for the
 * live-tail WebSocket (GET /v1/query/tail?q=TAIL …). The API key is passed as
 * a query param because browsers can't set custom headers on a WebSocket
 * handshake.
 */
export function wsURL(path: string, query?: Record<string, QueryValue>): string {
  const url = buildURL(path, query);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  const key = keyStore.get();
  if (key) url.searchParams.set("api_key", key);
  return url.toString();
}
