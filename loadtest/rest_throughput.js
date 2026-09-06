// REST throughput scenario — measures requests/sec against spec §7's
// 5,000 req/s target.
//
// Route choice, deliberately: every *authenticated* REST route in this
// codebase is rate-limited to 60 requests/minute per user
// (backend/internal/httpapi/middleware.go's apiThrottle) — sustaining
// 5,000 req/s against one would mean minting a fresh account roughly
// every 200 microseconds, at which point account.Register's argon2id
// hashing (deliberately slow — CLAUDE.md) becomes the bottleneck under
// test, not the REST server's own throughput. GET /healthz is the one
// route with no auth and no rate limit, so it is what this scenario
// drives — it still measures the same net/http server, middleware
// chain, and Go runtime the SLA cares about; it just doesn't touch
// Redis. See loadtest/README.md for why the server-side histograms
// (Task 3/4's wager and websocket paths) are the authoritative source
// for the *authenticated write-path* p99 targets instead.

import http from 'k6/http';
import { check } from 'k6';

const BASE = __ENV.API_BASE_URL || 'http://localhost:8080';

export const options = {
  scenarios: {
    throughput: {
      executor: 'ramping-arrival-rate',
      startRate: 100,
      timeUnit: '1s',
      preAllocatedVUs: 200,
      maxVUs: 2000,
      stages: [
        { target: 1000, duration: '10s' },
        { target: 3000, duration: '15s' },
        { target: 5000, duration: '20s' },
        { target: 5000, duration: '15s' },
      ],
    },
  },
  thresholds: {
    // Spec §7's double-spend tolerance is 0.00% — a throughput run that
    // quietly sheds requests is not a throughput run, so this is pinned
    // at exactly zero rather than a lenient percentage.
    http_req_failed: ['rate==0'],
    http_req_duration: ['p(99)<15'],
  },
};

export default function () {
  const res = http.get(`${BASE}/healthz`);
  check(res, { 'status 200': (r) => r.status === 200 });
}
