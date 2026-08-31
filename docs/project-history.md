# CallIt — Project History

Phase-by-phase outcomes, security-review findings, and coverage snapshots.

**This file is not loaded into context.** It exists so `CLAUDE.md` doesn't
have to carry it — that file is re-sent on every single turn, so anything
there costs tokens continuously whether or not it's relevant. Read this when
you need the archaeology: why a convention exists, what a past phase actually
cost, what a security review already checked.

The primary record is still `journal/` (one entry per session, newest first)
and each phase's own plan under `docs/plans/`. This file collects the parts
that used to live in `CLAUDE.md`.

---

## Commit-granularity convention: how it was validated

**Phase 0 landed as a single commit — that was the mistake the
branch-per-phase / commit-per-checkpoint convention exists to prevent.**

**Phase 1** validated the fix: 22 checkpoint commits, one per behavior, on
`phase-1-domain-core`. That run also surfaced a real defect — checkpoints
whose test passed the moment it was written, because an earlier checkpoint's
implementation already satisfied it. `writing-plans` now requires a checkpoint
to be a genuine RED→GREEN cycle, not just a labeled commit.

**Phase 2** was the first plan written under the spec-driven format (adopted
in `ab190b9`, *after* Phase 1's plan was written — which is why Phase 1 ran
under the old code-heavy format and hit 3,111 lines). It held up under a cold
executor: 1,591 lines for a phase with more moving parts, no precision lost.
The plan's own contracts caught a real plan defect during execution — its CP2
assertion `Σ wallets + Σ pools == 1500` was inconsistent with
`settle_round.lua`'s own KEYS list, which never touches the pools key; fixed
to the correct invariant, `Σ wallets + Dust`, while implementing. Landed as 28
commits against an estimated 31–32, because two checkpoints combined
(`lock_round.lua`'s `ALREADY_LOCKED` case turned out black-box
indistinguishable from its unconditional-OK predecessor at the Go API surface)
and the four concurrency verifications committed as two batches.

**Phase 3** landed ~49 commits against an estimate of ~44 — the largest phase
(12 tasks, 4 deliverables). The delta was real defects the plan didn't
anticipate, each fixed in its own commit:

- an early `go mod tidy` stripped the phase's new dependencies before anything
  imported them (the same failure Phase 2's journal already warned about, from
  `go get` immediately followed by `tidy`)
- a rate-limit-window test that passed even with eviction disabled, because the
  key's own `PEXPIRE` TTL masked the missing `ZREMRANGEBYSCORE`
- a `TestMain`-per-package `FLUSHDB` race across
  `redisstore`/`account`/`room`/`httpapi` once a second integration-test
  package existed — fixed with `-p 1`, which was also missing from CI
- Task 7 CP5's own test, whose "both succeed" narrative didn't arithmetically
  agree with its own contract; redesigned as a 20-trial statistical test after
  a single racing pair proved unfalsifiable in practice (~5/50 hits cold vs
  ~49/50 warm)

The observable-signal rule held for the cases the plan flagged in advance
(Task 6 CP3, Task 7 CP2 — both passed immediately as anticipated, verified as
genuine RED by disabling the guard). The CP5 case is a fourth pattern that rule
doesn't yet name: **an interaction test whose result depends on a resource's
warm-up state, not just its code path.** Worth folding into `writing-plans` if
it recurs.

**Phase 4** was split into 4a (transport) and 4b (round lifecycle) — the first
phase split before planning rather than after. Both ran under the two-step
checkpoint format. 4a: 25 checkpoints, 25 commits. 4b: 31 checkpoints, 32
commits, 6 real plan defects found during execution (including a genuine
`round`↔`ws` import cycle and a typed-nil-in-interface panic). Full accounting
in `journal/2026-08-26_0250_ansh_tuned-plan-experiment-verdict.md`.

---

## Amendments discovered mid-phase

**Phase 6b, Task 1 — three backend socket-contract gaps, found while planning
against a real browser client (the same class of gap 6a's newcomer-roster fix
was: invisible to every backend-only test, because nothing before 6a/6b put a
browser in front of the server).**

1. **No host/player discriminator reached the client.** `RoomSummary.guest:
   false` is not "is host" — an account holder who *joins* also gets `guest:
   false`, so `app/room/[code]/page.tsx` rendered identically for a host and
   a player. Fixed: `auth.Claims` and `ws.ConnectedEvent` gain a `Host bool`
   claim/field, set at the two room-token issue sites
   (`internal/room/service.go:81,147`). Advisory-for-rendering only —
   `round.Service` still re-checks `rm.HostID` against Redis for every
   host-gated action, so a forged claim buys nothing.
2. **The router computed a wager's authoritative post-wager balance and threw
   it away.** `_, err := r.wagers.Place(...)` in `internal/ws/router.go`
   discarded `wager.Accepted`, leaving a player with no server-anchored
   balance after wagering. Fixed: `wager.Accepted` gains `RoundID`, and the
   router sends a private `wager_accepted` reply (`c.Send`, never broadcast)
   carrying the placer's own new balance and stake.
3. **`round.ErrInvalidSpec` was unmapped** in `replyServiceError`, so a host
   who mistyped a round spec got a generic `internal_error` instead of an
   actionable code. Fixed: mapped to `invalid_spec`.

All three landed as Task 1 of `docs/plans/2026-08-30-phase-6b-gameplay-ui.md`,
each with its own RED→GREEN commit, before any gameplay UI was built against
them.

## Security reviews

### Phase 3 — `internal/auth`, `internal/account`, `internal/httpapi`, three new Lua scripts

No CRITICAL or HIGH. Every item the plan called out to confirm held:
constant-time password comparison, HS256-only JWT verification,
no-enumeration login, generic 500s, rate limiter fail-closed,
`X-Forwarded-For` ignored. One LOW (a hardcoded `"3"` instead of
`domain.RefillQuota` in a response header) fixed on the spot.

### Phase 4b — `internal/round`, `internal/wager`, `internal/ws`

One HIGH, fixed on the spot: the socket router's error-code table had no case
for a throttled wager (`wager.ErrRateLimited`), so it fell through to the
generic `internal_error` code instead of `rate_limited`, giving a rate-limited
client no signal to back off. Now mapped, with the retry-after duration folded
into the message.

Every invariant the plan called out held: host-cannot-wager enforced in Lua and
mirrored in the router's error table; wager anonymity, with the `odds_updated`
payload's key set asserted directly rather than assumed; lockout via Redis
`TIME` not Go; UUIDv4 idempotency keys validated; settlement math computed once
in `internal/domain`; room ID always taken from the verified token claim rather
than the message payload; one shared rate limiter; no hardcoded secrets.

One MEDIUM raised (a session ending twice on a rapid disconnect/reconnect in
the same window) was the known limitation Task 8 CP2 already documented — not
a new gap.

### Phase 5a — `internal/relay`, `internal/events`, `internal/migrate`, ledger schema

No CRITICAL or HIGH. Scoped to the four surfaces the plan named:

- **Event payloads as an exfiltration path** — clear. Settlement and refund
  outbox events only emit per-user payout detail after `settle_round.lua` /
  `refund_round.lua` CAS the round to a terminal status inside the same
  atomic script that emits the event; nothing added this phase broadcasts or
  logs that detail to a client pre-resolution.
- **`payouts` JSON decoding** — clear. `events.Decode` unmarshals directly
  into typed `[]Payout` (never `interface{}`), so amounts stay `int64`
  without precision loss; `TestDecodeRoundSettled_PayoutPrecision` pins a
  2^53+1 round-trip. No unbounded-allocation path found on a malformed array.
- **The PostgreSQL DSN** — clear, with one MEDIUM accepted rather than fixed:
  a migration failure could in principle surface a DSN-bearing error string
  from the postgres driver. Accepted because the practical risk is low
  (pgx/lib/pq redact the password in their own error formatting; verified
  empirically — an unreachable-Postgres error during this phase's own testing
  read `failed to connect to `user=callit database=callit`: ...` with no
  password) and every default DSN in this repo (`CLAUDE.md`, `docker-compose.yml`,
  CI) is unambiguously local-dev-shaped. Revisit if a production-shaped DSN
  is ever wired through the same code path.
- **Kafka connection** — clear. PLAINTEXT-to-localhost is the documented
  local-dev posture (`docker-compose.yml`); no SASL/TLS material is
  hardcoded anywhere, and the plaintext choice is a recorded decision, not
  something silently inherited.

One LOW noted, not fixed: `internal/relay`'s field-map conversion silently
turns a non-string Redis stream value into `""` before handing it to
`events.Decode`, which then reports a clearer "missing field" error instead of
"wrong type" for that (currently unreachable, since every writer in this
codebase writes strings) case. Left as-is — fixing it would be speculative
hardening against a value shape nothing in this codebase produces.

### Phase 5b — `internal/ledger`, `internal/events` (Kafka consumer), `cmd/ledger-worker`

Scoped to the four surfaces the plan named. No CRITICAL. One HIGH raised,
investigated, and **not reproducible** — the rest are MEDIUM/LOW, one fixed
and two accepted.

- **`internal/events/message.go` decoding attacker-influenceable JSON** —
  clear on the field-substitution path: `DisallowUnknownFields` plus this
  phase's own validation correctly rejects a renamed field rather than
  silently decoding it to zero (`TestDecodeMessageRejectsUnknownField`).
  One MEDIUM accepted: `RoundSettled.Payouts` has no slice-length cap, so a
  message with an extreme payout count could pressure memory before
  `ledger.TransactionFor`'s balance check rejects it. Accepted rather than
  fixed — this attack surface is the same one the trust-boundary finding
  below already covers (Kafka broker access), and a size cap on top of a
  documented, topology-enforced trust boundary is defense-in-depth on an
  already-mitigated path. Candidate for Phase 7 if broker ACLs are ever
  relaxed. One LOW-MEDIUM noted and accepted: extreme values near
  `int64` max could in principle overflow the `Dust + Σpayouts` sum before
  the equality-against-`Total` check catches it; the check itself provides
  defense-in-depth, and no path in this codebase produces amounts anywhere
  near that range.
- **`internal/ledger/repo.go` SQL construction** — clear. Every query in
  `WriteBatch` and the four read methods uses `$N` positional parameters;
  no string concatenation or identifier interpolation anywhere in the
  package.
- **`cmd/ledger-worker/main.go` credential surface** — clear.
  `config.LoadLedger` does not require `JWT_SECRET` (the worker neither
  issues nor verifies tokens). The HIGH finding — that a `pgxpool.New`/
  `pool.Ping` connection error could surface the DSN's password — was
  **investigated and not reproduced**: this is the same question Phase 5a's
  review already answered for `internal/migrate`'s connection path, and the
  answer is unchanged. Verified empirically again here against both an
  unreachable host and a wrong-password auth failure against the real local
  Postgres; pgx's error formatting includes `user=` and `database=` but
  never the password (`failed to connect to `user=callit database=callit`:
  ...`, and `failed SASL auth: FATAL: password authentication failed for
  user "callit"` — no password value in either). Every default DSN in this
  repo remains unambiguously local-dev-shaped; revisit if that changes.
- **The trust boundary: an unauthenticated Kafka consumer writing money
  rows** — this is a real, MEDIUM, now-fixed gap, but the fix is
  documentation, not code. `cmd/ledger-worker` writes a PostgreSQL
  transaction from any message on `wagers-placed`/`rounds-settled` without
  authenticating the producer — wire-format validation rejects a malformed
  message, not one from an unauthorized producer. Nothing in `CLAUDE.md`
  said so explicitly before this phase. Now recorded as a Critical
  Invariant: broker access is ledger-write access, currently enforced by
  topology alone (only `cmd/relay` produces) rather than by the broker,
  since local dev runs Kafka PLAINTEXT with no ACLs (Phase 5a's recorded
  decision, above). Revisit before any shared or production deployment.

### Phase 6a — browser origin admission (`httpapi.CORS`, `ws.WithAllowedOrigins`), `frontend/` token storage

No CRITICAL or HIGH. All four surfaces the plan named to confirm held:

- **`httpapi.CORS`** — clear. Origin is echoed only on an exact allowlist
  match, never `*`; `config.loadAllowedOrigins` rejects a bare or
  mixed-in wildcard in every environment; `Vary: Origin` is set
  unconditionally; `Access-Control-Allow-Credentials: true` only appears
  alongside a specific echoed origin, never unpaired or with `*`.
- **`ws.WithAllowedOrigins`** — clear. A missing `Origin` header is allowed
  deliberately for non-browser clients (`cmd/callit-cli`); a browser
  cannot suppress `Origin` on a WebSocket handshake, so this isn't an
  attacker-reachable bypass. The pre-existing same-origin default is
  unchanged for the six call sites that pass no option.
- **`sessionStorage` token storage** — holds as designed. XSS-readable
  (same exposure `localStorage` would have), narrowed by dying with the
  tab and never being auto-attached (`lib/api.ts` only ever sends the
  token via an explicit `Authorization` header a caller supplies —
  no interceptor, no cookie, no CSRF surface opened by enabling CORS).
  The token's only non-header appearance is `lib/socket.ts`'s WebSocket
  URL query parameter, a pre-existing backend contract
  (`ws.ExtractToken`) since browsers cannot set headers on a WS
  handshake — not new exposure from this diff.
- **Token never reaches a log, error, or rendered URL** — clear. The
  only `console.*` call in `frontend/` logs a caught handler exception,
  never a token or envelope payload; no `dangerouslySetInnerHTML`
  anywhere in the frontend; the host page's shareable link is built from
  `window.location.origin` and the room code only, never the token.

Two LOW findings, both fixed on the spot rather than deferred:

- The landing page's join handler interpolated the user-typed room code
  into the REST path unencoded. Not exploitable (a valid code is a fixed
  server-generated alphabet, and the `Authorization` header is unaffected
  by the path), but a `/` in typed input would 404 confusingly. Fixed:
  `encodeURIComponent(code)`.
- `Access-Control-Allow-Credentials: true` is set for every allowed
  origin even though the app has no cookie-based credential today.
  Harmless now, but the assumption it rests on ("no cookies, ever")
  wasn't written down anywhere a future change would see it. Fixed: a
  comment on the line in `cors.go` naming the assumption explicitly, so
  a future cookie-based auth flow can't inherit it unreviewed.

One more LOW, accepted rather than fixed: `loadAllowedOrigins`'s
validation would accept a subdomain-wildcard-looking entry like
`https://*.example.com` as a syntactically valid absolute URL (it has a
scheme and a host), but `CORS`'s matching is a plain map lookup with no
wildcard expansion, so such an entry simply never matches any real
`Origin` and fails closed — an operator-misconfiguration footgun, not an
exploitable gap.

### Open by design, deferred to Phase 7 hardening

1. **Login timing.** The unknown-email path skips argon2id and so responds
   faster than the wrong-password path, even though response bodies are
   byte-identical. Closing it needs a dummy-hash verify on the miss path.
2. **Room-code modulo bias.** Accepted — the code is a lookup handle, not a
   secret; authorization rests on the JWT.
3. **Reconnect ends a session.** `EndSession` fires on socket disconnect, so a
   dropped player restarts at the room buy-in. Resume needs a grace window.
4. **`RoundSettled.Payouts` has no slice-length cap (Phase 5b).** A message
   with an extreme payout count pressures memory before
   `ledger.TransactionFor`'s balance check rejects it. Deferred because the
   only path to an attacker-controlled `Payouts` array is already Kafka
   broker access, which Phase 5b's Critical Invariants now document as
   ledger-write access — a size cap is defense-in-depth on an
   already-mitigated path. Revisit if broker ACLs are ever relaxed from
   today's topology-only enforcement.

---

## Coverage notes

**Don't keep a coverage table here or in `CLAUDE.md` — it goes stale every
phase.** Run `make test` for current figures. What's worth recording is the
two standing interpretations:

**`internal/account` and `internal/room` read low (~29%/50%) under a plain
per-package `go test ./pkg/ -cover`. That's a measurement artifact, not a
gap.** Their `Register`/`Login`/`Create`/`Join` methods are exercised entirely
by `internal/httpapi`'s black-box HTTP tests, and Go's coverage tool attributes
a line only to the test binary that directly executed it. Under
`go test ./... -coverpkg=./...` (which attributes to the *source* package):
`account.Register` 85.7%, `account.Login` 85.7%, `account.ClaimRefill` 82.4%,
`room.Create` 85.7%, `room.Join` 82.6%. **Don't add redundant direct tests to
move the per-package number** — that pads a metric rather than closing a gap.
Suspect a real gap? Check the `-coverpkg` profile first.

**`internal/redisstore` sits just under its ≥85% aspirational floor.** The
remainder is defensive `if err != nil` branches after a Redis call,
unreachable without fault-injecting the connection itself. Dead-but-safe —
don't chase them with a fake Redis to flip the percentage.

**`internal/ledger` (Phase 5b) carries the same class of gap as
`redisstore`, now over `pgx`.** Merged-profile figure (`go test ./... -coverpkg=./...`,
lines attributed to `internal/ledger` deduplicated across every test
binary that instruments it): **85.8%**; `internal/events` (also touched
this phase): **89.6%**. Both clear the 80% floor. The uncovered lines in
`internal/ledger/repo.go` are exclusively the `err != nil` branches after
`Query`/`QueryRow`/`Scan`/`rows.Err()` — unreachable without fault-injecting
the PostgreSQL connection mid-call, same as `redisstore`'s accepted gap.
Don't chase them with a fault-injecting pool to flip the percentage.

**`cmd/*` at 0% is expected**, not a gap: thin wiring with no branching logic.

**Frontend coverage (Phase 6a): 97.4% statements / 95.9% branches**,
measured by `vitest run --coverage` (`@vitest/coverage-v8`) over `lib/**`
and `components/**` only — `app/**` route files are excluded, the same
allowance `cmd/*` has on the Go side. Comfortably clears the 80% floor;
the handful of uncovered lines in `lib/session.ts` and `lib/socket.ts` are
defensive branches (a corrupt `sessionStorage` read, an unregistered
handler set) of the same class as `redisstore`'s accepted gap above.

---

## Tooling decisions

Skills are installed per-phase rather than up front, per the parent plan's
"Tooling to import" column (plan §9) — rule dirs are always-loaded into every
turn and shouldn't sit in context for a stack the project isn't touching yet.

- **Installed:** `golang-patterns`, `golang-testing`, `docker-patterns`
  (before Phase 0) · `redis-patterns` (before Phase 2) · `api-design` (before
  Phase 3 — drove `/api/v1` versioning, resource naming like
  `POST .../participants` rather than a `/rooms/join` verb, and the
  envelope/error-code conventions) · `postgres-patterns`,
  `database-migrations` (before Phase 5a — drove the `NNNN_name.up.sql` /
  `.down.sql` migration convention and the `DEFERRABLE INITIALLY DEFERRED`
  balance trigger) · `react-patterns`, `nextjs-turbopack`, `accessibility`
  plus the `react` and `typescript` rule packs (before Phase 6 planning,
  commit `1a2c2f2`). The Phase 6 set is installed but **not yet exercised by
  any code** — no frontend exists until Phase 6a Task 2 scaffolds one. The
  `typescript` pack was installed on an assumption, which Phase 6a's plan
  then confirmed: the stack is TypeScript + App Router + Tailwind.
- **Not yet installed:** nothing. Every skill the plan §9 table names through
  Phase 6b is in place; Phase 7 adds none ("None new — spec already names k6
  directly").
- **Dormant:** `continuous-learning-v2` is present under `.claude/skills/`
  from the bulk ECC install but deliberately off (`observer.enabled: false`,
  no hooks wired). Don't enable it without re-reading
  `dev-workflow-guide.md` §9 — it was evaluated and declined for this project
  (redundant with memory + journal, statistically rather than
  judgment-curated).
- **Declined:** `subagent-driven-development`, same section. Its stated
  revisit trigger fired at Phase 5b, and the response was **not** to install
  it — see `journal/2026-08-26_0250_ansh_tuned-plan-experiment-verdict.md`
  for why a thin project-local delegation skill was preferred instead. That
  skill, `.claude/skills/delegating-plan-tasks/SKILL.md`, now exists, is
  wired into `executing-plans` Step 2 (invoked per task, only when a plan's
  header opts that task in — inline stays the default), and has run once:
  Phase 5b Tasks 1–5, 3.0× token saving and 9× fewer turns per checkpoint
  against the Phase 5a inline control, both pre-registered bars cleared.
  One process gap surfaced (a subagent misreported having kept
  commit-per-checkpoint discipline) and was patched the same day — see
  `dev-workflow-guide.md` §9's "Update 2026-08-27" note for the full
  account.

---

## Environment verification log

Recorded once so it doesn't have to be re-derived; the actionable rules stay
in `CLAUDE.md`'s Known Environment Gotchas.

- **Go toolchain pins** (verified 2026-08-24/25): go-redis v9.19.0+ declares
  `go 1.24`; `golang.org/x/crypto` v0.34.0 declares `go 1.23.0` and `go get`
  will silently rewrite this module's `go` directive to accept it. Checked
  across go-redis v9.18.0 through v9.22.0. `gorilla/websocket` v1.5.3 declares
  `go 1.12` — safe (verified 2026-08-26).
- **Phase 5 dependency pins** (verified 2026-08-26, at Phase 5's planning
  pass): every *current* release of all three Phase 5 dependencies declares
  `go >= 1.23` and would rewrite this module's directive. A compatible set
  exists and was proven by building a probe module against
  `go 1.22.10` — `go build` clean with `pgxpool`, migrate's `postgres`
  driver, and `kafka.Writer` all imported, directive unchanged:

  | Module | Pin | First incompatible release |
  |---|---|---|
  | `github.com/jackc/pgx/v5` | **v5.7.4** | v5.7.5 (`go 1.23.0`) |
  | `github.com/segmentio/kafka-go` | **v0.4.48** | v0.4.49 (`go 1.23`) |
  | `github.com/golang-migrate/migrate/v4` | **v4.18.2** | v4.18.3 (`go 1.23.0`) |

  Rejected alternatives, both walled off at the same boundary:
  `twmb/franz-go` (v1.19.0+ needs `go 1.23.8`; last compatible v1.18.1) and
  `IBM/sarama` (v1.45.2 needs `go 1.23.0`). `kafka-go` was preferred anyway —
  its last four releases all declare `go 1.15`, so it has the most headroom
  before the next wall.

  **This is the phase where the 1.22.10 pin started costing something.** It
  was free through Phase 4; here it constrains three of three new
  dependencies. Go 1.22 is past upstream EOL, so each future phase's
  dependency search gets narrower. Raising the toolchain is not required for
  Phase 5 and was deliberately not bundled into it, but it is now a real
  candidate for Phase 7 hardening rather than a theoretical one.
- **Docker/WSL2** (verified 2026-08-23): with per-distro WSL integration on,
  `docker compose up -d` brings up Redis and PostgreSQL, both reporting
  `healthy` (`redis-cli ping` → `PONG`, `pg_isready` → accepting connections).
  The `full` profile was also verified — Kafka in KRaft mode starts, reports
  `healthy`, ~290MB RSS. The whole compose file is confirmed working, not just
  YAML-valid.
- **Playwright/Chromium, no sudo** (verified 2026-08-30, Phase 6a Task 9):
  the plan's contingency — `npx playwright install chromium` (user-local
  browser only, skipping `--with-deps`) — was never needed. Chromium
  151.0.7922.34 downloaded and launched cleanly on the first attempt; the
  two-browser E2E test ran to completion. CI's `frontend-e2e` job still
  uses `--with-deps` since GitHub-hosted runners have sudo.
- **A Hyper-V dynamic port exclusion can claim 6379** (hit 2026-08-30,
  this machine, after a Windows/WSL restart): `docker compose up` failed
  with "ports are not available... forbidden by its access permissions"
  for Redis specifically, even though nothing was listening on it —
  `netsh interface ipv4 show excludedportrange protocol=tcp` confirmed
  6379 fell inside an OS-reserved range (`6303–6402`). The reliable fix is
  an admin-PowerShell `net stop winnat && net start winnat`, but the
  lighter fix taken here was to make `docker-compose.yml`'s Redis host
  port configurable via `REDIS_HOST_PORT` (default unchanged at `6379`) —
  set it and `REDIS_ADDR` together for any session hitting this. Not a
  code or Docker bug; a local Windows networking quirk, most likely after
  a restart.
