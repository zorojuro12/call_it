# 2026-08-24 — ansh — Phase 2 Redis layer plan

**Status:** Phase 1 merged into `dev` (`c0bd875`). Phase 2's plan is written, self-reviewed, and committed (`ab31048`) — 1,591 lines, 9 tasks, 25 checkpoints + 4 verification suites. No Phase 2 code exists yet.
**Decided:** Settlement math stays in Go — `domain.Settle` computes, `settle_round.lua` only applies. Amends parent plan §5; full reasoning in the new plan's Amendment A1.
**Spec:** No change yet — Phase 2's Task 9 Checkpoint 2 adds the bettors-counter mechanism to spec §4 at close-out.
**Next:** Hand off to the Sonnet window: invoke `executing-plans` against `docs/plans/2026-08-24-phase-2-redis-layer.md`. It creates the branch itself.
**Blocked on:** Nothing.
**Touches:** `docs/plans/2026-08-24-phase-2-redis-layer.md`, `docs/plans/2026-08-21-implementation-plan.md` (§4, §5, §9 — amended at close-out), `backend/internal/redisstore/` (to be created), `backend/scripts/lua/` (to be created)

---

## What We Worked On

Planning Phase 2, the Redis layer: key schema, the Lua scripts, Go wrappers,
and the concurrency suite that has to demonstrate zero double-spend and exact
token conservation.

This is also **the first plan written under the spec-driven `writing-plans`
format** (`ab190b9`), so its length was a live question. It landed at 1,591
lines against Phase 1's 3,111 — roughly half, for a phase with more moving
parts. The format change works; no precision was lost that self-review could
find.

## Decisions Made

Five amendments, all recorded in full at the top of the new plan and folded
into the committed docs by its Task 9. One line each here — the reasoning
lives in the plan:

- **A1 — Go computes settlement, Lua applies it.** Amends parent plan §5.
- **A2 — New key `round:{roundID}:bettors` (SET).** Spec §4's "2/5 players
  have placed their bets" counter had no key that could produce it;
  `:wagers` can't, since one player betting two outcomes is two fields.
- **A3 — New script `lock_round.lua`.** The `open → locked` write had no
  owner: Phase 4 owns the timer, not the Redis write.
- **A4 — Room/round writers live in `internal/redisstore`.** Short-code
  generation stays Phase 3's; `CreateRoom` takes a code as a parameter.
- **A5 — Shared rate limiter deferred to Phase 3**, next to its first caller.

Two decisions about how the phase gets tested, both the user's call:

- **Integration tests fail, never skip, when Redis is unreachable** — and
  `make test` starts Redis and waits for health first. See "What Didn't Work".
- **Real Redis, no miniredis.** A concurrency suite run against a Lua
  reimplementation proves the fake is correct, not that Redis is.

## What Worked

Everything below was run against the live stack rather than assumed — the
practice that caught two compile errors in Phase 1's plan:

- **`go-redis` v9.18.0 is the version ceiling.** v9.19.0 through v9.22.0 all
  declare `go 1.24` in their `go.mod` and cannot build against this project's
  Go 1.22.10. Checked v9.7.3 / v9.17.3 / v9.18.0 / v9.19.0 / v9.20.0 /
  v9.20.1 / v9.21.0 / v9.22.0 individually. v9.18.0 compiles clean and pulls
  only three indirect deps.
- **The whole Task 3 idempotency mechanism, compiled and run** against Redis
  on DB 15 through go-redis: accept and replay returned deep-equal replies,
  every element a string, wallet debited exactly once (500 → 300), outbox
  `XLEN` exactly 1, `idem:` TTL 24h.
- **`redis.call('TIME')` inside a script** accepts a future lockout and
  rejects a past one with the wallet untouched — the authoritative-clock
  guarantee is real, not just intended.
- **`cjson` under Redis 7.2 / Lua 5.1** round-trips a flat string array
  exactly, which is what makes the idempotency cache viable.
- **`HGETALL` inside Lua** yields a flat `field, value, field, value` array;
  `SADD`/`SCARD` give the distinct-bettor count.

## What Didn't Work

- **"Skip integration tests when Redis is absent" — proposed, then reversed.**
  My first recommendation was the standard Go convention: env-gate the tests
  so `go test ./...` stays green on a bare checkout. The user pushed back
  ("wouldn't it be better to have it running so everything can be validly
  tested?") and was right. That convention exists for open-source repos where
  a contributor may not have the stack; here it means a suite whose entire
  purpose is proving zero double-spend can report **PASS** while executing
  nothing. **Do not reintroduce a skip.** Unreachable Redis is a test failure.
- **Build tags (`//go:build integration`) rejected for the same reason** —
  they exclude the suite from `go test ./...` entirely, which is the strongest
  form of the same false-green problem.
- **A "repeat wagers sum" checkpoint was drafted, then folded away.** `HINCRBY`
  satisfies it the moment it's written, so it could never fail first. This is
  the RED→GREEN rule added after Phase 1 doing its job at plan-writing time
  rather than at execution time. Same call for the guard-precedence test
  (folded into Task 4 CP6) and for all of Task 8.

## Test Coverage

- **Covered by the plan:** `internal/redisstore` to ≥80%; six `place_wager`
  rejection paths each asserting zero mutation; idempotency including a
  50-goroutine race on one key; settlement and refund idempotency; four
  concurrency suites run with `-race -count=3`.
- **Not covered yet — nothing is implemented.** This session produced a plan,
  not code. Current live coverage is unchanged from Phase 1: `internal/domain`
  100%, `internal/config` 100%, `internal/httpapi` 85.7%, `cmd/api` 0%.
- **Deliberately not covered in Phase 2:** spec §7's latency targets (needs k6,
  Phase 7); the outbox relay and Kafka producer (Phase 5, though this phase
  writes the stream they read); round countdown timers (Phase 4).

## Open Questions / Blockers

- **Task 8 is the likeliest place execution goes wrong.** Its suites verify
  behavior Tasks 3–7 already build, so they pass when written — there is no
  RED step. The plan says so explicitly and instructs the executor to stop and
  report a failure rather than weaken the assertion. Worth checking that
  Sonnet honored it.
- **`ReadStakes` sorts by `(UserID, Outcome)`** because `HGETALL` order is
  unspecified while `domain.Settle` emits `Results` in input order. This
  narrows Phase 1's documented "order they first staked" to "ascending by user
  ID" for anything sourced from Redis. `domain.Settle` itself is unchanged.
- **`go get -u` is the single most likely way a future session breaks the
  build** — it would pull go-redis past the Go 1.22 ceiling. Task 9 puts this
  in CLAUDE.md, but it is not there yet.
- **Watch item carried from last session:** whether the spec-driven plan
  format holds up under a cold executor. Half the length is promising;
  execution is the real test.

## Relevant Commits

- `ab31048` — docs: add Phase 2 Redis layer implementation plan
- `c4e33ea` — chore: install redis-patterns, refresh CLAUDE.md for post-Phase-1 state *(other window)*
- `c0bd875` — Merge branch 'phase-1-domain-core' into dev *(other window)*

## Next Step

Hand off to the Sonnet window pointed at
`docs/plans/2026-08-24-phase-2-redis-layer.md`. `executing-plans` runs
`git checkout -b phase-2-redis-layer dev` itself — no manual branching. Expect
it to raise questions before starting; its Step 1 requires a critical review
pass, and that is the skill working rather than a defect in the plan.

Docker must be running: `make up` brings up Redis, and the tests now fail
rather than skip without it.
