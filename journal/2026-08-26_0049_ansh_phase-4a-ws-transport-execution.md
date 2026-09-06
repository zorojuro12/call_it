# 2026-08-26 — ansh — Phase 4a WebSocket transport execution

**Status:** Phase 4a (WebSocket transport) is fully implemented on branch `phase-4a-ws-transport`, all 25 checkpoints landed as 25 commits, `make test`/`make lint`/`make build` all green, `internal/ws` at 96.2% coverage. Branch is **not merged into `dev`** — committed and pushed only, per explicit instruction this session.
**Decided:** Task 5 CP2's literal plan contract (delete the room from the hub's registry directly on the empty notification) had a real race — a client rejoining between notification and reap would land in an orphaned room while the hub spun up a second, empty one under the same ID. Implemented the plan's own design-note alternative instead: `Room.close()` replies `false` if a member re-joined before the hub could reap, and the hub only deletes on `true`. This also required *not* returning from the hub's `run()` goroutine on `Shutdown`, since Task 5 CP3's own test calls `RoomCount()` immediately after `Shutdown()` returns and would otherwise deadlock forever waiting on an unread `h.cmds` channel.
**Spec:** No change to the design spec. Parent plan (`docs/plans/2026-08-21-implementation-plan.md`) §9 updated: Phase 4 row split into 4a (this phase, ✅) and 4b (round lifecycle, next), with dependents renumbered `4` → `4b`.
**Next:** Someone needs to decide whether to merge `phase-4a-ws-transport` into `dev` before starting Phase 4b's `writing-plans` pass — 4b's `MessageHandler`/`Room.Broadcast` seam depends on 4a's code existing on `dev` (or 4b branches off this feature branch instead). Not decided this session.
**Blocked on:** Nothing technical. Waiting on a merge decision (see Next).
**Touches:** `backend/internal/ws/` (new package: `protocol.go`, `room.go`, `client.go`, `hub.go`, `handler.go` + tests), `backend/internal/httpapi/health.go` (`Deps.Hub`, route registration), `backend/internal/httpapi/ws_handlers.go` (new), `backend/cmd/api/main.go` (hub wiring + shutdown), `docs/plans/2026-08-21-implementation-plan.md` §9, `docs/plans/2026-08-26-phase-4a-ws-transport.md` (Measured section added).

---

## What We Worked On

Executed `docs/plans/2026-08-26-phase-4a-ws-transport.md` end to end via the `executing-plans` skill: authenticated WebSocket upgrade, per-room owner goroutine (channel-based, zero mutexes), client read/write pumps with ping/pong heartbeat, slow-client eviction, hub registry with empty-room reaping and graceful shutdown, and join/leave presence broadcasts — wired into the existing `httpapi` mux and `cmd/api` process. No round/wager/money logic touched (that's Phase 4b, deliberately deferred by the plan's own Amendment C1).

Followed strict TDD per checkpoint: wrote the failing test, confirmed RED, implemented, confirmed GREEN, committed. Where a checkpoint's test passed immediately because an earlier checkpoint's implementation already satisfied its contract (5 of 25: Task 1 CP2, Task 3 CP2/CP3/CP4, Task 6 CP2), verified genuineness by disabling the relevant guard, confirming the test failed, then restoring — per the observable-signal rule from Phase 3's retrospective.

## Decisions Made

- **Room reaping uses a close-confirmation round-trip, not a direct delete** — see Decided above. Real defect in the plan's literal CP2 spec, fixed while implementing per the plan's own design note that anticipated exactly this race but didn't fully spell out the `Hub.Shutdown` interaction.
- **Test for Task 7 CP1 needed a correction to the plan's own narrative** — the plan's CP1 text implies A's "next message" after `connected` is B's `player_joined`, but the documented design (broadcast-to-whole-room-including-newcomer, stated explicitly in the plan for B's case) means A *also* receives a self-directed `player_joined` about its own join first. Adjusted the test to drain that self-broadcast before asserting on B's join — consistent with the stated design, not a deviation from it.
- **Ran `make up` once, before Task 7 CP3** — that checkpoint's `httpapi` test needs Redis; Tasks 1–6 needed none, matching the plan's own global constraint.

## What Worked

- All 25 checkpoints, 25 commits, one checkpoint one commit, zero un-batching of the plan's combined "verify-and-commit in one command" step — see the plan file's new "Measured" section for full detail (this is the first phase executed under the experimental `writing-plans-tuned` format).
- `make test && make lint && make build` all green at Task 7 CP4's final verification. `internal/ws` package coverage: 96.2%.
- Concurrent commits from an unrelated process (see below) never collided with this session's file changes — confirmed via `git log` timestamps and `git status` before proceeding.

## What Didn't Work

- Nothing was abandoned mid-approach this session — no dead ends to warn a future session away from.

## Test Coverage

- **Covered:** Envelope encode/decode and malformed-input rejection; room join/leave/broadcast/eviction/empty-notification (including double-leave, double-eviction-adjacent idempotency); write pump (ordering, heartbeat, close-on-channel-close, close-on-write-error); read pump (dispatch, malformed/unknown-type private error replies, pong deadline extension, close-callback exactly-once); hub get-or-create/reap/shutdown; authenticated upgrade (valid token via query param and `Authorization` header, missing/garbage/wrong-secret/expired token rejection, account-scoped-token rejection); join/leave presence broadcasts including the self-broadcast case; full route wiring through `httpapi`'s real mux with real Redis-backed `Deps`.
- **Not covered yet:** Nothing flagged as a gap — 96.2% is well above the 80% floor and the package's own aspirational bar. Phase 4b will add coverage for the `MessageHandler` seam once real gameplay messages exist.

## Open Questions / Blockers

- Merge timing for `phase-4a-ws-transport` → `dev` is unresolved — explicitly deferred this session, branch is pushed but not merged.
- Mid-session, two commits (`d94897f`, `47ea676`) landed on this same branch from what appears to be a separate, concurrent process — both scoped to `.claude/skills/writing-plans-tuned/BASELINE.md` and `session_tokens.py` (baseline token measurement for the tuned-planning experiment), authored as the same git identity, interleaved in time with this session's own commits. User confirmed these don't touch any files this session worked on and won't make further changes — noted here in case the branch's commit history looks unexpected to a future reader.

## Relevant Commits

- `67fc922`..`6b11b12` — the 24 `feat` commits implementing Tasks 1–7 of `docs/plans/2026-08-26-phase-4a-ws-transport.md`, one per checkpoint.
- `cde31ad` — `docs: split Phase 4 into 4a and 4b in the parent plan` (Task 7 CP4 close-out: parent plan §9 amendment + this plan's own "Measured" section).
- (Not this session's own work, but present on this branch) `d94897f`, `47ea676` — tuned-planning-experiment baseline measurement, see Open Questions above.

## Next Step

Decide merge timing for `phase-4a-ws-transport`, then run a `writing-plans` pass for Phase 4b (round lifecycle) — its plan already exists at `docs/plans/2026-08-26-phase-4b-round-lifecycle.md` per an earlier session, so this is likely straight to `executing-plans` rather than a fresh planning pass, once the merge question is settled.
