# CallIt — Design Spec

**Date:** 2026-08-21
**Status:** Approved for planning

## 1. Purpose

CallIt is a real-time flash-prediction / micro-wagering platform for group
watch parties (Discord hangouts, esports streams, live sports). A host
launches a short prediction round (e.g. a 10-second window: "Will he clutch
this 1v2?"). Connected participants place instant wagers with virtual
tokens; live odds update via a pari-mutuel model; the host resolves the
round and payouts settle automatically.

Secondary goal (explicitly prioritized where it conflicts with fastest
build speed): the project should demonstrate depth on a specific set of
resume-relevant technologies — Go, Redis (atomic Lua scripting), Kafka,
PostgreSQL, WebSockets — with real, defensible implementations rather than
token gestures at each technology.

## 2. Core Tech Stack

- **Backend:** Go 1.22+ — goroutines, channels, WebSocket broadcast hub.
- **In-memory store & locking:** Redis 7.2+ — atomic Lua scripts for wager
  placement, sliding-window rate limiting (reused for both wager throttling
  and the account refill quota), room presence.
- **Event broker:** Apache Kafka, run via Docker Compose (not Redpanda —
  chosen deliberately for deeper, more defensible hands-on Kafka experience
  even though it's heavier to run locally).
- **Relational database:** PostgreSQL 16 — double-entry ledger, user
  accounts, historical analytics.
- **Frontend:** React 19 / Next.js, Tailwind CSS, WebSockets, Web Audio API.
- **Infra:** Docker Compose (primary local dev/demo target), GitHub
  Actions CI, Terraform scripts written as real infra-as-code but not
  necessarily applied to a live cloud deployment in the first phase.
- **Repo structure:** monorepo — `/backend`, `/frontend`, root-level
  `docker-compose.yml`.

## 3. Identity & Account Model

- All participants join a room via a **short alphanumeric code + shareable
  link** generated at room creation.
- **Guests**: no account. Provide only a display name. Get a session-scoped
  balance equal to the room's buy-in; wiped when the session ends.
- **Account holders**: persistent identity (login mechanism — email or
  OAuth — to be finalized during implementation planning). Persistent token
  balance that carries across sessions.
- **Room buy-in is host-configurable** at room creation time (not a fixed
  platform constant).
- Account holders may stake **up to 3x the room's buy-in**, bounded by
  their actual account balance — whichever is lower. If their balance is
  below the room's buy-in, they join with a **partial buy-in** (whatever
  they have), surfaced transparently in the UI (e.g. "joined with
  200/2000").
- At session end, an account holder's **net profit/loss** for that session
  (not their final balance) is added to their persistent account total.
  Persistent balance floors at 0 — a session can never cost more than what
  was staked into it.

### Refills

- If an account holder's persistent balance drops below a low threshold
  (exact value TBD in planning — e.g. 20% of the platform refill target),
  they may **manually claim a refill**.
- A refill tops the account balance up to a **fixed platform-wide amount**
  (independent of any specific room's buy-in, since refills happen before
  a room is chosen).
- Max **3 refill claims per rolling 7-day window** per account.
- Implemented via the same Redis sliding-window rate-limiting primitive
  used for wager-placement throttling — one mechanism, two use cases.

## 4. Gameplay & Round Lifecycle

- The host manually types each round's question and defines **2-4 custom
  outcome options** (not fixed to binary Yes/No) — there is no external
  data feed to consult, since these are live, in-the-moment events on
  whatever the group is watching.
- **The host cannot place wagers in their own room.** This removes the
  conflict of interest inherent in a single party both staking on and
  resolving an outcome. (Cheap to reverse later — a single guard clause
  against `room.host_id` in the wager-placement path, no schema change,
  since `host_id` is already tracked for room lifecycle regardless.)
- Server-side countdown timer enforces lockout at t=0. Any wager arriving
  with a server-side timestamp after lockout is rejected, regardless of
  when the client sent it — closes the client-latency exploit.
- After lockout, the **host manually resolves** which outcome occurred.
  The engine settles payouts using the pari-mutuel model, generalized to N
  outcome pools:

  `Payout Multiplier_X = Total Pool / Pool_X`

- **Host disconnect handling:** if the host disconnects mid-round, the
  round auto-locks at t=0 as normal. If it remains unresolved 60 seconds
  after lockout, all wagers in that round are **auto-refunded** via a
  fallback queue.
- **Wagers are anonymous until the round reaches a terminal state.** No
  participant may learn who backed which outcome, or for how much, until
  the round resolves or refunds; at that point every participant's stake
  and net result are revealed together. This is not presentation polish:
  the host resolves the outcome, so a host who could see positions
  beforehand could favour an outcome to benefit a friend, reintroducing
  through a side channel the conflict of interest that the
  host-cannot-wager rule removes. Live odds are unaffected — they are
  computed from pool totals, never from per-user positions.

  While a round is open, the only permitted progress signal is an
  aggregate counter of how many players have wagered — "2/5 players have
  placed their bets". No per-user notification, indicator, or wager
  count. The denominator excludes the host, who cannot wager; the
  counter counts players rather than wagers, so a player's second wager
  moves the pools but not the counter.

  Known limitation: each broadcast is triggered by one wager, so a pool
  delta is one player's exact stake with only the identity missing. In a
  room of three or four the wagerer is easy to guess; in a room of
  thirty the crowd hides them. Closing the gap would mean batching pool
  updates, which conflicts with the <30 ms target in §7, or adding noise
  to odds that must be exact at settlement. It is accepted. What the
  rule guarantees is that the host never has a systematic, complete view
  of the board before resolving — not that no individual stake can ever
  be guessed.

## 5. Write Path / Data Flow

1. Client sends a wager over an authenticated WebSocket connection
   (`idempotency_key` UUIDv4 attached).
2. Go WS handler invokes an **atomic Redis Lua script** that, in one
   step: checks the pool is open (`is_open == "1"`) and the room's lockout
   hasn't passed, checks sufficient balance, deducts the wallet, increments
   the outcome pool and total pool, records the wager, dedupes on
   `idempotency_key` (returns the existing transaction if it's a retry),
   and appends the accepted wager to a Redis Stream outbox (see step 4).
   Target: reject/accept in **< 5ms**, zero state corruption.
3. On acceptance, updated odds broadcast to all WebSocket clients in the
   room. Target: **< 30ms** end-to-end.
4. **Transactional outbox.** The `XADD` to the `wager-outbox` stream
   happens *inside* the same atomic Lua script as the balance mutation, so
   an accepted wager and its event record commit together or not at all.
   A separate relay process consumes the stream, produces to Kafka topic
   **`wagers-placed`**, and acknowledges only after a successful produce.
   A crash between the two leaves the event in the stream to be replayed.
   Delivery into Kafka is therefore at-least-once, deduplicated downstream
   by `idempotency_key`.

   Without this, a crash between the Redis write and the Kafka produce
   would debit a wallet that the ledger never learns about — Redis and
   PostgreSQL would diverge silently, defeating the audit-trail and
   double-spend guarantees below.
5. The WebSocket server **never writes directly to PostgreSQL** on this
   path. A dedicated Go consumer reads `wagers-placed` and batch-writes to
   the PostgreSQL double-entry ledger (`transactions`, `ledger_entries`),
   maintaining a permanent audit trail decoupled from the hot path.

## 6. Auth

- On joining a room (REST call with room code + display name, or account
  login), the server issues a **short-lived signed JWT** containing the
  participant's identity and room id.
- The client presents this JWT when opening the WebSocket connection.
  Verified server-side without a per-message database hit.

## 7. Performance & Scale Targets (unchanged from original brief)

- p99 bet placement latency: < 15ms.
- Global WebSocket sync latency: < 30ms.
- Target throughput: 5,000+ requests/sec, benchmarked with k6.
- Double-spend tolerance: exactly 0.00%.

## 8. Explicitly Deferred (not in first implementation phase)

- LLM-generated flash questions / live commentary (Groq / Claude 3.5
  Haiku) — architecturally bolted on later via an async worker off the
  critical path; not needed to validate the core wager/settlement loop.
- Live public cloud deployment — build and demo locally via Docker Compose
  first; Terraform scripts are written for real but not necessarily
  applied to a running cloud environment in this phase.
- N-outcome UI polish beyond functional correctness (visual design of
  multi-outcome selection) — functional first, polish later.
- Observability stack (Prometheus/Grafana) — structured logging is
  sufficient for the first phase; can be added once the core loop is
  proven.

## 9. Items Resolved in the Implementation Plan

These were left for the planning phase rather than this design doc, since
they're implementation details rather than architectural decisions. All of
them are now settled in
[`docs/plans/2026-08-21-implementation-plan.md`](../plans/2026-08-21-implementation-plan.md);
that plan is the authoritative reference for each:

- Account login mechanism → email + password, argon2id, JWT HS256
  (plan §2).
- Refill threshold and platform-wide refill target → plan §8.
- Redis key schema and Lua script contracts → plan §4 and §5.
- Go WebSocket hub internals → plan §9, phase 4 (per-room owner goroutine,
  bounded send buffers, ping/pong heartbeat).
- Kafka topic partitioning and consumer group design → plan §7.
- PostgreSQL double-entry schema → plan §6.
- Go repository folder layout → plan §3.
