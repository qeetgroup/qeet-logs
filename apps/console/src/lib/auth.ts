import { api, isAuthenticated, keyStore } from "./api";

export { isAuthenticated, keyStore };

export type ReadyzResponse = {
  healthy: boolean;
  details: {
    postgres: boolean;
    redis: boolean;
    clickhouse: boolean;
    nats: boolean;
  };
};

// Validate that the stored API key is functional by hitting /readyz.
// Returns true when the key authenticates and the backend is healthy.
export async function validateKey(key: string): Promise<boolean> {
  try {
    // Temporarily set the key so api() picks it up.
    keyStore.set(key);
    await api<ReadyzResponse>("/readyz");
    return true;
  } catch {
    keyStore.clear();
    return false;
  }
}
