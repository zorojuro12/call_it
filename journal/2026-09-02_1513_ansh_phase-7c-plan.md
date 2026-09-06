# 2026-09-02 — ansh — Phase 7c planning: the three deferred security items + README

**Status:** Phase 7b merged into `dev` (`2453f2f`), working tree clean. Phase
7c is planned but not started — no `phase-7c-*` branch exists yet. The plan is
committed to `dev` as `22e3978` so the executing session starts clean.
**Decided:** The reconnect item is **two bugs, not one**, and the plan closes
both: a 30-second grace window (the recorded item) *plus* a once-only fold
(the "session ends twice" MEDIUM from Phase 4b's review, same root cause).
`Store.ClearSession` claims a session via the **opening stake's** `HDEL` reply,
never the wallet's — `settle_round.lua` can resurrect a deleted wallet field
with `HINCRBY`, so the wallet cannot serve as a claim token.
**Spec:** No change yet — the plan's Task 8 rewrites §4's
"no reconnect-with-session-resume" known-limitation bullet, which this phase
makes false.
**Next:** Execute `docs/plans/2026-09-02-phase-7c-security-debt-docs.md` with
`executing-plans` — `git checkout -b phase-7c-security-debt-docs dev` first.
**Blocked on:** Nothing.
**Touches:** `docs/plans/2026-09-02-phase-7c-security-debt-docs.md` (new),
and — as planned, not yet written — `backend/internal/{auth,account,events,redisstore,round,ws,httpapi}/`,
`README.md`, `CLAUDE.md`, `docs/specs/2026-08-21-callit-design.md`,
`frontend/lib/socket.ts`.

---

## What We Worked On

Resumed from the Phase 7b execution entry, confirmed 7b was already merged
(its "Next" line — merge into `dev` — had been done), answered how much work
remains after 7c, then ran a full `writing-plans` pass for Phase 7c.

Phase 7c is the last *committed* phase: 8 tasks, 13 commits, closing the three
security items this project has carried open by design since Phase 5b, plus
the README and architecture diagram. Only Phase 8 follows it, and Phase 8 is
explicitly parked ("Decide when unblocked" — LLM question suggestions,
Terraform, Prometheus/Grafana).

## Decisions Made

Seven decisions are recorded in the plan's own "Decisions This Plan Fixes"
section rather than restated here. The three that changed the shape of the
phase:

- **The grace window lives in process memory, not Redis** — reason: the
  WebSocket hub is already in-process (every client of one room must share an
  API instance for `Broadcast` to reach them), so an in-process timer adds no
  single-instance assumption the architecture doesn't already make. Revisit
  with a multi-instance hub, never separately.
- **Claim-then-credit, not credit-then-claim** — reason: a crash between the
  two loses a session result; the reverse order mints tokens on the retry, and
  this codebase's invariants forbid minting.
- **No frontend auto-reconnect this phase** — reason: it would be a fourth
  deliverable, which is exactly what the parent plan's phase-sizing note warns
  against. A page refresh already exercises the grace window end to end
  (`app/room/[code]/page.tsx:34` reads the room token from `sessionStorage`),
  so the fix delivers user-visible value with no frontend change. Recorded as
  a Phase 8 candidate; Task 8 corrects the now-stale comment in
  `lib/socket.ts` that cites the limitation being removed.

## What Worked

- Reading the actual code before planning caught the two-bugs-not-one problem.
  `JoinRoom` uses `HSETNX` (`redisstore/room.go:114`), so a stale wallet
  survives a fold and a second disconnect folds the same loss again. A plan
  written from the security item's one-line description ("resume needs a grace
  window") would have shipped a grace window over a still-broken fold.
- Checking `session_test.go:162` before designing `EndSession`'s new error
  handling. It pins `EndSession(never-joined) == ErrNotFound`. Ordering the
  reads so `store.User` comes first keeps that assertion green while an
  already-ended *session* becomes a `(0, nil)` no-op — an unknown user stays a
  bug signal, which is the sharper semantic anyway. No test churn.

## What Didn't Work

- **Considered and rejected: making the fold idempotent by rewriting the
  opening stake to the current balance.** It works arithmetically (a second
  fold computes a zero delta) but breaks the reconciliation identity
  `redis_wallet − opening_stake == ledger_balance` for an ended session — the
  left side goes to 0 while the ledger still holds the session's net
  movements. Clearing both fields instead leaves the identity meaningful for
  every session it is actually checked against (a live one).
- **Considered and rejected: a pure timing test as the only evidence for the
  login fix.** Timing alone is flaky as a unit test. The plan pairs a fast
  deterministic test of the decoy primitive (it must parse as PHC under the
  *current* argon2 parameters) with one ratio-based timing test at the account
  layer. The ratio, not an absolute threshold — both paths scale together
  under machine load, and the pre-fix gap is ~2 orders of magnitude against a
  2× assertion.
- **Considered and rejected: a hardcoded decoy hash constant.** If
  `argon2Memory`/`argon2Time` are ever raised, a committed constant keeps
  burning the old parameters' cost and quietly reopens the timing gap. Deriving
  it once at first use via `sync.OnceValue` re-costs it automatically.

## Test Coverage

- **Covered by the plan:** 11 RED→GREEN checkpoints across Tasks 1–6, each
  with an exact input→output/error spec and a named observable signal.
- **Not covered by any automated test, deliberately:** the browser-refresh
  path. No test in this repo drives a real browser through a reload, so Task
  6's boundary requires a manual check with before/after balances recorded in
  the execution journal.
- **Not covered, stated rather than closed:** a payout credited to an
  already-departed player lands in a resurrected wallet field and is never
  folded. This is today's behavior exactly — the phase neither fixes nor
  worsens it. Phase 8 candidate.

## Open Questions / Blockers

- None blocking execution. One product question deferred: what settlement owes
  a player who left before the round resolved.

## Relevant Commits

- `22e3978` — docs: plan Phase 7c — the three deferred security items plus the README

## Next Step

Execute the plan with `executing-plans`, inline, branching
`phase-7c-security-debt-docs` off `dev`. Tasks 1–2 are independent of the
rest and can land first; Tasks 3→4→5→6 are a strict dependency chain
(`ClearSession` → once-only fold → grace window → socket wiring).
`security-reviewer` runs in Task 8 before the phase closes — this phase
touches auth, money movement, and a network surface, all three of `CLAUDE.md`'s
triggers.
