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

Go 1.22.10 (backend) · Redis 7.2 (atomic Lua, rate limiting) · Kafka 3.7
KRaft-mode (event backbone, Phase 5+) · PostgreSQL 16 (double-entry ledger,
Phase 5+) · Next.js/React (frontend, Phase 6+) · Docker Compose (local dev).

**Never run `go get -u`.** Two dependencies are pinned because newer versions
declare a `go` directive above 1.22.10 and `go get` will silently rewrite this
module's directive to accept them, breaking CI: `go-redis/v9` **v9.18.0**
(v9.19.0+ needs `go 1.24`) and `golang.org/x/crypto` **v0.33.0** (v0.34.0 needs
`go 1.23.0`). Raising either means moving the Go toolchain and CI's
`go-version` pin first. `golang-jwt/jwt/v5`, `google/uuid`, and
`gorilla/websocket` are safe. Before adding any dependency, check its `go`
directive. Verification log: `docs/project-history.md`.

Monorepo: `backend/`, `frontend/` (not yet scaffolded — Phase 6), root
`docker-compose.yml`.

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
```

`make test` now starts Redis and waits for it to report healthy before
running Go, and `internal/redisstore`'s integration tests **fail rather
than skip** when Redis is unreachable — a suite whose whole purpose is
proving zero double-spend must not report PASS while executing nothing.
They run against Redis **DB 15**, never DB 0, so a run can't touch local
dev state; `REDIS_ADDR` (default `localhost:6379`) overrides the address.
Use `make test-unit` when Redis is already up and the Docker round trip
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
`JWT_TTL` (default `2h`, valid `1m`–`24h`). Example:
`JWT_SECRET=$(openssl rand -hex 32) go run ./cmd/api`.

`make migrate` and `make loadtest` exist as stubs — no migrations or k6
scripts exist yet (Phase 5 and Phase 7 respectively).

CI (`.github/workflows/ci.yml`) runs `go vet`, `gofmt -l` (fails on any
unformatted file), `go build`, and `go test -race -cover -p 1`, in that
order, on push/PR to `main` and `dev`. Nothing merges with any of those
red.

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
  (REST payloads), Phase 4 (WebSocket broadcasts), and Phase 6 (the
  frontend must not reconstruct per-user state client-side). Implemented
  as `round:{roundID}:bettors`, a Redis SET — `SCARD` is the numerator,
  and a player's repeat wager is a no-op `SADD` that doesn't move it.
- **Settlement math is not duplicated in Lua.** `internal/domain.Settle`
  computes payouts, dust, and the nobody-backed-the-winner refund path at
  100% coverage with a fuzz test proving `Σ payouts + dust == Σ stakes`;
  `settle_round.lua` only applies what Go already computed (Phase 2
  Amendment A1). Reimplementing the payout formula in Lua would create a
  second, less-tested copy that has to agree with the first exactly —
  don't add one.
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
├── internal/ledger/       # PostgreSQL double-entry repository
└── migrations/            # NNNN_name.up.sql / .down.sql — this naming is the convention
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
`redis-patterns`, `api-design`. Not yet installed: `postgres-patterns`,
`database-migrations` (Phase 5). `continuous-learning-v2` is present but
**deliberately dormant** — don't enable it without re-reading
`dev-workflow-guide.md` §9.

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
  hands-on experience (spec §2) — it's heavier to run locally, which is why
  it's gated behind `make up-full` / the `full` Compose profile rather than
  running by default through Phases 0-4.
