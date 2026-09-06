# 2026-08-26 — ansh — Post-merge security review finds and fixes a WS rate-limit gap

**Status:** `phase-4a-ws-transport` was merged into `dev` (by the user, outside this session) and pushed. A follow-up `security-reviewer` pass against the merged code found one CRITICAL finding — `GET /api/v1/socket` had no rate limiting, unlike every other authenticated route — which has now been fixed and pushed directly to `dev`.
**Decided:** The reviewer's own suggested fix (`RequireAuth(d.Issuer)(apiThrottle(d.Store)(...))`) was wrong for this codebase and was not applied as-is: `RequireAuth`'s `verifyBearer` only reads the `Authorization` header, but the socket route also accepts `?token=` (required for browsers, which can't set headers on a WS handshake — Amendment C2 in the 4a plan). Wrapping with it would have broken query-param auth. `apiThrottle` also keys off `ClaimsFrom(ctx)`, populated only by `RequireAuth`/`OptionalAuth` — used alone it would bucket every caller under one empty-string key. Built a purpose-fit `wsThrottle` middleware instead, reusing the existing shared limiter (`RateLimit`/`Store.Allow`, `CLAUDE.md`'s "one limiter, every call site" invariant) with a `wsThrottleKey` that keys by `user:<id>` on a verifiable token (via a newly-exported `ws.ExtractToken`, shared with `ws.Handler` itself) and falls back to `ip:<addr>` otherwise.
**Spec:** No change to the design spec.
**Next:** None outstanding from this session — Phase 4b (round lifecycle) is next when the user starts it, per the prior session's journal entry.
**Blocked on:** Nothing.
**Touches:** `backend/internal/httpapi/ws_handlers.go` (new `wsThrottle`/`wsThrottleKey`), `backend/internal/httpapi/ws_handlers_test.go` (new tests), `backend/internal/ws/handler.go` (`extractToken` → exported `ExtractToken`).

---

## What We Worked On

Picked up after the prior session's Phase 4a execution. The user mentioned they'd already merged `phase-4a-ws-transport` into `dev` (a merge I hadn't done and wasn't asked to do in that session — the earlier instruction had been "commit and push, don't merge"). Before that, I'd flagged a workflow gap: per `docs/dev-workflow-guide.md` §5 and `.claude/rules/ecc/common/code-review.md`'s mandatory trigger, auth-touching code (the JWT verification at the WS handshake) should get a `security-reviewer` pass, same as Phase 3 got before its close-out — Phase 4a's plan never called for one and I hadn't run one either.

User asked for the security review to run and for `dev` to be pushed. Ran both. The review's one CRITICAL finding held up on manual verification (grep confirmed `apiThrottle`/`RequireAuth` wrap every other authenticated route but not the socket route), but its suggested one-line fix didn't — see Decided above. Built and TDD'd a correct fix instead, per the user's choice to commit it directly to `dev` (not a phase-style branch) since it's a small post-merge patch.

## Decisions Made

See **Decided** above for the main call. Secondary: chose a dedicated `ws_connect` rate-limit scope (30/min) rather than folding WS connection attempts into the existing `api` scope (60/min, shared with REST calls) — a WebSocket connection holds two goroutines and a buffer for its whole life, unlike a single REST request, so it gets its own budget rather than competing with a user's REST quota.

## What Worked

- `security-reviewer` agent run against `internal/ws/handler.go`, `client.go`, `room.go`, and the `httpapi`/`cmd/api` wiring — confirmed JWT/HS256 verification, message-size limits, read/write deadlines, slow-client eviction, and graceful shutdown all sound; one CRITICAL (missing throttle), nothing HIGH/MEDIUM/LOW.
- `wsThrottleKey` tested directly as a pure function (valid token → `user:<id>`, missing/garbage token → `ip:<addr>`) — no Redis needed for that test.
- Exhaustion behavior (429 on the 3rd request past a small test-scale limit) verified by composing `RateLimit` + `wsThrottleKey` directly, mirroring the existing `TestRateLimit` pattern — avoided needing 31 real WS dials to prove the production 30/min limit.
- Wiring itself confirmed via a plain (non-upgrade) request through the real mux, checking for the `X-RateLimit-Limit` response header — proves `wsThrottle` is actually in the chain, not just defined and unused.
- Full suite green: `internal/ws` 96.2%, `internal/httpapi` 92.0% coverage; `make lint`/`make build` clean.

## What Didn't Work

- Tried to verify the rate-limit fix's headers by inspecting `resp.Header` on a **successful** WebSocket dial (via `gorilla/websocket`'s `DefaultDialer.Dial`) — doesn't work. Gorilla's `Upgrader.Upgrade` writes the 101 response by hijacking the connection and only includes headers passed via its own `responseHeader` parameter (which `ws.Handler` passes as `nil`); it does not consult whatever was set on the `http.ResponseWriter` via `w.Header().Set(...)` beforehand, since that path is bypassed once hijacked. Rate-limit headers set by `RateLimit` middleware upstream of `ws.Handler` are real and correctly set, but invisible on a successful upgrade's response — they only surface on a request that fails to upgrade (error paths use the normal `http.Error`/`ResponseWriter` path, not the hijack). Switched the wiring-confirmation test to a plain non-upgrade GET request instead, where the recorder does capture them.

## Test Coverage

- **Covered:** `wsThrottleKey`'s dual keying behavior (valid/missing/garbage token cases), rate-limit exhaustion → 429 with the shared `RateLimit` mechanism, and route-wiring (headers present on the real mux).
- **Not covered yet:** No test exercises the production 30-request/minute limit end-to-end through 31 real dials — deliberately, matching how `authThrottle`'s real 10/min limit isn't end-to-end tested elsewhere in this codebase either. The exhaustion logic itself (shared `RateLimit`) already has full coverage from its own test suite.

## Relevant Commits

- `fe6dde7` — `fix: rate-limit WebSocket connection attempts on the socket route` (direct commit to `dev`, not a phase branch, per user's explicit choice this session).

## Next Step

Nothing queued. Next real work is Phase 4b (round lifecycle) whenever the user starts it — its plan (`docs/plans/2026-08-26-phase-4b-round-lifecycle.md`) already exists and was checked by the planning-side session against what actually landed in 4a (commit `bc6e510`, "verify Phase 4b's plan against 4a's landed interfaces, no revision needed").
