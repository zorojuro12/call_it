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
