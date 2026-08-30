# 2026-08-30 — ansh — Executed Phase 6a (frontend shell) end to end, Tasks 1–9 through the E2E checkpoint

**Status:** All 9 tasks of `docs/plans/2026-08-30-phase-6a-frontend-shell.md` are implemented and committed on `phase-6a-frontend-shell`. Backend CORS/origin admission (Task 1), the full `frontend/` scaffold (Tasks 2–8: typed REST/WS clients, session storage, register/login, room create/join, presence roster), and the two-browser Playwright E2E test (Task 9 CP1) are all green. Task 9 CP2 (docs) is this commit. **CP3 (security review) has not run yet** — that's the immediate next step, not yet started.
**Decided:** Fixed a real backend gap found by the E2E test *in this phase*, not by amending the plan first — `internal/ws/handler.go` only ever broadcast future `player_joined`/`player_left`, so a client joining second never learned who was already in the room. User explicitly approved fixing it inline under Task 9's own "genuine integration defect" allowance rather than pausing to write a plan amendment first.
**Spec:** No product spec change. Parent plan (`docs/plans/2026-08-21-implementation-plan.md`) §9 amended: 6a row marked ✅, and a new paragraph records the backend fix as a same-phase amendment discovered by the E2E test.
**Next:** Run the `security-reviewer` agent (Task 9 CP3) over `git diff dev...HEAD`, specifically the CORS middleware, `ws.WithAllowedOrigins`, and the `sessionStorage` token-storage decision this plan flagged as not-yet-final. Then `finishing-a-development-branch` to merge into `dev`.
**Blocked on:** Nothing.
**Touches:** `backend/internal/config/`, `backend/internal/httpapi/cors.go`, `backend/internal/ws/handler.go`, `backend/cmd/api/main.go`, `frontend/` (all of it — new this phase), `docker-compose.yml`, `Makefile`, `.github/workflows/ci.yml`, `README.md`, `CLAUDE.md`, `docs/plans/2026-08-21-implementation-plan.md`, `docs/project-history.md`

---

## What We Worked On

Picked up mid-branch: the phase-6a plan already existed from the prior session (split from Phase 6, stack fixed as TypeScript + App Router). This session was pure execution — `executing-plans` end to end, Task 1 through Task 9's first two checkpoints.

## Decisions Made

- **Fixed the newcomer-roster backend gap inline, in Task 9, rather than stopping to amend the plan first.** Asked the user directly (three options: fix now, stop and re-plan, or show the diff first); they chose "fix now." Reasoning recorded in the parent plan's new amendment paragraph rather than repeated here.
- **Redis's compose host port made configurable via `REDIS_HOST_PORT`** (default unchanged at `6379`). This machine hit a Hyper-V dynamic port exclusion claiming 6379 after a Windows/WSL restart — not a code bug, but blocked `make up` outright. User explicitly preferred remapping the port over the admin-PowerShell `net stop winnat` fix.
- **Split the E2E checkpoint's commit in two** (`fix: tell a newcomer...` then `test: prove two browsers...`) rather than one bundled commit, keeping the backend correctness fix and the frontend acceptance test as separately revertible units — matches this project's "one behavior, one commit" convention even though the plan's own checkpoint text implied a single commit.

## What Worked

- **Delegation (Tasks 3 and 4, per the plan's header) ran clean.** Task 3 (typed protocol + REST client): 3 commits, one per checkpoint, 96,520 subagent tokens, 30 tool calls, ~167s. Task 4 (session storage): 3 commits, one per checkpoint, 73,309 subagent tokens, 23 tool calls, ~110s. Both matched the plan's `Produces` interfaces exactly — no rework needed in later tasks. **This is the first real exercise of `writing-plans`' delegation-tagging check** (added 2026-08-28, unused until this plan) — it correctly flagged the two mechanical layers and correctly left Task 1 (security surface) and Task 2 (network-dependent scaffold) inline.
- **Task 3's subagent caught its own over-implementation before committing** — it built CP1's client with CP2/CP3's error-handling and bearer-token support already in, realized that would false-pass CP2/CP3's RED steps, and reverted before committing. Reported honestly in its own DEVIATIONS section.
- **The E2E test found a real, pre-existing backend bug** the entire rest of the test suite had never exercised: `internal/ws/handler.go` never told a second-joining client about players already in the room. Every other WS test's "second client" scenario happened to check message *ordering* from the first client's perspective, never what the *second* client itself learned about pre-existing occupants. Fixed with a 6-line addition reusing the existing `player_joined` message type (no protocol change), verified against a new dedicated test plus two updated existing ones (`TestPresenceJoin`, `TestPresenceLeave`), full `internal/ws` suite green afterward (93.6% coverage, up from 93.1%).
- **A `data:` URL smoke test caught that Chromium genuinely launches in this WSL2 environment** before writing the real E2E spec — the plan's own contingency (manual two-window fallback, `continue-on-error` CI) was never needed. `npx playwright install chromium` (no `--with-deps`, since there's no sudo here) downloaded and ran cleanly first try.
- **`net test` discipline held under real integration pressure**: 60/60 frontend tests, full backend suite green, 97.4% frontend statement coverage over `lib/**`+`components/**`, both comfortably above the 80% floor.

## What Didn't Work

- **Repeatedly over-implemented a checkpoint's later contract ahead of schedule, inline, twice** — login page (CP1 quietly included CP2's error-handling/`ErrorBanner`) and the join-by-code page (CP2 quietly included CP3's error-handling). Each time, this was caught only by explicitly writing the *next* checkpoint's test and finding it passed with zero new code — a genuine "no RED" signal, not a false alarm. Fixed both times the same way: `git stash` the not-yet-committed next-checkpoint test additions, `git reset --soft HEAD~1` to un-commit the over-scoped checkpoint, strip the extra code back to the checkpoint's actual contract, re-verify and recommit, then restore the stashed test and continue. This is the same failure mode Task 3's delegated subagent caught *itself* doing once (see above) — worth naming as a real, recurring temptation when a later checkpoint's shape is already obvious while writing an earlier one, inline or delegated.
- **Two silent environment/tooling snags, not plan defects:** (1) `getByText("Ann")` failed to match `<li>Ann (you)</li>` because the `" (you)"` suffix was a second JSX text-expression, producing two separate text nodes — RTL's exact-text matcher only matches a single node's full text. Fixed by using a regex matcher, same pattern the plan itself uses elsewhere for compound text. (2) The room page's roster test intermittently showed *stale* DOM state after firing multiple synchronous fake-WebSocket messages in sequence without `act()` — only the first `await findByText` flushed a render; every synchronous `fire()` call after it queued a state update that never got painted before the next assertion ran. Fixed by wrapping the test's `fire()` helper in `act()`.
- **Vitest picked up `e2e/join.spec.ts` as one of its own test files** (matches its default include glob) once Playwright's spec existed, and failed loudly since it calls Playwright's `test()`, not Vitest's. Needed an explicit `exclude: ["e2e/**"]` in `vitest.config.ts` — not mentioned anywhere in the plan, discovered only once both frameworks' files coexisted.
- **`next build` (which sets `NODE_ENV=production`) started failing once a page imported `lib/config.ts`'s module-level `API_BASE_URL` export**, because CI's `frontend` job (added back in Task 2, before any page imported it) never set `NEXT_PUBLIC_API_BASE_URL`. This is the *designed* fail-fast behavior working as intended — the gap was that CI's build step didn't supply the required value once Task 6+ pages started actually reaching that code path. Fixed by adding the env var to CI's build step and to Playwright's `webServer` command.
- **Hyper-V's dynamic port exclusion range silently claimed 6379** on this machine after a Windows/WSL restart, blocking `make up` before Task 1 could even run its tests. Not a code issue; recorded in `docs/project-history.md`'s Environment verification log, with the `REDIS_HOST_PORT` workaround now in `docker-compose.yml`.
- **A quoting bug in `playwright.config.ts`'s backend `webServer` command**: `export PATH=$PATH:...` without quotes broke because this WSL2 environment's inherited Windows `PATH` contains space-laden fragments (`Program Files`, etc.), which the unquoted expansion word-split into garbage `export` arguments. Fixed with `export PATH="$PATH:..."`.

## Test Coverage

- **Backend:** full suite green (`go vet`, `gofmt -l`, `go test ./... -race -cover -p 1`). `internal/httpapi` 92.4%, `internal/ws` 93.6% (up from 93.1% — the new roster-snapshot test), everything else unchanged from Phase 5b.
- **Frontend:** 60/60 Vitest tests green. Coverage over `lib/**`+`components/**`: 97.43% statements, 95.91% branches, 97.05% functions, 97.39% lines (`@vitest/coverage-v8`). `app/**` route files excluded by design, same allowance `cmd/*` has.
- **E2E:** 1/1 Playwright test green — two isolated browser contexts, one room, host creates, guest joins, both see each other, guest leaving drops them from the host's roster.
- **Not yet run:** the `security-reviewer` pass (Task 9 CP3) — CORS middleware, WS origin check, and the `sessionStorage` token-storage decision are all still open for that review, not yet independently checked.

## Open Questions / Blockers

None blocking. Two things the next session (or the rest of this one) should close:
- Task 9 CP3 (security review) — required by `CLAUDE.md` before any phase touching auth/money/a network surface can close, and this phase opens a new network surface (CORS) and introduces browser-side token storage.
- The `sessionStorage` XSS trade-off (plan's Decisions §2) is explicitly *not* final — flagged for the security reviewer to accept or overturn, not assumed to pass.

## Relevant Commits

All on `phase-6a-frontend-shell`, this session, in order:
- `eb343e0` — chore: parameterize Redis's compose host port
- `fe8df49`, `32f26e1`, `2fd4a8a`, `4fe62dd` — Task 1 (browser origin admission), 4 checkpoints
- `d1e5b08`, `f119a4d` — Task 2 (Next.js scaffold + Vitest harness), 2 checkpoints
- `fb2fe55`, `787deb8`, `82d70c2` — Task 3 (typed protocol + REST client), delegated, 3 checkpoints
- `960426d`, `032742a`, `b3c3a0e` — Task 4 (session token storage), delegated, 3 checkpoints
- `08980e9`, `c95015d`, `b619771` — Task 5 (register/login pages), 3 checkpoints
- `5eb1a81`, `763e988`, `2096ef3` — Task 6 (create/join a room), 3 checkpoints
- `348b5e4`, `685decb`, `94d8fb6` — Task 7 (typed WS client), 3 checkpoints
- `4c35518`, `becdf22` — Task 8 (room page + presence roster), 2 checkpoints
- `25ca2e6` — fix: tell a newcomer about players already in the room
- `7731dcc` — test: prove two browsers share one room end to end
- `8bffb83` — fix: keep vitest and playwright out of each other's way

## Spec Changes

None to `docs/specs/2026-08-21-callit-design.md`. Parent-plan §9 amended (6a row ✅, backend-gap amendment paragraph) — see Decided/Spec above.

## Next Step

Run `security-reviewer` over `git diff dev...HEAD` per Task 9 CP3, directed at the four points the plan names (CORS allow-origin correctness, `WithAllowedOrigins` missing-header allowance, the `sessionStorage` trade-off, token never reaching a log/URL). Fix CRITICAL/HIGH findings, record MEDIUM/LOW in `docs/project-history.md`, then hand off to `finishing-a-development-branch`.
