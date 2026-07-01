import { API_BASE_URL } from "@/env";

// API key stored in localStorage — set on sign-in, cleared on sign-out.
const KEY_STORAGE = "qeet-logs.api-key";

export const keyStore = {
  get: (): string | null =>
    typeof window !== "undefined" ? window.localStorage.getItem(KEY_STORAGE) : null,
  set: (k: string) => window.localStorage.setItem(KEY_STORAGE, k),
  clear: () => window.localStorage.removeItem(KEY_STORAGE),
};

export function isAuthenticated(): boolean {
  return Boolean(keyStore.get());
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

type RequestOpts = {
  method?: string;
  body?: unknown;
  query?: Record<string, string | number | boolean | undefined | null>;
  signal?: AbortSignal;
  anonymous?: boolean;
};

function buildURL(path: string, query?: RequestOpts["query"]): URL {
  const url = new URL(
    path.startsWith("/") ? path.slice(1) : path,
    API_BASE_URL.endsWith("/") ? API_BASE_URL : `${API_BASE_URL}/`,
  );
  if (query) {
    for (const [k, v] of Object.entries(query)) {
      if (v !== undefined && v !== null && v !== "") {
        url.searchParams.set(k, String(v));
      }
    }
  }
  return url;
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

function safeParse(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

export async function api<T = unknown>(
  path: string,
  opts: RequestOpts = {},
): Promise<T> {
  const { method = "GET", body, query, signal, anonymous = false } = opts;
  const url = buildURL(path, query);
  const res = await doFetch(url, method, body, signal, anonymous);

  if (res.status === 204) return undefined as T;

  const text = await res.text();
  const data = text ? safeParse(text) : null;

  if (!res.ok) {
    const err = (data as { error?: { code?: string; message?: string; details?: unknown } })
      ?.error;
    throw new ApiError(
      res.status,
      err?.code ?? `http_${res.status}`,
      err?.message ?? res.statusText ?? "Request failed",
      err?.details,
    );
  }

  return data as T;
}

// Lightweight WebSocket URL builder using the same base host.
export function wsURL(path: string, query?: Record<string, string>): string {
  const url = buildURL(path, query);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return url.toString();
}
