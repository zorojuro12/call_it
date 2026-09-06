# 2026-08-26 — ansh — Phase 4b (round lifecycle) execution, full MVP close-out

**Status:** `phase-4b-round-lifecycle` branch is complete and green — all 31 planned checkpoints across 11 tasks landed as 34 commits, `make test && make lint && make build` all pass, `internal/round` 80.5% / `internal/wager` 84.6% coverage under the `-coverpkg=./...` profile. Security review clean after one HIGH fix. **Not merged** — user chose "keep as-is" when offered the merge/PR/keep menu at the end.
**Decided:** CallIt is now playable end to end — host opens a round over the socket, players wager against a live pari-mutuel pool, server-side timer locks it, host resolves with every stake revealed at once, host-disconnect auto-refunds after 60s. §12's first MVP acceptance box is checked, evidenced by `internal/ws.TestEndToEndRound`.
**Spec:** Updated — design spec §4 gained Amendment D1 (round control travels over the socket, not REST) and the reconnect-limitation note from Task 8 CP2; parent plan §4/§9/§12 updated (new Redis keys, 4b marked complete, acceptance box checked); CLAUDE.md gained a Phase 4b security-review paragraph mirroring Phase 3's.
**Next:** Merge `phase-4b-round-lifecycle` into `dev` whenever the user's ready (`git checkout dev && git merge --no-ff phase-4b-round-lifecycle`, per this project's self-merge convention) — then Phase 5 (Kafka + ledger) is next per the plan's phase table.
**Blocked on:** Nothing — branch is sitting ready, by explicit user choice.
**Touches:** `backend/internal/round/`, `backend/internal/wager/`, `backend/internal/ws/router.go` + `hub.go` + `handler.go`, `backend/internal/redisstore/round.go` + `room.go` + `user.go`, `backend/internal/httpapi/health.go` + `ws_handlers.go`, `backend/cmd/api/main.go`, `backend/cmd/callit-cli/`, `docs/plans/2026-08-26-phase-4b-round-lifecycle.md`, `docs/plans/2026-08-21-implementation-plan.md`, `docs/specs/2026-08-21-callit-design.md`, `CLAUDE.md`.

---

## What We Worked On

Executed the full Phase 4b plan (`docs/plans/2026-08-26-phase-4b-round-lifecycle.md`) task-by-task via the `executing-plans` skill, starting from a clean `dev` (Phase 4a already merged). Reviewed the plan critically first — no concerns, it had already been pre-verified against 4a's landed interfaces in a prior session. Worked through all 11 tasks in order: Redis schema additions → round service (open/lock timer/resolve/refund) → wager service (place/odds/rate-limit) → message routing over the socket → CLI client → wiring into the real server → close-out.

## Decisions Made

- **Round/wager packages define their own local envelope encoder rather than importing `internal/ws`'s** — reason: Task 9 makes `ws` import `round`/`wager` for the message router; `round` importing `ws` back would cycle. Task 2's plan text said "reuse ws's Encode," which turned out inconsistent with Task 9's own stated import direction; resolved in Task 9's favor since that's a hard Go compilation rule, not a preference. `round.EncodeEnvelope` mirrors `ws.Envelope`'s wire format; `wager` reuses `round`'s copy.
- **`internal/ws/e2e_test.go` is `package ws_test`, not `package ws`** — it needs `internal/httpapi` for the REST half of the flow, and `httpapi` imports `ws`; an internal test file pulling that in is a real build cycle (confirmed via `go vet`, not just theorized).
- **Timer tests (`internal/round/timer_test.go`) bypass `Service.Open` and call `store.CreateRound` + `svc.watch(...)` directly** — Task 2's `MinLockIn = 3s` validation conflicts with Task 3/7/10's own checkpoint text specifying 100ms–500ms test lock windows; rather than weaken real validation to make fast tests possible, the timer tests construct state directly and the e2e test uses 3000ms (the real minimum) with a wider read timeout instead of the plan's literal 500.
- **User chose to keep the branch as-is rather than merge or PR** — full option-1/2/3 menu was presented per `finishing-a-development-branch`; no reason given, just the choice. Branch is ready to merge whenever wanted.

## What Worked

- Full TDD discipline held for all 34 commits — every checkpoint got its own RED→GREEN cycle and its own commit, `type: description` format.
- Premature-pass checkpoints (6 total, 4 named in advance by the plan) were all verified genuine via the disable-and-confirm procedure this project established in Phase 3 — temporarily strip the guard, confirm the test fails, restore, record the outcome in the commit body.
- The end-to-end test (`TestEndToEndRound`) passed on its very first real run against live Redis and real sockets — host + two players register/join over REST, play a full round over the socket, and token conservation (`wallets + dust == combined opening stakes`) holds exactly.
- `security-reviewer` run against `internal/round`, `internal/wager`, `internal/ws` before close-out found one HIGH (rate-limit errors falling through to a generic `internal_error` code instead of `rate_limited`) and confirmed every other invariant held; fixed on the spot.
- Two real production bugs were caught specifically *because* this phase wired real services together for the first time: (1) a typed-nil `*round.Service` passed into the `ws.SessionEnder` interface parameter panicked on disconnect for any caller that didn't set it, since a nil pointer wrapped in an interface isn't the nil interface; (2) `internal/wager` tests reusing literal `"u1"`/`"u2"` IDs across test functions shared the same Redis rate-limit keyspace (keyed by user, not room), causing `TestPlaceRateLimited` to fail only under the full package run, never in isolation.

## What Didn't Work

- Manually verifying `watch`'s two `ctx.Done()` cancellation branches by temporarily removing the `select` and confirming the test fails — didn't produce a real RED in either case (lock wait, in Phase 4a-era work already recorded; refund wait, new this session). A cancelled context also makes the subsequent Redis call fail harmlessly on its own, so the observable assertion (round status unchanged, no broadcast) holds whether or not the explicit `select` exists. The `select` is still correct and kept — it stops the goroutine immediately instead of sleeping out the full window first — but this specific test design can't distinguish its presence from its absence. Noted in the plan's own Measured section as a real, if narrow, limitation of the observable-signal testing approach for this particular kind of resource-cleanup code.
- First attempt at the coverage-closing test used a 500ms/2s LockIn to match the plan's literal e2e spec text — rejected by `Open`'s real `MinLockIn=3s` validation. Same root cause as the timer-test conflict above; fixed by using `MinLockIn` itself.

## Test Coverage

- **Covered:** Full round lifecycle (open/validate/concurrent-refuse, lock timer + terminal-skip + cancellation on both waits, resolve + refusal cases + nobody-won refund, auto-refund + no-double-refund, session-end fold + guest no-op), full wager lifecycle (place/reject/idempotency/rate-limit/broadcast/suppression), message routing (dispatch + error-code table including the new rate-limit case + hub broadcast/names), and one real end-to-end socket-driven round with token-conservation assertion.
- **Not covered yet:** `internal/round`'s handful of defensive `log.Printf`-after-Redis-error branches (unreachable without fault-injecting the Redis connection itself) — same accepted category as `internal/redisstore`'s own documented gap; not chased with a fake Redis per project convention.

## Open Questions / Blockers

None. Branch is green, reviewed, documented, and waiting on the user to decide when to merge.

## Relevant Commits

34 commits from `bab5d69` (first Task 1 checkpoint) through `1259d37` (`docs: close out Phase 4b and the MVP acceptance criteria`) on `phase-4b-round-lifecycle`, branched off `dev`. Full list in `git log dev..phase-4b-round-lifecycle`.

## Spec Changes

`docs/specs/2026-08-21-callit-design.md` §4: added Amendment D1 (round control — open/wager/resolve — travels over the WebSocket, not REST; room creation/joining stay on REST since they precede any socket) and the reconnect-with-session-resume known limitation (Task 8 CP2: `EndSession` fires on disconnect, so a drop-and-reconnect starts a fresh session at the room's buy-in; deferred to Phase 7).

`docs/plans/2026-08-21-implementation-plan.md`: §4's key-schema table gained `room:{roomID}:round`, `room:{roomID}:opening`, and the round hash's `question`/`outcomes` fields; §9 marked 4b complete; §12's first acceptance box checked with `TestEndToEndRound` as evidence.

## Next Step

Whenever the user wants to move forward: merge `phase-4b-round-lifecycle` into `dev` (self-merge, `--no-ff`, no PR — this project's established convention), then start Phase 5 (Kafka + ledger) per the plan's phase table — needs `postgres-patterns`/`database-migrations` tooling installed first, per the plan's own "Tooling to import" column.
