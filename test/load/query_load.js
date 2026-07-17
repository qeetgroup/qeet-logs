// query_load.js — k6 load test for the qeet-logs query API (/v1/query).
//
// Ramps virtual users against the LogQL++ query endpoint and enforces a p95
// latency threshold. Auth is API-key → tenant + scopes; the injected tenant_id
// predicate means every VU only ever sees its own tenant's data.
//
// Run:
//   QEET_LOGS_API_URL=http://localhost:8100 \
//   QEET_LOGS_API_KEY=qeel_... \
//   k6 run test/load/query_load.js
//
// Tunables (env):
//   QEET_LOGS_API_URL   query API base URL      (default http://localhost:8100)
//   QEET_LOGS_API_KEY   API key with logs:read or logs:query (REQUIRED)
//   P95_MS              p95 threshold in ms for /v1/query (default 800)
//   MAX_VUS             peak virtual users      (default 50)

import http from 'k6/http';
import { check } from 'k6';
import { Rate } from 'k6/metrics';

const BASE = (__ENV.QEET_LOGS_API_URL || 'http://localhost:8100').replace(/\/+$/, '');
const KEY = __ENV.QEET_LOGS_API_KEY || '';
const P95 = __ENV.P95_MS || '800';
const MAX_VUS = parseInt(__ENV.MAX_VUS || '50', 10);

const queryErrors = new Rate('query_errors');

// A spread of representative LogQL++ statements — a full scan, a filtered
// search, and a grouped aggregate — to exercise different compiler paths.
const QUERIES = [
  'SELECT * FROM logs LIMIT 100',
  "SELECT service, count(*) AS errors FROM logs WHERE level = 'error' AND time > now() - 1h GROUP BY service",
  'SEARCH "timeout" FROM logs WHERE time > now() - 24h LIMIT 200',
];

export const options = {
  scenarios: {
    query_ramp: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: Math.ceil(MAX_VUS / 5) }, // warm up
        { duration: '1m', target: MAX_VUS }, // ramp to peak
        { duration: '2m', target: MAX_VUS }, // sustain
        { duration: '30s', target: 0 }, // ramp down
      ],
      gracefulStop: '30s',
    },
  },
  thresholds: {
    // p95 latency budget on the query endpoint specifically.
    'http_req_duration{endpoint:query}': [`p(95)<${P95}`],
    // Fewer than 1% of query requests may fail.
    query_errors: ['rate<0.01'],
    // No more than 1% HTTP-level failures overall.
    http_req_failed: ['rate<0.01'],
  },
};

export function setup() {
  if (!KEY) {
    throw new Error('QEET_LOGS_API_KEY is required — set it to a key with logs:read or logs:query');
  }
}

export default function () {
  const q = QUERIES[Math.floor(Math.random() * QUERIES.length)];
  const url = `${BASE}/v1/query?q=${encodeURIComponent(q)}`;
  const res = http.get(url, {
    headers: { 'X-Qeet-Api-Key': KEY, Accept: 'application/json' },
    tags: { endpoint: 'query' },
  });

  const ok = check(res, {
    'status is 200': (r) => r.status === 200,
    'has columns[]': (r) => {
      try {
        return Array.isArray(r.json('columns'));
      } catch (_e) {
        return false;
      }
    },
  });
  queryErrors.add(!ok);
}
