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

Go 1.22.10 (backend) · `github.com/redis/go-redis/v9` **v9.18.0** — the
project's first external dependency, and **pinned**: v9.19.0 and later
declare `go 1.24` in their `go.mod` and will not build against this
project's Go 1.22.10 (verified 2026-08-24 across v9.18.0 through v9.22.0).
Raising it means moving the Go toolchain and CI's `go-version` pin first —
this is the single most likely thing a future `go get -u` breaks. ·
`golang.org/x/crypto` **v0.33.0** — also pinned: v0.34.0 declares `go
1.23.0`, and `go get` will silently rewrite this module's `go` directive
to accept it, breaking CI the same way an unpinned go-redis would
(verified 2026-08-25). · `github.com/golang-jwt/jwt/v5` **v5.3.1** and
`github.com/google/uuid` **v1.6.0** — both declare an older/no `go`
directive, safe. · Redis 7.2 (atomic Lua, rate limiting) · Kafka 3.7
KRaft-mode (event backbone, Phase 5+) · PostgreSQL 16 (double-entry
ledger, Phase 5+) · Next.js/React (frontend, Phase 6+) · Docker Compose
(local dev).

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
├── cmd/api/              # HTTP/WS server entrypoint (exists)
├── cmd/relay/             # Redis Stream → Kafka outbox relay (Phase 5)
├── cmd/ledger-worker/     # Kafka → PostgreSQL ledger writer (Phase 5)
├── internal/config/       # env config, fail-fast validation (exists)
├── internal/httpapi/      # REST handlers, mux, error envelope, auth/rate-limit middleware
│                          #   (exists, 91.6% coverage)
├── internal/domain/       # odds, payout+dust, round FSM, wallet rules (exists, 100% coverage)
├── internal/auth/         # argon2id + credential validation + JWT issue/verify — pure,
│                          #   no I/O (exists, 93.5% coverage)
├── internal/account/      # account lifecycle: register, login, refill claims (exists —
│                          #   wraps redisstore.CreateUser/User/UserByEmail/TopUpBalance/Allow)
├── internal/room/         # room lifecycle, short-code generation, joining (exists — wraps
│                          #   redisstore.CreateRoom/JoinRoom, does not write hashes directly)
├── internal/round/        # round orchestration, server-side timers (Phase 4 — wraps
│                          #   redisstore.CreateRound/LockRound)
├── internal/wager/        # wager service: validate → Lua → broadcast (later phase —
│                          #   wraps redisstore.PlaceWager)
├── internal/redisstore/   # redis client, key schema, Lua wrappers (exists, 82.4% own-package
│                          #   coverage — room/round/wager/settlement/refund/account/ratelimit
│                          #   writers all live here)
├── internal/ws/           # hub, room, client pumps (Phase 4)
├── internal/events/       # event schemas, Kafka producer/consumer (Phase 5)
├── internal/ledger/       # PostgreSQL double-entry repository (Phase 5)
├── migrations/            # NNNN_name.up.sql / .down.sql (Phase 5)
└── scripts/lua/           # place_wager.lua, lock_round.lua, settle_round.lua, refund_round.lua,
                           #   claim_unique.lua, rate_limit.lua, top_up_balance.lua (exists)
```

Packages are organized by feature, not by type (`.claude/rules/ecc/golang/patterns.md`).
Full rationale for this layout: plan §3.

## Git Workflow

**Branch per plan phase**, not per sub-task, off `dev`:
`git checkout -b phase-N-<slug> dev`.

**Commit at logical checkpoints within that branch**, not once at the end.
A checkpoint is one behavior/case with its own passing test — a feature
covering 3 distinct behaviors gets 3 commits, not 1. (Phase 0 landed as a
single commit; that was the mistake this convention exists to prevent.)
**Validated in Phase 1** — 22 checkpoint commits, one per behavior, on
`phase-1-domain-core`. That run also surfaced a real defect (checkpoints
whose test passed the moment it was written, because an earlier checkpoint's
implementation already satisfied it) — `writing-plans` now requires a
checkpoint to be a genuine RED→GREEN cycle, not just a labeled commit.

`writing-plans` also moved from pre-writing full code per checkpoint to
specifying exact input→output/error contracts (`ab190b9`) — adopted *after*
Phase 1's plan was written, so Phase 1 ran under the old code-heavy format
(one contributor to that plan hitting 3000+ lines). **Phase 2 was the first
plan written under the new spec-driven format**, and it held up under a
cold executor: 1,591 lines against Phase 1's 3,111 for a phase with more
moving parts, no precision lost, and the plan's own contracts caught a real
plan defect during execution (its CP2 assertion `Σ wallets + Σ pools ==
1500` was inconsistent with `settle_round.lua`'s own KEYS list, which never
touches the pools key — fixed to the correct invariant, `Σ wallets + Dust`,
while implementing). Landed as **28 commits**, not the plan's estimated
31–32 — two checkpoints (`lock_round.lua`'s ALREADY_LOCKED case, which
turned out black-box indistinguishable from its unconditional-OK
predecessor at the Go API surface) and the four concurrency verifications
(committed as two batches rather than four) combined rather than splitting
1:1 with the plan's checkpoint list. Every commit still follows
`type: description` and is independently green.

See `docs/dev-workflow-guide.md` §2a for the current Opus-plans/Sonnet-executes
split across separate windows, and `journal/2026-08-23_1544_ansh_workflow-tooling-and-git-granularity.md`
for the original decision.

**Phase 3 landed as ~49 commits against the plan's own estimate of
~44** — the largest phase so far (12 tasks, 4 deliverables: credentials,
tokens, the shared limiter, and the REST surface over rooms/refills).
The delta from plan is real defects the plan itself didn't anticipate,
each fixed and documented in its commit rather than silently folded in:
an early `go mod tidy` stripped the phase's new dependencies before
anything imported them (same failure mode Phase 2's journal already
warned about, from `go get` immediately followed by `tidy`); a
rate-limit-window test that passed even with eviction disabled, because
the key's own `PEXPIRE` TTL masked the missing `ZREMRANGEBYSCORE`; a
`TestMain`-per-package `FLUSHDB` race across `redisstore`/`account`/
`room`/`httpapi` once a second integration-test package existed (fixed
with `-p 1`, also missing from CI until this phase); and Task 7 CP5's
own test, whose "both succeed" narrative didn't arithmetically agree
with its own contract, redesigned as a 20-trial statistical test after
a single racing pair proved unfalsifiable in practice (~5/50 hits cold
vs ~49/50 warm). The observable-signal rule (added after Phase 2) held
up for the cases the plan flagged in advance — Task 6 CP3 and Task 7
CP2 both did pass immediately as anticipated, verified as genuine RED
by disabling the guard and confirming failure, then restored — but the
CP5 case is a fourth pattern it doesn't yet name: an interaction test
whose result depends on a resource's *warm-up state*, not just its
code path. Worth folding into `writing-plans` if it recurs.

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
Table-driven tests per `.claude/skills/golang-testing/`. Current coverage
(verified live, Phase 3 close-out): `internal/config` 100%, `internal/domain`
100% (this package's floor is 100%, not the project's 80% — plan §9's
"near-total unit coverage" call, since there is no wiring code here to
excuse a gap), `internal/auth` 93.5%, `internal/httpapi` 91.6%,
`internal/redisstore` 82.4% (own-package `go test` figure — integration +
concurrency suites run with `-race`, against real Redis on DB 15, no fake,
no build tag), `cmd/api` 0% — expected, not a gap: thin wiring with no
branching logic of its own, per the plan's own note on `main.go`.

**`internal/account` and `internal/room` show low numbers (29%/50%) under
a plain per-package `go test ./pkg/ -cover` — this is a measurement
artifact, not a real gap.** Their `Register`/`Login`/`Create`/`Join`
methods are exercised entirely by `internal/httpapi`'s black-box HTTP
tests, and Go's coverage tool only attributes a line to the test binary
that directly executed it — a different package's test binary doesn't
count, even though the line genuinely ran. Verified with
`go test ./... -coverpkg=./...` (attributes every line to its *source*
package regardless of which test binary hit it): `account.Register`
85.7%, `account.Login` 85.7%, `account.ClaimRefill` 82.4%,
`room.Create` 85.7%, `room.Join` 82.6% — all solid. Don't "fix" the
per-package number by adding redundant direct tests solely to move the
metric; that's padding a coverage figure, not closing a real gap. If a
real gap is suspected in either package, check the `-coverpkg=./...`
profile first, not the per-package one.

`redisstore`'s remaining gap (82.4%, just under its ≥85% aspirational
floor) is the same category Phase 2 already accepted: defensive
`if err != nil` branches after a Redis call, unreachable without fault-
injecting the Redis connection itself (e.g. a mid-call network drop).
Dead-but-safe, not a behavior gap — don't chase these with a fake/mock
Redis just to flip the percentage.

## Installed Tooling

`golang-patterns`, `golang-testing`, `docker-patterns` — installed ahead of
Phase 0 since it writes `go.mod` and `docker-compose.yml`. `redis-patterns`
is now in use, installed ahead of Phase 2. `api-design` was installed ahead
of Phase 3 and used throughout — `/api/v1` versioning, resource-naming
(joining as `POST .../participants`, not a `/rooms/join` verb), the
envelope/error-code conventions. `postgres-patterns`/`database-migrations`
(Phase 5) are **not yet installed** — staggered per-phase per the plan's
"Tooling to import" column (plan §9), since rule dirs are always-loaded
into every turn and shouldn't sit in context for a stack this project
isn't touching yet. Phase 4 needs no new tooling (plan §9). Check that
column before starting a new phase.

**Phase 3's security review** (`security-reviewer` agent, run against
`internal/auth`, `internal/account`, `internal/httpapi`, and the three
new Lua scripts) found no CRITICAL or HIGH issues — every item the plan
called out to confirm (constant-time password comparison, HS256-only
JWT verification, no-enumeration login, generic 500s, rate limiter
fail-closed, `X-Forwarded-For` ignored) held. One LOW finding (a
hardcoded `"3"` instead of `domain.RefillQuota` in a response header)
was fixed on the spot. Two items remain open by design, not oversight:
login timing (the unknown-email path skips argon2id and so responds
faster than the wrong-password path, even though response bodies are
identical — closing this needs a dummy-hash verify on the miss path,
deferred to Phase 7 hardening) and the room-code generator's slight
modulo bias (accepted — the code is a lookup handle, not a secret;
authorization rests on the JWT).

**Phase 4b's security review** (`security-reviewer` agent, run against
`internal/round`, `internal/wager`, and `internal/ws` before close-out)
found one HIGH, fixed on the spot: the socket router's error-code table
had no case for a throttled wager (`wager.ErrRateLimited`), so it fell
through to the generic `internal_error` code instead of `rate_limited`,
giving a rate-limited client no signal to back off — now mapped, with
the retry-after duration folded into the message. Every invariant the
plan called out to confirm held (host-cannot-wager enforced in Lua and
mirrored in the router's error table, wager anonymity — the
`odds_updated` payload's key set asserted directly, not just assumed,
lockout via Redis `TIME` not Go, UUIDv4 idempotency keys validated,
settlement math computed once in `internal/domain` and never
recomputed, room ID always taken from the verified token claim rather
than the message payload, the one shared rate limiter, no hardcoded
secrets). One MEDIUM was raised — a session ending twice on a rapid
disconnect/reconnect within the same window, since `EndSession` folds
a delta into the persistent balance without clearing the session's
opening-stake marker — but this is exactly the known limitation Task 8
CP2 already documented and this file already named above (under "A
session's opening stake..."): reconnect-with-session-resume needs a
grace window this phase doesn't have, deferred to Phase 7 hardening
alongside login timing. Not a new gap this review found, its known
mechanism.

`continuous-learning-v2` is present under `.claude/skills/` (from the bulk
ECC tooling install) but **intentionally dormant** — `observer.enabled:
false`, no hooks wired in `~/.claude/settings.json`. Don't enable it without
re-reading `dev-workflow-guide.md` §9 first; it was evaluated and declined
for this project specifically (redundant with memory + journal, statistically
curated rather than judgment-curated). `subagent-driven-development` was
evaluated and not installed at all, same section, same reasoning: the
context-pollution problem it solves wasn't demonstrated here.

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
  Docker Desktop itself is installed and running on the Windows side. Once
  enabled, a *new* shell is needed (PATH changes don't reach shells already
  open). Verified 2026-08-23: with integration on, `docker compose up -d`
  brings up Redis and PostgreSQL, both report `healthy`
  (`redis-cli ping` → `PONG`, `pg_isready` → accepting connections). The
  `full` profile was also verified — Kafka (KRaft mode) starts, reports
  `healthy`, and uses a modest ~290MB RSS — so the whole compose file is
  confirmed working, not just YAML-valid.
- Kafka is real Kafka (not Redpanda), chosen deliberately for deeper
  hands-on experience (spec §2) — it's heavier to run locally, which is why
  it's gated behind `make up-full` / the `full` Compose profile rather than
  running by default through Phases 0-4.
