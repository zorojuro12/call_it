# 2026-08-30 — ansh — Corrected the stale Phase 6a entry, then planned Phase 6b

**Status:** `dev` is clean and Phases 0–6a are merged and marked ✅. Two commits this session: `e9a0c7e` repairs the Phase 6a journal entry, which was finalized against an integration decision the user reversed two minutes later; `d451cc7` adds `docs/plans/2026-08-30-phase-6b-gameplay-ui.md` — 10 tasks, 29 checkpoints, ~53 lines/checkpoint, Tasks 4–9 tagged for delegation. No implementation started; no `phase-6b-*` branch exists yet.
**Decided:** Do **not** split Phase 6b. Every deliverable in it (host console, wager pad, live odds, countdown, bettors counter, settlement reveal, Web Audio) converges on one screen driven by one state machine, so any cut line yields a half that can't be demonstrated on its own — which violates the parent plan §9's "each phase ends in something runnable and verifiable." Recorded in the plan's Self-Review §5 rather than argued again here.
**Spec:** No change to `docs/specs/2026-08-21-callit-design.md` and none to the parent plan. The 6b plan *proposes* three backend amendments (Task 1) but commits none — they land when the phase executes.
**Next:** Execute Phase 6b — `git checkout -b phase-6b-gameplay-ui dev`, then `executing-plans` from Task 1 (the three backend socket-contract gaps), which must land before any gameplay UI can render correctly.
**Blocked on:** Nothing.
**Touches:** `journal/2026-08-30_1554_ansh_phase-6a-execution.md`, `docs/plans/2026-08-30-phase-6b-gameplay-ui.md`

---

## What We Worked On

Opened as a get-up-to-speed request plus "read the Phase 6 journal entry."
Reading it surfaced that it contradicted the git history, so fixing it became
the first task; planning 6b was the second.

## Decisions Made

- **Fixed the 6a entry in place rather than appending a correction entry.** The
  entry's summary block is what a future session reads on resume (this skill's
  own design), so a correction living in a *different* file would leave the
  misleading version as the one actually read. Amended the six stale spots and
  added a `Decisions Made` bullet naming the reversal, because the reverted
  state is still visible in the history (`d13ab86` removes the ✅, `4e9ac61`
  restores it) and would otherwise read as a contradiction.
- **Phase 6b carries three backend gaps as Task 1, not a separate micro-phase** —
  the same call 6a made for CORS, for the same reason: each is a prerequisite of
  the frontend deliverable and none is verifiable without a browser client.
- **The `host` discriminator becomes a JWT claim, not a client-side flag.**
  Considered storing `is_host` in `RoomSummary` (free, and the server enforces
  anyway via `ErrNotHost`/`ErrHostCannotBet`), but that makes "which REST call
  did I make earlier" a second source for a fact the server already knows —
  the shape `CLAUDE.md` rejects elsewhere. Counting issue sites made it cheap:
  only two (`internal/room/service.go:81,147`), so the claim is symmetric with
  the existing `guest` and costs no per-connection Redis read. Enforcement stays
  in Redis; the claim is advisory-for-rendering only.
- **Delegated 6 of 10 tasks** (4–9: countdown, and the four presentational
  components, plus Web Audio) against 6a's 2 of 9. Tasks 2–3 stay inline as the
  phase's flagship correctness work, Task 1 inline as a security surface,
  Task 10 inline as acceptance evidence.

## What Worked

- **Reading the backend contract before planning against it found three real
  gaps**, exactly as the 6a planning pass did with CORS and the bare
  `websocket.Upgrader{}`. None appeared in the spec, the parent plan, or
  `CLAUDE.md`:
  1. **No host/player discriminator reaches the client.** `RoomSummary.guest:
     false` is not "is host" — an account holder who *joins* also gets
     `guest: false`. `app/room/[code]/page.tsx` renders identically for both.
  2. **The router discards the authoritative post-wager balance.**
     `wager.Accepted.Balance` is computed and thrown away
     (`_, err := r.wagers.Place(...)`, `internal/ws/router.go`), so a player has
     no server-anchored balance after wagering. `cmd/callit-cli` didn't reveal
     this because it's a raw protocol dump that never tracks balance.
  3. **`round.ErrInvalidSpec` is unmapped** in `replyServiceError`, so it falls
     through to the default and a host who types one outcome gets "an internal
     error occurred."
- **Grepping for the fact *before* choosing a design settled the host question
  cheaply.** The instinct was that adding a JWT claim would have a wide blast
  radius; `grep 'Claims{'` returned exactly four non-test sites, two of which
  are the room-token issuers. That turned a rejected option into the chosen one.
- **Distinguishing the two refund paths at plan time.** `round_resolved` with
  `refunded: true` (nobody backed the winner — `results[]` *is* populated) and
  `round_refunded` (host-disconnect fallback — `RoundID` and `Total` only, no
  per-player rows) are different events with different payloads. Conflating them
  would have had the settlement component inventing rows the server never sent.
  Task 8 CP2 now tests them apart.
- **Pinned a counting trap.** `redisstore.PlayerCount` is already
  `HLEN wallets - 1` (`internal/redisstore/room.go:187`), so `OddsEvent.players`
  *already* excludes the host. The obvious frontend mistake is subtracting again;
  Task 5 CP3 has a test asserting the rendered output contains no such value.

## What Didn't Work

- **Writing Go struct tags into the plan through a quoted heredoc mangled
  them.** `cat > file <<'EOF'` does no expansion, so the escaped backticks in
  `Host bool \`json:"host"\`` landed literally as backslash-backtick in the
  markdown — three lines corrupted. Caught by grepping `'\\`'` right after
  writing. Fixed by moving the struct into a fenced ```go block and rewriting
  the two prose mentions as "a `Host bool` field tagged `json:\"host\"`". Worth
  remembering: **backticks inside a quoted heredoc cannot be escaped** — there
  is no escape processing to consume the backslash. Put backtick-bearing code in
  a fenced block, or write the file with a different tool.
- **The self-review's own checkpoint count was wrong** — it claimed 30, the file
  has 29. Caught by counting with grep rather than trusting the prose, then
  corrected. A plan that miscounts itself is a small thing, but it's the kind of
  number a later session quotes without re-deriving.
- **The plan's security-review checkpoint originally said `git add -u`**, which
  contradicts this project's own "name exact paths, never `-A`/`.`" rule.
  Rewritten to instruct the executor to list what it actually changed, since
  which files a review touches isn't knowable when the plan is written.

## Test Coverage

No code was written this session, so no coverage moved. What the 6b plan
commits to:

- **Covered by design:** 29 checkpoints, each a genuine RED→GREEN cycle with
  exact inputs and exact expected outputs. `lib/roundState.ts` is held to
  **100% statements** at its task boundary — it's pure and fully enumerable, so
  a gap there is a real gap, not wiring. Backend keeps its existing gates.
- **Not covered:** Phase 7's k6 load work and the §7 latency targets. The plan
  explicitly leaves `domain.Multipliers`' empty-pool return value unpinned and
  specifies the *client's* display rule against the client's own input instead,
  so the test holds whatever the server sends.

## Open Questions / Blockers

None blocking. Three risks are pre-recorded in the plan's Known Risks with
contingencies: the E2E test needs a real 3-second lockout wait (raise that one
assertion's timeout, don't lengthen the lock); jsdom has no Web Audio (hence
Task 9's injectable factory, with `vi.mock` as the fallback); and
`domain.Multipliers`' empty-pool behavior is deliberately not depended on.

## Relevant Commits

- `e9a0c7e` — docs: correct the Phase 6a journal entry to reflect the merge
- `d451cc7` — docs: plan Phase 6b, the gameplay UI

## Next Step

Execute Phase 6b. Branch `phase-6b-gameplay-ui` off `dev` and run
`executing-plans` from Task 1 — the three backend socket-contract gaps, which
gate everything after them (the host can't be told apart from a player, and no
balance updates, until they land). Tasks 2–9 can be built against stubbed
sockets with no backend running; only Task 10 needs the live stack.

Two things to note in that session's entry: whether delegating 6 of 10 tasks
actually beat 6a's 2 of 9 on token cost, and whether the reducer-plus-
presentational-components split held up when the room page was finally wired,
or whether state leaked back into the components.
