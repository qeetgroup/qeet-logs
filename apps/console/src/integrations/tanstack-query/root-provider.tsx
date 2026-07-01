import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiError } from "@/lib/api";

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

function reportError(error: unknown, meta?: Record<string, unknown>) {
  if (meta?.silent) return;
  if (!(error instanceof ApiError)) return;
  if (error.status === 401 || error.status === 400 || error.status === 422) return;
  toast.error(error.message);
}

function reportSuccess(meta?: MutationMeta) {
  if (meta?.silent) return;
  toast.success(meta?.successMessage ?? "Saved", {
    description: meta?.successDescription,
  });
}

let client: QueryClient | undefined;

export function getContext() {
  if (!client) {
    client = new QueryClient({
      queryCache: new QueryCache({
        onError: (error, query) => reportError(error, query.meta as Record<string, unknown>),
      }),
      mutationCache: new MutationCache({
        onError: (error, _vars, _ctx, mutation) =>
          reportError(error, mutation.meta as Record<string, unknown>),
        onSuccess: (_data, _vars, _ctx, mutation) => reportSuccess(mutation.meta),
      }),
      defaultOptions: {
        queries: { staleTime: 30_000, retry: 1 },
      },
    });
  }
  return { queryClient: client };
}
