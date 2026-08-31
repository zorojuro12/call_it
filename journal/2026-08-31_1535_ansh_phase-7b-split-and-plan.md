# 2026-08-31 — ansh — Phase 7b split into 7b/7c, and the tuning + reconciliation plan

**Status:** Phase 7b planned and committed to `dev` (`5cfbeb4`) —
`docs/plans/2026-08-31-phase-7b-tuning-reconciliation.md`, 8 tasks (6
RED→GREEN checkpoints, 4 verification gates, 11 commits). The parent plan's
§9 row 7b was split into 7b/7c *before* the task breakdown was written, per
the phase-sizing rule. Nothing implemented yet — this session was planning
only.
**Decided:** Split at the same evidence boundary the 7a/7b seam used: **7b**
acts on 7a's two MISSED numbers (wager p99, throughput) and closes §12's
post-load reconciliation box; **7c** takes the three deferred security items
(login timing, reconnect grace window, `RoundSettled.Payouts` cap) plus the
README/architecture diagram. Also fixed the throughput posture: close what's
closeable on an optimized binary, then document this environment's ceiling
with evidence rather than chasing 5,000 rps on a 4-core WSL2 VM that also
hosts Redis, PostgreSQL, and Kafka.
**Spec:** No change — 7b measures and tunes against spec §7, it doesn't
change it.
**Next:** Execute the 7b plan — `git checkout -b phase-7b-tuning-reconciliation dev`,
then `executing-plans` task by task. Task 2 must land before Task 3 (see
below); the plan says so in its own text.
**Blocked on:** Nothing.
**Touches:** `docs/plans/2026-08-31-phase-7b-tuning-reconciliation.md`,
`docs/plans/2026-08-21-implementation-plan.md`.

---

## What We Worked On

Resumed cold: my first `git status` was a stale snapshot showing `dev` three
commits ahead of origin, and I reported Phase 7a as unstarted. The user
corrected it — 7a had been executed, journaled, merged, and pushed by a
concurrent session. A `git fetch` put `dev` at `ce815d2`, in sync with
origin. Worth remembering as a pattern: this repo has had two sessions active
at once more than once now (the WSL2 session hit the same thing on
2026-08-30), so a status check at session start can be stale by the time it's
read.

From there: read the 7a baseline, the parent plan's 7b row, the open-by-design
security list, and spec §7, then split the phase and wrote the plan.

## Decisions Made

- **Split 7b → 7b/7c at the evidence boundary.** Reasoning is written into
  the parent plan alongside the 5a/5b, 6a/6b, and 7a/7b split notes rather
  than restated here. The short version: 7a's own split rationale already
  argues the deferred security items "change the very paths under
  measurement," and that argument doesn't expire when 7a ships — the
  reconnect grace window changes socket session lifecycle and the
  login-timing fix adds an argon2id verify to the auth miss path, both on
  routes 7b needs a clean before/after number for.
- **The §12 reconciliation re-run goes in 7b, not 7c** — it needs a real k6
  load run to have happened, which is exactly what 7b's re-baseline produces.
  One load run serves both.
- **Throughput gets an attribution, not a number to hit.** The honest
  deliverable is either "5,000 rps MET on an optimized binary" or "N rps is
  this environment's ceiling, and here is the evidence it's host contention
  rather than server cost." Task 6 forbids speculative tuning — no pool
  sizes, no `GOMAXPROCS`, no timeouts on a hunch.
- **Kept the plan fully inline, no `delegating-plan-tasks`.** Every task here
  is either flagship money-path correctness or a measurement gate whose value
  is the judgment in reading the numbers. Neither is what delegation is for.

## What Worked

- **Verifying the plan's two load-bearing claims empirically instead of
  asserting them.** Both turned out to be true *and* to have details that
  would have derailed a cold executor. This cost maybe ten minutes and
  changed two tasks.
- **`place_wager.lua` has no stake-sign guard — confirmed against live
  Redis.** Seeded `player-1` with 1000, invoked the real script with
  `amount = -100`:
  ```
  reply : OK, 1100, 1, -100, -100
  wallet: 1000 → 1100     (credited, not debited)
  pool 0: -100  /  total: -100
  outbox: 1 entry written
  ```
  It returns `OK`, mints 100 tokens, drives the pool and total negative, and
  **emits an outbox event** that `cmd/relay` would carry to Kafka and
  `cmd/ledger-worker` would write into the double-entry ledger as a real
  money row.
- **That hole is latent, not live** — verified by grep: the only caller chain
  is `ws.Router` → `wager.Service.Place` → `Store.PlaceWager`, and `Place`'s
  balance pre-check runs `domain.ValidateStake`, whose `amount <= 0` arm
  rejects it first. The catch is that the pre-check is precisely what 7b
  deletes for performance, so the plan orders Task 2 (guard in the script)
  strictly before Task 3 (delete the check) and says why.

## What Didn't Work

- **`redis.Script.Run(ctx, pipe, ...)` inside a go-redis pipeline — do not
  write this.** `Run` executes `EvalSha` and then inspects `r.Err()` for a
  `NOSCRIPT` prefix to decide whether to fall back to a full `Eval`. Inside a
  pipeline nothing has executed at queue time, so `r.Err()` is nil, the
  fallback never fires, and the queued `EVALSHA` fails at `Exec`.

  It's a silent trap, not a compile error: `redis.Pipeliner` *does* satisfy
  `redis.Scripter`, so the wrong form type-checks and reads correctly. Probed
  against go-redis v9.18.0 and live Redis:
  ```
  Pipeliner satisfies Scripter : yes (compiles)
  after SCRIPT FLUSH:
    Exec err        : NOSCRIPT No matching script. Please use EVAL.
    script cmd err  : NOSCRIPT No matching script. Please use EVAL.
  after script.Load + EvalSha:
    value           : [OK world]   (err <nil>)
  ```
  Worse than a plain failure: in ordinary operation the first non-pipelined
  `Allow` caches the script, so the pipelined path works — and then breaks
  after a Redis restart or `SCRIPT FLUSH`, intermittently, in production
  only. It would have passed the suite.

  The plan's Task 4 uses `EvalSha` with a preload in `Store.New` plus a
  single reload-and-retry on `NOSCRIPT`, and carries its own checkpoint that
  flushes the script cache and calls again.
- **`make up` first, then `cd backend` in a later Bash call** — the shell's
  working directory persists across tool calls in this harness, so a second
  `cd backend` fails with "No such file or directory". Cost two wasted calls.
  Use absolute paths, or re-`cd` from the repo root each time.

## Test Coverage

No test files were touched — this was a planning session. No coverage impact.
The plan itself specifies coverage expectations per task: `internal/domain`
stays at its 100% floor through Task 3 Checkpoint 1, and Task 4's boundary
verification re-runs `internal/redisstore`'s concurrency suite because it is
the standing zero-double-spend proof and the task changes the path that
reaches the write.

## Open Questions / Blockers

- **7a's 50 ms p99 came from 150 samples** — that's the second-worst sample,
  movable a whole bucket by one GC pause. Whether the wager path has a real
  systemic cost or 7a measured two outliers is genuinely unknown right now;
  Task 1 exists to answer it before anything is tuned. If Task 5 shows the
  5→2 round-trip reduction moved the p99 by less than one bucket, that's the
  finding — it would mean Redis round trips were never the dominant cost.
- **Bucket resolution bounds every verdict.** `Histogram.Quantile` returns
  bucket *upper bounds*, so a p99 rendered as `15` proves "≤ 15 ms", not
  "< 15 ms". The plan makes MET require a rendered `10` or lower and reports
  a `15` as INCONCLUSIVE-AT-BOUNDARY. If the tuned number lands there, 7b
  closes without a clean verdict on its headline target, and finer bucket
  bounds become a 7c or Phase 8 question.
- The full gameplay loop still has not been exercised in a browser end to end
  (host + guest, two windows) — carried over from the 2026-08-31 0221 entry,
  untouched by 7a or this session.
- Redis and PostgreSQL were left running from this session's verification
  (`make up`). `make down` if that's unwanted.

## Relevant Commits

- `5cfbeb4` — docs: split Phase 7b into 7b/7c and plan the tuning +
  reconciliation pass (`docs/plans/2026-08-31-phase-7b-tuning-reconciliation.md`,
  `docs/plans/2026-08-21-implementation-plan.md`)

## Next Step

Execute the plan: `git checkout -b phase-7b-tuning-reconciliation dev`, then
`executing-plans` task by task. The plan is written to be executed cold in a
separate session if the Opus-plans/Sonnet-executes split is wanted — it
carries its own Global Constraints and both empirical findings, so no
conversation history is needed.
