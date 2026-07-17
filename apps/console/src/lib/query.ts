// Hooks for the LogQL++ query endpoint (GET /v1/query). The response is the
// `{columns, count, rows}` envelope; pages render it through the generic
// results table.

import { keepPreviousData, useQuery } from "@tanstack/react-query";

import { api, type QueryEnvelope } from "./api";

export type { QueryEnvelope };

type UseLogQueryOpts = {
  /** Only fire when true (e.g. after the user presses Run). */
  enabled?: boolean;
  /** Response encoding — JSON is decoded into the envelope. */
  format?: "json";
  /** Auto-refetch interval in ms (Overview widgets poll; ad-hoc search doesn't). */
  refetchInterval?: number;
};

/**
 * Run a LogQL++ statement. Non-TAIL statements only — live tailing goes through
 * the WebSocket in `useLiveTail`. Results are kept while a new query runs so the
 * table doesn't flash empty between edits.
 */
export function useLogQuery(q: string, opts: UseLogQueryOpts = {}) {
  const { enabled = true, refetchInterval } = opts;
  return useQuery({
    queryKey: ["query", q],
    queryFn: ({ signal }) => api<QueryEnvelope>("/v1/query", { query: { q }, signal }),
    enabled: enabled && q.trim().length > 0,
    placeholderData: keepPreviousData,
    refetchInterval,
    retry: false,
    meta: { silent: true },
  });
}
