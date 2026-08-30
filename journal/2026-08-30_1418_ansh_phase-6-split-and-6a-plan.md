# 2026-08-30 — ansh — Split Phase 6 into 6a/6b, fixed the frontend stack, and planned the frontend shell

**Status:** Phase 5b is merged and `dev` is clean. Phase 6 is split into 6a (frontend shell) and 6b (gameplay UI) in the parent plan §9, the frontend stack is fixed as TypeScript + App Router, and `docs/plans/2026-08-30-phase-6a-frontend-shell.md` is written and committed — 9 tasks, 26 checkpoints, ~67 lines/checkpoint. No implementation started; no `phase-6a-*` branch exists yet.
**Decided:** Split 6a/6b **at the same transport seam the backend already uses**, so each frontend half consumes exactly one backend half — 6a ends at an authenticated socket showing a live presence roster (the client counterpart of Phase 4a's game-knowledge-free transport), 6b is rounds/wagers/odds/settlement over it. The seam is concrete, not thematic: 6a builds `lib/socket.ts` with `on(type, handler)`, and 6b registers gameplay handlers on that same table without reopening the transport.
**Spec:** No change to the product spec. `docs/plans/2026-08-21-implementation-plan.md` §9 amended — Phase 6 row replaced by 6a/6b rows, Phase 7's dependency retargeted `5b, 6` → `5b, 6b`, and a split note added covering the seam, the backend task, and the stack decision.
**Next:** Execute Phase 6a — `git checkout -b phase-6a-frontend-shell dev`, then `executing-plans` from Task 1 (backend origin admission).
**Blocked on:** Nothing. Two execution-time environment risks are pre-recorded in the plan (Playwright vs. no-sudo; `create-next-app` interactivity) with concrete contingencies.
**Touches:** `docs/plans/2026-08-30-phase-6a-frontend-shell.md`, `docs/plans/2026-08-21-implementation-plan.md`, `backend/internal/httpapi/`, `backend/internal/ws/handler.go`, `backend/internal/config/config.go`

---

## What We Worked On

Session opened as a get-up-to-speed request after the Phase 5b merge, then
turned into Phase 6's planning pass. Three threads: survey the backend
contract a frontend would actually have to consume, settle the decisions
`CLAUDE.md` and the parent plan had left open against Phase 6, and write the
6a plan.

## Decisions Made

Four, all reflected in the plan's own "Decisions This Plan Fixes" section —
recorded there rather than re-argued here.

- **Stack: TypeScript + App Router + Tailwind.** Resolves the open question
  `CLAUDE.md` recorded against the `typescript` rule pack imported in
  `1a2c2f2` ("confirm or correct this when Phase 6's plan fixes the actual
  stack"). Confirmed as assumed, so the rule pack stays.
- **Split before the task breakdown, not after.** This is the parent plan's
  own phase-sizing note being followed for the third time (Phases 4 and 5
  were the first two). Phase 6 as written bundled nine separable
  deliverables — more than Phase 3 carried when it became the most expensive
  phase measured at 2,904 lines.
- **The CORS fix lands as 6a Task 1, not a separate micro-phase.** It is a
  prerequisite of the frontend deliverable and is unverifiable without a
  browser client to prove it against, so it belongs with the thing that
  proves it. A whole planning pass for three checkpoints of middleware is
  ceremony the project has consistently declined.
- **Token storage: `sessionStorage`, two separate keys, with the XSS
  trade-off stated rather than hand-waved.** Picked over an httpOnly cookie
  specifically because enabling CORS in Task 1 would otherwise open a CSRF
  surface at the same moment. Routed explicitly to `security-reviewer` at
  phase close instead of being assumed to pass.

## What Worked

- **Reading the backend contract before planning against it caught two real
  blockers that no doc recorded.** `grep -i "cors\|Access-Control"` over the
  Go tree returns nothing, and `internal/ws/handler.go:14` is a bare
  `websocket.Upgrader{}` — gorilla's zero value, whose default `CheckOrigin`
  rejects any upgrade whose `Origin` host differs from `Host`. A Next.js dev
  server on `:3000` would have failed at *both* doors on the first browser
  request of Phase 6. Neither appeared in the spec, the parent plan, or
  `CLAUDE.md`.
- **The same sweep confirmed the half that already worked**, which mattered
  as much: `internal/ws/handler.go:89` already reads a `?token=` query
  parameter precisely because browsers cannot set headers on a WebSocket
  handshake. So the plan needed no backend change there — worth knowing
  before writing a task for it.
- **A zero-churn design for Task 1 came out of counting call sites first.**
  `ws.Handler` has six existing call sites and `NewMux` sixteen. Making the
  origin allowlist a trailing variadic `opts ...HandlerOption` (the
  functional-options pattern `.claude/rules/ecc/golang/patterns.md` already
  prescribes) leaves all six compiling untouched, and wrapping CORS *outside*
  the mux in `cmd/api` leaves `NewMux`'s signature and all sixteen callers
  alone. Wrapping outside is also the only correct option, not just the
  cheapest: a preflight is an `OPTIONS` to a path registered `POST`-only, so
  `http.ServeMux` answers 405 before any per-route middleware runs.
- **Writing the tasks surfaced a data-flow gap the file structure had
  hidden.** Task 8 renders the session balance, but the `connected` socket
  event carries only `user_id`/`display_name`/`room_id`/`guest` — the balance
  and the partial-buy-in flag exist only in the REST join response, one
  navigation earlier. Fixed by adding `RoomSummary` + a third checkpoint to
  Task 4 (storage owns it) and amending Task 6's two contracts to write it,
  rather than letting Task 8 invent its own storage. Caught at plan time,
  which is the cheap place.
- **`writing-plans`' delegation check got its first real use** (added
  2026-08-28, unexercised until now). It produced a clean answer: Tasks 3 and
  4 delegated (mechanical layers over contracts the plan states in full),
  Tasks 1 and 9 explicitly inline (security surface; acceptance evidence),
  Task 2 inline for an environment reason the criteria don't mention — it
  shells out to `create-next-app` over the network in WSL2, which a cold
  subagent can't troubleshoot from the plan alone.

## What Didn't Work

Nothing was tried and abandoned — this was a survey-and-plan session with no
implementation. One design was considered and rejected on paper rather than
attempted: changing `NewMux`'s return type to `http.Handler` so it could
wrap itself in CORS. Rejected because it touches sixteen callers to buy
nothing the `cmd/api` wrapper doesn't already give, and because preflight
handling has to sit outside the mux regardless.

## Test Coverage

No code was written, so no coverage changed. What the plan commits to:

- **Covered by design:** 26 checkpoints, each a genuine RED→GREEN cycle with
  exact inputs and exact expected outputs. Backend keeps its existing gates
  (`go vet`, `gofmt -l`, `-race -cover -p 1`). Frontend adds an 80% floor via
  `vitest run --coverage` over `lib/**` and `components/**`.
- **Deliberately excluded from the frontend floor:** `app/**` route files —
  thin wiring, the same allowance `cmd/*` already has on the Go side.
- **Not covered:** everything in Phase 6b. `lib/protocol.ts` deliberately
  declares no odds or wager types yet.

## Open Questions / Blockers

- **Playwright vs. no sudo.** `npx playwright install --with-deps` will fail
  in this environment — it shells out to the system package manager, and
  `CLAUDE.md` records that no sudo is available. Task 9 specifies `npx
  playwright install chromium` (user-local browser only) and carries an
  explicit contingency if Chromium still won't launch, including a ban on
  reporting an unexecuted E2E as passing. Unresolved until Task 9 actually
  runs.
- **`create-next-app` is interactive by default** and needs network. Task 9's
  sibling risk: the plan gives non-interactive flags plus instructions to map
  them via `--help` if a flag has been renamed, rather than falling back to
  prompts that would stall a non-interactive shell.
- **The `sessionStorage` decision is deliberately not final** — it is written
  down with its trade-off so `security-reviewer` can overturn it at phase
  close rather than rubber-stamp it.

## Relevant Commits

- `7f9d041` — docs: split Phase 6 into 6a/6b and plan the frontend shell
  (both the parent-plan §9 amendment and the new 6a plan, committed together
  so the executor starts from a clean tree)

## Spec Changes

None to `docs/specs/2026-08-21-callit-design.md`. Parent-plan changes only,
listed in the **Spec** line above.

## Next Step

Execute Phase 6a. Branch `phase-6a-frontend-shell` off `dev` and run
`executing-plans` from Task 1 — the backend origin admission, which must land
before any browser check or the Task 9 E2E can work at all. Tasks 2–8 can be
built against stubbed `fetch`/`WebSocket` without a running backend; only
Task 9 needs the live stack.

Two things to note in that session's journal entry: whether the delegation
tagging on Tasks 3 and 4 held up in practice (this plan is the first written
*with* the check rather than tagged after the fact), and the measured token
cost of the two delegated tasks against the inline ones.
