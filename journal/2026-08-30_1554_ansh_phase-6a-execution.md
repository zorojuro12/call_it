# 2026-08-30 — ansh — Executed Phase 6a (frontend shell) end to end, all 9 tasks through security review, merged into `dev`

**Status:** All 9 tasks of `docs/plans/2026-08-30-phase-6a-frontend-shell.md` are implemented, tested, and committed on `phase-6a-frontend-shell` — backend CORS/origin admission (Task 1), the full `frontend/` scaffold (Tasks 2–8), the two-browser Playwright E2E test (Task 9 CP1), docs (CP2), and the security review (CP3, no CRITICAL/HIGH, two LOW fixed on the spot) are all done. Full backend suite, 60 frontend tests at 97.4% coverage, and the E2E test all green on the final boundary check. **Merged into `dev`** (`24296fe`, `--no-ff`). The standard 3-option integration menu (`finishing-a-development-branch`) was offered and first answered "keep the branch as-is"; the user reversed that a couple of minutes later in the same session, after this entry had already been finalized against the earlier answer. The branch was pushed to `origin` and then merged.
**Decided:** Fixed a real backend gap found by the E2E test *in this phase*, not by amending the plan first — `internal/ws/handler.go` only ever broadcast future `player_joined`/`player_left`, so a client joining second never learned who was already in the room. User explicitly approved fixing it inline under Task 9's own "genuine integration defect" allowance rather than pausing to write a plan amendment first.
**Spec:** No product spec change. Parent plan (`docs/plans/2026-08-21-implementation-plan.md`) §9 amended: a paragraph records the backend fix as a same-phase amendment discovered by the E2E test. **The 6a row is marked ✅** (`4e9ac61`). This doc's own convention ties ✅ to "merged into `dev`," so an early pass that marked it before the integration decision existed was premature and got reverted (`d13ab86`) — the mark went back on only once the merge actually landed.
**Next:** Phase 6b (gameplay UI). No `6b` plan exists yet — it needs its own `writing-plans` pass before any code, same as every prior phase.
**Blocked on:** Nothing.
**Touches:** `backend/internal/config/`, `backend/internal/httpapi/cors.go`, `backend/internal/ws/handler.go`, `backend/cmd/api/main.go`, `frontend/` (all of it — new this phase), `docker-compose.yml`, `Makefile`, `.github/workflows/ci.yml`, `README.md`, `CLAUDE.md`, `docs/plans/2026-08-21-implementation-plan.md`, `docs/project-history.md`

---

## What We Worked On

Picked up mid-branch: the phase-6a plan already existed from the prior session (split from Phase 6, stack fixed as TypeScript + App Router). This session was pure execution — `executing-plans` end to end, Task 1 through Task 9's first two checkpoints.

## Decisions Made

- **Fixed the newcomer-roster backend gap inline, in Task 9, rather than stopping to amend the plan first.** Asked the user directly (three options: fix now, stop and re-plan, or show the diff first); they chose "fix now." Reasoning recorded in the parent plan's new amendment paragraph rather than repeated here.
- **Redis's compose host port made configurable via `REDIS_HOST_PORT`** (default unchanged at `6379`). This machine hit a Hyper-V dynamic port exclusion claiming 6379 after a Windows/WSL restart — not a code bug, but blocked `make up` outright. User explicitly preferred remapping the port over the admin-PowerShell `net stop winnat` fix.
- **Reversed the integration decision after the fact.** The `finishing-a-development-branch` menu was answered "keep the branch as-is," and this entry plus the parent plan's ✅ removal were both committed against that answer (`d13ab86`). Two minutes later the user chose to merge after all, so `--no-ff` merge (`24296fe`) and ✅ restoration (`4e9ac61`) followed, and this entry was amended to match. Recorded because the reverted state is still visible in the commit history and would otherwise read as a contradiction.
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
- **Security review (Task 9 CP3):** ran `security-reviewer` over `git diff dev...HEAD`, directed at the four points the plan named. No CRITICAL or HIGH. All four verdicts hold: CORS allow-origin correctness (no wildcard, credentials only paired with a specific echoed origin), `WithAllowedOrigins`' missing-`Origin` allowance (safe — a browser can't suppress it), the `sessionStorage` trade-off (XSS-readable but tab-scoped and never auto-attached, so enabling CORS didn't open a CSRF hole), and the token never reaching a log/error/rendered URL. Two LOW findings fixed on the spot: unencoded room code in a REST path (`encodeURIComponent`), and an undocumented assumption behind `Access-Control-Allow-Credentials: true` (added a comment naming it). Full findings and dispositions in `docs/project-history.md`'s Phase 6a security-review section.

## Open Questions / Blockers

None. The `sessionStorage` XSS trade-off (plan's Decisions §2) was explicitly not assumed to pass — the security review confirmed it holds as designed, so this is resolved, not deferred.

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
- `f3ecd88` — docs: record Phase 6a's stack decision, targets, and origin invariant
- `2f1132f` — fix: address Phase 6a security review findings
- `d13ab86` — docs: correct the premature 6a ✅ marking and finalize the journal entry
- `24296fe` — Merge phase-6a-frontend-shell into dev (`--no-ff`, after the integration decision was reversed)
- `4e9ac61` — docs: mark Phase 6a merged into dev (the ✅ restored, this time earned)

## Spec Changes

None to `docs/specs/2026-08-21-callit-design.md`. Parent-plan §9 amended (backend-gap amendment paragraph, plus the ✅ marking removed and then restored — see Decided/Spec above).

## Next Step

Phase 6b (gameplay UI): host console, wager pad, live odds, lockout countdown, aggregate bettors counter, settlement reveal, Web Audio. It depends only on 6a and imports no new tooling, but it has no plan yet — write one with `writing-plans` before touching code.

The binding constraint to carry into that plan is the anonymity invariant: 6b renders the reveal **only** from `round_resolved`, and the sole permitted in-round progress signal is the aggregate bettors count (`SCARD` over `round:{roundID}:bettors`, host excluded from the denominator). No per-user wager state may be reconstructed client-side.
