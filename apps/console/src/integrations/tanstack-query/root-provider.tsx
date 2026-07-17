import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiError } from "@/lib/api";

// Per-mutation overrides go on the `meta` object. `silent: true` opts out
// of both the success toast and the error toast (e.g. background polls
// where a toast would be noisy). `successMessage` overrides the default
// "Saved" string; `successDescription` adds a second line.
interface MutationMeta {
  silent?: boolean;
  successMessage?: string;
  successDescription?: string;
}

declare module "@tanstack/react-query" {
  interface Register {
    mutationMeta: MutationMeta;
    queryMeta: { silent?: boolean };
  }
}

// Surface backend errors via a single toast handler. 401 is excluded because
// api.ts redirects to /sign-in after an auth failure, and 400/422 is excluded
// so form validation can render inline messages without a duplicate toast.
// Individual mutations can opt out by setting `meta: { silent: true }`.
function reportError(error: unknown, meta?: Record<string, unknown>) {
  if (meta?.silent) return;
  if (!(error instanceof ApiError)) return;
  if (error.status === 401 || error.status === 400 || error.status === 422) return;
  toast.error(error.message);
}

// Success-side toast: emitted for every mutation that doesn't opt out.
// Default message is intentionally generic ("Saved"); screens that want
// a better word ("Created rule", "Revoked key") set
// `meta: { successMessage: "..." }` on the useMutation call.
function reportSuccess(meta?: MutationMeta) {
  if (meta?.silent) return;
  toast.success(meta?.successMessage ?? "Saved", {
    description: meta?.successDescription,
  });
}

export function getContext() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { staleTime: 30_000, retry: 1 },
    },
    queryCache: new QueryCache({
      onError: (error, query) => reportError(error, query.meta),
    }),
    mutationCache: new MutationCache({
      onError: (error, _vars, _ctx, mutation) => reportError(error, mutation.meta),
      onSuccess: (_data, _vars, _ctx, mutation) =>
        reportSuccess(mutation.meta as MutationMeta | undefined),
    }),
  });

  return {
    queryClient,
  };
}

export default function TanstackQueryProvider() {}
