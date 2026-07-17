import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

// Mock the HTTP layer so the hook never touches fetch.
vi.mock("@/lib/api", () => ({
  api: vi.fn().mockResolvedValue({ columns: ["a"], count: 1, rows: [{ a: "1" }] }),
}));

import { api } from "@/lib/api";
import { useLogQuery } from "./query";

const apiMock = vi.mocked(api);

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

afterEach(() => {
  apiMock.mockClear();
});

describe("useLogQuery", () => {
  it("is disabled and does not fetch for an empty query", () => {
    const { result } = renderHook(() => useLogQuery(""), { wrapper });
    expect(result.current.fetchStatus).toBe("idle");
    expect(apiMock).not.toHaveBeenCalled();
  });

  it("does not fetch when explicitly disabled", () => {
    renderHook(() => useLogQuery('level = "error"', { enabled: false }), { wrapper });
    expect(apiMock).not.toHaveBeenCalled();
  });

  it("runs the query and returns the envelope when enabled", async () => {
    const { result } = renderHook(() => useLogQuery('level = "error"'), { wrapper });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(apiMock).toHaveBeenCalledWith(
      "/v1/query",
      expect.objectContaining({
        query: { q: 'level = "error"' },
      }),
    );
    expect(result.current.data).toEqual({ columns: ["a"], count: 1, rows: [{ a: "1" }] });
  });
});
