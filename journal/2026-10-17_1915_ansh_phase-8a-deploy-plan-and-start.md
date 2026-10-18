# 2026-10-17 — ansh — Phase 8a deploy plan, v0.1.0 baseline, Tasks 1–2

**Status:** Phase 8a paused on `phase-8a-backend-deploy` after Tasks 1–2 of 8; full `make test` green; 5 commits, not pushed.
**Decided:** Deploy free: Oracle Cloud Always Free (arm64, 2 OCPU / 12 GB) + DuckDNS + Caddy TLS. Reasoning: plan `docs/plans/2026-10-17-phase-8a-backend-deploy.md`, "Decisions This Plan Fixes".
**Spec:** No change yet. §8's "live deployment deferred" is lifted by the parent plan's Phase 8 split note; the spec text itself gets updated in 8a's Task 8.
**Next:** Decide on the e2e email-collision fix and on keeping `logic-check.spec.ts`; then resume `executing-plans` at Task 3 (container image).
**Blocked on:** Nothing. Docker Desktop is running and reachable from WSL as of this session.
**Touches:** `docs/plans/2026-10-17-phase-8a-backend-deploy.md`, `docs/plans/2026-08-21-implementation-plan.md` §9, `backend/internal/config/`, `backend/internal/httpapi/{middleware,health,ws_handlers,auth_handlers}.go`, `backend/cmd/callit-cli/`, `README.md`

---

## What We Worked On
The goal moved from a local demo to a polished public release that ends on
the iOS App Store via Expo. This session researched hosting, froze the
current state, planned the backend deploy, and started executing it.

## Decisions Made
- **Expo means a new React Native app, not a wrapper.** The Next.js frontend
  can't be packaged as an iOS app, and Apple guideline 4.2 rejects
  repackaged websites. The framework-free `frontend/lib/` modules
  (`protocol`, `api`, `roundState`, `countdown`, most of `socket`) carry
  over. Order: backend deploy (8a), then web (8b), then Expo (8c), then App
  Store readiness (8d, including mandatory account deletion).
- **Free hosting over paid.** The user wants $0 for now. Recorded tradeoffs:
  - Oracle free capacity can be unavailable in a region.
  - Oracle reclaims a VM idle for 7 days (CPU p95, network and memory all
    under 20%).
  - DuckDNS is a volunteer service with no service guarantee, and its
    hostname looks less polished.
  - Recommendation: buy a domain (~$10–15/yr) before the App Store
    submission. The hostname is one value (`CALLIT_HOSTNAME`), so the swap
    is cheap.
- **Tag `v0.1.0`, not a branch, to freeze the pre-deploy state**, because a
  tag can't move.
- **Phase 8 split into 8a–8d; Terraform dropped.** See the parent plan §9
  "Phase 8 split" note.
- **Trusted-proxy client IP is part of 8a, not a follow-up.** Behind Caddy
  every peer is Caddy, so `ClientIP` (RemoteAddr only) would put the whole
  site in one `auth` bucket of 10 per minute. The rule is the rightmost
  untrusted `X-Forwarded-For` entry, and only from a peer listed in
  `TRUSTED_PROXIES`. Caddy's docs say it "sets or augments" the header, so
  the design doesn't depend on Caddy stripping client values.

## What Worked
- **Playwright fact-check of the hosting claims.** Headless Chromium from
  `frontend/node_modules` loaded each provider's own pages:
  - Confirmed: Oracle needs a credit card and reclaims idle VMs; DuckDNS is
    free; `caddy-dns/duckdns` exists; `apache/kafka` publishes
    `linux/arm64`; Cloudflare quick tunnels are test-only (200 in-flight
    request cap, no uptime guarantee); Apple's program costs $99/yr;
    guideline 4.2's "repackaged website" text.
  - Not verified: Hetzner prices (the pricing table didn't render), and the
    "17+ rating for simulated gambling" claim.
- **`main` updated from `dev`.** A merge commit (`71e78ea`), no squash:
  `main` has 434 commits, 0 of `dev`'s missing.
- **Task 1's wiring checkpoint showed a real behavioral RED.** With
  `Deps.TrustedProxies` added but not wired, client B behind the same proxy
  got 429 on both the login and socket routes. After wiring, both passed.
- **`make test` fully green** after Task 1. `httpapi` coverage is 93.5%,
  `config` 92.4%, `domain` still 100%.
- **App-logic check through the real UI, with Playwright** (plan paused for
  it). An ad-hoc spec, `frontend/e2e/logic-check.spec.ts` (uncommitted), ran
  one host and two guests, with `cmd/relay` and `cmd/ledger-worker` running
  against the dev stack. All passed:
  - The host gets no wager pad.
  - Odds show pool totals only: 100/300 pools give 4.00× and 1.33×, and the
    counter reads "2/2 players".
  - No per-player row appears before the reveal, and no `staked` field
    appears in any WebSocket frame before it.
  - Lockout closes betting.
  - Payout: Bo gets floor(300×400/300) = 400, ending at 1100. Ann ends at
    900. Dust is 0.
  - Nobody-backed-the-winner refund: Ann goes 850 → 900.
  - Ledger for the room: Ann −100, Bo +100, round pool 0, and all 5
    transactions balanced. This is the reconciliation identity
    `redis_wallet − opening_stake == ledger_balance`, confirmed live.
- **The auth rate limiter works.** Rapid repeated e2e runs from localhost
  hit 429 "rate limit exceeded" after 10 registrations per minute, as
  designed.

## What Didn't Work
- **I said Oracle Always Free is 4 OCPU / 24 GB.** Wrong: Oracle's docs say
  2 OCPU / 12 GB (the older figure is out of date). Caught by the Playwright
  check. It's still enough for this stack.
- **I said local `main` held only the initial commit.** Wrong: local `main`
  was stale, and `origin/main` had two PR merge commits (#1, #2) carrying no
  file changes. The `--ff-only` fast-forward failed and was replaced with a
  `--no-ff` merge. Always `git fetch` before reasoning about a remote
  branch's state.
- **The plan said Tasks 1–2 were Docker-free.** Wrong for Task 1:
  `internal/httpapi`'s `TestMain` requires Redis for the whole package. The
  plan was amended (`df47805`); only Task 2 is Docker-free.
- **The existing e2e suite is flaky (about 1 run in 10): a test bug, not an
  app bug.** `join.spec.ts` and `round.spec.ts` run in parallel workers and
  both build the host email as `host-${Date.now()}@e2e.test`. When they
  start in the same millisecond they register the same email, and one gets
  a correct `409`. Evidence:
  - The trace shows a single POST.
  - Redis holds email keys a few milliseconds apart (`…918353` and
    `…918358`).
  - With `--workers 1` and unique emails, 40 runs in a row passed.

  Fix (not applied yet): use `crypto.randomUUID()` in both specs' emails.
- **First ledger query in the logic check was wrong:** it filtered
  `accounts.room_id`, but `user_wallet` accounts are per user (their
  `room_id` is NULL); the room is on `transactions.room_id`.
- **The first commit of `b88db87` failed gofmt** (struct field alignment)
  and was amended before any push. Every commit command now chains
  `test -z "$(gofmt -l …)"`.

## Test Coverage
- **Covered:**
  - `TestLoadTrustedProxies`: 8 cases, including catch-all rejection and
    bare-address rejection.
  - `TestNewClientIP`: 17 cases, including a spoofed prefix, multiple
    header lines, IPv4-mapped addresses, failing closed on garbage, and
    IPv6 prefix containment.
  - `TestThrottleTrustedProxy`: login and socket buckets per forwarded
    client, through the real `NewMux`.
  - `TestEndpoints` for the CLI's `-secure` flag.
- **Not covered yet:** whether Caddy plus Docker's port publishing actually
  delivers the real client IP on the VM (Docker's userland proxy can
  rewrite the source address). The plan checks this at go-live by reading
  Caddy's access log.

## Relevant Commits
- `dae0dac`, `2723ea1`: ECC tooling update and the earlier journal, committed before tagging
- `v0.1.0` → `2723ea1`: pre-deploy baseline tag, pushed
- `71e78ea`: merge `dev` into `main`, pushed
- `e5fa97b`: 8a plan and the Phase 8 split (on `dev`, not pushed)
- `b88db87`: parse `TRUSTED_PROXIES`
- `67d7189`: `callit-cli -secure`
- `df47805`: plan amendment (Docker needed from Task 1)
- `3ec69be`: `NewClientIP`
- `c01ddd5`: wire the resolver into the auth and socket throttles

## Next Step
Resume at Task 3. Integration path once the phase is green: phase branch →
`dev` (`--no-ff`) → `main`, then go-live per the plan's "After merge"
section.
