# Phase 7b — Tuning + Reconciliation Under Load Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Act on the two targets Phase 7a measured as MISSED — cut the
wager-placement path from five sequential Redis round trips to two and
re-measure its p99 against spec §7's < 15 ms, re-baseline throughput on an
optimized binary and either close the 5,000 rps gap or record this
environment's ceiling with evidence — and prove the Redis↔PostgreSQL
reconciliation identity still holds after a real k6 load run, closing §12's
last unchecked money-correctness box.

**Architecture:** The tuning is structural, not parametric: no timeouts,
pool sizes, or GC knobs are touched. `wager.Service.Place` issues five
sequential Redis round trips today (`Allow` → `CurrentRound` → `Balance` →
`PlaceWager` → `PlayerCount`). One of those reads is redundant with a check
`place_wager.lua` already performs, and three of them are independent of each
other and of the write. Removing the redundant read and pipelining the rest
leaves two round trips: one batched preflight, then the atomic Lua write.
`place_wager.lua` gains one guard it should always have had, so the money
invariant it enforces stops depending on a caller-side check that this phase
deletes. Measurement bookends the tuning: a re-taken pre-baseline with enough
samples for a p99 to mean anything, then the same procedure again afterward.

**Tech Stack:** Go 1.26.7 · Redis 7.2 (Lua, pipelining) · PostgreSQL 16 ·
Kafka 3.7 · k6 (already installed by 7a) · no new Go dependencies.

**Spec:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md) §7 (Performance & Scale Targets)
**Parent plan:** [`docs/plans/2026-08-21-implementation-plan.md`](2026-08-21-implementation-plan.md) §9 (row 7b), §12 (Acceptance)
**Baseline this phase acts on:** [`docs/reports/2026-08-31-phase-7a-baseline.md`](../reports/2026-08-31-phase-7a-baseline.md)

---

## Global Constraints

Every task's requirements implicitly include this section.

- **SLA targets, verbatim from spec §7:** p99 bet placement latency **< 15 ms**;
  global WebSocket sync latency **< 30 ms**; target throughput **5,000+
  requests/sec**; double-spend tolerance **exactly 0.00%**.
- **This phase adds no new Go dependency.** Never run `go get -u`; pin every
  `go get` target explicitly, subpackages included (`CLAUDE.md`). If a task
  appears to need a dependency, stop and report rather than adding one.
- **`go` may report "not found" in a non-interactive shell.** Prefix every Go
  command with `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin`
  (`CLAUDE.md`, Known Environment Gotchas). No sudo is available; install
  user-locally.
- **`-p 1` is load-bearing.** Never drop it from a `go test ./...` invocation.
  Integration suites share Redis **DB 15**.
- **`internal/domain` stays free of I/O.** Task 3 adds a function to it; that
  function takes an amount and returns an error, and touches nothing else.
- **All amounts are integer token units.** No float ever reaches Redis or the
  ledger. Latency values are `time.Duration` until rendering.
- **`internal/redisstore/keys.go` is the only place a Redis key may be
  constructed.** Task 4's pipelined method calls the existing builders
  (`RateLimitKey`, `RoomRoundKey`, `RoomWalletsKey`); it constructs no key by
  hand.
- **One sliding-window rate limiter, every call site.** Task 4 issues
  `rate_limit.lua` inside a pipeline instead of standalone. That is the same
  script through a different transport — it is **not** a second
  implementation, and no task may fork one.
- **Settlement math is not duplicated in Lua.** Task 2 adds a stake-sign
  guard to `place_wager.lua`, which is input validation, not payout math.
  No task moves a payout, dust, or odds computation into Lua.
- **Wagers stay anonymous until the round is terminal.** No metric, log line,
  report row, or load-test output may carry a per-user stake, a room ID tied
  to a user, or a per-wager amount. Every number this phase records is a
  process aggregate or a scenario-wide summary.
- **Judge coverage from `go test ./... -coverpkg=./...`**, never the
  per-package figure. `cmd/*` at 0% is expected, not a gap.
- **Measurement runs are not checkpoints.** See the note below.

### Bucket resolution binds every verdict in this phase

`internal/metrics.Histogram.Quantile` returns the **upper bound** of the
bucket a quantile lands in, never an interpolation. Bounds are `0.5, 1, 2, 5,
10, 15, 20, 30, 50, 100, 250, 500, 1000` ms plus an overflow bucket rendered
as `-1`.

Therefore: **a p99 rendered as `15` means "≤ 15 ms" and does NOT prove
"< 15 ms".** To claim the p99 bet-placement target MET, the rendered
`callit_wager_place_ok_p99_ms` must be **10 or lower**. A rendered `15` is
reported as INCONCLUSIVE-AT-BOUNDARY, never as MET. No task may soften this.

### A note on checkpoint honesty in Tasks 1, 5, 6, and 7

Tasks 2, 3, and 4 are ordinary RED→GREEN checkpoints. **Tasks 1, 5, 6, and 7
are numbered *verification gates*, not checkpoints, because their
deliverables are measurements and a report — none has a Go test that can fail
before the measurement exists.** Writing them as checkpoints would produce a
Step 1 saying "expect FAIL" for something that cannot fail, which is the
contradiction that halts a cold executor (`writing-plans`, Bite-Sized Task
Granularity). This is the same convention Phase 7a's plan used for its Tasks
6 and 7. Each gate still ends in one commit, chained behind a command whose
exit code gates it.

---

## The measured problem this phase acts on

`wager.Service.Place` (`backend/internal/wager/service.go`) issues **five
sequential Redis round trips** on the happy path:

| # | Call | Redis command | Independent of the others? |
|---|---|---|---|
| 1 | `store.Allow` | `rate_limit.lua` (EVALSHA) | yes |
| 2 | `store.CurrentRound` | `GET room:{id}:round` | yes |
| 3 | `store.Balance` | `HGET room:{id}:wallets {user}` | yes — **and redundant** |
| 4 | `store.PlaceWager` | `place_wager.lua` (EVALSHA) | the write; needs #2's round ID |
| 5 | `store.PlayerCount` | `HLEN room:{id}:wallets` | yes — placing a wager does not change it |

Round trip #3 is redundant: `place_wager.lua` already rejects
`INSUFFICIENT_FUNDS` (line 71 of the script) and `NOT_IN_ROOM` itself, and
`mapWagerStatus` maps that reply to **the same `domain.ErrInsufficientFunds`
sentinel** `domain.ValidateStake` returns. The read buys no error the script
does not already produce.

Round trips #1, #2, and #5 are mutually independent and independent of the
write, so they can travel in one pipeline. #5 currently sits *after* the
write only because the reply is assembled there; nothing about it depends on
the wager.

**Five round trips become two: one pipelined preflight, then the Lua write.**

### The load-bearing accident Task 2 exists to fix

`place_wager.lua` does **not** check the sign of `amount`. Its only balance
guard is `tonumber(existingBalance) < amount`, which a negative amount passes
(`1000 < -100` is false). It then runs `HINCRBY walletsKey userID -amount`,
which for `amount = -100` **credits the wallet by 100** and decrements the
outcome pool.

That hole is closed today only because `Place` reads the balance (#3) and
calls `domain.ValidateStake`, whose `amount <= 0` arm rejects it first. In
other words, **the round trip this phase deletes is the only thing standing
between the money path and a wallet-minting bug.** Task 2 therefore lands the
guard in the script *before* Task 3 removes the caller-side check. Order is
not negotiable: Task 3 must not be executed before Task 2 is committed.

**Verified empirically during this planning pass**, against the real script
on a live Redis (DB 14, flushed after), seeding `player-1` with a balance of
`1000` and invoking `place_wager.lua` with `amount = -100`:

```
reply : OK, 1100, 1, -100, -100
wallet: 1000 → 1100     (credited, not debited)
pool 0: -100
total : -100
outbox: 1 entry written
```

The script accepted it as `OK`, minted 100 tokens, drove the pool and total
negative, **and emitted an outbox event** — which the relay would carry to
Kafka and `cmd/ledger-worker` would write into the double-entry ledger as a
real money row. The executor does not need to re-derive this; Task 2
Checkpoint 1's RED is this behavior.

**Reachability, stated precisely so this is not over- or under-sold:** it is
**latent, not live**. The only path to `Store.PlaceWager` is
`ws.Router` → `wager.Service.Place` → `Store.PlaceWager`, and the pre-check
sits on it, so no current caller can reach the hole. What makes it worth
fixing first is that the guard's placement is an accident of an optimization
rather than a decision — a future caller of `Store.PlaceWager`, or this
phase's own Task 3, removes it without the script noticing.

---

## File Structure

**Created:**

| Path | Responsibility |
|---|---|
| `backend/internal/ledger/reconcile_after_load_test.go` | The post-load reconciliation assertion — runs against the live stack over rooms a real k6 run created, gated on `RECONCILE_ROOM_IDS`. |
| `docs/reports/2026-08-31-phase-7b-baseline.md` | The revised measured-vs-target table, the before/after wager-path delta, and the throughput ceiling finding. |

**Modified:**

| Path | Change |
|---|---|
| `backend/scripts/lua/place_wager.lua` | Reject a non-positive or non-numeric `amount` with a new `INVALID_STAKE` status. |
| `backend/internal/redisstore/errors.go` | Map `INVALID_STAKE` → `domain.ErrInvalidStake` in `mapWagerStatus`. |
| `backend/internal/redisstore/wager_test.go` | Non-positive-stake rejection tests. |
| `backend/internal/redisstore/ratelimit.go` | Extract the `rate_limit.lua` reply decoding so the pipelined path reuses it rather than forking a second decoder. |
| `backend/internal/redisstore/wager.go` | Add `WagerPreflight` — the one-round-trip pipelined preflight, with its NOSCRIPT reload retry. |
| `backend/internal/redisstore/client.go` | Preload `rate_limit.lua` in `Store.New` so a pipelined `EVALSHA` cannot miss on a cold cache. |
| `backend/internal/domain/wallet.go` | Add `ValidateStakeAmount`; `ValidateStake` delegates its sign arm to it. |
| `backend/internal/domain/wallet_test.go` | `ValidateStakeAmount` table test. |
| `backend/internal/wager/service.go` | Hoist the stake-sign check above every Redis call; delete the `Balance` pre-check; use `WagerPreflight`. |
| `backend/internal/wager/service_test.go` | Error-precedence tests for the two observable changes. |
| `loadtest/README.md` | The optimized-binary run procedure and the sample-count floor. |
| `Makefile` | `loadtest-api` target — builds and runs an optimized `cmd/api` for load runs. |
| `docs/plans/2026-08-21-implementation-plan.md` | §12 reconciliation checkbox; §9 row 7b marked complete. |
| `docs/project-history.md` | Phase 7b outcomes and the coverage note. |
| `CLAUDE.md` | The `place_wager.lua` stake-guard invariant; `make loadtest-api`. |

---

## Task 1: Re-take the wager-path baseline with enough samples to mean something

**Gate, not a checkpoint** (see the note in Global Constraints).

7a's wager run produced **150 successful placements**. The p99 of 150 samples
is the second-worst sample — a single GC pause or scheduler hiccup moves it a
whole bucket. The reported 50 ms may be a systemic cost or may be two
outliers, and nothing in 7a's data distinguishes those. Tuning against a
number that cannot tell the difference is guesswork, which is exactly what
the 7a/7b seam exists to prevent.

This task produces the *real* pre-tuning number, on an optimized binary,
which every later comparison in this phase is made against.

**Files:**
- Modify: `Makefile`, `loadtest/README.md`

**Interfaces:**
- Produces: `make loadtest-api` — builds `backend/cmd/api` with `go build`
  into `backend/bin/api` and runs it with `METRICS_ADDR=127.0.0.1:9090`.
  Later tasks and gates invoke this, never `go run ./cmd/api`.

- [ ] **Gate 1: `make loadtest-api` builds and serves an optimized binary**

Add a `loadtest-api` target to the `Makefile` that runs
`cd backend && go build -o bin/api ./cmd/api` and then execs
`bin/api`, requiring `JWT_SECRET` from the environment and defaulting
`METRICS_ADDR` to `127.0.0.1:9090`. `.gitignore` already carries
`backend/bin/` (line 2) — verified during this planning pass, so the built
binary will not be staged; no `.gitignore` change is needed.

Every load run in this phase uses this target. 7a's numbers were taken under
`go run`, which builds an unoptimized binary with disabled inlining — a
confound the baseline report itself flags and asks 7b to control for.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  make up-full && \
  JWT_SECRET=$(openssl rand -hex 32) make loadtest-api
```
Expected: the API serves on its normal port; `curl -s http://127.0.0.1:9090/`
returns the metric text with every histogram at `_count 0`.

- [ ] **Gate 2: capture the pre-tuning wager baseline over ≥ 5,000 placements**

With a **freshly started** `make loadtest-api` process (empty histograms),
run the wager scenario long enough and wide enough to clear 5,000 successful
placements. 25 players over 240 s at the scenario's ~1 wager/sec/VU pacing
yields roughly 6,000:

```bash
WAGER_TEST_DURATION_S=240 WAGER_PLAYERS=25 k6 run loadtest/wager_latency.js
curl -s http://127.0.0.1:9090/
```

**Acceptance:** `callit_wager_place_ok_count` ≥ 5000. If it is lower, raise
`WAGER_PLAYERS` and re-run against a fresh process — do not report a p99 over
fewer samples, and do not sum two runs' histograms by hand.

Record verbatim, into `loadtest/README.md` under a new "Phase 7b pre-tuning
baseline" heading: `callit_wager_place_ok_count`, `callit_wager_place_ok_p50_ms`,
`callit_wager_place_ok_p99_ms`, `callit_wager_place_err_count`,
`callit_ws_sync_p50_ms`, `callit_ws_sync_p99_ms`, and
`callit_ws_send_dropped`. Note the k6 scenario knobs used, and that the
process was an optimized `bin/api` build.

**Do not tune anything in this task.** Its only output is a trustworthy
number and the procedure that produced it.

```bash
git add Makefile loadtest/README.md && \
  git commit -m "test: re-baseline the wager path on an optimized binary"
```

Expected: one commit.

---

## Task 2: `place_wager.lua` rejects a non-positive stake

Closes the wallet-minting hole described above, **before** Task 3 removes the
Go-side check that is currently hiding it.

**Files:**
- Modify: `backend/scripts/lua/place_wager.lua`, `backend/internal/redisstore/errors.go`
- Test: `backend/internal/redisstore/wager_test.go`

**Interfaces:**
- Produces: `place_wager.lua` reply status `{'INVALID_STAKE'}`;
  `mapWagerStatus` maps it to `domain.ErrInvalidStake`.

**Checkpoint 1: a negative stake is rejected and mutates nothing**

- [ ] **Step 1: Write the failing test, then run it**

Spec: seed a room whose host is `host-1` and whose wallet hash gives
`player-1` a balance of `1000`, with an open round having 2 outcomes and a
`lock_at_ms` in the future. Call
`Store.PlaceWager` with `UserID: "player-1"`, `Outcome: 0`,
`Amount: domain.Tokens(-100)`, and a fresh UUIDv4 idempotency key.

Assert all of:
- the returned error satisfies `errors.Is(err, domain.ErrInvalidStake)`
- `HGET room:{roomID}:wallets player-1` is still `1000`
- `HGET round:{roundID}:pools 0` is still absent or `0`
- `HGET round:{roundID}:pools total` is still absent or `0`
- `XLEN` of the store's outbox stream is unchanged from before the call
- `GET idem:{key}` is absent — a rejected wager caches no reply

Add a second case in the same table with `Amount: domain.Tokens(0)`,
asserting the identical outcome.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/redisstore/ -run TestPlaceWagerRejectsNonPositiveStake -count=1 -race
```
Expected: FAIL. Today the script credits the wallet — the balance assertion
reports `1100`, not `1000`, and the error assertion fails because
`PlaceWager` returned `nil`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: in `place_wager.lua`, immediately **after** the idempotency-cache
block and **before** the `status` HGET, add
`if amount == nil or amount <= 0 then return {'INVALID_STAKE'} end`.

Placement is exact and load-bearing in both directions: after the idempotency
check so a replayed key still returns its cached reply verbatim (preserving
the script's documented "idempotency check first, before any mutation"
ordering), and before every other read so the cheapest rejection costs no
further Redis work. Add the `INVALID_STAKE` case to `mapWagerStatus` in
`backend/internal/redisstore/errors.go`, returning `domain.ErrInvalidStake`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/redisstore/ -run TestPlaceWagerRejectsNonPositiveStake -count=1 -race && \
  git add scripts/lua/place_wager.lua internal/redisstore/errors.go internal/redisstore/wager_test.go && \
  git commit -m "fix: reject a non-positive stake inside place_wager.lua"
```

Expected: PASS, then one commit.

**Task boundary verification**

- [ ] Run the full suite once:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make test
```
Expected: PASS. The script change is additive — no existing test places a
non-positive stake through `Store.PlaceWager`.

---

## Task 3: Retire the redundant balance pre-check round trip

Removes round trip #3. Two observable behavior changes come with it, and both
are the point rather than collateral: with the pre-check gone,
`place_wager.lua` becomes the sole authority on balance *and* on the ordering
of its own rejections, which is what `wager.Service`'s own doc comment
already claims ("No Go-side balance or lockout check — place_wager.lua is the
authority on both").

**Files:**
- Modify: `backend/internal/domain/wallet.go`, `backend/internal/wager/service.go`
- Test: `backend/internal/domain/wallet_test.go`, `backend/internal/wager/service_test.go`

**Interfaces:**
- Consumes: `domain.ErrInvalidStake`, `domain.ErrInsufficientFunds`,
  `redisstore.ErrPoolLocked`, and Task 2's `INVALID_STAKE` mapping.
- Produces: `func ValidateStakeAmount(amount Tokens) error` in
  `internal/domain` — returns `ErrInvalidStake` wrapped with the offending
  amount when `amount <= 0`, else `nil`. `ValidateStake` delegates its sign
  arm to it and keeps its own signature `ValidateStake(amount, sessionBalance Tokens) error`
  unchanged.

**Checkpoint 1: `domain.ValidateStakeAmount` judges a stake without a balance**

- [ ] **Step 1: Write the failing test, then run it**

Spec: table test over `ValidateStakeAmount`:
- `amount = 1` → `nil`
- `amount = 1000000` → `nil`
- `amount = 0` → error satisfying `errors.Is(err, ErrInvalidStake)`
- `amount = -1` → error satisfying `errors.Is(err, ErrInvalidStake)`

Plus one case asserting `ValidateStake(-5, 1000)` still returns
`ErrInvalidStake`, pinning that the delegation preserves existing behavior.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/domain/ -run TestValidateStakeAmount -count=1 -race
```
Expected: FAIL — compile error, `undefined: ValidateStakeAmount`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `ValidateStakeAmount(amount Tokens) error` returns
`fmt.Errorf("%w: got %d", ErrInvalidStake, amount)` when `amount <= 0`, else
`nil`. `ValidateStake` calls it first and returns its error unchanged,
keeping its own `amount > sessionBalance` arm untouched. `internal/domain`
gains no import beyond what it already has.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/domain/ -count=1 -race -cover && \
  git add internal/domain/wallet.go internal/domain/wallet_test.go && \
  git commit -m "refactor: split the stake-sign rule out of ValidateStake"
```

Expected: PASS with `internal/domain` at 100% coverage (its floor —
`CLAUDE.md`), then one commit.

**Checkpoint 2: a non-positive stake is rejected before any room lookup**

- [ ] **Step 1: Write the failing test, then run it**

Spec: through `wager.Service.Place`, against a room that has **no open
round**, with `Amount: domain.Tokens(-100)` and a valid UUIDv4 idempotency
key. Assert the returned error satisfies
`errors.Is(err, domain.ErrInvalidStake)`.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/wager/ -run TestPlaceRejectsNonPositiveStakeBeforeRoundLookup -count=1 -race
```
Expected: FAIL — today `CurrentRound` runs before the stake is ever examined,
so the call returns `ErrNoActiveRound`, not `ErrInvalidStake`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: in `Place`, immediately after the idempotency-key parse and before
the round lookup, call `domain.ValidateStakeAmount(req.Amount)` and return
its error when non-nil. Validating the request's own shape before looking
anything up matches how `ErrBadIdempotency` is already handled.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/wager/ -count=1 -race && \
  git add internal/wager/service.go internal/wager/service_test.go && \
  git commit -m "fix: reject a non-positive stake before any Redis lookup"
```

Expected: PASS, then one commit.

**Checkpoint 3: a locked round reports POOL_LOCKED even when the stake also exceeds the balance**

This is the observable signal that the balance pre-check is gone.

- [ ] **Step 1: Write the failing test, then run it**

Spec: through `wager.Service.Place`, with a round whose `lock_at_ms` is in
the past (or whose `status` is not `open`), a player whose wallet holds
`100`, and `Amount: domain.Tokens(500)` — a stake that both exceeds the
balance and targets a locked round. Assert the returned error satisfies
`errors.Is(err, redisstore.ErrPoolLocked)`.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/wager/ -run TestPlaceOnLockedRoundReportsLockedNotInsufficientFunds -count=1 -race
```
Expected: FAIL — today the Go-side `Balance` read plus `ValidateStake`
returns `domain.ErrInsufficientFunds` before the script is ever consulted.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: delete the entire `if balance, err := s.store.Balance(...)` block
from `Place`, including its `else if !errors.Is(err, redisstore.ErrNotFound)`
arm. Nothing replaces it: `place_wager.lua` already returns
`INSUFFICIENT_FUNDS` (mapped to the same `domain.ErrInsufficientFunds`
sentinel) and `NOT_IN_ROOM`, and Checkpoint 2 now covers the sign rule
without a balance.

Existing `internal/wager` and `internal/httpapi` tests that assert
`domain.ErrInsufficientFunds` on an over-balance wager must keep passing —
the sentinel is identical on both paths, only the wrapped detail string
changes. If any test asserts on that message text rather than the sentinel,
change the assertion to `errors.Is`; do not restore the pre-check to keep a
string assertion alive.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/wager/ ./internal/httpapi/ ./internal/ws/ -count=1 -race && \
  git add internal/wager/service.go internal/wager/service_test.go && \
  git commit -m "perf: drop the redundant balance read from the wager path"
```

Expected: PASS, then one commit.

**Task boundary verification**

- [ ] Run the full suite once:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make test
```
Expected: PASS.

---

## Task 4: Collapse the three pre-write reads into one pipelined round trip

Removes round trips #2 and #5 by folding the rate-limit script, the
current-round lookup, and the player count into a single pipelined call
issued before the write. After this task the happy path is **two** round
trips: the preflight pipeline, then `place_wager.lua`.

**Files:**
- Modify: `backend/internal/redisstore/ratelimit.go`, `backend/internal/redisstore/wager.go`, `backend/internal/wager/service.go`
- Test: `backend/internal/redisstore/wager_test.go`

**Interfaces:**
- Consumes: `RateLimitKey`, `RoomRoundKey`, `RoomWalletsKey` from
  `internal/redisstore/keys.go`; `rateLimitScript`; `Decision`.
- Produces:
  ```go
  type Preflight struct {
      Decision Decision // the rate-limit outcome, identical to Allow's
      RoundID  string   // "" when the room has no current round
      Players  int      // wallet count minus the host, floored at 0
  }

  func (s *Store) WagerPreflight(ctx context.Context, scope, userID, roomID string,
      limit int, window time.Duration) (Preflight, error)
  ```
  A room with no current round yields `RoundID == ""` and a nil error — the
  pipeline's `redis.Nil` on that leg is an expected state, not a failure.
  `error` is returned only for genuine infrastructure or decode failures.
  `Store.Allow`, `Store.CurrentRound`, and `Store.PlayerCount` all remain —
  other call sites use them and this method is additive.

**Checkpoint 1: `WagerPreflight` returns all three values in one round trip**

- [ ] **Step 1: Write the failing test, then run it**

Spec, as one test with three assertions plus a round-trip count:

*Values.* Seed a room with host `host-1` and three wallet fields
(`host-1`, `player-1`, `player-2`), and a current-round index pointing at
`round-abc`. Call `WagerPreflight(ctx, "wager", "player-1", roomID, 20, 10*time.Second)`.
Assert `p.Decision.Allowed == true`, `p.Decision.Remaining == 19`,
`p.RoundID == "round-abc"`, and `p.Players == 2` (three wallets minus the
host) — the same values `Allow`, `CurrentRound`, and `PlayerCount` return
individually.

*Missing round.* Against a room with no current-round key, assert
`p.RoundID == ""` and `err == nil`.

*Round-trip count.* Install a counting `redis.Hook` on the store's client
(the test lives in package `redisstore`, so `s.client.AddHook` is reachable)
whose `ProcessPipelineHook` increments a counter once per pipeline execution
and whose `ProcessHook` increments it once per standalone command. Assert the
total is exactly **1** across one `WagerPreflight` call.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/redisstore/ -run TestWagerPreflight -count=1 -race
```
Expected: FAIL — compile error, `s.WagerPreflight undefined`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `WagerPreflight` opens one `s.client.Pipeline()`, queues the
rate-limit script with **`rateLimitScript.EvalSha(ctx, pipe, []string{RateLimitKey(scope, userID)}, ...)`**
and the same three arguments `Allow` passes, then `pipe.Get(ctx, RoomRoundKey(roomID))`
and `pipe.HLen(ctx, RoomWalletsKey(roomID))`, then `pipe.Exec(ctx)` once.

**Use `EvalSha`, never `Script.Run`, inside the pipeline — verified during
this planning pass, see the trap below.**

`Exec` returns a non-nil error when any queued command errored, including the
expected `redis.Nil` from the `Get` on a room with no round — so the error
from `Exec` is not itself decisive. Read each command's own result: treat
`redis.Nil` from the `Get` as `RoundID == ""`, and return the error only when
a leg failed for any other reason.

Extract the existing reply-decoding block from `Allow` into an unexported
`decodeRateLimitReply(res interface{}, scope, id string) (Decision, error)`
and call it from both `Allow` and `WagerPreflight`. **Do not copy the
decoding** — one limiter, one decoder (`CLAUDE.md`). The Lua script is
unchanged; this is the same `rate_limit.lua` invoked through a pipeline.

Player count is `HLen - 1`, floored at 0, matching `Store.PlayerCount`
exactly.

Then rewire `wager.Service.Place` to call `WagerPreflight` once in place of
`store.Allow`, `store.CurrentRound`, and `store.PlayerCount`, **keeping the
existing check order unchanged**: rate-limit denial first (`&RateLimitError{}`),
then idempotency parse, then stake sign, then `ErrNoActiveRound` when
`req.RoundID` is empty and `pre.RoundID` is empty. Fetching all three values
earlier does not change which error wins — only the checks' order does, and
that order is preserved. `pre.Players` feeds `Accepted.Players`, replacing
the post-write `PlayerCount` call.

`internal/wager`'s existing suite is the regression net for the rewiring;
this is a pure refactor at that layer with no new observable behavior, so it
gets no new test of its own.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/redisstore/ ./internal/wager/ ./internal/httpapi/ ./internal/ws/ -count=1 -race && \
  git add internal/redisstore/ratelimit.go internal/redisstore/wager.go internal/redisstore/wager_test.go internal/wager/service.go && \
  git commit -m "perf: fetch the wager preflight in one pipelined round trip"
```

Expected: PASS, then one commit.

### The `Script.Run`-in-a-pipeline trap, verified during this planning pass

`redis.Script.Run` runs `EvalSha` and then inspects `r.Err()` for a
`NOSCRIPT` prefix to decide whether to fall back to a full `Eval`. **Inside a
pipeline nothing has executed at queue time**, so `r.Err()` is nil, the
fallback never fires, and the queued `EVALSHA` fails at `Exec` time against a
Redis that has not cached the script.

This is a silent trap rather than a compile error: `redis.Pipeliner` *does*
satisfy `redis.Scripter`, so `rateLimitScript.Run(ctx, pipe, ...)` type-checks
and reads correctly. Probed directly against go-redis v9.18.0 and a live
Redis:

```
Pipeliner satisfies Scripter : yes (compiles)
after SCRIPT FLUSH:
  Exec err                   : NOSCRIPT No matching script. Please use EVAL.
  script cmd err             : NOSCRIPT No matching script. Please use EVAL.
after script.Load + EvalSha:
  value                      : [OK world]   (err <nil>)
```

Worse than a plain failure: in ordinary operation the first non-pipelined
`Allow` caches the script, so the pipelined path works — and then breaks after
a Redis restart or a `SCRIPT FLUSH`, intermittently, in production only. The
same probe confirmed the two behaviors Checkpoint 1's contract relies on:
`Exec` returns non-nil when any leg errors, and the `Get` leg's own `.Err()`
is distinguishably `redis.Nil`.

**Checkpoint 2: `WagerPreflight` survives a script-cache flush**

- [ ] **Step 1: Write the failing test, then run it**

Spec: seed the same fixture as Checkpoint 1. Call `WagerPreflight` once and
assert it succeeds (this caches the script). Then issue `SCRIPT FLUSH` on the
test client, and call `WagerPreflight` again with the same arguments.

Assert the second call returns a nil error, `p.Decision.Allowed == true`,
`p.RoundID == "round-abc"`, and `p.Players == 2` — identical to the first.

Note for the executor: `SCRIPT FLUSH` is instance-wide, not per-database.
Issue it through the same test client the suite already uses against DB 15,
and be aware it also evicts `place_wager.lua`'s cache entry — which is
harmless, since every other call site goes through `Script.Run`, whose
fallback works fine outside a pipeline.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/redisstore/ -run TestWagerPreflightSurvivesScriptFlush -count=1 -race
```
Expected: FAIL with `NOSCRIPT No matching script. Please use EVAL.` on the
second call.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: two layers, both needed.

1. In `Store.New`, after the successful `Ping`, call
   `rateLimitScript.Load(ctx, client)` so a freshly constructed `Store` never
   depends on some other call site having warmed the cache. A load failure is
   fatal the same way the `Ping` failure is — fail fast, matching the
   package's existing posture.
2. In `WagerPreflight`, when `Exec` returns an error and the script leg's own
   `.Err()` has the `NOSCRIPT` prefix (use `redis.HasErrorPrefix(err, "NOSCRIPT")`,
   not a string compare), call `rateLimitScript.Load(ctx, s.client)` and
   re-run the whole pipeline **exactly once**. A second NOSCRIPT is returned
   as an error rather than retried — one retry closes the restart/flush race,
   a loop would hide a genuine fault.

Layer 1 alone is insufficient: Redis can restart or be flushed at any time
after construction. Layer 2 alone would work but pays a guaranteed extra
round trip on the very first wager after every process start, which is the
opposite of this task's purpose.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/redisstore/ ./internal/wager/ -count=1 -race && \
  git add internal/redisstore/client.go internal/redisstore/wager.go internal/redisstore/wager_test.go && \
  git commit -m "fix: reload the rate-limit script when a pipelined EVALSHA misses"
```

Expected: PASS, then one commit.

**Task boundary verification**

- [ ] Run the full suite once:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make test
```
Expected: PASS. Pay attention to `internal/redisstore`'s concurrency suite —
it is the standing zero-double-spend proof and this task changed the path
that reaches the write.

---

## Task 5: Re-measure the tuned wager path

**Gate, not a checkpoint.**

**Files:**
- Modify: `loadtest/README.md`

- [ ] **Gate 1: re-run Task 1's procedure verbatim against the tuned build**

Same knobs, same optimized-binary target, a freshly started process:

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  JWT_SECRET=$(openssl rand -hex 32) make loadtest-api
WAGER_TEST_DURATION_S=240 WAGER_PLAYERS=25 k6 run loadtest/wager_latency.js
curl -s http://127.0.0.1:9090/
```

**Acceptance:** `callit_wager_place_ok_count` ≥ 5000, and
`callit_wager_place_err_count` no higher than Task 1's — a tuning pass that
traded latency for rejections has not tuned anything.

Record the same field list Task 1 recorded, under a "Phase 7b post-tuning"
heading, beside it.

- [ ] **Gate 2: state the verdict against the bucket-resolution rule**

Apply the rule from Global Constraints without softening it:

- `callit_wager_place_ok_p99_ms` ≤ **10** → target **MET**.
- Exactly **15** → **INCONCLUSIVE-AT-BOUNDARY**: the instrument proves
  "≤ 15 ms" and the target is "< 15 ms". Report it as inconclusive and say
  what finer bucket bounds would be needed to settle it. Do not report MET.
- **20 or higher** → still **MISSED**. Record the p50 alongside it: a p50 in
  the smallest buckets with a p99 several buckets up is a tail problem
  (contention, GC, scheduling), not a per-request cost, and that distinction
  is the finding — it is what Task 6's ceiling analysis and any future
  tuning would act on.

In every branch, record the **before/after delta** from Task 1's numbers and
the round-trip reduction (5 → 2) that produced it. A reduction that moved the
p99 by less than one bucket is itself a result worth stating plainly: it
would mean Redis round trips were never the dominant cost.

```bash
git add loadtest/README.md && \
  git commit -m "test: record the post-tuning wager-path measurement"
```

Expected: one commit.

---

## Task 6: Re-baseline throughput and account for the gap

**Gate, not a checkpoint.**

7a measured **3,174 req/s** against the 5,000 target, on `GET /healthz` —
the cheapest route in the system — under `go run`, on a 4-core WSL2 VM with
Redis, PostgreSQL, and Kafka co-resident. A ceiling below target on the
cheapest route is not a business-logic cost, so this gate's job is to
separate what the server controls from what the environment imposes, and to
report whichever it turns out to be.

**Files:**
- Modify: `loadtest/README.md`

- [ ] **Gate 1: re-run the REST scenario on the optimized binary**

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  JWT_SECRET=$(openssl rand -hex 32) make loadtest-api
k6 run loadtest/rest_throughput.js
```

Record `http_reqs` rate, `http_req_failed`, `dropped_iterations`, and k6's
`http_req_duration` p95/p99. Compare against 7a's 3,174 req/s, 0 failed, 6
dropped.

- [ ] **Gate 2: establish whether the server or the host is the binding constraint**

Run the scenario once more with the host quiesced as far as is possible here
— nothing else running that this session controls — and capture, during the
run:

```bash
nproc
uptime            # load average during the run
docker stats --no-stream
```

The question to answer is narrow and answerable: **is `cmd/api` CPU-saturated
at the ceiling, or is the host?** If the API process is pinned at roughly one
core's worth while three cores sit idle, that points at the server. If total
host CPU is saturated across all four cores with the database stack taking a
large share, the environment is the constraint and 5,000 rps is not
measurable here at all.

**Do not tune speculatively in this task.** No pool sizes, no `GOMAXPROCS`,
no timeouts. If Gate 2 identifies a specific, evidenced server-side limit,
record it as a named finding for Phase 7c or 8 — the honest deliverable of
this gate is the attribution, not a knob turned on a hunch.

- [ ] **Gate 3: write the throughput finding**

State one of:
- **MET** — the optimized binary reaches 5,000+ req/s. Record the number.
- **MISSED, environment-bound** — with the evidence from Gate 2: core count,
  load average, the API process's own CPU share, and the resulting statement
  that this environment's ceiling is N req/s. Name what would be needed to
  measure the target fairly (a host that is not sharing four cores with three
  databases).
- **MISSED, server-bound** — with the specific evidenced limit, named and
  handed to a later phase.

```bash
git add loadtest/README.md && \
  git commit -m "test: re-baseline throughput and attribute the gap"
```

Expected: one commit.

---

## Task 7: Prove reconciliation after a real load run

**Gate, with one committed test.** Closes the parent plan §12 checkbox
"Redis↔PostgreSQL reconciliation test passes after a load run."

`internal/ledger/reconcile_test.go` already proves the identity
`redis_wallet(user, room) − opening_stake(user, room) == ledger_balance(user, room)`
over fixtures it builds itself, including a concurrent case. What has never
been proven is that it holds over state produced by **real load** — thousands
of wagers driven through the socket by k6, relayed to Kafka, and consumed
into PostgreSQL.

**Files:**
- Create: `backend/internal/ledger/reconcile_after_load_test.go`
- Modify: `loadtest/README.md`

**Interfaces:**
- Consumes: the existing `internal/ledger` reconciliation helpers and `Repo`;
  `redisstore.Store`.
- Produces: `TestReconcileAfterLoad`, which runs only when the environment
  variable `RECONCILE_ROOM_IDS` is set to a comma-separated list of room IDs.

- [ ] **Gate 1: the load run emits the room IDs it created**

The scenario already creates rooms through `loadtest/lib/setup.js`. Have
`loadtest/wager_latency.js` print each room ID it created once, on a single
line prefixed `RECONCILE_ROOM_ID=`, so the gate below can collect them. A
room ID is not per-user data — this prints no player identity, no stake, and
no wager.

- [ ] **Gate 2: write `TestReconcileAfterLoad`**

Spec: for each room ID in `RECONCILE_ROOM_IDS`, read every wallet field in
`room:{roomID}:wallets` from live Redis and, for each user in it, assert

```
redis_wallet(user, room) − opening_stake(user, room) == ledger_balance(user, room)
```

using the same identity and the same helpers the existing reconciliation
tests use. Assert separately that the room's ledger transactions each balance
(`Σ debits == Σ credits`) and that the room's wager count in PostgreSQL
matches the count of `wager_placed` events its wallets imply.

**Skip semantics, stated deliberately because they brush against a documented
invariant:** `CLAUDE.md` requires that `internal/ledger`'s integration tests
*fail rather than skip* when their dependencies are unreachable. This test
keeps that rule where it applies — if `RECONCILE_ROOM_IDS` is set and Redis,
PostgreSQL, or Kafka is unreachable, it **fails**. It skips only when
`RECONCILE_ROOM_IDS` is unset, which means "no load run has happened in this
environment," not "a dependency is down." That keeps `make test` and CI green
without a load run while making the check unskippable once one exists.

- [ ] **Gate 3: run it against real load**

Full stack, both binaries, one load run, then the check:

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  make up-full && make migrate && \
  JWT_SECRET=$(openssl rand -hex 32) make loadtest-api
make relay &          # cmd/relay — Redis Stream → Kafka
make ledger-worker &  # cmd/ledger-worker — Kafka → PostgreSQL
WAGER_TEST_DURATION_S=240 WAGER_PLAYERS=25 k6 run loadtest/wager_latency.js
# let the relay and worker drain, then:
cd backend && RECONCILE_ROOM_IDS=<ids printed by the run> \
  go test ./internal/ledger/ -run TestReconcileAfterLoad -count=1 -race -v
```

If `make relay` does not exist as a target, run `cd backend && go run ./cmd/relay`
directly; adding the target is optional and belongs to this gate's commit if
done.

**Acceptance:** the test passes over at least one room that received ≥ 1,000
wagers during the run. A pass over a room with a handful of wagers does not
close §12's box — the point of this check is load.

**If it fails, stop and report.** A reconciliation failure after load is a
money-correctness defect and outranks every remaining task in this phase. Do
not proceed to Task 8 with it open.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ledger/ -count=1 -race && \
  git add internal/ledger/reconcile_after_load_test.go ../loadtest/wager_latency.js ../loadtest/README.md && \
  git commit -m "test: prove Redis-PostgreSQL reconciliation after a real load run"
```

Expected: PASS, then one commit.

---

## Task 8: Revised baseline report, parent-plan amendments, and wrap-up

**Gate, not a checkpoint.**

**Files:**
- Create: `docs/reports/2026-08-31-phase-7b-baseline.md`
- Modify: `docs/plans/2026-08-21-implementation-plan.md`, `docs/project-history.md`, `CLAUDE.md`

- [ ] **Gate 1: write the revised baseline report**

Mirror 7a's report structure so the two can be read side by side. It must
carry:
- The four spec §7 targets with revised verdicts, drawn from the **server-side**
  histograms, with k6's client-side figures marked secondary — the same
  primary/secondary rule 7a's report established under the WSL2 fidelity risk.
- A before/after table: Task 1's pre-tuning numbers against Task 5's, with the
  round-trip reduction (5 → 2) named as the change that produced the delta.
- The throughput finding from Task 6, verbatim, including its attribution.
- The reconciliation result from Task 7, with the wager count it ran over.
- A restatement of the bucket-resolution rule and, if the p99 landed at 15,
  the INCONCLUSIVE-AT-BOUNDARY verdict spelled out rather than rounded away.
- The same "no per-user, per-room, or per-round data" section 7a's report
  carries, and the same honesty about it.

- [ ] **Gate 2: amend the parent plan and the project history**

In `docs/plans/2026-08-21-implementation-plan.md`:
- Check §12's "Redis↔PostgreSQL reconciliation test passes after a load run"
  box, pointing at `TestReconcileAfterLoad` and this phase's report as the
  evidence.
- Mark §9's row 7b complete (✅).

In `docs/project-history.md`, add a `### Phase 7b` section recording: the
`place_wager.lua` non-positive-stake hole and that the balance pre-check was
the only thing closing it before Task 2; the 5 → 2 round-trip reduction; the
measured before/after; the throughput attribution; and the post-load
reconciliation result.

In `CLAUDE.md`, add one Critical Invariant — that `place_wager.lua` rejects a
non-positive stake itself, and that no caller-side check may be treated as
the guard for it, since the amount's sign reaching `HINCRBY` unchecked mints
tokens — and document `make loadtest-api` in the Build & Test section beside
`make loadtest`.

- [ ] **Gate 3: run the security review**

`CLAUDE.md` requires it before closing any phase that touches money movement.
This phase changed the wager write path and the script that guards it.

Run the `security-reviewer` agent against the diff between `dev` and this
branch. Record its findings in `docs/project-history.md` under the Phase 7b
section, fixing anything CRITICAL or HIGH before the phase closes and stating
explicitly which items were deferred and why.

- [ ] **Gate 4: full suite green, then commit**

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make test && make lint && \
  git add docs/reports/2026-08-31-phase-7b-baseline.md docs/plans/2026-08-21-implementation-plan.md docs/project-history.md CLAUDE.md && \
  git commit -m "docs: record the Phase 7b baseline and close the reconciliation box"
```

Expected: PASS, then one commit. **The branch is now green and verified.**
Merging is not this plan's to do — `executing-plans` Step 3 hands off to
`finishing-a-development-branch`, which owns that decision.

---

## Self-Review

**1. Spec coverage.** Spec §7 has four targets. p99 bet placement — Tasks 1,
3, 4, 5. WebSocket sync latency — already MET in 7a; Tasks 1 and 5 re-record
it to confirm the tuning did not regress it. Throughput — Task 6.
Double-spend 0.00% — Task 4's boundary verification re-runs the concurrency
suite (the standing proof) over a changed write path, and Task 7 proves the
ledger identity after load. Parent plan §9 row 7b's three named deliverables:
tuning (Tasks 2–5), throughput (Task 6), §12 reconciliation re-run (Task 7).
The three security items and the README/diagram moved to 7c by this planning
pass's split — recorded in the parent plan.

**2. Placeholder scan.** No "TBD", no "add appropriate error handling", no
"similar to Task N". Each checkpoint names exact inputs and exact expected
errors or values. The gates name exact commands and exact acceptance
thresholds, and each of Tasks 5 and 6 enumerates every branch its verdict can
take rather than leaving the outcome open.

**3. Type consistency.** `ValidateStakeAmount(amount Tokens) error` is used
under that name in Task 3 Checkpoints 1 and 2. `WagerPreflight(ctx, scope,
userID, roomID string, limit int, window time.Duration) (Preflight, error)`
and `Preflight{Decision, RoundID, Players}` are used under those names in
Task 4's Interfaces and its Step 2 contract. `INVALID_STAKE` →
`domain.ErrInvalidStake` is defined in Task 2 and consumed in Task 3.
`make loadtest-api` is produced in Task 1 and invoked by Tasks 5, 6, and 7.
`RECONCILE_ROOM_IDS` is named identically in Task 7's Gates 2 and 3.
Task 4 uses `rateLimitScript.EvalSha` inside the pipeline in both of its
checkpoints, never `Script.Run` — the trap section explains why, and the
`Store.New` preload it adds is referenced only there.

**4. Delegation.** Not used — this plan runs fully inline, so the header
carries no `**Delegation:**` line. Every task here is either the phase's
flagship correctness work (Task 2's money guard, Task 3's error-precedence
changes, Task 4's write-path refactor, Task 7's reconciliation) or a
measurement gate whose value is the judgment call in reading the numbers.
Neither is what `delegating-plan-tasks` is for.

**One risk this plan accepts explicitly.** Task 4 changes the reply-path
plumbing of the rate limiter, which every authenticated route depends on. The
mitigation is that `rate_limit.lua` itself is untouched and the decoder is
extracted rather than duplicated, so a regression would surface in
`internal/redisstore`'s and `internal/httpapi`'s existing rate-limit suites,
both of which Task 4's boundary verification runs.
