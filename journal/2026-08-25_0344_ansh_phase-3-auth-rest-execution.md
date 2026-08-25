# 2026-08-25 — ansh — Phase 3 auth + REST execution

**Status:** Phase 3 fully implemented, verified, and **ready to merge** — all 12 tasks / 38 checkpoints done on `phase-3-auth-rest`. `make down && make up && make test && make lint && make build` all green from a cold-started Redis. Not yet merged into `dev` — that's `finishing-a-development-branch`'s call, next.
**Decided:** No new architectural decisions beyond the plan's own Amendments B1–B7 (already recorded in the plan's header and folded back into the parent plan/spec this session). The real decisions this session were mid-execution fixes to defects the plan didn't anticipate — see What Didn't Work.
**Spec:** Updated — `docs/specs/2026-08-21-callit-design.md` §3 gained the Redis-not-Postgres-yet note and the "opening stake doesn't debit persistent balance" clarification (Amendment B1); §6 gained the token-carries-display-name note (Amendment B5).
**Next:** Hand off to `finishing-a-development-branch` to decide the merge. Assuming self-merge per convention, Phase 4 (WebSocket hub + round lifecycle) gets its own `writing-plans` pass next — no new tooling needed per plan §9.
**Blocked on:** Nothing.
**Touches:** `backend/internal/auth/`, `backend/internal/account/`, `backend/internal/room/`, `backend/internal/redisstore/{user,room,ratelimit}.go`, `backend/internal/httpapi/*`, `backend/scripts/lua/{claim_unique,rate_limit,top_up_balance}.lua`, `backend/cmd/api/main.go`, `Makefile`, `.github/workflows/ci.yml`, `docs/plans/2026-08-21-implementation-plan.md`, `docs/specs/2026-08-21-callit-design.md`, `CLAUDE.md`

---

## What We Worked On

Executed the Phase 3 plan (`docs/plans/2026-08-25-phase-3-auth-rest.md`,
written last session) end to end via `executing-plans`, task by task:

1. Dependencies (argon2, JWT, uuid), config's `JWT_SECRET`/`JWT_TTL`.
2. `internal/auth` — argon2id hashing, credential validation, JWT issue/verify (HS256-pinned, alg:none/HS512 rejected).
3. `internal/redisstore` — `claim_unique.lua` + account storage, the `CreateRoom` collision fix.
4. The shared sliding-window rate limiter (`rate_limit.lua` + `Store.Allow`/`Revoke`).
5. Refill claims (`top_up_balance.lua` + `internal/account.ClaimRefill`, including the no-op-returns-quota race).
6. `internal/httpapi`'s wire contract — envelope, error-mapping table, bearer-token middleware, rate-limit throttle.
7. Registration and login endpoints, including the no-enumeration fix.
8. `internal/room` — code generation, creation, the `JoinRoom` rejoin-preserves-balance fix (Amendment B4), guest and account-holder joining.
9. The refill endpoint, `Retry-After` handling, and `cmd/api/main.go`'s dependency wiring + route throttles.
10. Coverage check, a `security-reviewer` agent pass, folding amendments back into the parent plan/spec/CLAUDE.md, and this entry.

## Decisions Made

- Everything architectural was already decided in the plan (Amendments B1–B7) — see `docs/plans/2026-08-25-phase-3-auth-rest.md`'s header, now also folded into the parent plan and spec.
- **CP5's test narrative was arithmetically inconsistent with its own contract** — the plan's prose said the race's losing call "succeeds crediting 0," but Checkpoint 5's own Step 3 contract returns `domain.ErrRefillNotEligible` (an error) for that case. Implemented per the contract (the concrete, actionable part); rewrote the test around the contract instead of the prose.
- **`-p 1` added to `make test`/`make test-unit`/CI**, not in the original plan at all — became necessary once a second Redis-DB-15 integration-test package existed. See What Didn't Work.

## What Worked

- Every checkpoint followed real RED→GREEN; four checkpoints legitimately passed on first write because an earlier checkpoint's implementation already satisfied them (Task 5 CP2, Task 7 CP2, Task 9 CP2, Task 10 CP7) — each verified as a genuine RED by disabling the relevant guard, confirming failure, then restoring, per the observable-signal rule.
- The security-critical checkpoints all landed clean on the first real attempt once implemented: HS256-pin/alg:none rejection (Task 4 CP4), the login no-enumeration collapse (Task 9 CP4), 500s leaking nothing (Task 8 CP3), rate limiter fail-closed, `X-Forwarded-For` ignored.
- End-to-end smoke test against a running server: healthz → register → create room → guest join, plus the fail-fast check (no `JWT_SECRET` → exits 1, names the missing var).
- `security-reviewer` agent pass: no CRITICAL/HIGH findings; one LOW (hardcoded `"3"` instead of `domain.RefillQuota` in a header) fixed on the spot.
- Coverage: `auth` 93.5%, `httpapi` 91.6%, `redisstore` 82.4% (own-package), `config`/`domain` 100%. `account`/`room`'s per-package numbers (29%/50%) looked alarming but are a Go coverage-tool measurement artifact — `-coverpkg=./...` shows their actual methods (exercised via `httpapi`'s black-box tests) at 82–86%, genuinely solid.

## What Didn't Work

- **`go mod tidy` run immediately after `go get` stripped all three new dependencies from `go.mod`**, since nothing imported them yet — exactly the pitfall the Phase 2 journal already warned about, and the plan's own Task 1 Step 1 still had `go mod tidy` literally in the command sequence. Fixed by re-running `go get` without `tidy`.
- **A rate-limit-window eviction test passed even with `ZREMRANGEBYSCORE` disabled**, because the key's own `PEXPIRE` TTL happened to expire the whole key at the same moment a full-window sleep would have, masking the missing eviction logic entirely. Redesigned to keep the key alive via continued traffic (a second call partway through the window) while one member individually ages out — only this version actually goes RED when eviction is disabled.
- **`internal/account`'s new `TestMain` doing its own `FLUSHDB` on Redis DB 15 raced with `internal/redisstore`'s identical `TestMain`** once `go test ./...`'s default package-level parallelism ran both test binaries concurrently — one package's flush could wipe another's in-flight test data. Same race would have hit `room` and `httpapi` too, once they gained their own `TestMain`s. Fixed with `-p 1` in the Makefile — and CI's `go test` invocation was still missing it entirely (never had `-p 1`), a latent CI flake caught while updating CLAUDE.md, not while writing the fix itself.
- **Task 7 CP5's single-racing-pair test was near-unfalsifiable in practice**: empirically, the specific code path it exists to test (`credited==0` after passing a stale eligibility check) fired ~49/50 times when raced in a warm loop, but ~0/5 times for a single pair against a freshly-connected `Store` — a cold connection pool doesn't interleave the way the checkpoint assumes. Redesigned as 20 independent races in one warm loop, asserting the aggregate invariant (net exactly one quota slot consumed per race) rather than depending on any single pair's timing.
- **Several of my own test helpers derived unique IDs/emails from `t.Name()` via `testID()`, and long subtest names + `NormalizeEmail`'s full lowercasing (including the local part) pushed the generated email over the 64-byte local-part limit** — hit this three separate times (`TestRegister_Rejections`, `TestJoinRoom_Account`'s balance subtests, `TestClaimRefill`'s unknown-field subtest) before recognizing the pattern. Fix each time was shortening the subtest name; worth remembering for Phase 4's tests too.
- **The IP-keyed `auth` rate-limit throttle, once wired into `NewMux`, broke test isolation across the whole `httpapi` package** — `httptest.NewRequest` gives every request the same synthetic `RemoteAddr`, so all register/login calls across every test in the package shared one 10/minute bucket and started 429ing each other once the package's cumulative call count crossed 10. Fixed by giving every such test request a fresh synthetic `RemoteAddr` via a shared helper.

## Test Coverage

- **Covered:** All of Tasks 1–11's checkpoints — argon2id + credential validation + JWT (auth), account storage + rate limiter + refill top-up (redisstore/account), the full HTTP wire contract including auth middleware and throttling, registration/login with no-enumeration, room creation/joining (guest and account-holder, with the rejoin-preserves-balance fix), and the refill endpoint with `Retry-After`. Concurrency-sensitive paths (`Allow`/`Revoke`, `ClaimRefill`'s no-op race, rate-limit window sliding) run under `-race`.
- **Not covered yet:** Session-end profit/loss application (Phase 4 owns session close) and the wager-placement throttle's actual call site (Phase 4 owns the WebSocket handshake — the limiter it will call already exists and is tested). Login timing side-channel (accepted, Phase 7). `-coverpkg=./...` wasn't wired into `make test`/CI itself, only run manually this session to verify `account`/`room`'s true coverage — future sessions re-deriving this should rerun it rather than trusting the per-package number.

## Open Questions / Blockers

- None blocking. Two accepted-open items carried forward from the security review: login timing (Phase 7 hardening) and room-code modulo bias (accepted permanently, not deferred).
- Phase 5's open question (B1): do persistent accounts stay in Redis or migrate to PostgreSQL alongside the ledger? Recorded in both the parent plan §9 and spec §3, not decided here.

## Relevant Commits

50 commits on `phase-3-auth-rest` (branched off `dev` at `2c00eb6`), from `7674db7` (chore: pin argon2, JWT, and UUID dependencies and enable Redis AOF) through `5dab986` (docs: fold Phase 3 amendments into CLAUDE.md's conventions). Full list via `git log --oneline phase-3-auth-rest ^dev`. Against the plan's own estimate of ~44, the delta is entirely the five defects listed under What Didn't Work, each fixed and documented in its own commit rather than silently folded in.

## Spec Changes

- `docs/specs/2026-08-21-callit-design.md` §3 — one bullet on persistent accounts living in Redis until Phase 5 (Amendment B1), and a clarification that joining a room doesn't debit the persistent balance up front (only the net delta at session end does).
- §6 — one bullet noting the JWT carries the display name (Amendment B5), so no per-message database lookup is needed for identity.
- `docs/plans/2026-08-21-implementation-plan.md` §3 (added `internal/account`), §4 (added `user`/`email` keys, annotated `ratelimit` scopes), §5 (added the three new Lua script contracts), §9 (marked Phase 3 done, added Phase 4 and Phase 5 notes).

## Next Step

Hand off to `finishing-a-development-branch` for the merge decision — per `CLAUDE.md`, the expected choice is self-merge into `dev` with `--no-ff`. After that, Phase 4 (WebSocket hub + round lifecycle) gets its own `writing-plans` pass; no new tooling to install first per plan §9, and everything Phase 4 needs from Phase 3 (room/account services, the rate limiter, the token issuer) already exists and is tested.
