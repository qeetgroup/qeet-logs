// ingest_load.js — k6 load test for the qeet-logs ingest gateway (/v1/ingest).
//
// The ingest gateway (Rust, default :8101) is the write plane: it authenticates
// X-Qeet-Api-Key → tenant, runs the synchronous PII gate, and publishes to NATS.
// A single record POSTs to /v1/ingest; the batch path (/v1/ingest/batch) takes
// newline-delimited JSON. This script drives the single-record path under a
// ramping arrival rate and asserts the gateway accepts (HTTP 202).
//
// Run:
//   QEET_LOGS_INGEST_URL=http://localhost:8101 \
//   QEET_LOGS_API_KEY=qeel_...   (needs logs:ingest) \
//   k6 run test/load/ingest_load.js
//
// Tunables (env):
//   QEET_LOGS_INGEST_URL  ingest gateway base URL (default http://localhost:8101)
//   QEET_LOGS_API_KEY     API key with logs:ingest (REQUIRED)
//   P95_MS                p95 threshold in ms for /v1/ingest (default 200)
//   TARGET_RPS            peak requests/sec       (default 500)

import http from 'k6/http';
import { check } from 'k6';
import { Rate } from 'k6/metrics';

const BASE = (__ENV.QEET_LOGS_INGEST_URL || 'http://localhost:8101').replace(/\/+$/, '');
const KEY = __ENV.QEET_LOGS_API_KEY || '';
const P95 = __ENV.P95_MS || '200';
const TARGET_RPS = parseInt(__ENV.TARGET_RPS || '500', 10);

const ingestErrors = new Rate('ingest_errors');

const SERVICES = ['payments-api', 'auth-svc', 'web-gateway', 'billing-worker'];
const LEVELS = ['debug', 'info', 'warn', 'error'];

export const options = {
  scenarios: {
    ingest_ramp: {
      executor: 'ramping-arrival-rate',
      startRate: 20,
      timeUnit: '1s',
      preAllocatedVUs: 50,
      maxVUs: 300,
      stages: [
        { duration: '30s', target: Math.ceil(TARGET_RPS / 5) },
        { duration: '1m', target: TARGET_RPS },
        { duration: '2m', target: TARGET_RPS },
        { duration: '30s', target: 0 },
      ],
    },
  },
  thresholds: {
    'http_req_duration{endpoint:ingest}': [`p(95)<${P95}`],
    ingest_errors: ['rate<0.01'],
    http_req_failed: ['rate<0.01'],
  },
};

export function setup() {
  if (!KEY) {
    throw new Error('QEET_LOGS_API_KEY is required — set it to a key with logs:ingest');
  }
}

export default function () {
  const svc = SERVICES[Math.floor(Math.random() * SERVICES.length)];
  const level = LEVELS[Math.floor(Math.random() * LEVELS.length)];
  const payload = JSON.stringify({
    timestamp: new Date().toISOString(),
    service: svc,
    level: level,
    message: `k6 load event vu=${__VU} iter=${__ITER} svc=${svc}`,
    environment: 'loadtest',
    attributes: { source: 'k6', region: 'ap-south-1' },
  });

  const res = http.post(`${BASE}/v1/ingest`, payload, {
    headers: { 'X-Qeet-Api-Key': KEY, 'Content-Type': 'application/json' },
    tags: { endpoint: 'ingest' },
  });

  // The gateway returns 202 Accepted for both stored and PII-dropped records.
  const ok = check(res, { 'status is 202': (r) => r.status === 202 });
  ingestErrors.add(!ok);
}
