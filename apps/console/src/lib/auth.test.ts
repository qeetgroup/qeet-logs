import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Keep the real keyStore (localStorage-backed) but stub the network call.
vi.mock("@/lib/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api")>();
  return { ...actual, api: vi.fn() };
});

import { api, keyStore } from "@/lib/api";
import { validateKey } from "./auth";

const apiMock = vi.mocked(api);

beforeEach(() => {
  window.localStorage.clear();
  apiMock.mockReset();
});

afterEach(() => {
  window.localStorage.clear();
});

describe("validateKey", () => {
  it("stores the candidate and returns true when the probe succeeds", async () => {
    apiMock.mockResolvedValueOnce([]);
    await expect(validateKey("good-key")).resolves.toBe(true);
    expect(keyStore.get()).toBe("good-key");
    expect(apiMock).toHaveBeenCalledWith("/v1/admin/api-keys", { method: "GET" });
  });

  it("rolls back to the previous key when the probe fails", async () => {
    keyStore.set("existing-key");
    apiMock.mockRejectedValueOnce(new Error("401"));
    await expect(validateKey("bad-key")).resolves.toBe(false);
    expect(keyStore.get()).toBe("existing-key");
  });

  it("clears the key when the probe fails and there was none before", async () => {
    apiMock.mockRejectedValueOnce(new Error("401"));
    await expect(validateKey("bad-key")).resolves.toBe(false);
    expect(keyStore.get()).toBeNull();
  });
});
