// API-key auth for the console. Unlike qeet-id (OIDC/JWT), the qeet-logs query
// API authenticates every request with an `X-Qeet-Api-Key` header in dev. The
// console stores one admin-scoped key and treats "having a valid key" as being
// signed in.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";

import { api, isAuthenticated, keyPrefix, keyStore } from "./api";

export { isAuthenticated, keyPrefix, keyStore };

export type ReadyzResponse = {
  healthy?: boolean;
  status?: string;
  details?: Record<string, boolean>;
};

/**
 * Validate a candidate key by calling an authenticated admin endpoint. We
 * temporarily store the key so `api()` attaches it, and roll it back if the
 * call fails. A 200 means the key resolves to a tenant with `logs:admin`.
 */
export async function validateKey(candidate: string): Promise<boolean> {
  const previous = keyStore.get();
  keyStore.set(candidate);
  try {
    await api("/v1/admin/api-keys", { method: "GET" });
    return true;
  } catch {
    if (previous) keyStore.set(previous);
    else keyStore.clear();
    return false;
  }
}

/** Sign in by storing (and validating) an API key, then land on the overview. */
export function useSignIn() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (key: string) => {
      const ok = await validateKey(key.trim());
      if (!ok) {
        throw new Error("That API key was rejected. Check the value and that it has logs:admin.");
      }
      return key.trim();
    },
    onSuccess: () => {
      qc.clear();
      navigate({ to: "/" });
    },
    // Errors are surfaced inline on the sign-in form.
    meta: { silent: true },
  });
}

/** Clear the stored key and return to sign-in. */
export function useSignOut() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      keyStore.clear();
    },
    onSettled: () => {
      qc.clear();
      navigate({ to: "/sign-in" });
    },
    meta: { silent: true },
  });
}

/** Readiness probe — used to show backend health in the header/overview. */
export function useReadyz() {
  return useQuery({
    queryKey: ["readyz"],
    queryFn: () => api<ReadyzResponse>("/readyz", { anonymous: true }),
    staleTime: 15_000,
    retry: false,
  });
}
