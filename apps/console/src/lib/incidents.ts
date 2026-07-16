// Query hooks for the incident-intelligence endpoints. All are `retry: false`
// + `silent` so a not-yet-live Phase-2 endpoint surfaces as an empty/again
// state rather than an error toast storm.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api } from "./api";
import type {
  Change,
  DeployCulprit,
  FeedbackVerdict,
  Incident,
  IncidentContext,
  Rca,
} from "./domain";

function unwrapList<T>(data: unknown): T[] {
  if (Array.isArray(data)) return data as T[];
  if (data && typeof data === "object") {
    const obj = data as Record<string, unknown>;
    for (const key of ["items", "incidents", "changes", "results", "rows", "data"]) {
      if (Array.isArray(obj[key])) return obj[key] as T[];
    }
  }
  return [];
}

export function useIncidents() {
  return useQuery({
    queryKey: ["incidents"],
    queryFn: () => api<unknown>("/v1/incidents").then((d) => unwrapList<Incident>(d)),
    retry: false,
    meta: { silent: true },
  });
}

export function useChanges() {
  return useQuery({
    queryKey: ["changes"],
    queryFn: () => api<unknown>("/v1/changes").then((d) => unwrapList<Change>(d)),
    retry: false,
    meta: { silent: true },
  });
}

export function useRca(service: string | undefined) {
  return useQuery({
    queryKey: ["rca", service],
    queryFn: () => api<Rca>("/v1/rca", { query: { service } }),
    enabled: Boolean(service),
    retry: false,
    meta: { silent: true },
  });
}

export function useDeployCulprits(service: string | undefined) {
  return useQuery({
    queryKey: ["deploy-culprits", service],
    queryFn: () =>
      api<unknown>("/v1/deploy/culprits", { query: { service } }).then((d) =>
        unwrapList<DeployCulprit>(d),
      ),
    enabled: Boolean(service),
    retry: false,
    meta: { silent: true },
  });
}

export function useIncidentContext(id: string | undefined) {
  return useQuery({
    queryKey: ["incident-context", id],
    queryFn: () => api<IncidentContext>(`/v1/incidents/${id}/context`),
    enabled: Boolean(id),
    retry: false,
    meta: { silent: true },
  });
}

export function useIncidentFeedback() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { id: string; verdict: FeedbackVerdict }) =>
      api<void>(`/v1/admin/incidents/${input.id}/feedback`, {
        method: "POST",
        body: { verdict: input.verdict },
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["incidents"] }),
    meta: { successMessage: "Feedback recorded" },
  });
}
