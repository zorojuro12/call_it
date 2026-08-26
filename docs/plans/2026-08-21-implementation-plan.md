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
│   │   ├── auth/               # argon2id hashing, JWT issue/verify — pure, no I/O (Phase 3)
│   │   ├── account/            # account lifecycle: register, login, refill claims (Phase 3)
│   │   ├── room/               # room lifecycle, short-code generation
│   │   ├── round/              # round orchestration + server-side timers
│   │   ├── wager/              # wager service (validate → Lua → broadcast)
│   │   ├── redisstore/         # redis client + go:embed'd Lua scripts
│   │   ├── ws/                 # hub, room, client pumps
│   │   ├── httpapi/            # REST handlers, middleware, error envelope
│   │   ├── events/             # event schemas, Kafka producer/consumer
│   │   └── ledger/             # PostgreSQL double-entry repository
│   ├── migrations/             # NNNN_name.up.sql / .down.sql
│   ├── scripts/lua/            # place_wager.lua, lock_round.lua, settle_round.lua, refund_round.lua
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

Brace placeholders (`{roomID}`, `{roundID}`, ...) are substitutions, **not**
Redis Cluster hash tags — `room:{roomID}` means the literal key
`room:7f3a...`. This deployment is single-node; if it ever moves to
Cluster, co-locating a room's keys would need real hash tags, which is a
schema change, not a wrapper change.

| Key | Type | Contents |
|---|---|---|
| `code:{roomCode}` | STRING | → `roomID` (join lookup) |
| `room:{roomID}` | HASH | `host_id`, `buy_in`, `status`, `created_at` |
| `room:{roomID}:wallets` | HASH | `userID` → session balance |
| `room:{roomID}:round` | STRING | → current `roundID`, set when a round opens and deleted at a terminal state (Phase 4b Amendment D2) |
| `room:{roomID}:opening` | HASH | `userID` → the effective balance granted at join, fixed for the session (Phase 4b Amendment D3) |
| `round:{roundID}` | HASH | `room_id`, `question`, `outcomes` (JSON array), `status`, `lock_at_ms`, `outcome_count`, `resolved_outcome` — `question`/`outcomes` added Phase 4b (Amendment D4) |
| `round:{roundID}:pools` | HASH | `0..n` → pool amount, `total` → sum |
| `round:{roundID}:wagers` | HASH | `{userID}:{outcomeIdx}` → amount |
| `round:{roundID}:bettors` | SET | distinct user IDs that have wagered |
| `idem:{key}` | STRING | cached result, TTL 24h |
| `ratelimit:{scope}:{id}` | ZSET | sliding window — score is the hit's ms timestamp, member a per-attempt UUID |
| `wager-outbox` | STREAM | outbox events awaiting relay to Kafka |
| `user:{userID}` | HASH | `email`, `display_name`, `password_hash`, `balance`, `created_at` |
| `email:{normalizedEmail}` | STRING | → `userID` (unique index, claimed via `claim_unique.lua`) |

`user:{userID}`/`email:{normalizedEmail}` and `ratelimit:{scope}:{id}`'s
actual implementation both landed in Phase 3 (Amendments B1, B6/B7,
`docs/plans/2026-08-25-phase-3-auth-rest.md`). `ratelimit` scopes in use:
`auth` (client IP, 10/1min, the register/login throttle), `api` (user ID,
60/1min, the authenticated-route throttle), `refill` (user ID,
`domain.RefillQuota`/7 days). Persistent accounts live in Redis rather
than PostgreSQL (B1) — Phase 3's dependency table only lists 0 and 2, not
5, so storing credentials in Postgres would have pulled most of Phase 5
forward. Phase 5's planning pass **resolved this in favour of keeping
them in Redis permanently**, with PostgreSQL holding monetary history
only; see §9's Phase 5 note for the reasoning and the Phase 7 revisit.

`round:{roundID}:bettors` was added in Phase 2 (Amendment A2,
`docs/plans/2026-08-24-phase-2-redis-layer.md`): the "N/M players have
wagered" progress signal (§4 of the design spec) counts distinct
*players*, not wagers, and `round:{roundID}:wagers`'s
`{userID}:{outcomeIdx}` fields can't answer that — one player betting two
outcomes is two fields there. `SCARD` on this set is the numerator; the
set's members never leave the server, only the count does.

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

### `lock_round.lua`

Added in Phase 2 (Amendment A3). A fourth script, not named above: something
has to perform the `open → locked` compare-and-set. Phase 4 owns the
countdown timer that decides *when* to fire it, but the Redis write itself
had no owner, and `settle_round.lua`'s flow (below) depends on the round
already being locked before it runs. A status CAS, nothing more; idempotent
in the sense that locking an already-locked round is a benign no-op, and
locking a resolved or refunded round is refused.

### `settle_round.lua`

Idempotent; a second invocation returns `ALREADY_RESOLVED`. Payout for a
stake `a` on winning outcome `W` is `floor(a * total / pool_W)`.

**Settlement math runs in Go, not Lua** (Phase 2 Amendment A1,
`docs/plans/2026-08-24-phase-2-redis-layer.md`) — this supersedes the
original design of computing the payout formula inside the script.
`internal/domain.Settle` already implements it at 100% coverage with a fuzz
test proving `Σ payouts + dust == Σ stakes`; reimplementing it in Lua would
be a second, less-tested copy that has to agree with the first exactly. The
duplication is unavoidable the other way too: `Settlement.Results` — the
per-player reveal Phase 4 broadcasts when a round closes — comes from
`domain.Settle` regardless, so Go runs the settlement math either way.
Settlement is therefore: `lock_round.lua` CASes `open → locked`; Go reads
the round's stakes and calls `domain.Settle`; `settle_round.lua` CASes
`locked → resolved|refunded` and applies the computed payouts atomically,
crediting each wallet and emitting one outbox event. The read-then-write
window between Go's read and the script's write is safe because
`place_wager.lua` already rejects any wager once the round is locked, so
the wagers hash cannot grow in between.

Two edge cases the spec did not cover, decided here — both still true
under the Go-computes/Lua-applies split, since `domain.Settle` produces
them, not the script:

- **`pool_W == 0`** (nobody picked the winning outcome): every participant
  is refunded in full and the round is marked `refunded` rather than
  `resolved`.
- **Rounding dust.** Flooring each payout means `Σ payouts ≤ total`. The
  remainder is credited to a `system_dust` ledger account so debits and
  credits still balance exactly. Without this the double-entry invariant
  would fail on nearly every round.

### `refund_round.lua`

The host-disconnect and 60-second-timeout path. Also idempotent. Unlike
settlement there is nothing to compute — refunding is the identity function
on stakes — so this script reads the wagers hash inside its own atomic unit
rather than taking amounts from Go.

### Phase 3 additions (Amendment B6)

Three more scripts, none on the wager path:

- **`claim_unique.lua`** — `SETNX KEYS[1] ARGV[1]`; on success `HSET
  KEYS[2]` with the remaining `ARGV` pairs and return `{'OK'}`; on
  collision return `{'TAKEN', existingID}` without mutating anything.
  One script serves both call sites — claiming an email at registration
  and claiming a room code at creation are the same operation (claim a
  unique secondary index and create the entity it points at,
  atomically), so a second copy would be duplication, not clarity.
- **`rate_limit.lua`** — the sliding-window limiter behind `Store.Allow`.
  Evicts aged-out members (`ZREMRANGEBYSCORE`), then either `ZADD`s the
  new attempt and returns `{'ALLOWED', remaining, member, resetAtMs}` or
  returns `{'DENIED', '0', '', retryAfterMs, resetAtMs}` without
  recording anything — a limiter that counted denied attempts would
  extend its own window under sustained load.
- **`top_up_balance.lua`** — sets a user's balance to a target only if it
  is currently below the target, returning the credited delta. Setting
  to the target rather than incrementing by a Go-computed delta is what
  makes a concurrent double-claim safe: the second call reads the
  already-topped balance and credits nothing.

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

| Constant | Value |
|---|---|
| New account starting balance | 1,000 |
| Refill target | 1,000 |
| Refill quota | 3 per rolling 7-day window |
| Room buy-in bounds (host-set) | 100 – 10,000 |
| Account-holder stake cap | min(3 × room buy-in, account balance) |

Defined in `internal/domain/economy.go`, not `internal/config`: these are
platform invariants rather than deployment configuration, and the domain
must not depend on the env loader. The separate "refill eligibility
threshold" this table previously carried was removed — it created a dead
zone between the threshold and the target, and the quota was always the
real limiter. Buy-in ceiling lowered from 100,000, which sat a hundred
times above the refill target. See
`docs/plans/2026-08-23-phase-1-domain-core.md` §A1–A3.

---

## 9. Phases

Each phase ends in something runnable and verifiable. Phases 0–4 are the
MVP and contain the demo; 5 onward are separate milestones.

| # | Phase | Deliverable | Depends on | Tooling to import (see `ecc-survey.md`) |
|---|---|---|---|---|
| 0 | **Foundations** | Monorepo skeleton, `docker-compose.yml` (Redis/PostgreSQL/Kafka-KRaft), Makefile, GitHub Actions CI, config loader with fail-fast validation, structured logging, `/healthz` | — | `golang-*` (patterns/testing/tdd/verification) rules + skills; `docker-patterns` skill — import *before* starting this phase, not after |
| 1 | **Domain core (pure Go)** | Odds math, payout and dust distribution, round state machine, wallet rules (buy-in, 3× cap, partial buy-in, refill quota). No I/O; near-total unit coverage | 0 | None new — covered by Phase 0's Go tooling |
| 2 | **Redis layer** | Key schema, four Lua scripts (`place_wager`, `lock_round`, `settle_round`, `refund_round`), Go wrappers, integration tests, and a concurrency suite: N goroutines racing a single wallet, asserting zero double-spend and exact token conservation | 1 | `redis-patterns` skill |
| 3 | **Auth + REST** ✅ | Register/login, room creation, join-by-code, JWT issuance, rate-limit middleware | 0, 2 | `api-design` skill |
| 4a | **WebSocket transport** ✅ | Authenticated room socket (JWT verified at handshake, no per-message lookup), per-room owner goroutine (state owned by one goroutine receiving commands over a channel, no mutexes), client read/write pumps, ping/pong heartbeat, slow-client eviction, join/leave presence broadcast | 3 | None new |
| 4b | **Round lifecycle** ✅ | Rounds, wagers, live odds, server-side lock timer and 60-second auto-refund fallback, host-resolve settlement reveal, session-end persistence, playable end to end from a CLI client | 4a | None new |
| 5a | **Outbox → Kafka + ledger schema** | Outbox relay binary (`cmd/relay`), `wagers-placed`/`rounds-settled` producers, `internal/events` schemas, PostgreSQL migrations, ledger schema, deferred constraint trigger | 2, 4b | `postgres-patterns`, `database-migrations` skills |
| 5b | **Double-entry ledger** | `cmd/ledger-worker` consumer, `internal/ledger` repository, idempotent replay on the `idempotency_key` unique constraint, Redis↔PostgreSQL reconciliation test | 5a | None new |
| 6 | **Frontend** | Next.js host console and participant view, live odds, countdown, Web Audio feedback | 4b | `react-patterns`, `nextjs-turbopack`, `accessibility` skills |
| 7 | **Load test + hardening** | k6 scripts, server-side p99 histograms, tuning against the SLAs, README with architecture diagram | 5b, 6 | None new — spec already names k6 directly |
| 8 | **Deferred** | LLM question suggestions, Terraform live deployment, Prometheus/Grafana | 7 | Decide when unblocked |

**Phase 5 split into 5a/5b (added at Phase 5's planning pass).** Done
*before* writing the detailed task breakdown, which is what the
Phase-sizing note below prescribes and what Phase 3 failed to do. The
original Phase 5 row bundled four separable deliverables — a relay, Kafka
producers, a schema with migrations, and a ledger consumer with a
reconciliation test — the same shape that made Phase 3 the most expensive
phase measured. Split at the **durability boundary**: 5a ends when events
reach Kafka durably and an empty-but-correct ledger schema rejects an
unbalanced transaction; 5b ends when those events have become ledger rows
that reconcile against Redis.

The boundary is drawn there rather than at "all Postgres work in one
phase" for two reasons. It puts the migrations and the deferred
constraint trigger next to the two skills that serve them
(`database-migrations`, `postgres-patterns`) instead of stranding them a
phase away from their tooling. And it isolates the flagship correctness
work — the ledger writer and the reconciliation test §6 calls "the
evidence behind the 0.00% double-spend claim" — into 5b alone, so 5a is
plumbing that can be verified structurally while 5b keeps the
cross-cutting attention that kind of proof needs.

**Phase 4 split into 4a/4b (added at Phase 4a close-out).** Scoping Phase 4
as written above produced ~13 tasks / ~38 checkpoints — the shape this
table's own **Phase-sizing note** (added at Phase 3 close-out, see below)
warns against, after Phase 3 landed at 2,904 lines and exhausted a full
token window. Split at the layer boundary — 4a is pure transport with zero
game knowledge (`docs/plans/2026-08-26-phase-4a-ws-transport.md`), 4b is
rounds, wagers, and money over that transport
(`docs/plans/2026-08-26-phase-4b-round-lifecycle.md`) — via the seam
`ws.MessageHandler` plus `ws.Room.Broadcast`. 4a is also the first phase
planned under `writing-plans-tuned`, an experimental token-budget-tuned
variant of `writing-plans`; see that plan's own "Measured" section for the
experiment's outcome.

**Phase 3 note (added at Phase 2 close-out, Amendment A4/A5).** Phase 2
already wrote the real room and round writers — `CreateRoom`, `JoinRoom`,
`CreateRound`, `LockRound` — in `internal/redisstore`, since it owns "key
schema" per this table and test-only fixtures would have drifted from the
real thing. Phase 3's `internal/room` and `internal/round` wrap these
functions rather than writing hashes directly; short-code generation still
belongs to Phase 3 (`CreateRoom` takes a code as a parameter, it does not
invent one). The shared sliding-window rate limiter this table's key schema
names (`ratelimit:{scope}:{id}`) was deferred to Phase 3 as well — nothing
in Phase 2 calls it, so it landed next to its first caller (the refill
endpoint and wager-placement middleware) instead of sitting unused for a
full phase.

**Phase 4 note (added at Phase 3 close-out).** `internal/room`'s
`Service.Create`/`Service.Join`, `internal/account`'s `Service`, the
shared rate limiter, and `internal/auth`'s token `Issuer` all already
exist and are tested — Phase 4 wraps and calls them from the WebSocket
handshake and round lifecycle, it does not reimplement any of them. In
particular, the wager-placement throttle's call site (keyed how the
WS handshake determines identity) is Phase 4's to wire; `rate_limit.lua`
and `Store.Allow`/`Revoke` are already built and proven.

**Phase 5 note (added at Phase 3 close-out, Amendment B1) — RESOLVED at
Phase 5's planning pass.** Persistent accounts (`user:{userID}`,
`email:{normalizedEmail}`) **stay in Redis.** PostgreSQL holds monetary
history only. Credentials are not monetary history, and nothing in the
double-entry design needs the user row co-located: the ledger references
`user_id` as an opaque identifier, never joining to it. Migrating would
have pulled `internal/account`, `claim_unique.lua`'s email path,
`top_up_balance.lua`, and a live-data migration into the phase already
carrying the most risk, for no benefit the ledger can actually use. The
one thing that might have forced the issue is not a conflict: §6's
`accounts` table holds **ledger** accounts (`user_wallet`, `room_escrow`,
`system_dust`), a different and non-colliding sense of the word.

Revisit at Phase 7, not before. The real long-term argument for migrating
is foreign-key integrity between ledger accounts and users, which is a
hardening concern — it buys nothing while the ledger is being built and
would obscure the reconciliation test's result if introduced alongside it.

**Import rule:** skills (Bucket 2/3) are cheap — one line in the availability listing until invoked — so pull each phase's skills in *before* that phase starts, no need to batch them all up front. Language-specific **rule dirs** (`.claude/rules/ecc/<language>/`) are different: they're always-loaded full text into every turn once installed, per `.claude/rules/ecc/common/agents.md`'s description of rules as passive/always-on. Install a rule dir only right before the phase that needs it, so irrelevant stack rules don't sit in every turn's context for phases that don't touch that stack yet.

**Phase-sizing note (added at Phase 3 close-out).** Read this before
scoping any future phase's task/checkpoint breakdown. Phase 3's plan
landed at 2,904 lines / 38 checkpoints — close to Phase 1's old
code-heavy-format length (2,999 lines) and nearly double Phase 2's
(1,599 lines), despite using the same spec-driven contract format Phase
2 introduced. Normalized per checkpoint the format held up reasonably
(Phase 2: ~64 lines/checkpoint; Phase 3: ~76, a ~19% increase, not the
~80% the raw totals suggest) — so this was **not** a regression in
`writing-plans` itself. The actual cause: Phase 3 bundled four
separable deliverables into one phase (credentials, tokens, the shared
rate limiter, and the full REST surface over rooms/refills), which is
also why it produced the most checkpoints of any phase so far (38, vs.
Phase 2's 25). The plan's own self-review flagged this at write
time — "Tasks 1–10 are a coherent stopping point" — meaning the phase
boundary itself was drawn too wide, not that any one task was
over-specified.

**Recommendation:** when a phase's own draft self-review names a
mid-phase stopping point like that, treat it as a signal to split the
phase in the parent plan (here, §9's phase table) *before* writing the
detailed task/checkpoint plan — not as an interesting note to leave in
place. A phase should be one deliverable people would actually want to
ship independently, not several bundled because they're thematically
related. Check this before writing Phase 5's plan in particular — it
touches Kafka, the ledger, and a reconciliation test, which risks the
same bundling Phase 3 hit.

**Checked, and it did.** Phase 5 was split into 5a/5b at its planning
pass, before the task breakdown was written — see the split note above.
This is the first time the recommendation was applied as intended rather
than noted after the fact.

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

- [x] Phases 0–4 complete, producing an end-to-end playable round — `internal/ws.TestEndToEndRound` (Phase 4b Task 10 CP1) is the evidence: a host and two players register/join over REST, open a round, wager, lock, and resolve over the real socket transport, with token conservation (`wallets + dust == combined opening stakes`) asserted at the end.
- [ ] Concurrency suite proves zero double-spend under contention
- [ ] Redis↔PostgreSQL reconciliation test passes after a load run
- [ ] Test coverage meets the project's 80% minimum
- [ ] Security review run against the auth and wager-placement paths
