// Official Node.js SDK for Qeet Logs (PRD Module 01.1 / 29.1).
//
// Zero-config: one API key, one line to send a structured, searchable event.
// Sends are best-effort — a network failure never rejects into the host app
// (logging must never break the request it observes). Pass { raiseOnError: true }
// to surface failures.
//
//   import { QeetLogs } from "@qeetgroup/qeet-logs";
//   const logs = new QeetLogs({ apiKey: process.env.QEET_LOGS_API_KEY, baseURL: "http://localhost:8101" });
//   await logs.log("payments", "charge failed", { level: "error", route: "/charge" });

const DEFAULT_BASE = "https://ingest.logs.qeet.in";
const PASSTHROUGH = new Set([
  "trace_id", "span_id", "environment", "timestamp",
  "git_sha", "deploy_id", "pr_number", "level",
]);

export class QeetLogs {
  constructor({ apiKey, baseURL = DEFAULT_BASE, timeoutMs = 10000, retries = 2, raiseOnError = false } = {}) {
    if (!apiKey) throw new Error("qeetlogs: apiKey is required");
    this.apiKey = apiKey;
    this.baseURL = baseURL.replace(/\/$/, "");
    this.timeoutMs = timeoutMs;
    this.retries = retries;
    this.raiseOnError = raiseOnError;
  }

  // Build + send one structured event. Extra opts become attributes.
  async log(service, message, opts = {}) {
    const record = { service, message, level: opts.level || "info" };
    for (const [k, v] of Object.entries(opts)) {
      if (k === "level") continue;
      if (PASSTHROUGH.has(k)) record[k] = v;
      else record[k] = v;
    }
    return this.ingest(record);
  }

  // Send a single record to /v1/ingest.
  async ingest(record) {
    return this.#post("/v1/ingest", JSON.stringify(record), "application/json");
  }

  // Send many records to /v1/ingest/batch as newline-delimited JSON.
  async ingestBatch(records) {
    if (!records || records.length === 0) return;
    const body = records.map((r) => JSON.stringify(r)).join("\n");
    return this.#post("/v1/ingest/batch", body, "application/x-ndjson");
  }

  async #post(path, body, contentType) {
    let lastErr;
    for (let attempt = 0; attempt <= this.retries; attempt++) {
      const ctrl = new AbortController();
      const timer = setTimeout(() => ctrl.abort(), this.timeoutMs);
      try {
        const res = await fetch(this.baseURL + path, {
          method: "POST",
          headers: { "X-Qeet-Api-Key": this.apiKey, "Content-Type": contentType },
          body,
          signal: ctrl.signal,
        });
        if (res.ok) return;
        lastErr = new Error(`HTTP ${res.status}`);
        if (res.status >= 400 && res.status < 500) break; // client error: no retry
      } catch (err) {
        lastErr = err;
      } finally {
        clearTimeout(timer);
      }
      await new Promise((r) => setTimeout(r, 100 * (attempt + 1)));
    }
    if (this.raiseOnError && lastErr) throw lastErr;
  }
}

export default QeetLogs;
