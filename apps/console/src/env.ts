export const API_BASE_URL =
  (import.meta.env?.VITE_API_URL as string | undefined) ?? "http://localhost:8100";

export const APP_TITLE =
  (import.meta.env?.VITE_APP_TITLE as string | undefined) ?? "Qeet Logs";
