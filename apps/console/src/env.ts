import { createEnv } from "@t3-oss/env-core";
import { z } from "zod";

export const env = createEnv({
  server: {
    SERVER_URL: z.string().url().optional(),
  },

  /**
   * The prefix that client-side variables must have. This is enforced both at
   * a type-level and at runtime.
   */
  clientPrefix: "VITE_",

  client: {
    VITE_APP_TITLE: z.string().min(1).optional(),
    VITE_API_URL: z.string().url().optional(),
  },

  /**
   * What object holds the environment variables at runtime. This is usually
   * `process.env` or `import.meta.env`.
   */
  runtimeEnv: import.meta.env,

  /**
   * Empty strings (e.g. `VITE_API_URL=` in a `.env`) are treated as undefined
   * so declared defaults still apply instead of failing validation.
   */
  emptyStringAsUndefined: true,
});

/** Base URL of the qeet-logs query API. Falls back to the docker-compose port. */
export const API_BASE_URL = env.VITE_API_URL ?? "http://localhost:8100";

/** Visible app name / window title. */
export const APP_TITLE = env.VITE_APP_TITLE ?? "Qeet Logs";
