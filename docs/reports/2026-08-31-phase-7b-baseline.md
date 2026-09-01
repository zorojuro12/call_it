# Phase 7b — Tuning + Reconciliation Baseline

**Date:** 2026-08-31 / 2026-09-01
**Measured against:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md) §7
**Produced by:** [`docs/plans/2026-08-31-phase-7b-tuning-reconciliation.md`](../plans/2026-08-31-phase-7b-tuning-reconciliation.md), Tasks 1–7
**Acts on:** [`docs/reports/2026-08-31-phase-7a-baseline.md`](2026-08-31-phase-7a-baseline.md), which named the two MISSED targets and left §12's post-load reconciliation box unchecked

This report reads side by side with 7a's. 7a measured; 7b acted — cut the
wager-placement path from five sequential Redis round trips to two,
re-baselined throughput on an optimized binary, and proved the
Redis↔PostgreSQL reconciliation identity holds after a real k6 load run.

## Results

| Target | 7a (server-side) | 7b (server-side) | Verdict |
|---|---|---|---|
| p99 bet placement latency < 15 ms | 50 ms (150 samples) | **15 ms** (~6,000 samples) | **INCONCLUSIVE-AT-BOUNDARY** |
| Global WebSocket sync latency < 30 ms | 0 ms\* | **1 ms** | **MET** |
| Target throughput 5,000+ req/s | 3,174 req/s | **3,174 req/s** (optimized binary, 16 cores) | **MISSED, generator-bound** |
| Double-spend tolerance exactly 0.00% | Not re-measured | **Held** — concurrency suite re-run + reconciliation proven after 5,983 real wagers | **MET** |

\* 7a's `0 ms` is the bucket-resolution floor for a value ≤ 0.5 ms, not a
literal zero — see "Bucket-resolution honesty" below, restated from 7a.

## p99 bet placement latency — before/after, and why this isn't MET either

**7a's number was untrustworthy on its own terms.** 150 samples' p99 is
the second-worst sample — one GC pause moves it a whole bucket. Task 1
re-took it on an optimized `bin/api` build (`go build`, not `go run`,
which disables inlining) over ~6,000 samples before touching any code:

| | Task 1 (pre-tuning) | Task 5 (post-tuning) |
|---|---|---|
| `callit_wager_place_ok_count` | 5,971 | 5,980 |
| `callit_wager_place_ok_p50_ms` | 5 | 5 |
| `callit_wager_place_ok_p99_ms` | **15** | **15** |
| avg latency (`sum_ms`/`count`) | 4.76 ms | 2.73 ms |

The optimized-binary re-baseline alone dropped the reported p99 from 7a's
50 ms to 15 ms — before Tasks 2–4 changed a single line. That is itself
worth naming: **most of 7a's MISS was measurement noise (150 samples,
`go run`, host contention), not a real 50 ms cost.**

**Tasks 2–4 then cut the wager-placement path from five sequential Redis
round trips to two** — one pipelined preflight (rate limit + current
round + player count, via `Store.WagerPreflight`), then the atomic
`place_wager.lua` write. `place_wager.lua` also gained a stake-sign guard
(Task 2) it needed regardless: the balance pre-check Task 3 removes was
the only thing standing between the write path and a wallet-minting bug
(a negative `amount` passed the script's only guard and credited instead
of debited — verified empirically during planning, never reachable in
production because the pre-check sat in front of it).

**Applying the bucket-resolution rule without softening it: the rendered
p99 is `15` both before and after.** That proves "≤ 15 ms" on both sides,
never "< 15 ms" — **INCONCLUSIVE-AT-BOUNDARY, not MET**, on both the
pre-tuning and post-tuning number. The 5→2 round-trip reduction did not
move the tail into a lower bucket. It did cut the *average* case nearly in
half (4.76 ms → 2.73 ms, from the server's own `sum_ms`/`count`) — a real,
substantial win that the bucket-resolution instrument simply cannot see
at p99. Whatever sits at the tail (GC pause, goroutine scheduling, or the
WSL2 network stack) evidently isn't Redis round-trip count. Settling it
needs finer histogram bucket bounds than the current
`[…10, 15, 20…]` ladder provides — carried to Phase 7c or 8, not solved
here.

**A confound investigated rather than waved through:** the post-tuning
run's `err_count` (8, then 20 on a repeat) exceeded Task 1's (4), which
the plan's Gate 1 acceptance bar treats as a potential sign the tuning
traded latency for rejections. A dedicated control run — 70s/40 players,
short enough to fit inside one round with zero lock→resolve→reopen
transitions — produced **0 errors, 2,800/2,800 successes** (the
theoretical max). That is conclusive: the tuned path rejects nothing on
its own. The 4/8/20 error counts across round-cycling runs are attempts
landing in the brief locked-or-between-rounds window
`wager_latency.js`'s own host cycling opens roughly every 90 seconds (a
Task 1 harness artifact, not a Task 2–4 code path) — sensitive to exact
timing, present in Task 1's own baseline too. Full investigation in
`loadtest/README.md`'s "Phase 7b post-tuning measurement" section.

## Throughput — still MISSED, but not for the reason 7a named

7a attributed 3,174 req/s (against the 5,000 target, on `GET /healthz`,
the cheapest route in the system) to "a 4-core WSL2 VM that also hosts
Redis, PostgreSQL, and Kafka." Task 6 re-ran the identical scenario on an
optimized binary, twice, on **this** environment — `nproc` reports **16**
cores, not 4 — and got **3,174.30/s and 3,174.24/s**, within 0.06 req/s of
7a's figure and each other.

Every variable 7a's attribution named has changed (4× the cores, an
optimized binary instead of `go run`, database containers idle at <1%
CPU throughout) and the ceiling didn't move. Mid-run sampling found
neither `cmd/api` (26–46% of *one* core) nor the database stack anywhere
near saturated, while k6 itself ran hotter than the server it was
testing (120% CPU) using only 3–6 of its 200 preallocated VUs — the
signature of the load generator's own scheduler being the limit, not
VUs blocked waiting on a slow server.

**Verdict: MISSED, environment-bound — but the environment is k6's own
`ramping-arrival-rate` executor on this host (most likely WSL2
syscall/timer characteristics), not `cmd/api`, not the database stack,
and not core count.** Named finding for Phase 7c or 8: measuring 5,000
req/s fairly needs a load generator proven not to bind on its own
scheduler at this rate, run natively or on a separate host — not further
`cmd/api` tuning, since nothing gathered here points at the server. Full
data in `loadtest/README.md`'s "Phase 7b throughput re-baseline" section.

## Double-spend / reconciliation — the §12 box, now checked

Zero-double-spend was already proven two ways before this phase:
`internal/redisstore/concurrency_test.go` (N goroutines racing one
wallet) and `internal/ledger/reconcile_test.go` (the reconciliation
identity over fixtures the test itself builds). Task 4's boundary
verification re-ran the concurrency suite because it changed the path
that reaches the write — it still holds.

**What had never been proven: that the reconciliation identity holds
over state a real load run produced**, not a fixture. Task 7's
`TestReconcileAfterLoad` (`backend/internal/ledger/reconcile_after_load_test.go`)
closes that: run against the room a real 240s/25-player k6 run created,

```
redis_wallet(user, room) − opening_stake(user, room) == ledger_balance(user, room)
```

held for every player, every transaction balanced (Σdebits == Σcredits),
and the wager-transaction count matched the wallet-debit-entry count
exactly — over **5,983 wagers plus 2 settlements**, well past the plan's
1,000-wager floor for this to count as load-scale evidence. Full
procedure and result in `loadtest/README.md`'s "Phase 7b reconciliation
after a real load run" section, including a build-time correction (the
plan named the transaction kind `wager_placed`; the schema actually
writes `wager`) and an unrelated environmental fix (this phase's own load
runs had pushed ~44,000 messages into the shared Kafka topics, causing
three fixture-based reconciliation tests to time out on an unrelated
consumer-group backlog — fixed by deleting and letting the topics
recreate empty, touching no data of record).

## Bucket-resolution honesty

Restated from 7a, unchanged: `internal/metrics.Histogram.Quantile`
returns the bucket's **upper bound**, never an interpolated estimate.
Bounds: `0.5, 1, 2, 5, 10, 15, 20, 30, 50, 100, 250, 500, 1000` ms plus an
overflow bucket. A rendered `15` proves "≤ 15 ms," never "< 15 ms" — the
reason this report's headline number is INCONCLUSIVE-AT-BOUNDARY rather
than MET, stated plainly rather than rounded away.

## No per-user, per-room, or per-round data

Every number in this report is a process-aggregate count or latency
quantile from `internal/metrics`'s registry, a k6 scenario-wide summary
statistic, or a room-level aggregate check
(`TestReconcileAfterLoad`'s own assertions never print an individual
stake). Nothing here identifies an individual wager, player, or the
contents of any room — the same anonymity invariant `Settlement.Results`
exists to protect binds this document too.
