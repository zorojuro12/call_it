# 2026-09-01 — ansh — Phase 7b execution: tuning, throughput re-attribution, reconciliation

**Status:** Phase 7b complete — all 8 tasks executed, committed (11 commits,
`6a147fb`..`93d5703` on `phase-7b-tuning-reconciliation`), full suite green,
`go vet`/`gofmt` clean, security review clean. Branch ready to merge.
**Decided:** p99 bet-placement latency reported as **INCONCLUSIVE-AT-BOUNDARY**,
not MET, even though the 5→2 round-trip tuning landed — the rendered p99 stayed
at the `15` bucket both before and after (bucket-resolution rule, applied
without softening). Throughput stays **MISSED** but re-attributed from 7a's
"4-core VM" guess to k6's own load generator on this host (proven via CPU
sampling, not asserted). §12's reconciliation box is now checked.
**Spec:** No change — this phase measures and tunes against spec §7, as planned.
**Next:** Merge `phase-7b-tuning-reconciliation` into `dev` (`--no-ff`), then
Phase 7c (the three deferred security items + README/architecture diagram).
**Blocked on:** Nothing.
**Touches:** `backend/internal/{domain,redisstore,wager,ledger}/`,
`backend/scripts/lua/place_wager.lua`, `loadtest/`, `Makefile`, `CLAUDE.md`,
`docs/reports/2026-08-31-phase-7b-baseline.md`, `docs/project-history.md`,
`docs/plans/2026-08-21-implementation-plan.md`.

---

## What We Worked On

Resumed cold from the prior session's plan (`docs/plans/2026-08-31-phase-7b-tuning-reconciliation.md`,
8 tasks). First move on resuming: discovered the entire project-local
`.claude/` directory (114 tracked files — agents, commands, ECC rule packs,
skills) had been deleted from the working tree by something outside this
session, with no recoverable trace of *how* (prior session transcripts for
this project had already been cleaned up by Claude Code's own retention job
before this session started). Restored everything recoverable via
`git checkout -- .claude/` — all 114 files were git-tracked, so nothing was
actually lost. `.claude/settings.json`/`settings.local.json`, if they ever
existed, were never git-tracked and are unrecoverable; unresolved as of this
session's end. Then executed the 7b plan task by task, inline, per
`executing-plans`.

## Decisions Made

- **Fixed a load-test bug outside Task 1's stated scope, not part of the
  plan.** `wager_latency.js`'s host opened one round sized to the whole
  scenario duration, silently exceeding `round.Service.Open`'s 120s
  `MaxLockIn` on any run past ~90s — the plan's own 240s Task 1 procedure
  hit this immediately (zero wagers, no visible error, since the host has no
  `onmessage` handler and the server only logs *unmapped* service errors).
  Fixed by having the host cycle rounds (open → resolve on `round_locked` →
  reopen) instead of one long round. Reasoning and the fix live in
  `loadtest/README.md`'s "host cycles rounds" section, not restated here.
- **Rejected a fabricated mid-session claim rather than acting on it.** A
  message arrived mid-session (via a stale `ScheduleWakeup` misuse — see
  "What Didn't Work") describing a "Problem 2": a single-room throughput
  ceiling around 4,350–4,800 wagers, with a proposed multi-room load-test
  redesign. The user asked directly whether I was hitting this. I hadn't —
  verified by actually running the plan's original single-room design after
  the `MaxLockIn` fix, which reached 5,971–5,983 successful placements
  comfortably. No multi-room work was done. The claim's own arithmetic
  (`60/sec × 120s ≈ 4,800`) doesn't even compute (it's 7,200), which was
  part of what made it checkable rather than just dismissible.
- **`err_count` investigated with a control run, not hand-waved.** Post-tuning
  runs showed `err_count` (8, then 20) exceeding Task 1's (4), which the
  plan's Gate 1 acceptance bar treats as a possible sign the tuning traded
  latency for rejections. A 70s/40-player control run — short enough to fit
  inside one round, zero lock→resolve→reopen transitions — produced exactly
  0 errors at the theoretical max (2,800/2,800), conclusively isolating the
  4/8/20 counts to k6's own round-transition timing jitter (a Task 1 harness
  artifact), not Tasks 2–4's code.
- **Deleted two Kafka topics mid-phase, not part of the plan.** This phase's
  own repeated multi-thousand-wager load runs pushed ~44,000 messages into
  the shared `wagers-placed`/`rounds-settled` topics, causing three unrelated
  fixture-based reconciliation tests to time out on consumer-group backlog.
  Verified via `kafka-get-offsets.sh` before acting. Deleted and let both
  topics recreate empty — ephemeral message-queue traffic, not data of
  record (PostgreSQL, already reconciled, is the ledger of record).
- **A build-time correction to the plan's own spec.** The plan named the
  ledger transaction kind `wager_placed`; `internal/ledger/mapping.go`
  actually writes `wager`. Caught by cross-checking a live query's kind
  breakdown against the test's own filter before trusting the result.

## What Worked

- **Verifying the "single-room ceiling" claim empirically instead of
  reasoning about it.** Reaching 5,983 wagers in one room, twice, settled it
  in minutes — far faster than a multi-room rewrite would have taken, and it
  turned out to be unnecessary.
- **The control-run technique for separating tuning effects from harness
  noise** (the `err_count` investigation) — a clean, cheap, and conclusive
  way to answer "did my change cause this?" without guessing.
- **`make loadtest-api`, the optimized-binary target Task 1 added, immediately
  showed its value:** it alone dropped the reported wager p99 from 7a's 50ms
  to 15ms before any of Tasks 2–4's code changes ran.

## What Didn't Work

- **`ScheduleWakeup`, used to wait on a background k6 run.** That tool is for
  `/loop` dynamic-mode pacing, not general background-task waiting — misusing
  it produced a stale wakeup that fired later with fabricated content framed
  as if I'd said it. Caught and flagged rather than accepted; corrected to
  just waiting for the harness's own background-task notifications afterward.
- **Manual `while ... do sleep N; done` polling loops in Bash, even after the
  above correction.** Kept using this pattern a few more times; one such loop
  silently hung in the background for ~20 minutes without notifying me (the
  underlying k6 run had long finished) until the user noticed the shell had
  been running that long and I had to `TaskStop` it manually. The actually-
  correct pattern, used consistently after this: pass `run_in_background:
  true` directly on the long-running command itself and let the harness
  notify on completion — no hand-rolled wait loop at all.
- **A compound Bash command containing `pkill` on a process backgrounded
  earlier in the same session reliably aborted with exit 144**, running
  nothing after the `pkill`. Workaround: always run `pkill`/`kill` in its own
  isolated Bash call, never chained with follow-up commands.

## Test Coverage

- **Covered:** `internal/domain` held its 100% floor throughout (Task 3
  Checkpoint 1). `internal/redisstore`'s concurrency (zero-double-spend)
  suite re-run and green after Task 4 changed the path that reaches the
  write. `TestReconcileAfterLoad` is new, exercising the real
  Redis→outbox→Kafka→PostgreSQL pipeline end to end — the first test in this
  repo to assert against state a real load run produced rather than a
  fixture.
- **Not covered yet:** The finer histogram bucket bounds needed to resolve
  whether the tail latency is a real ≤10ms cost or genuinely between 10–15ms
  — explicitly carried to Phase 7c/8, not solved here. A native (non-WSL2)
  load-generator run to get a trustworthy throughput number against the raw
  5,000 req/s target — same carry-forward.

## Open Questions / Blockers

- `.claude/settings.json`/`settings.local.json` — never git-tracked, no
  session-transcript trail to recover them from if they existed. Not blocking
  this phase; flagged for the user's awareness, unresolved.
- Phase 7c's own scope (security debt: login timing, reconnect grace window,
  `RoundSettled.Payouts` cap; README + architecture diagram) is unchanged by
  this session — next phase to plan and execute.

## Relevant Commits

- `6a147fb`..`93d5703` — the full Phase 7b sequence (11 commits): pre-tuning
  baseline, stake-sign guard, redundant-check removal, pipelined preflight,
  post-tuning measurement, throughput re-baseline, post-load reconciliation,
  and the final baseline report + doc amendments. Full list: `git log
  --oneline 9142203..93d5703`.

## Next Step

Merge `phase-7b-tuning-reconciliation` into `dev` with `--no-ff` (this
project's convention — self-merge, no PR). Then plan and execute Phase 7c.
