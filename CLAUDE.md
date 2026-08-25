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
this is the single most likely thing a future `go get -u` breaks. · Redis
7.2 (atomic Lua, rate limiting) · Kafka 3.7 KRaft-mode (event backbone,
Phase 5+) · PostgreSQL 16 (double-entry ledger, Phase 5+) · Next.js/React
(frontend, Phase 6+) · Docker Compose (local dev).

Monorepo: `backend/`, `frontend/` (not yet scaffolded — Phase 6), root
`docker-compose.yml`.

## Build & Test

Run from the repo root; `Makefile` targets `cd backend` internally.

```bash
make build       # cd backend && go build ./...
make test        # cd backend && go test ./... -race -cover
make lint        # cd backend && go vet ./... && gofmt -l .
make up          # docker compose up -d          — Redis + PostgreSQL only (Phases 0-4)
make up-full     # docker compose --profile full up -d — adds Kafka (Phase 5+)
make down        # docker compose down
make test-unit   # cd backend && go test ./... -race -cover — assumes Redis is already up
```

`make test` now starts Redis and waits for it to report healthy before
running Go, and `internal/redisstore`'s integration tests **fail rather
than skip** when Redis is unreachable — a suite whose whole purpose is
proving zero double-spend must not report PASS while executing nothing.
They run against Redis **DB 15**, never DB 0, so a run can't touch local
dev state; `REDIS_ADDR` (default `localhost:6379`) overrides the address.
Use `make test-unit` when Redis is already up and the Docker round trip
through `make test` is unwanted.

`make migrate` and `make loadtest` exist as stubs — no migrations or k6
scripts exist yet (Phase 5 and Phase 7 respectively).

CI (`.github/workflows/ci.yml`) runs `go vet`, `gofmt -l` (fails on any
unformatted file), `go build`, and `go test -race -cover`, in that order, on
push/PR to `main` and `dev`. Nothing merges with any of those red.

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

(These bind Phases 1-5 as they're built; Phase 0 — config and health check —
doesn't yet touch most of them. See the plan for full context on each.)

## Repository Layout

```
backend/
├── cmd/api/              # HTTP/WS server entrypoint (exists)
├── cmd/relay/             # Redis Stream → Kafka outbox relay (Phase 5)
├── cmd/ledger-worker/     # Kafka → PostgreSQL ledger writer (Phase 5)
├── internal/config/       # env config, fail-fast validation (exists)
├── internal/httpapi/      # REST handlers, mux (exists — /healthz only so far)
├── internal/domain/       # odds, payout+dust, round FSM, wallet rules (exists, 100% coverage)
├── internal/auth/         # argon2id + JWT (Phase 3)
├── internal/room/         # room lifecycle, short-code generation (Phase 3 — wraps
│                          #   redisstore.CreateRoom/JoinRoom, does not write hashes directly)
├── internal/round/        # round orchestration, server-side timers (Phase 4 — wraps
│                          #   redisstore.CreateRound/LockRound)
├── internal/wager/        # wager service: validate → Lua → broadcast (later phase —
│                          #   wraps redisstore.PlaceWager)
├── internal/redisstore/   # redis client, key schema, Lua wrappers (exists, 87.2% coverage —
│                          #   room/round/wager/settlement/refund writers all live here)
├── internal/ws/           # hub, room, client pumps (Phase 4)
├── internal/events/       # event schemas, Kafka producer/consumer (Phase 5)
├── internal/ledger/       # PostgreSQL double-entry repository (Phase 5)
├── migrations/            # NNNN_name.up.sql / .down.sql (Phase 5)
└── scripts/lua/           # place_wager.lua, lock_round.lua, settle_round.lua, refund_round.lua (exists)
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
(verified live): `internal/config` 100%, `internal/httpapi` 85.7%,
`internal/domain` 100% (this package's floor is 100%, not the project's 80%
— plan §9's "near-total unit coverage" call, since there is no wiring code
here to excuse a gap), `internal/redisstore` 87.2% (integration + a
concurrency suite run with `-race -count=3`, against real Redis on DB 15 —
no fake, no build tag), `cmd/api` 0% — that last one is expected, not a gap:
it's thin wiring with no branching logic of its own, per the plan's own note
on `main.go`.

## Installed Tooling

`golang-patterns`, `golang-testing`, `docker-patterns` — installed ahead of
Phase 0 since it writes `go.mod` and `docker-compose.yml`. `redis-patterns`
is now in use, installed ahead of Phase 2. `api-design` is installed ahead
of Phase 3. `postgres-patterns`/`database-migrations` (Phase 5) are **not
yet installed** — staggered per-phase per the plan's "Tooling to import"
column (plan §9), since rule dirs are always-loaded into every turn and
shouldn't sit in context for a stack this project isn't touching yet. Check
that column before starting a new phase.

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
