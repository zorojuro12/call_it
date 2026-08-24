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

Go 1.22+ (backend, no external deps yet) · Redis 7.2 (atomic Lua, rate
limiting) · Kafka 3.7 KRaft-mode (event backbone, Phase 5+) · PostgreSQL 16
(double-entry ledger, Phase 5+) · Next.js/React (frontend, Phase 6+) ·
Docker Compose (local dev).

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
```

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
  frontend must not reconstruct per-user state client-side).

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
├── internal/domain/       # odds, payout+dust, round FSM, wallet rules (Phase 1)
├── internal/auth/         # argon2id + JWT (Phase 3)
├── internal/room/         # room lifecycle, short-code generation (Phase 3)
├── internal/round/        # round orchestration, server-side timers (Phase 4)
├── internal/wager/        # wager service: validate → Lua → broadcast (Phase 2)
├── internal/redisstore/   # redis client + go:embed'd Lua scripts (Phase 2)
├── internal/ws/           # hub, room, client pumps (Phase 4)
├── internal/events/       # event schemas, Kafka producer/consumer (Phase 5)
├── internal/ledger/       # PostgreSQL double-entry repository (Phase 5)
├── migrations/            # NNNN_name.up.sql / .down.sql (Phase 5)
└── scripts/lua/           # place_wager.lua, settle_round.lua, refund_round.lua (Phase 2)
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
**This checkpoint mechanism is new and unverified in practice** — adopted
`ad1027a`, 2026-08-23, no phase has executed under it yet. Phase 1 is the
first real test of whether checkpoints land where expected; adjust
`.claude/skills/writing-plans/SKILL.md` if they don't. See
`journal/2026-08-23_1544_ansh_workflow-tooling-and-git-granularity.md`.

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
here to excuse a gap), `cmd/api` 0% — that last one is expected, not a gap:
it's thin wiring with no branching logic of its own, per the plan's own note
on `main.go`.

## Installed Tooling

`golang-patterns`, `golang-testing`, `docker-patterns` — installed ahead of
Phase 0 since it writes `go.mod` and `docker-compose.yml`. `redis-patterns`,
`postgres-patterns`, `react-patterns`/`nextjs-turbopack` are **not yet
installed** — staggered per-phase per the plan's "Tooling to import" column
(plan §9), since rule dirs are always-loaded into every turn and shouldn't
sit in context for a stack this project isn't touching yet. Check that
column before starting a new phase.

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
