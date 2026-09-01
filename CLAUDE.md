# CallIt — Project CLAUDE.md

Real-time flash-prediction / micro-wagering platform for group watch parties.
A host runs short prediction rounds; participants place instant wagers with
virtual tokens; a pari-mutuel engine settles payouts when the host resolves
the round.

**Full design:** [`docs/specs/2026-08-21-callit-design.md`](docs/specs/2026-08-21-callit-design.md)
**Full build plan:** [`docs/plans/2026-08-21-implementation-plan.md`](docs/plans/2026-08-21-implementation-plan.md)
**Tool/skill choices and why:** [`docs/dev-workflow-guide.md`](docs/dev-workflow-guide.md)

This file states binding conventions and invariants. It does not restate the
spec, the plan, or the workflow guide — read those for design rationale and
tool selection; read this for "how work actually gets done in this repo."

## Stack

Go 1.26.7 (backend) · Redis 7.2 (atomic Lua, rate limiting) · Kafka 3.7
KRaft-mode (event backbone, Phase 5+) · PostgreSQL 16 (double-entry ledger,
Phase 5+) · Next.js/React 19 + TypeScript (frontend, Phase 6a+) · Docker
Compose (local dev).

**Toolchain raised to `go 1.26.7` (CI pin `1.26`) in Phase 7a**, off the
EOL 1.22.10 it started on. The raise lifted the `go`-directive ceiling
that used to force five dependency pins — `go-redis/v9` **v9.18.0**,
`golang.org/x/crypto` **v0.33.0**, `jackc/pgx/v5` **v5.7.4**,
`segmentio/kafka-go` **v0.4.48**, and `golang-migrate/migrate/v4`
**v4.18.2** — but Phase 7a held all five versions unchanged on purpose:
upgrading them inside the phase whose job is a stable performance
baseline would add a second variable to every number it measured. They're
a deliberate hold now, not a constraint; the upgrades themselves are 7b/8
work. `backend/internal/toolchain`'s `TestToolchainPinsMeetFloorAndAgree`
fails CI if `go.mod`'s directive and either CI `go-version` pin ever fall
below the `1.26` floor or drift apart from each other — the automated
form of the manual rule this paragraph used to state by hand. **Never run
`go get -u`, and always pin every `go get` target explicitly, subpackages
included** — a version-less `go get` on a multi-package module can still
upgrade the parent past its pin, which is exactly how Phase 5a picked up
`golang-migrate/migrate/v4/database/postgres` and `.../source/iofs`
without repeating `@v4.18.2` on each. Before adding any dependency, check
its `go` directive. Verification log: `docs/project-history.md`.

Monorepo: `backend/`, `frontend/` (Next.js App Router, scaffolded by Phase
6a Task 2), root `docker-compose.yml`.

## Build & Test

Run from the repo root; `Makefile` targets `cd backend` internally.

```bash
make build       # cd backend && go build ./...
make test        # cd backend && go test ./... -race -cover -p 1
make lint        # cd backend && go vet ./... && gofmt -l .
make up          # docker compose up -d          — Redis + PostgreSQL only (Phases 0-4)
make up-full     # docker compose --profile full up -d — adds Kafka (Phase 5+)
make down        # docker compose down
make test-unit   # cd backend && go test ./... -race -cover -p 1 — assumes Redis is already up
make migrate     # cd backend && go run ./cmd/migrate $(ARGS) — applies the ledger schema; `ARGS=down` reverts
make ledger-worker # cd backend && go run ./cmd/ledger-worker — Kafka → PostgreSQL ledger writer (Phase 5b); run `make migrate` first, this binary never migrates
```

`make test` now brings up the **full stack** — Redis, PostgreSQL, and
Kafka — and waits for all three to report healthy before running Go
(Phase 5a). `internal/redisstore`, `internal/migrate`, `internal/events`,
and `internal/ledger`'s integration tests **fail rather than skip** when
their respective dependency is unreachable — a suite whose whole purpose is
proving zero double-spend, or that a migration/event actually reaches its
target, must not report PASS while executing nothing. `internal/ledger`
runs against its own **`callit_test`** PostgreSQL database (dropped and
recreated per run, migrated fresh) plus Redis **DB 15** and the real Kafka
broker — its reconciliation suite is the one place all three
infrastructure dependencies are live in the same test run. `redisstore` runs
against Redis **DB 15**, never DB 0, so a run can't touch local dev state;
`REDIS_ADDR` (default `localhost:6379`), `POSTGRES_DSN` (default
`postgres://callit:callit@localhost:5432/callit?sslmode=disable`), and
`KAFKA_BROKERS` (default `localhost:9092`) override the addresses. Use
`make test-unit` when all three are already up and the Docker round trip
through `make test` is unwanted.

**`-p 1` is load-bearing, not incidental (added Phase 3).** Multiple
packages now hold integration suites that share Redis DB 15
(`redisstore`, `account`, `room`, `httpapi`), and each one's `TestMain`
does its own `FLUSHDB` for a clean slate — Go's default package-level
parallelism would let two of those race, one flush wiping another
package's in-flight test data mid-run. `-p 1` serializes package test
binaries and removes the race. Don't drop it to "speed up" `go test
./...`.

**Running the server itself needs `JWT_SECRET`** (32+ bytes, no
default — the process fails fast without it) and, optionally,
`JWT_TTL` (default `2h`, valid `1m`–`24h`) and `METRICS_ADDR` (optional
`host:port` for the separate metrics listener added in Phase 7a; unset
disables it; must be loopback under `ENV=production` — see README.md).
Example: `JWT_SECRET=$(openssl rand -hex 32) go run ./cmd/api`.

`make loadtest` runs `SCENARIO ?= rest_throughput` (`loadtest/rest_throughput.js`,
a REST throughput ramp); `make loadtest SCENARIO=wager_latency` runs the
WebSocket wager-latency scenario (`loadtest/wager_latency.js`) instead
(Phase 7a). Both need k6 on `PATH` (external binary, user-local install —
`loadtest/README.md` — deliberately not a Go dependency) and a live stack
(`make up` plus a running `cmd/api`, `METRICS_ADDR` set so the
server-side histograms are reachable). `make loadtest-api` (Phase 7b)
builds `cmd/api` with `go build` and runs the optimized binary with
`METRICS_ADDR` defaulted to `127.0.0.1:9090` — every load run against a
p99 or throughput target uses this, never `go run ./cmd/api`, whose
disabled inlining confounded 7a's own baseline. `make migrate`, `make relay`,
and `make ledger-worker` are real as of Phase 5a/5b/7b
respectively.

CI (`.github/workflows/ci.yml`) runs `go vet`, `gofmt -l` (fails on any
unformatted file), `go build`, and `go test -race -cover -p 1`, in that
order, on push/PR to `main` and `dev`. Nothing merges with any of those
red.

**Frontend (Phase 6a+):**

```bash
make fe-install  # cd frontend && npm ci
make fe-dev      # cd frontend && next dev
make fe-test     # cd frontend && npx vitest run
make fe-lint     # cd frontend && npm run lint && npx tsc --noEmit
make fe-build    # cd frontend && npm run build
make fe-e2e      # cd frontend && npx playwright test — needs the backend and Redis running
```

Coverage floor is 80% via `vitest run --coverage` over `lib/**` and
`components/**`; `app/**` route files are thin wiring and excluded, the
same allowance `cmd/*` has on the Go side. `next build`/`next start` fail
fast without `NEXT_PUBLIC_API_BASE_URL` when `NODE_ENV=production` — CI's
`frontend` job sets it for the build step. A separate `frontend-e2e` CI
job runs Playwright against a live backend + Redis, installed with
`--with-deps` (CI has sudo; local dev does not — see Known Environment
Gotchas).

## Critical Invariants

- **`internal/domain` stays free of I/O.** Odds math, payout/dust
  distribution, the round state machine, and wallet rules must be
  unit-testable with nothing running — no Redis, no DB, no network. This is
  deliberate sequencing (plan §9, Phase 1 before any infrastructure): money
  math is where correctness bugs hide, and it's cheapest to prove right when
  it can't touch the outside world by construction.
- **The WebSocket server never writes PostgreSQL directly.** The write path
  is: WS handler → atomic Redis Lua script (balance mutation + `XADD` to a
  `wager-outbox` stream, one atomic unit) → separate relay process reads the
  stream → produces to Kafka `wagers-placed` → a dedicated consumer
  batch-writes the double-entry ledger. This is a transactional-outbox
  amendment adopted during planning (plan §1) specifically to close a crash
  window where Redis could debit a wallet while the ledger never learns of
  it — do not "simplify" this back to a direct write, that reintroduces the
  gap it exists to close.
- **The host cannot place wagers in their own room.** Guard on `room.host_id`
  in the wager-placement path. Removes the conflict of interest of one party
  both staking on and resolving an outcome.
- **Lockout is evaluated with Redis `TIME` inside the Lua script, never a
  client- or app-server-supplied timestamp.** Multiple API instances must
  share one clock; trusting a client timestamp reopens the exact
  client-latency exploit the design closes (spec §4).
- **All amounts are integer token units everywhere they're stored.** No
  floats in Redis or the ledger — odds become floating point only at the
  presentation layer. Payout flooring produces rounding remainder; that
  remainder is credited to a `system_dust` ledger account so debits and
  credits stay exactly balanced (plan §5) — never let dust silently vanish.
- **Every wager carries a UUIDv4 `idempotency_key`.** The Lua script dedupes
  on it (cached result, TTL 24h) and it's a `UNIQUE` constraint on the
  Postgres `transactions` table — this is what makes at-least-once Kafka
  delivery safe. Don't add a second identity path that bypasses it.
- **Refills and wager-placement throttling share one Redis sliding-window
  rate limiter.** One mechanism, two call sites — don't fork a second
  implementation for either use case.
- **Wagers stay anonymous until the round is terminal.** No payload from
  any phase may carry per-user wager data before the round resolves or
  refunds — `internal/domain`'s `Settlement.Results` is the reveal, and
  nothing earlier. Live odds broadcast pool totals only. The host
  resolves outcomes, so early visibility would hand them the conflict of
  interest that the host-cannot-wager rule exists to remove. The only
  permitted in-round progress signal is an aggregate count of players
  who have wagered ("2/5 players have placed their bets") — denominator
  excludes the host, and it counts players, not wagers. Binds Phase 3
  (REST payloads), Phase 4 (WebSocket broadcasts), and Phase 6a/6b (the
  frontend must not reconstruct per-user state client-side — 6a receives
  no wager data at all and must stay that way; 6b renders the reveal only
  from `round_resolved`/`round_refunded`, in `lib/roundState.ts`'s reducer
  and `components/SettlementReveal.tsx`). Implemented
  as `round:{roundID}:bettors`, a Redis SET — `SCARD` is the numerator,
  and a player's repeat wager is a no-op `SADD` that doesn't move it.
  **One narrow, explicit exception (added Phase 6b):** `wager_accepted`
  privately tells a placer their own new balance and stake, over their
  own connection only (`ws.Router`'s `c.Send`, never `Broadcast`) — a
  wagerer already knows their own stake; this discloses nothing about
  anyone else's, and the room-wide `odds_updated` broadcast is unchanged.
- **Settlement math is not duplicated in Lua.** `internal/domain.Settle`
  computes payouts, dust, and the nobody-backed-the-winner refund path at
  100% coverage with a fuzz test proving `Σ payouts + dust == Σ stakes`;
  `settle_round.lua` only applies what Go already computed (Phase 2
  Amendment A1). Reimplementing the payout formula in Lua would create a
  second, less-tested copy that has to agree with the first exactly —
  don't add one.
- **`place_wager.lua` rejects a non-positive stake itself (Phase 7b).**
  Its balance guard (`tonumber(existingBalance) < amount`) is passed by a
  negative `amount`, after which `HINCRBY walletsKey userID -amount`
  **credits** the wallet instead of debiting it — the amount's sign
  reaching `HINCRBY` unchecked mints tokens. No caller-side check may be
  treated as this guard: `wager.Service.Place`'s old balance pre-check
  was the only thing standing between the write path and this hole until
  the script gained its own `INVALID_STAKE` rejection, and that pre-check
  is gone now (Task 3 removed it as the redundant round trip it was).
  Any future caller of `Store.PlaceWager` must be able to rely on the
  script alone.
- **`internal/redisstore/keys.go` is the only place a Redis key may be
  constructed.** Every other file in the package calls its builders
  (`RoomKey`, `RoundWagersKey`, ...) rather than concatenating a key by
  hand, so the schema has exactly one definition.
- **One sliding-window rate limiter, every call site.** `rate_limit.lua`
  / `Store.Allow` is the only implementation — the refill quota, the
  `auth` scope (register/login, keyed by client IP), and the `api` scope
  (authenticated routes, keyed by user ID) all call it with different
  scopes, limits, and windows. Don't fork a second one.
- **Unique secondary indexes (email, room code) are claimed through
  `claim_unique.lua`, never a bare `SET`.** A bare `SET` on collision
  silently repoints an existing index at a new entity — this is the
  exact bug Phase 3 Amendment B4 fixed for room codes (a colliding code
  used to hijack a live room, sending every later joiner to the wrong
  place while reporting success).
- **JWT verification pins `HS256` and rejects everything else,
  including `alg: none`.** `jwt.WithValidMethods([]string{"HS256"})` on
  every `Verify` call — a verifier that honors the attacker-supplied
  `alg` header is the classic JWT vulnerability. Don't relax this to
  "any algorithm the secret can produce."
- **A connection's room and identity come from the verified JWT claims,
  never from a request path or message payload.** The WebSocket route is
  `GET /api/v1/socket` with no room in the path, and the message router
  ignores any `room_id` a client sends. Two sources for the same fact means
  a mismatch check that can be forgotten; one source means there is nothing
  to check. Don't add a room path segment or trust a payload's room field.
- **Login gives byte-identical responses for an unknown email and a
  wrong password.** Both collapse into `account.ErrInvalidCredentials`;
  a corrupted/malformed stored hash collapses into the same sentinel
  too. An endpoint that answers "no such user" differently from "wrong
  password" is a free account-enumeration oracle — don't reintroduce a
  code path that lets the two be told apart from the response.
- **A session's opening stake never debits an account holder's
  persistent balance.** Only the net delta at session end does (Phase 4).
  Joining several rooms concurrently can therefore commit more than the
  persistent balance holds; that's safe by construction because
  `domain.ApplySessionResult` floors the persistent balance at 0 — don't
  "fix" this by debiting at join time, that would double-count against
  the floor rule.
- **Ledger sign convention (Phase 5b, `internal/ledger`): `credit` = tokens
  in, `debit` = tokens out.** An account's balance is `Σ credits − Σ
  debits`, and every transaction satisfies `Σ debits == Σ credits` —
  enforced by a `DEFERRABLE INITIALLY DEFERRED` constraint trigger at
  COMMIT, not application code. Deliberately not the classical accounting
  convention (where a debit increases an asset account): that would make
  every reader first decide whether a user wallet is an asset or a
  liability. One rule — money in is a credit — removes the question.
- **The ledger records outbox movements only; a `user_wallet` ledger
  balance is a net session delta, not an absolute holding.** Joining a
  room is not an outbox event (`Store.JoinRoom` is a Go pipeline,
  `redisstore/room.go:114`, not a Lua script), so the opening stake never
  reaches Kafka or the ledger. The reconciliation identity is
  `redis_wallet(user, room) − opening_stake(user, room) ==
  ledger_balance(user, room)` — do not "fix" a reconciliation check by
  comparing the ledger directly to the absolute Redis wallet, that
  comparison is false by construction.
- **Kafka broker access is equivalent to ledger-write access.**
  `cmd/ledger-worker` writes PostgreSQL money rows from any message on the
  `wagers-placed`/`rounds-settled` topics without authenticating the
  producer — the wire-format validation (`internal/events.DecodeMessage`)
  rejects a malformed message, but not one from an unauthorized producer.
  Restricting who can produce to these topics is a network/broker-config
  concern, not application code, and must hold for that reason: local dev
  runs Kafka PLAINTEXT with no ACLs (a recorded decision, `docs/project-history.md`),
  so this invariant is currently enforced by topology alone (only
  `cmd/relay` produces) rather than by the broker. Revisit before any
  shared or production deployment.
- **The browser origin allowlist has exactly one definition** (Phase 6a):
  `config.Config.AllowedOrigins`, parsed once from `CORS_ALLOWED_ORIGINS`
  and read by both `httpapi.CORS` (REST) and the WebSocket upgrader's
  `CheckOrigin` (`ws.WithAllowedOrigins`). Two lists that must agree is the
  shape this file already rejects for Redis keys
  (`internal/redisstore/keys.go`) and the rate limiter — don't fork a
  second list for either surface.
- **Metrics are process-aggregate only and never labelled by user, room,
  or round** (Phase 7a): `internal/metrics`'s histograms and counters
  observe totals across the whole process — a per-user label would let a
  scraper reconstruct wager activity the same anonymity invariant above
  exists to withhold. The metrics listener (`METRICS_ADDR`) is a
  separate `http.Server`, never wrapped in `httpapi.CORS` and never
  registered on the public mux, so it adds no second origin allowlist to
  the rule above.

(These bind Phases 1-5 as they're built; Phase 0 — config and health check —
doesn't yet touch most of them. See the plan for full context on each.)

## Repository Layout

```
backend/
├── cmd/api/               # HTTP/WS server entrypoint
├── cmd/callit-cli/        # CLI client — plays a full round end to end
├── internal/config/       # env config, fail-fast validation
├── internal/domain/       # PURE, no I/O: odds, payout+dust, round FSM, wallet rules
├── internal/auth/         # PURE, no I/O: argon2id, credential validation, JWT issue/verify
├── internal/redisstore/   # redis client, key schema, Lua wrappers — every writer lives here
├── internal/account/      # register, login, refill claims        ─┐ service layer:
├── internal/room/         # room lifecycle, short codes, joining   │ orchestrates
├── internal/round/        # round lifecycle, server-side timers    │ redisstore +
├── internal/wager/        # validate → Lua → broadcast            ─┘ domain; never
├── internal/httpapi/      # REST handlers, mux, error envelope,      writes a hash
│                          #   auth + rate-limit middleware           directly
├── internal/ws/           # hub, per-room goroutine, client pumps, message router
└── scripts/lua/           # place_wager, lock_round, settle_round, refund_round,
                           #   claim_unique, rate_limit, top_up_balance

Phase 5 adds:
├── cmd/relay/             # Redis Stream → Kafka outbox relay
├── cmd/ledger-worker/     # Kafka → PostgreSQL ledger writer
├── internal/events/       # event schemas, Kafka producer/consumer
├── internal/ledger/       # PostgreSQL double-entry repository, the pure
│                          #   event→transaction mapping, and the Kafka
│                          #   consume loop that feeds it
└── migrations/            # NNNN_name.up.sql / .down.sql — this naming is the convention

Phase 6a adds:
frontend/                    # Next.js App Router, TypeScript strict, Tailwind
├── app/                     # pages — thin, compose lib/ and components/
├── lib/
│   ├── protocol.ts          # types mirroring Go wire structs — no logic
│   ├── api.ts               # REST client, envelope unwrap, ApiError
│   ├── session.ts           # sessionStorage wrapper — account token, room token, room summary
│   └── socket.ts            # WebSocket client, typed on(type, handler) dispatch table
├── components/               # PresenceRoster, ErrorBanner
└── e2e/                      # Playwright acceptance tests
```

Phase 6b adds, in the same `frontend/` tree:
```
lib/
├── roundState.ts            # pure reducer — the client-side counterpart of internal/domain
├── countdown.ts              # remainingMs + useCountdown, the display-only lockout timer
└── audio.ts                  # Web Audio cues behind an injectable AudioContext factory
components/
├── OddsBoard.tsx              # pool totals, multipliers, the aggregate bettors counter
├── WagerPad.tsx                # outcome selection and stake entry
├── HostConsole.tsx             # open-round form and resolve picker
└── SettlementReveal.tsx        # the terminal-state reveal
```

**`relay` and `ledger-worker` are separate binaries under `cmd/` deliberately** —
that separation is what structurally enforces "the WebSocket server never writes
PostgreSQL directly." Running either as a goroutine inside the API process would
satisfy the written invariant while reintroducing the coupling it exists to
prevent.

Packages are organized by feature, not by type (`.claude/rules/ecc/golang/patterns.md`).
Full rationale for this layout: plan §3.

## Git Workflow

**Branch per plan phase**, not per sub-task, off `dev`:
`git checkout -b phase-N-<slug> dev`.

**Commit at logical checkpoints within that branch**, not once at the end.
A checkpoint is one behavior/case with its own passing test — a feature
covering 3 distinct behaviors gets 3 commits, not 1. A checkpoint must be a
genuine RED→GREEN cycle, not just a labeled commit.

**A phase is one deliverable**, not several bundled because they're
thematically related. If a plan's own self-review names a mid-phase stopping
point, split the phase in the parent plan's §9 table *before* writing the
detailed task breakdown.

See `docs/dev-workflow-guide.md` §2a for the Opus-plans/Sonnet-executes split
across separate windows, and `docs/project-history.md` for how these
conventions were validated phase by phase.

**Self-merge into `dev` once the phase's tests pass. No PR.** Solo project;
PR ceremony doesn't add value with one developer. Revisit immediately if a
collaborator joins.

**Commit format:** `type: description` (`.claude/rules/ecc/common/git-workflow.md`)
— `feat`, `fix`, `docs`, `test`, `chore`, `refactor`, `perf`, `ci`.

**Execution mechanism:** `/impl-plan` resolved the architecture (done —
`docs/plans/2026-08-21-implementation-plan.md`). Each phase gets its own
`writing-plans` → `executing-plans` pass before implementation starts —
don't skip straight to code without a task/checkpoint breakdown, that's how
Phase 0 lost its commit granularity.

## Testing

80% minimum coverage, TDD (RED → GREEN → IMPROVE), AAA structure —
`.claude/rules/ecc/common/testing.md` (always loaded, not restated here).
Table-driven tests per `.claude/skills/golang-testing/`.

**`internal/domain`'s floor is 100%, not 80%** — there is no wiring code here
to excuse a gap (plan §9). `cmd/*` at 0% is expected, not a gap.

**Judge coverage from `go test ./... -coverpkg=./...`, never the per-package
figure.** `internal/account` and `internal/room` read low per-package because
their methods are exercised by `internal/httpapi`'s black-box tests, and Go
attributes a line only to the test binary that directly ran it. Don't add
redundant direct tests to move that number — that pads a metric rather than
closing a gap. Standing interpretations, and `redisstore`'s accepted
defensive-branch gap: `docs/project-history.md`.

Run `make test` for current figures rather than trusting a table in a doc.

## Installed Tooling

**Skills are installed per-phase, not up front** — rule dirs load into every
turn, so they shouldn't sit in context for a stack the project isn't touching
yet. Check the parent plan §9's "Tooling to import" column before starting a
phase.

Installed: `golang-patterns`, `golang-testing`, `docker-patterns`,
`redis-patterns`, `api-design`, `postgres-patterns`, `database-migrations`,
`react-patterns`, `nextjs-turbopack`, `accessibility` (the last three staged
ahead of Phase 6a, per the plan §9 row's "Tooling to import" column, and now
exercised — Phase 6a built the real `frontend/` tree). Matching rule packs
`.claude/rules/ecc/react/` and `.claude/rules/ecc/typescript/` installed
alongside them. **The `typescript` pack's provisional status is resolved:**
it was installed on the assumption a Next.js scaffold defaults to TS, and
Phase 6a's plan confirmed that assumption — the stack is fixed as
**TypeScript (`strict: true`) + the Next.js App Router + Tailwind**
(`docs/plans/2026-08-30-phase-6a-frontend-shell.md`, "Decisions This Plan
Fixes"). Both packs stay.

`continuous-learning-v2` is present but **deliberately dormant** — don't
enable it without re-reading `dev-workflow-guide.md` §9.

`delegating-plan-tasks` (added ahead of Phase 5b) changes *where* a plan
task's turns execute — one cold subagent per task instead of inline — and
nothing else about the plan format, checkpoint discipline, or commit
granularity. It is invoked from `executing-plans` Step 2, per task, and only
for tasks a plan's header marks for delegation; a phase's flagship
correctness work stays inline. `subagent-driven-development` remains
declined — the objection was its ceremony, not delegation
(`dev-workflow-guide.md` §9).

**Run the `security-reviewer` agent before closing any phase that touches
auth, money movement, or a network surface.** Past findings, and the three
items open by design (login timing, room-code modulo bias, reconnect ending a
session — all deferred to Phase 7): `docs/project-history.md`.

## Known Environment Gotchas

- **`go` may report "not found" in a non-interactive shell even though it's
  installed.** No root/sudo is available in some tool-execution
  environments, so Go was installed user-locally to `~/.local/go` (not
  `/usr/local/go`) and added to `PATH` via `~/.bashrc` — which only loads
  for *interactive* shells. Non-interactive tool calls need the PATH set
  explicitly: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin` before
  `make build`/`test`/`lint`. If `go` is genuinely missing (not just off
  PATH), don't retry `sudo` — it fails here with "a terminal is required to
  read the password"; install user-locally instead.
- **Docker requires WSL2 integration enabled per-distro in Docker Desktop
  settings** (Settings → Resources → WSL Integration → toggle the specific
  distro, not just "the default distro") — it's not on by default even when
  Docker Desktop is installed and running on the Windows side. Once enabled, a
  *new* shell is needed; PATH changes don't reach already-open shells. The
  whole compose file including the Kafka `full` profile is verified working —
  see `docs/project-history.md`.
- Kafka is real Kafka (not Redpanda), chosen deliberately for deeper
  hands-on experience (spec §2). It was gated behind `make up-full` / the
  `full` Compose profile through Phases 0-4 to avoid running it before
  anything needed it — **now historical**: `make test` and CI both bring
  Kafka up unconditionally from Phase 5a onward, since `internal/events`'
  integration suite needs it and fails rather than skips without it.
- **A bare `docker compose down` does not stop or remove `full`-profile
  services** (Kafka) — it only touches services in the profiles that
  invocation activates, so `down` after `up-full` used to leave the Kafka
  container running. `make down` now runs `docker compose --profile full
  down` for exactly this reason — don't drop the flag if editing that
  target, and don't call bare `docker compose down` by hand expecting it
  to reach Kafka.
