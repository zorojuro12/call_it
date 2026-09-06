# CallIt

Real-time flash-prediction / micro-wagering platform for group watch
parties (Discord hangouts, esports streams, live sports). A host launches
a short prediction round; connected participants place instant wagers
with virtual tokens; live odds update via a pari-mutuel model; the host
resolves the round and payouts settle automatically.

**Status:** Phases 0–7c complete — playable end to end (backend, frontend,
Kafka/PostgreSQL ledger, load-tested, security-reviewed). Only Phase 8
remains, and it's explicitly parked (LLM question suggestions, Terraform,
Prometheus/Grafana). See `docs/plans/2026-08-21-implementation-plan.md` §9
for the full phase-by-phase table.

Full design: [`docs/specs/2026-08-21-callit-design.md`](docs/specs/2026-08-21-callit-design.md).
Full build plan: [`docs/plans/2026-08-21-implementation-plan.md`](docs/plans/2026-08-21-implementation-plan.md).
Binding conventions (stack, invariants, workflow): [`CLAUDE.md`](CLAUDE.md).

## Architecture

```mermaid
flowchart LR
    Browser["Browser (Next.js)"]
    API["cmd/api"]
    Redis[("Redis")]
    Relay["cmd/relay"]
    Kafka[("Kafka")]
    Worker["cmd/ledger-worker"]
    Postgres[("PostgreSQL — ledger")]
    Metrics["metrics listener (optional)"]

    Browser -- "REST + WebSocket" --> API
    API -- "atomic Lua: balance mutation + XADD wager-outbox" --> Redis
    Redis -- "read the outbox stream" --> Relay
    Relay -- "produce wagers-placed, rounds-settled" --> Kafka
    Kafka -- "consume" --> Worker
    Worker -- "double-entry write" --> Postgres
    API -. "server-aggregate histograms only" .-> Metrics
```

**The API process never writes PostgreSQL directly** — that missing edge
is the diagram's whole point. The write path is a transactional outbox:
`cmd/api` debits a wallet and appends to a Redis stream atomically in one
Lua script; `cmd/relay` reads that stream and produces to Kafka;
`cmd/ledger-worker` consumes from Kafka and writes the double-entry ledger.
This closes a crash window where Redis could debit a wallet while the
ledger never learned of it — see `CLAUDE.md`'s Critical Invariants for the
full list this architecture exists to uphold (anonymity until resolution,
the host-cannot-wager rule, the single rate limiter, and more).

## Repository layout

```
backend/
├── cmd/api/               # HTTP/WS server entrypoint
├── cmd/relay/              # Redis Stream → Kafka outbox relay
├── cmd/ledger-worker/       # Kafka → PostgreSQL ledger writer
├── cmd/migrate/             # applies/reverts the ledger schema
├── cmd/callit-cli/          # CLI client — plays a full round end to end
├── internal/domain/        # PURE, no I/O: odds, payout+dust, round FSM, wallet rules
├── internal/auth/           # PURE, no I/O: argon2id, credential validation, JWT issue/verify
├── internal/redisstore/     # Redis client, key schema, Lua wrappers — every writer lives here
├── internal/account/        # register, login, refill claims       ─┐ service layer:
├── internal/room/           # room lifecycle, short codes, joining  │ orchestrates
├── internal/round/          # round lifecycle, server-side timers   │ redisstore +
├── internal/wager/          # validate → Lua → broadcast           ─┘ domain
├── internal/httpapi/        # REST handlers, mux, auth + rate-limit middleware
├── internal/ws/              # hub, per-room goroutine, client pumps, message router
├── internal/events/          # Kafka event schemas, producer/consumer
├── internal/ledger/          # PostgreSQL double-entry repository
├── internal/metrics/         # process-aggregate latency histograms (METRICS_ADDR)
├── migrations/                # NNNN_name.up.sql / .down.sql
└── scripts/lua/               # place_wager, lock_round, settle_round, refund_round, ...

frontend/                       # Next.js App Router, TypeScript strict, Tailwind
├── app/                        # pages — thin, compose lib/ and components/
├── lib/                        # protocol types, REST/WS clients, the round-state reducer
├── components/                 # OddsBoard, WagerPad, HostConsole, SettlementReveal, ...
└── e2e/                        # Playwright acceptance tests

docs/
├── specs/                      # the living design spec
├── plans/                      # one implementation plan per phase
├── reports/                    # load-test baselines (Phase 7a/7b)
└── project-history.md          # phase-by-phase outcomes, security-review findings

journal/                        # dated session log — the working history behind the spec
```

Full rationale for this layout (organized by feature, not by type):
`docs/plans/2026-08-21-implementation-plan.md` §3.

## Running it

| Binary | `make` target | What it does | Needs first |
|---|---|---|---|
| `cmd/api` | `go run ./backend/cmd/api` (see below) | HTTP/WS server | `make up` |
| `cmd/relay` | `make relay` | Redis Stream → Kafka outbox relay | `make up-full` |
| `cmd/ledger-worker` | `make ledger-worker` | Kafka → PostgreSQL ledger writer | `make migrate`, then `make up-full` — this binary never migrates itself |
| `cmd/migrate` | `make migrate` | Applies the ledger schema (`ARGS=down` reverts) | `make up-full` (needs PostgreSQL) |
| `cmd/callit-cli` | `go run ./backend/cmd/callit-cli` | CLI client — plays a full round end to end | a running `cmd/api` |

### Backend

```bash
make up                                  # Redis + PostgreSQL (Kafka: make up-full)
JWT_SECRET=$(openssl rand -hex 32) go run ./backend/cmd/api
```

`JWT_SECRET` (32+ bytes) is required, no default. Other variables, all
optional with the defaults shown:

- `JWT_TTL` — token lifetime, default `2h`, valid `1m`–`24h`.
- `REDIS_ADDR` — default `localhost:6379`.
- `POSTGRES_DSN` — default `postgres://callit:callit@localhost:5432/callit?sslmode=disable`.
- `KAFKA_BROKERS` — default `localhost:9092`.

### `CORS_ALLOWED_ORIGINS`

A comma-separated allowlist of browser origins permitted to call the REST
API and open the WebSocket (`internal/httpapi.CORS` and the WS upgrader's
`CheckOrigin` both read this one value — there is exactly one allowlist,
never two).

- Defaults to `http://localhost:3000` outside `ENV=production`.
- **Required** when `ENV=production` — the process fails fast without it,
  the same way it does without `JWT_SECRET`.
- Never accepts `*`, in any environment.

### `METRICS_ADDR`

Optional `host:port` for a second, plain-text metrics listener
(`internal/metrics.Handler`) — server-side latency histograms for the wager
placement and WebSocket broadcast paths, added in Phase 7a.

- **Disabled by default** — unset or empty, the listener never starts and no
  `/metrics`-style route exists on the public API port at all.
- Serves the registry's rendered text at any path on its own
  `http.Server`, never wrapped in `httpapi.CORS` and never registered on the
  public mux — it adds no second origin allowlist.
- Under `ENV=production`, the host must be loopback (`127.0.0.1`, `::1`, or
  `localhost`) — the process fails fast otherwise.
- Carries no per-user, per-room, or per-round data — every metric is a
  process-aggregate count or latency quantile.

### Frontend

```bash
make fe-install   # npm ci
make fe-dev       # next dev, http://localhost:3000
```

#### `NEXT_PUBLIC_API_BASE_URL`

The backend's base URL, e.g. `http://localhost:8080`. The only environment
value the browser bundle may carry — no secret goes in a `NEXT_PUBLIC_*`
name.

- Defaults to `http://localhost:8080` outside `NODE_ENV=production`.
- **Required** when `NODE_ENV=production` — `npm run build` and
  `npm run start` both fail fast without it (`lib/config.ts`).

## Testing and load

```bash
make test        # brings up the full stack (Redis, PostgreSQL, Kafka), then go test ./... -race -cover -p 1
make test-unit    # same tests, assumes the stack is already up
make fe-test      # npx vitest run
make fe-lint      # npm run lint && npx tsc --noEmit
make fe-e2e       # Playwright — needs the backend and Redis running
```

`make loadtest` runs a k6 REST-throughput ramp against a running
`cmd/api`; `make loadtest SCENARIO=wager_latency` runs the WebSocket
wager-latency scenario instead. `make loadtest-api` builds and runs the
optimized `cmd/api` binary for exactly this purpose — never `go run
./cmd/api`, whose disabled inlining confounds a latency measurement. See
`loadtest/README.md` and the Phase 7a/7b baseline reports under
`docs/reports/`.

## Security posture

- Passwords are hashed with argon2id; a login for an unknown email pays the
  same argon2id cost as one for a wrong password, so the response body
  *and* its timing are identical for both.
- JWT verification pins `HS256` and rejects everything else, including
  `alg: none`.
- One sliding-window rate limiter (`internal/redisstore`'s `rate_limit.lua`)
  backs every throttle in the system — the refill quota, login/register (by
  client IP), authenticated REST routes (by user ID), and the WebSocket
  connect route — never a second implementation.
- **Known deployment caveat:** local dev runs Kafka PLAINTEXT with no ACLs
  (a recorded decision). Broker access is currently equivalent to
  ledger-write access, enforced only by topology (only `cmd/relay`
  produces) rather than the broker itself — restrict broker access before
  any shared or production deployment.
