# 2026-08-30 — ansh — Phase 6b execution: gameplay UI, merged into dev

**Status:** Phase 6b is done and merged. `phase-6b-gameplay-ui` merged into
`dev` with `--no-ff` (`6571fbe`), 35 commits, all 10 plan tasks complete.
Branch kept (not deleted) per explicit instruction. Full backend suite
(`go vet`, `gofmt`, `go build`, `go test -race -cover -p 1`), frontend suite
(108 tests, 97.6% coverage, lint, `tsc`), and the full Playwright E2E suite
(join + round) were all green on the branch immediately before merging; the
merge itself was a clean fast-forward-free merge with no conflicts.
**Decided:** Executed per `docs/plans/2026-08-30-phase-6b-gameplay-ui.md`
exactly as planned — Tasks 1–3 inline, Tasks 4–9 delegated one-subagent-per-
task via `delegating-plan-tasks`, Task 10 inline. Security review (5 required
points) found no CRITICAL/HIGH; approved as-is.
**Spec:** No change to the design doc or parent plan's architecture — the
parent implementation plan's 6b row and Known Amendments were updated to
describe what shipped (not marked ✅; that's this session's merge, recorded
separately). `docs/project-history.md` gained a new "Amendments discovered
mid-phase" section for Task 1's three backend gaps, plus a Phase 6b security
review entry.
**Next:** Phase 7 (load test + hardening) is next per the parent plan, once
the user is ready. Nothing else outstanding from 6b.
**Blocked on:** Nothing.
**Touches:** `backend/internal/{auth,room,wager,ws}`, `frontend/lib/{roundState,countdown,audio,protocol}.ts`,
`frontend/components/{OddsBoard,WagerPad,HostConsole,SettlementReveal}.tsx`,
`frontend/app/room/[code]/page.tsx`, `frontend/e2e/round.spec.ts`,
`CLAUDE.md`, `docs/plans/2026-08-21-implementation-plan.md`,
`docs/project-history.md`.

---

## What We Worked On

Picked up from the prior session's plan (`docs/plans/2026-08-30-phase-6b-gameplay-ui.md`,
written cold in a separate planning pass) and executed it task by task via
`executing-plans`: Task 1's three backend socket-contract gaps, Task 2/3's
pure `roundState.ts` reducer, six delegated presentational/utility tasks
(4–9), and Task 10's page wiring, E2E proof, docs, and security review.

## Decisions Made

- **Two real defects were found and fixed during execution, not anticipated
  by the plan** — both surfaced only once the reducer/hooks were wired into
  a real page rather than exercised in isolation:
  1. `RoundState` never stored `winning_outcome`, so `SettlementReveal`
     (which has a dedicated slot for the winning label, and whose own
     Task 8 test asserts it) had nothing real to render. Fixed by adding
     `winning_outcome: number | null` to `RoundState`, set on
     `round_resolved`, reset on `round_opened`/`round_refunded`. Caught
     while wiring Task 10's page, before committing the wiring.
  2. `useCountdown`'s `getSnapshot` read `Date.now()` fresh on every call.
     Task 4's own unit tests used fake timers so this never surfaced there;
     Task 10's page-level test uses real timers, and `useSyncExternalStore`
     detected the non-deterministic snapshot as "tearing" and looped,
     producing "Maximum update depth exceeded". Fixed by caching the
     remaining-time value in a ref, updated only inside `subscribe`'s
     interval tick — `getSnapshot` now just reads the cache.
  Both were fixed as their own commits, separate from the page-wiring
  commit that exposed them, matching this project's existing convention
  (6a's newcomer-roster fix, this plan's own CP2 guidance for the e2e test).
- **A pre-existing test bug was also found and fixed, unrelated to 6b's
  feature work**: `internal/auth`'s `flipLastChar` test helper flipped a
  base64 *character* (`'a'` ↔ `'b'`) rather than the underlying signature
  *bytes*. Those two characters decode to the same meaningful bits (differing
  only in a padding bit ignored by the decoder), so the tamper test could
  silently pass without actually corrupting the signature. Adding the `host`
  JWT claim changed the HMAC output enough that this run's last byte hit
  exactly that case, turning a latent bug into a real, reproducible failure.
  Fixed by decoding to bytes, flipping a real bit, and re-encoding.
- **Delegation continued at 6 of 10 tasks delegated** (vs. 6a's 2 of 9).
  One subagent (Task 4) hit a genuine plan contradiction — a 200ms countdown
  tick can't land on the RED test's exact 500ms assertion — and correctly
  stopped and escalated rather than picking a number itself, per
  `delegating-plan-tasks` Rule 4. Resolved by the parent (this session):
  100ms tick, keep the test's exact values.
- **Self-correction inside delegated tasks worked as intended.** Three
  subagents (6, 7, 8) caught themselves pre-implementing a later
  checkpoint's behavior while writing an earlier one, reverted to the
  minimal prior-checkpoint state, re-ran to confirm genuine RED, then
  reimplemented — preserving true TDD sequencing without the parent having
  to catch it.
- **Environment: the Hyper-V dynamic-port-exclusion issue (recorded in
  `docs/project-history.md`) recurred this session** — `docker compose up`
  refused port 6379 *and* 6380 with "ports are not available... forbidden by
  its access permissions". Worked around by remapping Redis to host port
  16379 (`REDIS_HOST_PORT=16379 docker compose up -d redis`) and exporting
  `REDIS_ADDR=localhost:16379` for every backend test/Playwright run this
  session. Not re-documented as a new gotcha since the existing entry
  already covers the class of problem and its fix.

## What Worked

- **The reducer's 100%-statement-coverage requirement caught real gaps
  immediately.** Removing the `default: return state` case from
  `reduceRound`'s switch (dead code once all 7 action types are explicitly
  handled) let TypeScript prove exhaustiveness at compile time and closed
  the coverage gap in one line — better than adding a test for
  unreachable code.
- **The delegation return contract (COMMITS/INTERFACES/DEVIATIONS/DEFECTS,
  <300 words) kept every task's verification cheap.** The parent re-ran only
  `vitest run && lint && tsc && git log --oneline dev..HEAD` after each
  delegated task — never re-reading the subagent's work — and every commit
  log matched the reported COMMITS section exactly, across all 6 delegated
  tasks.
- **The E2E test passed on the first real run** (`e2e/round.spec.ts`), no
  fix loop needed beyond the two defects already caught by the earlier unit
  test (CP1's page-level RTL test), which is exactly the ordering the plan
  intended — cheap unit-level feedback before the expensive full-stack run.

## What Didn't Work

- **A naive `getByText("Home")` assertion in the CP1 page test failed with
  "multiple elements found"** — "Home" legitimately renders twice (the
  `OddsBoard` table cell and the `WagerPad` outcome button). Fixed by
  scoping to `getByRole("button", { name: "Home" })`.
- **A custom `findByText` predicate matching `el.textContent === "Ann (you)"`
  also matched multiple elements** — `PresenceRoster` apparently marks the
  viewer's own roster entry with the same `(you)` suffix `SettlementReveal`
  uses, so both a roster `<li>` and a settlement `<td>` legitimately produce
  that exact text. Fixed by scoping the assertion to the settlement table
  specifically (`.closest("table")`), not by weakening the match.
- **Running `npx tsc`/`npm run lint`/`npx vitest` without an explicit `cd
  frontend` intermittently picked up the repo root instead** (shell cwd did
  not reliably persist as expected across tool calls this session), once
  producing a stray `node_modules/.vite` cache directory at the repo root.
  Removed as unrelated debris before committing; worth always prefixing
  frontend commands with an explicit `cd` rather than relying on prior `cd`
  state persisting.

## Test Coverage

- **Covered:** `lib/roundState.ts` at 100% statements (the plan's own bar,
  since it's pure and fully enumerable). Frontend overall 97.6%
  statements / 93.6% branches / 100% functions across `lib/**` and
  `components/**`. Backend: all touched packages green under
  `-race -cover -p 1`; `internal/auth` 93.6%, `internal/ws` 94.5%,
  `internal/wager` 84.6%.
- **Not covered:** Phase 7's k6 load work, as the plan states explicitly.
  `domain.Multipliers`'s empty-pool return value stays deliberately unpinned
  server-side; the client's own display rule (`pools[i] === 0` → `—`) is
  what's tested, and holds regardless of what the server sends.

## Open Questions / Blockers

None. Three risks the plan pre-recorded in Known Risks all resolved cleanly
in practice: the E2E lockout wait needed no timeout adjustment (3s lock,
default Playwright timeouts sufficed); jsdom's missing Web Audio was handled
by Task 9's injectable factory exactly as planned, no `vi.mock` fallback
needed; `domain.Multipliers`'s empty-pool behavior was never depended on.

## Relevant Commits

35 commits on `phase-6b-gameplay-ui` (`7f2612a`..`59e6ef5`), merged as
`6571fbe`. Highlights beyond the plan's own checkpoints:
- `e553daa` — fix: make the forged-signature test actually corrupt the
  signature bytes (pre-existing test bug, surfaced by the `host` claim)
- `193a219` — fix: track the winning outcome so settlement can show which
  one won (RoundState gap found while wiring Task 10)
- `552d14a` — fix: cache the countdown snapshot instead of reading the
  clock in getSnapshot (useSyncExternalStore tearing bug found the same way)
- `6571fbe` — Merge phase-6b-gameplay-ui into dev

## Next Step

Phase 7 (load test + hardening) is next per the parent plan's §9 table, when
the user is ready to start it. No planning has begun for it yet this session.
