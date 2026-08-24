# 2026-08-24 — ansh — Phase 2 Redis layer execution

**Status:** Phase 2 (`internal/redisstore`) fully implemented and verified on branch `phase-2-redis-layer`, off `dev`: key schema, all 4 Lua scripts (`place_wager`, `lock_round`, `settle_round`, `refund_round`), Go wrappers, and 4 concurrency suites proving zero double-spend and exact token conservation. 29 commits, all green from a clean `make down && make up`. Branch is **not yet merged** — `finishing-a-development-branch` presented the 3-option menu and the user chose "keep as-is."
**Decided:** No new decisions beyond the plan's own amendments (A1–A5, already recorded in `docs/plans/2026-08-24-phase-2-redis-layer.md`'s header) — folded back into the parent plan, spec, and CLAUDE.md per the plan's own Task 9 Checkpoint 2, matching how Phase 1 closed out its A1–A3.
**Spec:** Updated — `docs/specs/2026-08-21-callit-design.md` §4 gained one sentence on how the "N/M players wagered" counter is actually computed (`SCARD round:{roundID}:bettors` over `HLEN` wallets minus host); parent plan §4/§5/§9 updated with the `bettors` key, `lock_round.lua` as a fourth script, the Go-computes/Lua-applies settlement split, and Phase 3's inherited writers; CLAUDE.md's Stack/Build & Test/Critical Invariants/Repository Layout/Testing/Installed Tooling/Git Workflow sections all updated to match current state.
**Next:** Get the user's merge decision to close out `finishing-a-development-branch` (still pending — they chose "keep as-is" this session). After that, Phase 3 (Auth + REST) starts with its own `writing-plans` pass; `api-design` skill needs installing first per CLAUDE.md's per-phase tooling table.
**Blocked on:** User's merge/integration choice for `phase-2-redis-layer`.
**Touches:** `backend/internal/redisstore/*` (all files + tests), `backend/scripts/lua/*.lua`, `backend/internal/config/config.go`, `Makefile`, `.github/workflows/ci.yml`, `docs/plans/2026-08-21-implementation-plan.md`, `docs/specs/2026-08-21-callit-design.md`, `CLAUDE.md`, `docs/plans/2026-08-24-phase-2-redis-layer.md`

---

## What We Worked On

Executed the Phase 2 plan (`docs/plans/2026-08-24-phase-2-redis-layer.md`, written last session) end to end via the `executing-plans` skill, task by task:

1. Dependency, config, key schema, test harness (`go-redis` v9.18.0 pinned, `RedisAddr`/`RedisDB` config, `keys.go`, `Store`/`testmain_test.go`).
2. Room and round writers (`CreateRoom`, `JoinRoom`, `CreateRound`) — the real creators per Amendment A4, not test fixtures.
3. `place_wager.lua` accept path — debit, pool, wager record, bettor set, outbox `XADD`, all atomic.
4. `place_wager.lua` rejection paths — six guards in precedence order: idempotency → status/clock → outcome range → host → membership → funds.
5. `lock_round.lua` — `open → locked` CAS.
6. `settle_round.lua` — Go computes via `domain.Settle` (Amendment A1), Lua applies.
7. `refund_round.lua` — timeout/disconnect path, refunding read inside the script's own atomic unit.
8. Concurrency and conservation suite — 4 verifications under `-race -count=2/3`: no double-spend, mixed-load conservation, idempotency race, full-round settle/refund conservation.
9. Coverage top-up (79.0% → 87.2%), doc amendments, close-out.

Also part of this session: resumed from the last two journal entries, pushed 41 previously-unpushed local commits (Phase 0, Phase 1, the Phase 2 plan) to `origin/dev` at the user's request, before starting Phase 2 execution.

## Decisions Made

- Branched `phase-2-redis-layer` off `dev` per the project's standard workflow — no new decision, just following `docs/dev-workflow-guide.md` §8.
- Two mid-execution corrections, both judgment calls not written anywhere else:
  - **`go mod tidy` right after `go get` strips an unused dependency.** Nothing imported `go-redis` yet at Task 1 Step 1, so `tidy` removed the `require` line the moment it was added. Fixed by skipping `tidy` until Checkpoint 3 actually imports the package — `go get` alone populates `go.mod`/`go.sum` correctly. Minor plan-order issue, not worth amending the plan doc for.
  - **A real plan defect, caught and fixed during Task 6 Checkpoint 2.** The plan's own test assertion said `Σ wallets + Σ pools == 1500` after settlement, but `settle_round.lua`'s own KEYS list never touches the pools key — pools stays at its pre-settlement total forever. The correct invariant is `Σ wallets + Settlement.Dust == 1500`; pools staying unchanged is a separate fact (matching what Task 8 Verification 4 already checks). Fixed the test to assert the correct invariant rather than the plan's literal (inconsistent) text.

## What Worked

- Every checkpoint's RED state was verified before implementing — including catching two cases the plan itself flagged as likely unable to RED (a repeat-wager accumulation case in Task 3, and `lock_round.lua`'s `ALREADY_LOCKED` case in Task 5, which turned out black-box indistinguishable from the unconditional-OK version at the Go API surface — confirmed by running the "before" implementation against the "after" test and watching it pass).
- All 4 concurrency suites pass under `-race -count=3` (2 for the full-round one): `TestConcurrent_NoDoubleSpend` (100 goroutines, exactly 20/80 accept/reject split, final balance exactly 0), `TestConcurrent_TokenConservation` (200 goroutines, mixed players/outcomes/amounts, `Σ wallets + pools == 2500` exactly), `TestConcurrent_IdempotencyRace` (50 goroutines racing one idempotency key, debited exactly once), `TestFullRound_TokenConservation` (concurrent wagers → lock → settle, `Σ wallets + Dust == 2000`, and the empty-outcome refund path separately).
- `internal/redisstore` coverage 79.0% → 87.2% after adding targeted tests for genuinely unexercised error paths (malformed stored data, `New()`'s connection failure, the two status-mapper default branches) — all real gaps, not padding.
- `make lint && make build && make test` from a clean `make down && make up` — the whole toolchain, from scratch — all green.
- `internal/domain` and `internal/config` stayed at 100%, `internal/httpapi` unchanged at 85.7% — no regression from Phase 1/Phase 0 work.

## What Didn't Work

- **Writing the full `lock_round.lua` (all 3 status branches) and its wrapper in one pass, then discovering afterward that the `ALREADY_LOCKED` checkpoint's test couldn't independently RED.** Had to unwind: reverted the script to the unconditional-OK version, re-ran the test to confirm it still passed (proving the checkpoint really was black-box unverifiable), then re-added both remaining branches together as one commit rather than three. Lesson for future task-writing: a checkpoint whose only observable difference is an internal Lua status code, never exposed through the Go wrapper's return type, cannot RED at the wrapper level — flag this in the plan itself next time rather than discovering it mid-execution.
- **First cut of `TestConcurrent_NoDoubleSpend` used literal idempotency keys like `"dblspend-%d"`.** Passed on its own, but failed under `-count=3`: Redis state isn't flushed between `-count` reruns within one test binary process, so iteration 2's identical key string replayed iteration 1's *cached* reply (from a *different* room's wallet) instead of hitting iteration 2's real guard logic — inflating the apparent accept count (40, 60, fluctuating) without any actual over-debit. Fixed by scoping every idempotency key to the round ID, which is already unique per iteration. Same latent bug existed in `TestConcurrent_TokenConservation` and the `placeConcurrentWagers` helper — fixed identically.
- **`placeConcurrentWagers` initially called a shared `*rand.Rand` from inside each spawned goroutine.** `math/rand.Rand` isn't safe for concurrent use; `-race` caught it immediately (`WARNING: DATA RACE` on `rngSource.Uint64`). Fixed by precomputing every goroutine's random draws sequentially before spawning any of them — the same pattern `TestConcurrent_TokenConservation`'s job-precompute loop already used, just not yet applied to this second helper.

## Test Coverage

- **Covered:** `internal/redisstore` at 87.2% — every `place_wager.lua` guard and its precedence ordering, idempotency (including a 50-goroutine race), room/round/pools CRUD and their malformed-data error paths, lock/settle/refund lifecycles and their idempotency, and the full concurrency/conservation suite under `-race`.
- **Not covered yet:** `internal/redisstore`'s `PlayerCount` error branch and a few other defensive branches unreachable without fault-injecting Redis itself (e.g. `HLEN` failing) — accepted as dead-but-safe code, not gaps in real behavior coverage. Spec §7's latency targets (needs k6, Phase 7). The outbox relay and Kafka consumer (Phase 5) — this phase writes the stream they'll read, nothing more. Round countdown timers (Phase 4) — `lock_round.lua`/`refund_round.lua` exist, but nothing calls them on a schedule yet.

## Open Questions / Blockers

- Merge decision for `phase-2-redis-layer` → `dev` is the only open item — same shape as Phase 1's close-out. User chose "keep as-is" this session; branch and worktree are untouched, ready to merge whenever.
- Worth deciding before Phase 3's plan is written: should `writing-plans` explicitly flag checkpoints whose pass/fail can't be observed through the public API (like `lock_round.lua`'s `ALREADY_LOCKED` case), the same way it already flags checkpoints that can't RED because an earlier checkpoint's implementation already satisfies them? Both are variants of the same underlying problem — a specified checkpoint with no way to fail first.

## Relevant Commits

29 commits on `phase-2-redis-layer` (branched off `dev` at the tip after Phase 1's merge + Phase 2 plan), from `70b63af` (chore: add go-redis v9.18.0 and wire Redis into test and CI) through `b551b57` (docs: fold Phase 2 amendments into the spec, plan, and CLAUDE.md). Full list via `git log --oneline phase-2-redis-layer ^dev`.

## Spec Changes

- `docs/specs/2026-08-21-callit-design.md` §4 — added one sentence on how the "N/M players wagered" progress counter is computed: `SCARD round:{roundID}:bettors` over `HLEN room:{roomID}:wallets` minus the host.
- `docs/plans/2026-08-21-implementation-plan.md` §4 — added the `round:{roundID}:bettors` SET row and a note that brace placeholders aren't Cluster hash tags. §5 — replaced the original "`settle_round.lua` computes the payout formula" description with the actual Go-computes/Lua-applies split (Amendment A1) and its reasoning; added `lock_round.lua` as a fourth script. §9 — noted Phase 3's `internal/room`/`internal/round` wrap Phase 2's writers rather than reimplementing them, and that the shared rate limiter is still Phase 3's to build.
- `CLAUDE.md` — Stack now names the pinned `go-redis` v9.18.0 and the Go 1.22 ceiling reason; Build & Test documents `make test`'s Redis-start-and-wait behavior and the fail-rather-than-skip integration test posture; two new Critical Invariants (settlement math not duplicated in Lua, `keys.go` as the sole key-construction site); Repository Layout retagged from "(Phase 2)" to "exists"; Testing section's coverage line updated; Installed Tooling notes `redis-patterns` now in use; Git Workflow section records the actual 29-commit outcome against the plan's 31–32 estimate, and why the two deltas happened.

## Next Step

Get the user's merge decision for `phase-2-redis-layer` whenever they're ready. After that, Phase 3 (Auth + REST — register/login, room creation, join-by-code, JWT issuance, rate-limit middleware) gets its own `writing-plans` pass; per CLAUDE.md's per-phase tooling table, install `api-design` first.
