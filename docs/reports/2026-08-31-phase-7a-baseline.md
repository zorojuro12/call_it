# Phase 7a — Performance Baseline

**Date:** 2026-08-31
**Measured against:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md) §7
**Produced by:** [`docs/plans/2026-08-31-phase-7a-instrumentation-load-harness.md`](../plans/2026-08-31-phase-7a-instrumentation-load-harness.md) Task 7

This is the first time spec §7's four targets have real measured numbers
behind them, on the raised Go 1.26.7 toolchain (Task 1), via
`internal/metrics`'s server-side histograms (Tasks 2–4) and the k6 harness
(Task 6). 7b acts on whatever below is a MISS; this report only measures.

## Results

| Target | Server-side (authoritative) | k6 client-side (secondary) | Verdict |
|---|---|---|---|
| p99 bet placement latency < 15 ms | **50 ms** (`callit_wager_place_ok_p99_ms`) | `wager_ack_ms` p99 = 36 ms | **MISSED** |
| Global WebSocket sync latency < 30 ms | **0 ms**\* (`callit_ws_sync_p99_ms`) | — (not separately isolated by the scenario) | **MET** |
| Target throughput 5,000+ req/s | **3,174 req/s** (`http_reqs` rate) | k6 `http_reqs`: 3,174.16/s | **MISSED** |
| Double-spend tolerance exactly 0.00% | Not re-measured this phase — see below | — | **MET** (pre-existing proof) |

\* `callit_ws_sync_p99_ms` renders as an integer millisecond; the raw p50
and p99 both fell in the histogram's smallest bucket (≤ 0.5 ms), which
`internal/metrics`'s integer-millisecond rendering floors to `0`. See
"Bucket-resolution honesty" below.

## How each number was produced

**Environment for both runs:** WSL2, `uname -r` `6.6.87.2-microsoft-standard-WSL2`,
`nproc` 4, Go `go1.26.7 linux/amd64`, `cmd/api` run via `go run` (not a
compiled release binary), Redis/PostgreSQL/Kafka via `docker compose
--profile full up -d`. Each scenario ran once against a **freshly
restarted** `cmd/api` process (empty histograms at the start of each run),
so neither run's numbers are polluted by the other's traffic.

### p99 bet placement latency + throughput

```bash
JWT_SECRET=$(openssl rand -hex 32) METRICS_ADDR=127.0.0.1:9090 go run ./backend/cmd/api &
k6 run loadtest/rest_throughput.js
curl -s http://127.0.0.1:9090/
```

`loadtest/rest_throughput.js`, a `ramping-arrival-rate` scenario against
`GET /healthz` (every authenticated route in this codebase is
rate-limited to 60 req/min/user, which would make a 5,000 req/s
authenticated-route run measure the rate limiter rather than server
throughput — see the script's header comment), ramping 100 → 1000 → 3000
→ 5000 iterations/sec over 60s, `preAllocatedVUs: 200`, `maxVUs: 2000`.
190,444 total requests, 0 failed, `dropped_iterations: 6` (the executor
could not fully keep pace at the top of the ramp). k6's own
`http_req_duration p(99)` was 9.5 ms; this is the general REST path's
p99, not the wager-placement path's — bet placement's own p99 comes only
from `callit_wager_place_ok_p99_ms`, captured in the wager-latency run
below.

### WebSocket sync latency + wager placement latency (repeat, real path)

```bash
JWT_SECRET=$(openssl rand -hex 32) METRICS_ADDR=127.0.0.1:9090 go run ./backend/cmd/api &
WAGER_TEST_DURATION_S=30 WAGER_PLAYERS=5 k6 run loadtest/wager_latency.js
curl -s http://127.0.0.1:9090/
```

`loadtest/wager_latency.js`: 1 host VU opens one round covering the whole
run; 5 player VUs join as guests, each pacing itself to roughly 1
wager/sec (well under the 20-per-10s limiter) and sending a fresh UUIDv4
`idempotency_key` per wager. 150 wagers placed server-side
(`callit_wager_place_ok_count`), 0 rejected, 0 dropped broadcasts
(`callit_ws_send_dropped`). k6's own exit code was 99 — the
`wager_ack_ms p(99)<15` threshold failed at k6's measured 36 ms — which
is the *expected, informative* outcome the plan's Task 6 Gate 3 names:
this gate proves the harness produces a trustworthy number, not that the
number is good.

## The fidelity caveat

The parent implementation plan's §10 risk table names p99 measurement
fidelity under WSL2 as a known Medium risk. This report follows its
mitigation: **the server-side histograms
(`callit_wager_place_ok_p99_ms`, `callit_ws_sync_p99_ms`) are treated as
primary**, and k6's own client-side `http_req_duration` / `wager_ack_ms`
figures are secondary, directionally-useful cross-checks only — not the
number reported against spec §7. Every MET/MISSED verdict above is drawn
from the server-side column. Both runs in this session also carried a
`go run` build (unoptimized debug binary, not `go build`'s release
artifact) and ran on a 4-core WSL2 VM with the full Redis+PostgreSQL+Kafka
stack and this Claude Code session itself competing for the same CPUs —
none of that is corrected for below, and 7b's tuning pass should account
for it before drawing conclusions from the specific numbers, not just
their MET/MISSED direction.

## Bucket-resolution honesty

`internal/metrics.Histogram.Quantile` returns the **upper bound** of the
bucket a quantile falls in, never an interpolated estimate — a p99
rendered as `50` means "at or below 50 ms," not "exactly 50 ms." The
bucket boundaries are `0.5, 1, 2, 5, 10, 15, 20, 30, 50, 100, 250, 500,
1000` ms plus an overflow bucket (rendered as `-1`, not observed in
either run above). A value of `0` in the table above means the true
latency fell at or below the 0.5 ms bucket, not that it was literally
zero — stated explicitly here since a table implying more precision than
the instrument has would be worse than one that admits the bound.

## What 7b must act on

- **p99 bet placement latency (50 ms server-side, target < 15 ms) — MISSED.**
  The write path is rate-limit check → idempotency parse → round lookup →
  balance read → the atomic Lua write → the `odds_updated` broadcast
  encode, all inside `wager.Service.Place`'s timed span. 7b should profile
  which of those steps dominates before assuming it's the Lua round trip —
  this session's numbers were taken on an unoptimized `go run` build
  under host contention, which the profiling pass should control for.
- **Target throughput (3,174 req/s server-observed, target 5,000+) — MISSED,**
  measured against `GET /healthz`, the cheapest route in the system. A
  ceiling below target on the *cheapest* route means the gap isn't a
  business-logic cost — 7b should look at the HTTP server's own
  concurrency limits, WSL2 network stack overhead (the same fidelity
  caveat above), and whether `go build`'s optimized binary alone closes
  meaningfully more of the gap than the tuning below it.
- **`callit_ws_send_dropped` is 0 in both runs** — nothing to act on here;
  recorded per Task 7's checklist for completeness, not because it's a
  problem.
- **Double-spend 0.00% is not re-measured this phase.** It is already
  proven by `internal/redisstore/concurrency_test.go` (N goroutines racing
  one wallet, asserting zero double-spend and exact token conservation)
  and `internal/ledger/reconcile_test.go` (the Redis↔PostgreSQL
  reconciliation identity). The parent plan's §9 row assigns the
  **post-load** reconciliation re-run — proving that identity still holds
  *after* a real k6 run has driven traffic through the system — to **7b**,
  not 7a; this report restates the existing proof but adds none of its
  own, on purpose. See `docs/plans/2026-08-21-implementation-plan.md` §12.

## No per-user, per-room, or per-round data

Every number in this report is a process-aggregate count or latency
quantile from `internal/metrics`'s registry, or a k6 scenario-wide
summary statistic. Nothing here identifies an individual wager, player,
or room — the same anonymity invariant `Settlement.Results` exists to
protect binds this document too.
