# CallIt load test scripts

k6 scripts that drive real load against a live backend, added in Phase 7a
to give spec §7's performance targets real measured numbers. k6 is an
external binary, not a Go dependency — it installs user-locally and adds
nothing to `go.mod`.

## Install

```bash
mkdir -p "$HOME/.local/bin" && cd "$HOME" && \
  curl -fsSLO https://github.com/grafana/k6/releases/download/v2.2.0/k6-v2.2.0-linux-amd64.tar.gz && \
  tar -xzf k6-v2.2.0-linux-amd64.tar.gz && \
  mv k6-v2.2.0-linux-amd64/k6 "$HOME/.local/bin/k6" && \
  rm -rf k6-v2.2.0-linux-amd64 k6-v2.2.0-linux-amd64.tar.gz && \
  k6 version
```

`$HOME/.local/bin` must be on `PATH` (the same non-interactive-shell caveat
CLAUDE.md documents for Go — add it explicitly if a tool-execution shell
reports `k6: not found`). No sudo is available or needed; this is a
user-local install like the Go toolchain.

## Running a scenario

Both scenarios need the backend and Redis (`make up`) running, and
`cmd/api` started with `METRICS_ADDR` set so the server-side histograms are
reachable:

```bash
make up
JWT_SECRET=$(openssl rand -hex 32) METRICS_ADDR=127.0.0.1:9090 go run ./backend/cmd/api &
make loadtest                          # runs rest_throughput.js
make loadtest SCENARIO=wager_latency   # runs wager_latency.js
```

## What each scenario measures

- **`rest_throughput.js`** — a ramping-arrival-rate scenario climbing
  toward spec §7's 5,000 requests/sec target, each iteration an
  authenticated REST call. Threshold: `http_req_failed` pinned at exactly
  `rate==0` (spec §7's double-spend tolerance is 0.00%, so a run that
  quietly sheds requests isn't measuring throughput) and
  `http_req_duration p(99)<15`.
- **`wager_latency.js`** — a WebSocket scenario: one VU opens rounds as
  host, separate VUs join as players and place wagers, each with a fresh
  UUIDv4 idempotency key. Measures `wager_ack_ms`, the elapsed time from
  sending `place_wager` to receiving the matching `wager_accepted` reply.
  Threshold: `wager_ack_ms p(99)<15`.

## `wager_latency.js`'s host cycles rounds — required by `MaxLockIn`

`round.Service.Open` (`backend/internal/round/service.go`) caps a round's
lock window at `MaxLockIn = 120s` and rejects anything longer with
`ErrInvalidSpec`. Discovered during Phase 7b Task 1: the scenario's host
originally opened **one** round sized to the whole scenario duration
(`(SCENARIO_TIMEOUT_S + 10) * 1000` ms) — fine at 7a's 30s run, silently
rejected at 240s, since 250s far exceeds the cap. The rejection is
invisible in the scenario's own output: the host has no `onmessage`
handler, so the server's private error reply is never read, and
`round_opened` never broadcasts — every player waits the whole run for a
message that never arrives, and every wager metric reads zero. There is no
k6 or server error line to see; the only signal is the wager counters
staying at 0.

The host now cycles rounds instead: opens one with a fixed 90s lock window
(`ROUND_LOCK_IN_MS`, comfortably inside `[MinLockIn, MaxLockIn]` regardless
of scenario length), resolves it as soon as `round_locked` arrives, and
opens the next one on `round_resolved`. Players need no matching change —
a `place_wager` sent between rounds gets `no_active_round` or
`pool_locked`, which their existing error handler already retries a second
later, so the very next attempt lands against the fresh round. Verified at
150s/10 players: 1,500/1,500 wagers placed, zero errors, through a live
lock→resolve→reopen transition.

**A single room did not turn out to be a throughput ceiling.** 25 players
over 240s in one room reached 5,971 successful placements server-side
(Task 1 Gate 2, below) — no multi-room split was needed to clear the 5,000
floor once the `MaxLockIn` rejection was fixed.

## Phase 7b pre-tuning baseline (Task 1)

Optimized `bin/api` build (`make loadtest-api`), fresh process (empty
histograms), single room:

```bash
JWT_SECRET=$(openssl rand -hex 32) make loadtest-api &
WAGER_TEST_DURATION_S=240 WAGER_PLAYERS=25 k6 run loadtest/wager_latency.js
curl -s http://127.0.0.1:9090/
```

Server-side (`METRICS_ADDR`), the authoritative figures:

| Metric | Value |
|---|---|
| `callit_wager_place_ok_count` | 5971 |
| `callit_wager_place_ok_p50_ms` | 5 |
| `callit_wager_place_ok_p99_ms` | 15 |
| `callit_wager_place_err_count` | 4 |
| `callit_ws_sync_p50_ms` | 0 |
| `callit_ws_sync_p99_ms` | 1 |
| `callit_ws_send_dropped` | 0 |

Per the bucket-resolution rule (`docs/plans/2026-08-31-phase-7b-tuning-reconciliation.md`,
Global Constraints), a rendered `15` proves "≤ 15 ms," not "< 15 ms" — this
pre-tuning number is not itself a verdict (Task 1 tunes nothing); it is the
number Task 5's post-tuning run is compared against. Already a steep drop
from 7a's reported 50ms, but 7a's figure came from only 150 samples where
this comes from ~6,000 — consistent with the plan's own suspicion that
7a's number was one or two GC-pause outliers rather than a systemic cost.

## Phase 7b post-tuning measurement (Task 5)

Same procedure as Task 1, same knobs, a freshly started `bin/api` — after
Tasks 2–4 cut the wager path from five sequential Redis round trips to
two (one pipelined preflight, then `place_wager.lua`):

```bash
JWT_SECRET=$(openssl rand -hex 32) make loadtest-api &
WAGER_TEST_DURATION_S=240 WAGER_PLAYERS=25 k6 run loadtest/wager_latency.js
curl -s http://127.0.0.1:9090/
```

| Metric | Task 1 (pre) | Task 5 (post) | Delta |
|---|---|---|---|
| `callit_wager_place_ok_count` | 5971 | 5980 | — |
| `callit_wager_place_ok_p50_ms` | 5 | 5 | unchanged |
| `callit_wager_place_ok_p99_ms` | 15 | 15 | unchanged (same bucket) |
| avg latency (`sum_ms`/`count`) | 4.76ms | 2.73ms | **−43%** |
| `callit_wager_place_err_count` | 4 | 8 | +4 (investigated below) |
| `callit_ws_sync_p50_ms` / `p99_ms` | 0 / 1 | 0 / 1 | unchanged |

**Verdict, applying the bucket-resolution rule without softening it:**
`callit_wager_place_ok_p99_ms` rendered `15` both before and after —
**INCONCLUSIVE-AT-BOUNDARY**, not MET. The instrument proves "≤ 15 ms" on
both sides of the tuning; the rendered p99 did not move by even one
bucket. That is itself the finding the plan asked this gate to state
plainly if it happened: **the 5→2 round-trip reduction was not the
dominant cost at the tail.** The *average*-case win is real and
substantial — mean per-wager latency nearly halved (4.76ms → 2.73ms,
computed from the server's own `sum_ms`/`count`) — but whatever sits at
p99 (GC pause, goroutine scheduling, or the WSL2 network stack the
"server-side figures are primary" section below already flags as a
known fidelity risk) evidently isn't Redis round-trip count. Settling
which needs finer histogram bucket bounds than the current `[…10, 15,
20…]` ladder provides — out of this phase's scope, carried to Phase 7c
or 8.

**The `err_count` increase (4 → 8) does not clear the plan's literal Gate
1 acceptance bar** ("err_count no higher than Task 1's — a tuning pass
that traded latency for rejections has not tuned anything"), so it was
investigated rather than waved through:

- A second same-knobs run (240s/25 players, fresh process) produced
  `ok_count = 5980` — **bit-for-bit identical** to the first post-tuning
  run — with `err_count = 20` and `p99 = 10ms` this time. An identical
  success count across two independent runs, with only the error count
  and p99 bucket moving, is the signature of a fixed success ceiling
  plus a noisy tail, not a shifting success/failure split.
- A control run was added specifically to isolate the tuning from the
  scenario's own round-cycling: 70s/40 players, chosen so the whole run
  fits inside one 90s round and **no lock→resolve→reopen transition
  happens at all**. Result: `err_count = 0`, `ok_count = 2800` — exactly
  40 × 70, the theoretical maximum with zero rejections.

That control run is conclusive: with the round-transition window removed
entirely, Tasks 2–4's tuned path rejects nothing. The 4/8/20 error
counts across the three round-cycling runs are attempts landing in the
brief locked-or-between-rounds window `wager_latency.js`'s host cycling
(Task 1, not Task 2–4) opens roughly every 90 seconds — a k6-harness
artifact, present in Task 1's own baseline too, whose count is sensitive
to exact timing (a single slow resolve+reopen round trip, e.g. from a GC
pause, widens that window for one cycle and can multiply the hit count
for that run alone). Nothing in Tasks 2–4 touches the code paths that
decide `POOL_LOCKED` or `no_active_round` — only how the preflight values
reach `wager.Service.Place`, not when a round is considered locked or
absent. **Conclusion: no correctness regression from the tuning; the
literal Gate 1 comparison is confounded by a harness artifact orthogonal
to what Tasks 2–4 changed**, and the control run demonstrates that
directly rather than asserting it.

## Phase 7b throughput re-baseline and gap attribution (Task 6)

```bash
JWT_SECRET=$(openssl rand -hex 32) make loadtest-api &
k6 run loadtest/rest_throughput.js
```

Two runs, optimized `bin/api`, fresh process each time, this environment
(`nproc` reports **16** cores — not the 4 the 7a report assumed; see
below):

| Metric | Run 1 | Run 2 | 7a (for reference) |
|---|---|---|---|
| `http_reqs` rate | 3174.30/s | 3174.24/s | 3174.16/s |
| `http_req_failed` | 0.00% | 0.00% | 0 failed |
| `http_req_duration` p99 | 837µs | 725µs | — |
| `http_req_duration` avg | 363µs | 340µs | — |

**Gate 2 — is the server or the host the binding constraint?** Sampled
mid-run both times:

| Sample | `bin/api` CPU | `k6` CPU | Load avg (16 cores) | DB containers |
|---|---|---|---|---|
| Run 1, t=25s (ramping to 3000) | 26.5% of 1 core | not sampled | 1.36 | each <1% |
| Run 2, t=40s (at 5000 target) | 46.1% of 1 core | **120%** (>1 core) | 1.76 | each <1% |

Neither `cmd/api` nor Redis/PostgreSQL/Kafka were ever close to
saturated — at most 46% of a single core out of 16, on a host whose load
average never exceeded 1.76/16. k6 itself, though, was running **hotter
than the server it was testing** (120% CPU vs. 46%) while using only 3–6
of its 200 preallocated VUs — if the server were slow, k6 would need many
more concurrent VUs blocked waiting on responses; it needed almost none,
because `http_req_duration` averaged under 400µs the whole run.

**Gate 3 — MISSED, environment-bound, but not the environment 7a named.**
7a attributed its identical-shaped result to "a 4-core WSL2 VM that also
hosts Redis, PostgreSQL, and Kafka." That attribution doesn't survive
this run: this environment has 16 cores, the optimized binary removes
`go run`'s overhead, and the database containers sit at <1% CPU
throughout — yet the ceiling reproduced within 0.06/s of 7a's figure
across three independent runs (7a's, and this phase's two). Holding
constant while every named variable changed points at k6's own
`ramping-arrival-rate` executor as the actual ceiling on this host — most
likely WSL2's syscall/timer characteristics interacting with k6's
open-model iteration scheduler — not `cmd/api`'s request handling, not
the database stack, and not raw core count. **Named finding for Phase 7c
or 8:** measuring the 5,000 req/s target fairly needs either a load
generator run natively (outside WSL2, or on a dedicated host separate
from the server under test) or a k6 configuration proven not to bind on
its own scheduler at this rate — not more `cmd/api` tuning, since nothing
gathered here points at the server.

## Server-side figures are primary, k6's are secondary

k6's own client-side timing is measured from wherever k6 runs, which under
WSL2 crosses the VM's virtualized network stack before it ever reaches
`cmd/api` — the parent implementation plan's risk table names this as a
known Medium risk to p99 measurement fidelity. **Treat the
`callit_wager_place_ok_p99_ms` / `callit_ws_sync_p99_ms` values from the
`METRICS_ADDR` endpoint as authoritative**, and k6's own
`http_req_duration` / `wager_ack_ms` summaries as a secondary,
directionally-useful cross-check — not the number reported against spec
§7's targets.
