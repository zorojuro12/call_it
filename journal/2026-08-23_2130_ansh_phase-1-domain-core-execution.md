# 2026-08-23 — ansh — Phase 1 domain core execution

**Status:** Phase 1 (`internal/domain`) fully implemented and verified on branch `phase-1-domain-core`: round FSM, tokens/economy constants, wallet rules, refill rules, pari-mutuel payout with dust, empty-pool refund path, odds, and a token-conservation fuzz test. 21 checkpoint commits, all green. Branch is **not yet merged** — the `finishing-a-development-branch` menu was presented and the user asked for a readiness summary first; merge decision is still pending.
**Decided:** No new decisions this session beyond what the plan (`docs/plans/2026-08-23-phase-1-domain-core.md`) already settled — amendments A1–A3 (economy constants live in `domain`, refill threshold removed, buy-in ceiling 10,000) and the new wager-anonymity invariant were folded back into the parent plan, spec, and CLAUDE.md per the plan's own Task 8 Checkpoint 2.
**Spec:** Updated — `docs/specs/2026-08-21-callit-design.md` §4 gained the wager-anonymity-until-terminal invariant (host must never see positions before resolving); parent plan §8's economy table replaced with settled values; CLAUDE.md's Critical Invariants and Testing sections updated to match.
**Next:** Get the user's merge decision (merge to `dev` locally / push + PR / keep as-is) to close out `finishing-a-development-branch`. After that, Phase 2 (Redis wiring) starts with its own `writing-plans` pass.
**Blocked on:** User's merge/integration choice for `phase-1-domain-core`.
**Touches:** `backend/internal/domain/*` (all 8 files + tests), `docs/plans/2026-08-21-implementation-plan.md`, `docs/specs/2026-08-21-callit-design.md`, `CLAUDE.md`, `docs/plans/2026-08-23-phase-1-domain-core.md`

---

## What We Worked On

Executed the Phase 1 plan (`docs/plans/2026-08-23-phase-1-domain-core.md`) end to end via the `executing-plans` skill, task by task:

1. Round state machine (`round.go`) — status transitions, terminal states, wager acceptance, outcome count/index validation.
2. Token type, economy constants, buy-in bounds (`tokens.go`, `economy.go`, `wallet.go`).
3. Stake validation and session P&L folding (`wallet.go`).
4. Refill eligibility and top-up amount (`refill.go`).
5. Pari-mutuel payout formula with dust, per-player net results for the anonymity-preserving reveal (`payout.go`).
6. Empty-winning-pool refund path.
7. Live odds multipliers (`odds.go`) — the only floats in the package.
8. Token-conservation fuzz test, then folding the plan's amendments and the anonymity invariant back into the parent plan, spec, and CLAUDE.md.

## Decisions Made

- Amendments A1–A3 and the wager-anonymity invariant — reasoning already fully written in `docs/plans/2026-08-23-phase-1-domain-core.md` (header section) and now mirrored into the parent plan §8, spec §4, and CLAUDE.md. Not re-explained here.
- CLAUDE.md's checkpoint-per-commit convention (adopted `ad1027a`, flagged as "new and unverified") got its first real test this session: 21 commits for 22 checkpoints (Task 8 Checkpoint 3 produces no commit, as the plan predicted), and no checkpoint turned out to be a test-only addition pinning already-implemented behavior. The convention held up cleanly — no adjustment to `.claude/skills/writing-plans/SKILL.md` needed based on this run.

## What Worked

- Every checkpoint's RED state was independently verified (ran the failing test, read the actual compiler/test error) before writing the GREEN implementation — not just trusted from the plan's predicted output.
- `internal/domain` hit **100.0% statement coverage**, `go vet` clean, `gofmt -l` clean throughout.
- `FuzzSettleConservesTokens` ran 60s / ~4,008,141 executions with zero crashers — the token-conservation invariant (`payouts + dust == total staked`, `netSum == -dust`) held across all fuzzer-discovered inputs.
- `make lint && make build && make test` from the repo root — the same gate CI runs — all green, including `internal/config` 100%, `internal/httpapi` 85.7%.
- `backend/go.mod` still has no `require` block — no third-party dependency crept in.

## What Didn't Work

- No dead ends this session — the plan's exact code snippets and test predictions matched actual behavior in every case but one harmless deviation: the plan predicted `TestSettle_EmptyRound` would panic on division-by-zero before Task 5 Checkpoint 3's validation existed. In practice it didn't panic — with `nil` stakes the payout loop simply never executes (nothing has `s.Outcome == winningOutcome`), so it degraded safely to zero payouts instead. Not a bug, just a note in case a future plan reuses that exact prediction.

## Test Coverage

- **Covered:** All of `internal/domain` — round FSM, wallet/economy rules, refill rules, `Settle()` (proportional split, flooring/dust, refund path, per-player net results, input validation), odds multipliers. Property-based conservation proof via fuzzing on top of the example-based tests.
- **Not covered yet:** Nothing in this package — 100% is the floor per the plan. Redis/Postgres integration is out of scope by design (Phase 1 has zero I/O); that coverage starts in Phase 2.

## Open Questions / Blockers

- Merge decision for `phase-1-domain-core` → `dev` is the only open item. The `finishing-a-development-branch` skill's 3-option menu (merge locally / push+PR / keep as-is) was presented; the user asked for a summary first, which was given, but no option has been chosen yet as of this entry.

## Relevant Commits

21 commits on `phase-1-domain-core` (branched off `dev` at `bdf6657`), from `281294a` (round status transition table) through `e4e7135` (docs: settle economy constants and record wager anonymity invariant). Full list via `git log --oneline phase-1-domain-core ^dev`.

## Spec Changes

- `docs/specs/2026-08-21-callit-design.md` §4 — added the wager-anonymity-until-terminal invariant, including the aggregate-progress-counter mitigation and its accepted known limitation (small-room stake guessing).
- `docs/plans/2026-08-21-implementation-plan.md` §8 — economy table replaced with settled values (buy-in ceiling 10,000, no separate refill threshold); note added pointing to `internal/domain/economy.go` and the Phase 1 plan's §A1–A3.
- `CLAUDE.md` — new Critical Invariants bullet for wager anonymity; Testing section's coverage line now includes `internal/domain` at 100%.

## Next Step

Resolve the merge decision, then complete whichever path `finishing-a-development-branch` requires (merge `--no-ff` into `dev` and clean up the branch, or push + open a PR, or leave as-is). Once integration is settled, Phase 2 (Redis wiring: atomic Lua scripts, wager-outbox) gets its own `writing-plans` pass before any code — `redis-patterns` skill still needs installing first per CLAUDE.md's per-phase tooling table.
