# 2026-09-02 — ansh — Phase 7c execution: the three deferred security items + README

**Status:** All 8 tasks implemented, verified, and committed on
`phase-7c-security-debt-docs` (off `dev`). Full backend suite green
(`make test`, `-race -p 1`), frontend suite green (`npx vitest run`,
`npx tsc --noEmit`, `make fe-lint`), `security-reviewer` returned clean
(no CRITICAL/HIGH/MEDIUM/LOW across all five surfaces touched), merged
coverage profile **88.8%** excluding `cmd/*` (accepted at 0%),
`internal/domain` still 100%. Docs fully updated (spec §4, `CLAUDE.md`
invariants, `project-history.md`'s new Phase 7c section, parent plan's §9
row and §12 boxes). Ready for `finishing-a-development-branch`.
**Decided:** Three deviations from the written plan, all found empirically
mid-execution:
1. A pre-existing goroutine-leak race in `internal/ws/room.go` — fixed, not
   just noted, because it made Task 6's own new test flaky/hanging.
2. Task 6's stated assumption ("a page refresh already exercises the grace
   window... no frontend change needed") was empirically false — fixed with
   a small, scoped frontend change after asking the user, rather than either
   silently expanding scope or silently leaving the acceptance criterion
   unmet.
3. A second, smaller ordering bug in the frontend fix itself: the new
   `sessions.Balance` lookup on connect briefly sat *before* `hub.Join`,
   widening a pre-existing race window enough to make `TestEndToEndRound`
   intermittently miss a broadcast (~1 in 5 runs). Found via a coverage-run
   flake, root-caused, and fixed by moving the lookup after `hub.Join`.
**Spec:** Updated — `docs/specs/2026-08-21-callit-design.md` §4's
"no reconnect-with-session-resume" known-limitation bullet now describes
the implemented grace-window behavior instead.
**Next:** Hand off to `finishing-a-development-branch` to merge
`phase-7c-security-debt-docs` into `dev` with `--no-ff`.
**Blocked on:** Nothing.
**Touches:** `backend/internal/{auth,account,events,redisstore,round,ws,httpapi}/`,
`README.md`, `CLAUDE.md`, `docs/specs/2026-08-21-callit-design.md`,
`docs/project-history.md`, `docs/plans/2026-08-21-implementation-plan.md`,
`frontend/lib/{socket.ts,protocol.ts,roundState.ts}`.

---

## What We Worked On

Executed `docs/plans/2026-09-02-phase-7c-security-debt-docs.md` task by task
via `executing-plans`, inline (no delegation — the plan's own self-review
had already ruled that out). Docker needed to be started mid-session (WSL2
integration issue from a previous restart, resolved by the user starting
Docker Desktop) before any Redis/Postgres/Kafka-backed test could run.

**Task 1 — login timing.** `auth.VerifyDecoyPassword` derives a PHC-encoded
hash of a random 32-byte value once (`sync.OnceValue`, so raising
`argon2Memory`/`argon2Time` later re-costs it automatically) and burns one
argon2id verify against it, discarding the result. `account.Service.Login`
calls it on the unknown-email path only — the malformed-hash path already
pays a real `VerifyPassword` cost. Proven with a ratio-based timing test
(median unknown-email login ≥ half the median wrong-password login, over 5
samples each) rather than an absolute threshold.

**Task 2 — Kafka message bounding.** `events.MaxMessageBytes` (1 MiB) is
checked first in `DecodeMessage`, before either topic's `json.NewDecoder` is
even constructed. `events.MaxPayouts` (10,000) is checked in
`validateRoundSettled` before the payout-validation loop. Both wrap
`ErrInvalidEvent`, matching every other validation failure in the file, so
`cmd/ledger-worker`'s error handling needed no change.

**Tasks 3–6 — the reconnect fix, the largest piece.**
- `Store.ClearSession` (`redisstore/room.go`) atomically `HDEL`s a session's
  wallet and opening-stake fields in one pipeline, reporting whether the
  *opening-stake* deletion actually removed something — that's the claim
  token, not the wallet, because `settle_round.lua`'s `HINCRBY` can
  resurrect a deleted wallet field on credit.
- `round.Service.EndSession` now reads the session's state, claims it via
  `ClearSession`, and only then credits — claim-then-credit, not
  credit-then-claim, so a crash between the two loses a result rather than
  minting tokens on a retry. An unknown *user* stays a hard error (pins
  `session_test.go`'s existing never-joined case); a known user with no
  live *session* is a `(0, nil)` no-op.
- `round/grace.go` (new) adds `SessionGrace` (30s), `ScheduleEndSession`,
  and `ResumeSession` — in-process state (`map[string]chan struct{}` + a
  mutex on `Service`), deliberately not Redis-backed, since the WS hub is
  already single-instance-per-room.
- `ws.SessionEnder` became `ws.Sessions`, wired into `Handler`: disconnect
  calls `ScheduleEndSession`, connect calls `ResumeSession`.

**Task 7 — README rewrite** with a Mermaid `flowchart LR` making the
missing `cmd/api → PostgreSQL` edge the visual point, a binaries table, and
a security-posture section.

**Task 8 — closing the phase**, still in progress (see Next).

## Decisions Made

- **Found and fixed: a pre-existing goroutine-leak race in
  `internal/ws/room.go`.** Task 6's new disconnect test
  (`TestHandlerSchedulesSessionEndOnDisconnect`) flaked under repeated runs.
  Root cause, confirmed with a goroutine dump: `Room.run()` exits once its
  last member's `Leave` empties it and the hub's async reap decides to
  close it; the WS handler's disconnect path always calls `Broadcast` right
  after `Leave` to announce `player_left`, and if the reap finishes first,
  that `Broadcast`'s unguarded send to the room's `cmds` channel blocks
  forever (nobody left to receive). This has existed since Phase 4a/4b —
  Task 6 is just the first thing that ever waited for something *after*
  that `Broadcast` call, so it's the first thing to notice the hang.
  Fixed by adding a `done chan struct{}` to `Room`, closed when `run()`
  returns, and guarding `Leave`/`Broadcast`/`Members`/`Count` with
  `select { case cmds<-cmd: case <-done: }`. New regression test:
  `TestBroadcastAfterReapDoesNotHang` (`room_test.go`), which forces the
  race deterministically instead of hoping it reproduces.
- **Found (via a real Playwright run) and fixed, with the user's
  confirmation: Task 6's "no frontend change needed" assumption was false.**
  The room page's displayed balance came only from a `sessionStorage`
  snapshot cached at the original join (`session_balance`, written once,
  never refreshed) — a full page reload always re-mounts from that stale
  value, regardless of how correct the backend's grace window is. Worse,
  the "obvious" fix (re-call the join REST endpoint on mount) is actually
  broken for guests specifically: a guest has no stable identity to rejoin
  with, so a fresh join call mints a brand-new random UUID instead of
  resuming the existing session. Asked the user whether to add the minimal
  frontend fix now or document the gap and move on;
  chose to fix. Landed as: `ConnectedEvent` gains a `Balance` field (the
  same narrow per-connection disclosure `WagerAcceptedEvent` already makes
  for its own placer), populated via a new `round.Service.Balance`
  passthrough exposed on the widened `ws.Sessions` interface, consumed by
  `lib/roundState.ts`'s `"connected"` case as the balance source of truth
  on every connect instead of the cached snapshot.

## What Worked

- The full backend suite (`make test`, `-race -p 1`) passes clean after
  every task boundary.
- `npx vitest run` (109 tests) and `npx tsc --noEmit` pass clean on the
  frontend after the balance-sync fix and its fixture updates.
- **Manual end-to-end verification of Task 6's boundary, via a throwaway
  Playwright script** (not committed — deleted after use): registered a
  host, created a room, joined a guest, placed a 400-token wager (buy-in
  1000 → 600), reloaded the guest's page. Observed via the real WS frame
  and the rendered DOM: `BEFORE WAGER = 1000`, `AFTER WAGER = 600`,
  `AFTER RELOAD = 600`. The reconnect's `connected` frame explicitly
  carried `"balance":600`.
- Repeated the previously-flaky disconnect/reconnect WS tests 15–30× under
  `-race` after the `room.go` fix — zero failures, versus roughly 50%
  failure/hang before it.

## What Didn't Work

- **First attempt at verifying Task 6's manual check failed** — reload
  showed 1000, not 600 — before I'd actually implemented the
  `ConnectedEvent.Balance`/reducer fix. Not a dead end, just the RED half
  of that fix's own verification; recorded here only so it's clear the
  first Playwright run was a real RED, not a fluke.
- **Assumed a stale Next.js production build might explain an unexpected
  re-failure of the same Playwright check — it didn't.** No server was
  left running between runs (`ps`/`lsof` showed nothing on 3000 or 8080
  after either run), so Playwright's `webServer` really was rebuilding from
  scratch each time. The actual cause was simpler: the reducer fix hadn't
  been applied yet at that point in the sequence. Don't waste time chasing
  a "stale build" theory here again — verify the actual code state first.
- **Ran `go test ./internal/ws/ ./internal/round/ ./internal/httpapi/` once
  without `-p 1`** while cross-checking the interface-widening changes —
  got spurious failures (`redis: client is closed`, a guest balance
  "not found") from the three packages' shared Redis DB 15 racing each
  other's `TestMain` `FLUSHDB`. `CLAUDE.md` already documents why `-p 1` is
  load-bearing; this was just forgetting to pass it when running a subset
  of packages by hand instead of via `make test`.
- **`TestEndToEndRound` flaked (~1 in 5) during the coverage run** —
  `ReadMessage() waiting for "round_opened": i/o timeout`. Root cause:
  the new `sessions.Balance` lookup on connect (a real Redis round trip)
  had been placed *before* `hub.Join`, widening the window between a
  client's WS handshake completing (which is all `Dial()` waits for) and
  its actual room membership landing — long enough, occasionally, for it
  to miss a `round_opened` broadcast sent by another already-joined client
  in that gap. This was a real correctness regression from my own
  Task 6 follow-up work, not test flakiness to route around. Fixed by
  moving the `Balance` lookup to after `hub.Join` — `ResumeSession` alone
  (mutex-only, no I/O) stays ahead of `hub.Join`, matching the plan's
  original ordering intent for that call specifically. Confirmed clean
  over 8 separate fresh-process runs after the fix (each `-count=1`
  invocation resets `TestMain`'s `FLUSHDB`/rate-limit state, unlike
  repeating the same binary with `-count=20`, which exhausts the `auth`
  rate limit instead and produces a *different*, unrelated set of
  failures — don't mistake that for the same bug if it comes up again).

## Test Coverage

- **Covered:** every RED→GREEN checkpoint the plan specified (11 across
  Tasks 1–6), plus three unplanned regression tests added for the two
  deviations above: `TestBroadcastAfterReapDoesNotHang`,
  `TestHandlerConnectedEventCarriesBalance`, and the roundState "replaces a
  stale cached balance" case.
- **Not covered by any automated test, deliberately:** the browser-refresh
  path remains manual-only per the plan — Playwright proved it once (numbers
  above) but that script was throwaway, not added to `frontend/e2e/`.
- **Not covered, stated rather than closed (unchanged from the plan's own
  self-review):** a payout credited to an already-departed player lands in
  a resurrected wallet field and is never folded. Today's behavior exactly;
  this phase neither fixes nor worsens it.

## Open Questions / Blockers

- None. `security-reviewer` returned clean (no CRITICAL/HIGH/MEDIUM/LOW)
  across all five surfaces: login timing, session-fold ordering, Kafka
  bounds, the `room.go` goroutine-leak fix, and `ConnectedEvent.Balance`'s
  disclosure scope. Full report transcribed into
  `docs/project-history.md`'s new Phase 7c section.

## Relevant Commits

- `ae22795` — feat: add a constant-cost decoy password verify
- `4206f76` — fix: pay argon2id's cost on the unknown-email login path
- `08560b0` — fix: bound a Kafka message's size before decoding it
- `6a7325e` — fix: cap the payout count a settlement message may carry
- `de40bb2` — feat: add ClearSession to claim and clear a room session
- `77829d1` — fix: clear a session's Redis state when its result is folded
- `f9962eb` — fix: make EndSession a no-op on an already-ended session
- `676b131` — feat: fold a disconnected session after a grace window
- `5a8803a` — feat: cancel a pending session end when a player reconnects
- `65c69a9` — fix: stop a room's Leave/Broadcast/Members/Count from hanging after it reaps
- `741026a` — refactor: schedule a session end on disconnect instead of folding
- `bc96bfa` — feat: cancel a pending session end when a socket reconnects
- `abc5128` — feat: carry a reconnecting client's current balance in the connected event
- `0513ec3` — fix: sync the room page's displayed balance from the connected event
- `cad8b8f` — docs: rewrite the README around an architecture diagram
- `9ea4916` — fix: look up a connecting client's balance only after it joins the room
- (pending) — docs: close the three deferred security items and record Phase 7c

## Next Step

Hand off to `finishing-a-development-branch` for the `--no-ff` merge of
`phase-7c-security-debt-docs` into `dev`. Only Phase 8 (parked, no plan yet)
follows.
