import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, api, isAuthenticated, keyPrefix, keyStore, wsURL } from "./api";

// Minimal stand-in for the parts of `Response` that api() touches.
function mockResponse(opts: { status: number; ok?: boolean; body?: string }) {
  return {
    status: opts.status,
    ok: opts.ok ?? (opts.status >= 200 && opts.status < 300),
    statusText: "",
    text: async () => opts.body ?? "",
  };
}

beforeEach(() => {
  window.localStorage.clear();
  vi.restoreAllMocks();
});

afterEach(() => {
  window.localStorage.clear();
});

describe("keyStore", () => {
  it("persists, reads and clears the API key", () => {
    expect(keyStore.get()).toBeNull();
    keyStore.set("qlog_live_secret");
    expect(keyStore.get()).toBe("qlog_live_secret");
    keyStore.clear();
    expect(keyStore.get()).toBeNull();
  });
});

describe("isAuthenticated", () => {
  it("reflects whether a key is stored", () => {
    expect(isAuthenticated()).toBe(false);
    keyStore.set("k");
    expect(isAuthenticated()).toBe(true);
  });
});

describe("keyPrefix", () => {
  it("returns null with no key", () => {
    expect(keyPrefix()).toBeNull();
  });

  it("truncates long keys and preserves short ones", () => {
    keyStore.set("short");
    expect(keyPrefix()).toBe("short");
    keyStore.set("qlog_live_abcdefghijklmnop");
    expect(keyPrefix()).toBe("qlog_live_ab…");
  });
});

describe("ApiError", () => {
  it("carries status, code and message", () => {
    const err = new ApiError(404, "not_found", "missing");
    expect(err).toBeInstanceOf(Error);
    expect(err.status).toBe(404);
    expect(err.code).toBe("not_found");
    expect(err.message).toBe("missing");
  });
});

describe("api", () => {
  it("attaches the API key header and decodes the JSON body", async () => {
    keyStore.set("secret-key");
    const fetchMock = vi
      .fn()
      .mockResolvedValue(mockResponse({ status: 200, body: JSON.stringify({ ok: true }) }));
    vi.stubGlobal("fetch", fetchMock);

    const result = await api<{ ok: boolean }>("/v1/thing");

    expect(result).toEqual({ ok: true });
    const [, init] = fetchMock.mock.calls[0];
    expect(init.headers["X-Qeet-Api-Key"]).toBe("secret-key");
  });

  it("returns undefined for 204 responses", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(mockResponse({ status: 204 })));
    await expect(api("/v1/thing", { method: "DELETE" })).resolves.toBeUndefined();
  });

  it("throws a typed ApiError for a flat error envelope", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(mockResponse({ status: 500, body: JSON.stringify({ error: "boom" }) })),
    );
    await expect(api("/v1/thing")).rejects.toMatchObject({
      name: "ApiError",
      status: 500,
      message: "boom",
    });
  });

  it("throws a typed ApiError for a rich error envelope", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        mockResponse({
          status: 422,
          body: JSON.stringify({ error: { code: "invalid", message: "bad input" } }),
        }),
      ),
    );
    await expect(api("/v1/thing")).rejects.toMatchObject({
      status: 422,
      code: "invalid",
      message: "bad input",
    });
  });

  it("omits the auth header for anonymous calls", async () => {
    keyStore.set("secret-key");
    const fetchMock = vi
      .fn()
      .mockResolvedValue(mockResponse({ status: 200, body: JSON.stringify({}) }));
    vi.stubGlobal("fetch", fetchMock);

    await api("/readyz", { anonymous: true });
    const [, init] = fetchMock.mock.calls[0];
    expect(init.headers["X-Qeet-Api-Key"]).toBeUndefined();
  });
});

describe("wsURL", () => {
  it("swaps the scheme to ws and appends the key as a query param", () => {
    keyStore.set("ws-key");
    const url = wsURL("/v1/query/tail", { q: "TAIL FROM logs" });
    expect(url.startsWith("ws://")).toBe(true);
    expect(url).toContain("/v1/query/tail");
    expect(url).toContain("api_key=ws-key");
    expect(url).toContain("q=TAIL+FROM+logs");
  });
});
