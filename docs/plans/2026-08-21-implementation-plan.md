# CallIt — Implementation Plan

**Date:** 2026-08-21
**Status:** Approved — cleared to begin Phase 0
**Source spec:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md)

Resolves the seven open items left for planning in spec §9, then sequences
the build. Phases 0–4 constitute the MVP; phases 5+ are separate milestones
that should each get their own planning pass before implementation.

**Pattern grounding:** none available. This plan was written against an
empty repository (`README.md`, `docs/`, `.claude/` only — no `go.mod`, no
`backend/`, no `frontend/`). Every convention below is proposed rather than
extracted from existing code. Later plans should pattern-match against
whatever Phase 0 establishes instead of re-deriving conventions.

---

## 1. Approved amendment to the spec

Spec §5 originally ran the Redis Lua script and then produced to Kafka as
two separate operations sharing no transaction. If the process crashed
between them, or the broker was briefly unreachable, Redis would have
debited the wallet while the ledger never learned of it — Redis and
PostgreSQL diverge silently, defeating the "0.00% double-spend tolerance"
and immutable-audit-trail guarantees.

**Adopted fix — transactional outbox over Redis Streams.** The Lua script
performs an `XADD` to a `wager-outbox` stream inside the same atomic script
as the balance mutation. A separate relay process consumes that stream,
produces to Kafka, and acknowledges only after a successful produce. A
crash at any point leaves the event in the stream to be replayed. Delivery
into Kafka is therefore at-least-once, deduplicated downstream by the
`idempotency_key` that R5 already requires.

The spec's §5 has been updated to match. Cost is one line of Lua plus a
small relay binary; the benefit is that the durability guarantee actually
holds under crash.

## 2. Resolved library and mechanism decisions

| Decision | Choice | Rationale |
|---|---|---|
| WebSocket library | `gorilla/websocket` | Most widely used and recognizable; actively maintained again; the canonical hub pattern is well documented against it. `coder/websocket` has a cleaner context-aware API but less recognition value. |
| Kafka client | `segmentio/kafka-go` | Pure Go, simplest API to learn the concepts against. `twmb/franz-go` is faster and supports transactions — migrate later if exactly-once is wanted. |
| Auth | Email + password, argon2id, JWT HS256 | Avoids OAuth callback and secret plumbing while still exercising real credential handling. Guests get a JWT with `guest: true` and no database row. OAuth deferred. |

---

## 3. Repository layout

```
call_it/
├── docker-compose.yml          # redis, postgres, kafka (KRaft, no ZK)
├── Makefile                    # up/down/test/lint/migrate/loadtest
├── backend/
│   ├── go.mod                  # module github.com/zorojuro12/call_it/backend
│   ├── cmd/
│   │   ├── api/                # HTTP + WebSocket server
│   │   ├── relay/              # Redis Stream → Kafka outbox relay
│   │   └── ledger-worker/      # Kafka → PostgreSQL double-entry writer
│   ├── internal/
│   │   ├── config/             # env load + fail-fast validation at startup
│   │   ├── domain/             # PURE: odds, payout+dust, round FSM, wallet rules
│   │   ├── auth/               # argon2id hashing, JWT issue/verify
│   │   ├── room/               # room lifecycle, short-code generation
│   │   ├── round/              # round orchestration + server-side timers
│   │   ├── wager/              # wager service (validate → Lua → broadcast)
│   │   ├── redisstore/         # redis client + go:embed'd Lua scripts
│   │   ├── ws/                 # hub, room, client pumps
│   │   ├── httpapi/            # REST handlers, middleware, error envelope
│   │   ├── events/             # event schemas, Kafka producer/consumer
│   │   └── ledger/             # PostgreSQL double-entry repository
│   ├── migrations/             # NNNN_name.up.sql / .down.sql
│   ├── scripts/lua/            # place_wager.lua, settle_round.lua, refund_round.lua
│   └── test/                   # integration + concurrency suites (testcontainers)
├── frontend/                   # Next.js (Phase 6)
└── loadtest/                   # k6 scripts
```

Packages under `internal/` are organized by feature rather than by type,
per `.claude/rules/ecc/common/coding-style.md`. `internal/domain` is
deliberately free of I/O so the money math is unit-testable with no
container running.

---

## 4. Redis key schema

All amounts are **integer token units**. No floats appear in Redis or in
the ledger; odds become floating point only at the presentation layer.

| Key | Type | Contents |
|---|---|---|
| `code:{roomCode}` | STRING | → `roomID` (join lookup) |
| `room:{roomID}` | HASH | `host_id`, `buy_in`, `status`, `created_at` |
| `room:{roomID}:wallets` | HASH | `userID` → session balance |
| `round:{roundID}` | HASH | `room_id`, `status`, `lock_at_ms`, `outcome_count`, `resolved_outcome` |
| `round:{roundID}:pools` | HASH | `0..n` → pool amount, `total` → sum |
| `round:{roundID}:wagers` | HASH | `{userID}:{outcomeIdx}` → amount |
| `idem:{key}` | STRING | cached result, TTL 24h |
| `ratelimit:{scope}:{id}` | ZSET | sliding window — wager throttle *and* refill quota |
| `wager-outbox` | STREAM | outbox events awaiting relay to Kafka |

**Authoritative clock.** Lockout is evaluated using `redis.call('TIME')`
inside the script rather than a timestamp supplied by the application
server. This gives the system a single clock, immune to skew across
multiple API instances, and makes R3's "no client-latency exploit"
guarantee structural rather than merely intended. Redis 7 replicates
scripts by effects, so calling `TIME` inside a script is safe.

---

## 5. Lua script contracts

### `place_wager.lua`

One atomic unit:

1. `idem:{key}` exists → return the cached result, mutate nothing (R5).
2. Round status is not `open`, **or** `TIME` ≥ `lock_at_ms` → `POOL_LOCKED`.
3. `outcomeIdx` out of range → `INVALID_OUTCOME`.
4. Caller is `room.host_id` → `HOST_CANNOT_BET`.
5. Wallet field missing → `NOT_IN_ROOM`; balance < amount →
   `INSUFFICIENT_FUNDS`.
6. `HINCRBY` wallet by −amount; pools by +amount (both the outcome field
   and `total`); wagers by +amount.
7. `XADD wager-outbox` — the outbox write, atomic with the mutation above.
8. `SET idem:{key}` with TTL; return the new balance and updated pools.

### `settle_round.lua`

Idempotent; a second invocation returns `ALREADY_RESOLVED`. Payout for a
stake `a` on winning outcome `W` is `floor(a * total / pool_W)`. Two edge
cases the spec did not cover, decided here:

- **`pool_W == 0`** (nobody picked the winning outcome): every participant
  is refunded in full and the round is marked `refunded` rather than
  `resolved`.
- **Rounding dust.** Flooring each payout means `Σ payouts ≤ total`. The
  remainder is credited to a `system_dust` ledger account so debits and
  credits still balance exactly. Without this the double-entry invariant
  would fail on nearly every round.

### `refund_round.lua`

The host-disconnect and 60-second-timeout path. Also idempotent.

---

## 6. PostgreSQL double-entry schema

```sql
accounts        (id, kind, user_id NULL, room_id NULL, created_at)
                -- kind: user_wallet | room_escrow | round_pool
                --     | system_mint | system_dust
transactions    (id, idempotency_key UNIQUE, kind, room_id, round_id, occurred_at)
ledger_entries  (id, transaction_id, account_id, direction, amount CHECK (amount > 0))
```

The invariant that every transaction's debits equal its credits is enforced
by a `DEFERRABLE INITIALLY DEFERRED` constraint trigger validating the sum
at commit time — in the database, not in application code. The `UNIQUE`
constraint on `idempotency_key` is what makes at-least-once Kafka delivery
safe: a replayed event violates the constraint and is skipped.

**Flagship correctness test.** After a k6 run of thousands of concurrent
wagers, assert that each user's Redis balance equals the balance derived by
summing their `ledger_entries`, and that total tokens across the system are
conserved. This test is the evidence behind the 0.00% double-spend claim
and should be built deliberately rather than assumed.

---

## 7. Kafka topology

| Topic | Key | Partitions (local) | Consumer group |
|---|---|---|---|
| `wagers-placed` | `room_id` | 6 | `ledger-writer` |
| `rounds-settled` | `room_id` | 6 | `ledger-writer` |

Keying by `room_id` yields per-room ordering with cross-room parallelism.
Ordering matters concretely here: a settlement must never be processed
before the wagers it settles. Kafka runs single-node in **KRaft mode** (no
Zookeeper) to keep local resource use manageable.

---

## 8. Economy constants

Centralized in `internal/config` as named constants — no magic numbers at
call sites.

| Constant | Value |
|---|---|
| New account starting balance | 1,000 |
| Refill target | 1,000 |
| Refill eligibility threshold | balance < 200 |
| Refill quota | 3 per rolling 7-day window |
| Room buy-in bounds (host-set) | 100 – 100,000 |
| Account-holder stake cap | min(3 × room buy-in, account balance) |

---

## 9. Phases

Each phase ends in something runnable and verifiable. Phases 0–4 are the
MVP and contain the demo; 5 onward are separate milestones.

| # | Phase | Deliverable | Depends on | Tooling to import (see `ecc-survey.md`) |
|---|---|---|---|---|
| 0 | **Foundations** | Monorepo skeleton, `docker-compose.yml` (Redis/PostgreSQL/Kafka-KRaft), Makefile, GitHub Actions CI, config loader with fail-fast validation, structured logging, `/healthz` | — | `golang-*` (patterns/testing/tdd/verification) rules + skills; `docker-patterns` skill — import *before* starting this phase, not after |
| 1 | **Domain core (pure Go)** | Odds math, payout and dust distribution, round state machine, wallet rules (buy-in, 3× cap, partial buy-in, refill quota). No I/O; near-total unit coverage | 0 | None new — covered by Phase 0's Go tooling |
| 2 | **Redis layer** | Key schema, the three Lua scripts, Go wrappers, integration tests, and a concurrency suite: N goroutines racing a single wallet, asserting zero double-spend and exact token conservation | 1 | `redis-patterns` skill |
| 3 | **Auth + REST** | Register/login, room creation, join-by-code, JWT issuance, rate-limit middleware | 0, 2 | `api-design` skill |
| 4 | **WebSocket hub + round lifecycle** | Per-room owner goroutine (state owned by one goroutine receiving commands over a channel, no mutexes), client read/write pumps, ping/pong heartbeat, slow-client eviction, server-side lock timer and 60-second auto-refund fallback. Playable end to end from a CLI client | 3 | None new |
| 5 | **Kafka + ledger** | Outbox relay, `wagers-placed` and `rounds-settled` producers, ledger-worker consumer, migrations, deferred constraint trigger, Redis↔PostgreSQL reconciliation test | 2, 4 | `postgres-patterns`, `database-migrations` skills |
| 6 | **Frontend** | Next.js host console and participant view, live odds, countdown, Web Audio feedback | 4 | `react-patterns`, `nextjs-turbopack`, `accessibility` skills |
| 7 | **Load test + hardening** | k6 scripts, server-side p99 histograms, tuning against the SLAs, README with architecture diagram | 5, 6 | None new — spec already names k6 directly |
| 8 | **Deferred** | LLM question suggestions, Terraform live deployment, Prometheus/Grafana | 7 | Decide when unblocked |

**Import rule:** skills (Bucket 2/3) are cheap — one line in the availability listing until invoked — so pull each phase's skills in *before* that phase starts, no need to batch them all up front. Language-specific **rule dirs** (`.claude/rules/ecc/<language>/`) are different: they're always-loaded full text into every turn once installed, per `.claude/rules/ecc/common/agents.md`'s description of rules as passive/always-on. Install a rule dir only right before the phase that needs it, so irrelevant stack rules don't sit in every turn's context for phases that don't touch that stack yet.

Two sequencing choices are deliberate. **Phase 1 precedes all
infrastructure** because the money math is where correctness bugs hide, and
it is fully testable with nothing running — the cheapest possible place to
prove it right. **Phase 4 is the milestone that matters**: it is the first
point at which the product exists and can be shown to someone. Kafka and
the ledger are architecturally important but demo-invisible, so placing
them afterward means a stall there still leaves a working artifact.

---

## 10. Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Redis/Kafka divergence on crash | High without the fix | Critical — breaks the core audit claim | Redis Streams outbox (§1) |
| Payout rounding breaks ledger balance | High | High | Explicit floor plus `system_dust` policy; invariant trigger |
| Kafka resource-heavy under WSL2 | Medium | Medium | KRaft single-node, memory caps, a `make up-core` profile that omits Kafka for phases 0–4 |
| Scope overrun — eight phases, solo | High | High | Phases 0–4 are the shippable MVP; 5+ get their own planning passes |
| p99 measurement fidelity under WSL2 | Medium | Medium | Instrument server-side histograms; treat k6 client figures as secondary |
| Slow WebSocket client stalls room broadcast | Medium | High — breaks the <30 ms target | Bounded send buffer; evict on overflow rather than block |
| Lua script complexity hiding subtle bugs | Medium | High | Keep scripts small and single-purpose; cover every branch with an integration test |

---

## 11. Complexity: LARGE

Solo effort, MVP only (phases 0–4): roughly 40–60 hours. Through phase 7:
roughly 100–140 hours. The range is wide because phase 5 (Kafka plus
ledger) and phase 7 (hitting real latency targets) are the two areas that
reliably take longer than they appear.

---

## 12. Acceptance

- [ ] Phases 0–4 complete, producing an end-to-end playable round
- [ ] Concurrency suite proves zero double-spend under contention
- [ ] Redis↔PostgreSQL reconciliation test passes after a load run
- [ ] Test coverage meets the project's 80% minimum
- [ ] Security review run against the auth and wager-placement paths
