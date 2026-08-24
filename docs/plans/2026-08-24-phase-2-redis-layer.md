# Phase 2 — Redis Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `internal/redisstore` — the atomic Redis layer that owns the
key schema, the Lua scripts that mutate wallets and pools, and the Go wrappers
around them — proven by an integration suite and a concurrency suite that
demonstrates zero double-spend and exact token conservation.

**Architecture:** Every balance-mutating operation is a single Lua script, so
a wallet debit, the pool increments, the wager record, and the outbox `XADD`
commit together or not at all. Lockout is judged by `redis.call('TIME')` inside
the script, never by a timestamp the caller supplies. Money *math* stays in
`internal/domain` (Phase 1, 100% covered and fuzzed); the scripts apply results,
they do not derive them. Go wrappers translate Lua status codes into typed Go
errors, and are the only thing in the codebase that knows a Redis key's shape.

**Tech Stack:** Go 1.22.10 · `github.com/redis/go-redis/v9` **v9.18.0** ·
Redis 7.2 (Lua 5.1 + cjson) · Docker Compose.

**Spec:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md)
§4 (round lifecycle, anonymity), §5 (write path, outbox), §7 (latency targets).
Parent plan: [`docs/plans/2026-08-21-implementation-plan.md`](2026-08-21-implementation-plan.md)
§4 (key schema), §5 (script contracts), §9 (phase table).

---

## Global Constraints

- **Go 1.22.10.** CI pins `go-version: "1.22"`. Do not raise it.
- **`go-redis` is pinned to v9.18.0.** v9.19.0 and later carry `go 1.24` in
  their `go.mod` and will not build here. Verified 2026-08-24 across
  v9.18.0 / v9.19.0 / v9.20.0 / v9.20.1 / v9.21.0 / v9.22.0. If a later
  version is wanted, the Go toolchain and CI pin must move first.
- **This is the project's first external dependency.** `go.sum` appears in
  this phase; `actions/setup-go` caching turns on with it.
- **All amounts are integer token units.** No float ever reaches Redis. Odds
  become `float64` only at the presentation layer, via `domain.Multiplier`.
- **`internal/domain` stays free of I/O.** This phase imports domain; domain
  never imports this phase. No Redis type, context, or error crosses into it.
- **The money math is not reimplemented in Lua.** See Amendment A1.
- **Wagers stay anonymous until the round is terminal.** No script return
  value, and no wrapper return type, may carry per-user wager data before a
  round resolves or refunds. Pool totals and an aggregate distinct-bettor
  count are the only in-round signals permitted (spec §4).
- **80% coverage minimum**, TDD RED→GREEN→IMPROVE, AAA test structure,
  table-driven where the cases are homogeneous.
- **`gofmt` clean.** CI fails on any output from `gofmt -l .`.

---

## Amendments to the parent plan

These change decisions recorded in `docs/plans/2026-08-21-implementation-plan.md`.
Task 9 folds them back into the committed docs — the same close-out mechanism
Phase 1 used for its A1–A3.

### A1 — Settlement math stays in Go; `settle_round.lua` only applies it

Parent plan §5 specifies that `settle_round.lua` computes
`floor(a * total / pool_W)` per stake. **Superseded.** `domain.Settle` already
implements that formula, along with the dust remainder and the
nobody-backed-the-winner refund path, at 100% coverage with a fuzz test
asserting `Σ payouts + dust == Σ stakes`. Reimplementing it in Lua would create
a second, less-tested copy of the money rules that must agree exactly with the
first.

The decisive argument is that the duplication is unavoidable under the Lua
design anyway: `Settlement.Results` — the per-player reveal that Phase 4
broadcasts when a round closes — comes from `domain.Settle`. So Go must run
the settlement math regardless of what Lua does. Having Lua *also* compute it
means running it twice per round in two languages and hoping they match.

Settlement therefore runs:

1. `lock_round.lua` — CAS `open → locked` (Amendment A3).
2. Go reads `round:{roundID}:wagers` into `[]domain.Stake`.
3. Go calls `domain.Settle(stakes, winningOutcome, outcomeCount)`.
4. `settle_round.lua` — CAS `locked → resolved|refunded`, credit each payout,
   `XADD` the settlement event. One atomic unit.

**Why the read-then-write window is safe.** `place_wager.lua` rejects any wager
when `TIME >= lock_at_ms` (parent plan §5, step 2), independently of the status
field. Once the lock instant has passed, the wagers hash cannot grow, so the
stakes Go reads in step 2 are the stakes that exist in step 4. The `locked →`
CAS in step 4 additionally makes a concurrent second settlement a no-op.

`refund_round.lua` is **not** affected by this amendment: refunding is the
identity function on stakes, there is nothing to compute, so it reads the wagers
hash inside its own atomic unit and needs no Go round trip.

### A2 — New key: `round:{roundID}:bettors` (SET)

Spec §4 requires an aggregate progress signal while a round is open — "2/5
players have placed their bets" — and specifies it counts *players*, not
wagers, so a player's second wager must not move it. The parent plan's §4 key
schema has no key that answers this. `round:{roundID}:wagers` cannot: its
fields are `{userID}:{outcomeIdx}`, so one player betting two outcomes
contributes two fields.

Added: `round:{roundID}:bettors`, a SET of user IDs, `SADD`ed inside
`place_wager.lua`. `SCARD` is the numerator. The denominator (players in the
room, excluding the host) comes from `HLEN room:{roomID}:wallets` minus one.

This is anonymity-safe: a set cardinality reveals no identity and no amount.
The set's *members* must never leave the server — the wrapper returns the count,
never the set.

### A3 — New script: `lock_round.lua`

Parent plan §5 names three scripts. A fourth is required: something must
perform `open → locked`. Phase 4 owns the countdown timer that decides *when*
to fire it, but the Redis write itself had no owner, and A1's settlement flow
depends on it. It is a status compare-and-set, nothing more.

### A4 — Room and round writers live in `internal/redisstore`

The wager scripts read four structures that must already exist. Phase 2 owns
"key schema" per §9, so it writes the real creators — `CreateRoom`, `JoinRoom`,
`CreateRound` — rather than test-only fixtures that Phase 3 would later
reimplement and drift from.

Seam for Phase 3: **short-code generation stays in Phase 3.** `CreateRoom`
accepts a code as a parameter and writes the `code:{roomCode}` mapping; it does
not invent codes. Phase 3's `internal/room` generates the code and wraps these
functions — it must not write room hashes directly.

### A5 — The shared rate limiter is deferred to Phase 3

Parent plan §4 lists `ratelimit:{scope}:{id}`, and CLAUDE.md requires one
sliding-window limiter shared by refills and wager throttling. Nothing in
Phase 2 calls it: its consumers are Phase 3's refill endpoint and rate-limit
middleware. Building it here would leave a tested primitive unused for a full
phase. It lands in Phase 3, next to its first caller, still as one
implementation with two call sites.

---

## File Structure

```
backend/
├── go.mod                          # MODIFY — first external dependency
├── go.sum                          # CREATE
├── scripts/lua/
│   ├── embed.go                    # CREATE — package lua, go:embed the .lua files
│   ├── place_wager.lua             # CREATE
│   ├── lock_round.lua              # CREATE
│   ├── settle_round.lua            # CREATE
│   └── refund_round.lua            # CREATE
├── internal/config/config.go       # MODIFY — add RedisAddr, RedisDB
└── internal/redisstore/
    ├── keys.go                     # CREATE — every key name in the system
    ├── client.go                   # CREATE — client construction
    ├── errors.go                   # CREATE — Lua status codes → Go errors
    ├── room.go                     # CREATE — CreateRoom, JoinRoom, wallet reads
    ├── round.go                    # CREATE — CreateRound, LockRound, RoundState
    ├── wager.go                    # CREATE — PlaceWager
    ├── settle.go                   # CREATE — ReadStakes, SettleRound, RefundRound
    └── testmain_test.go            # CREATE — shared integration harness
```

**Why `scripts/lua` is its own package.** `go:embed` cannot reach outside the
embedding package's directory, and CLAUDE.md's layout puts the scripts at
`backend/scripts/lua/`. A one-file `package lua` there exposes each script as
an exported `string`, which keeps the `.lua` files as real `.lua` files
(editor syntax highlighting, external linting) rather than Go string literals.

**Why one file per concern in `redisstore`.** `keys.go` is the single place a
key name is constructed — no other file may build a key by concatenation, so
the schema has exactly one definition. The rest split by the entity they
mutate, which is also how their tests split.

---

## Key schema (authoritative for this phase)

Placeholders in braces are substitutions, **not** Redis Cluster hash tags —
`room:{roomID}` means the literal key `room:7f3a...`. This deployment is
single-node; if it ever moves to Cluster, co-locating a room's keys would need
real hash tags, which is a schema change, not a wrapper change.

| Key | Type | Fields / contents |
|---|---|---|
| `code:{roomCode}` | STRING | `roomID` |
| `room:{roomID}` | HASH | `host_id`, `buy_in`, `status`, `created_at` |
| `room:{roomID}:wallets` | HASH | `userID` → session balance (integer) |
| `round:{roundID}` | HASH | `room_id`, `status`, `lock_at_ms`, `outcome_count`, `resolved_outcome` |
| `round:{roundID}:pools` | HASH | `0`..`n-1` → pool amount, `total` → sum |
| `round:{roundID}:wagers` | HASH | `{userID}:{outcomeIdx}` → amount |
| `round:{roundID}:bettors` | SET | user IDs that have wagered (A2) |
| `idem:{key}` | STRING | cjson-encoded cached reply, TTL 86400s |
| `wager-outbox` | STREAM | `XADD`ed inside the mutating scripts |

`resolved_outcome` is absent until the round resolves, and stays absent on a
refunded round.

---

## Lua conventions

Every script returns a **flat array of strings** whose first element is a
status code. Callers switch on element 0.

| Code | Meaning | Produced by |
|---|---|---|
| `OK` | Applied | all |
| `POOL_LOCKED` | Round not open, or `TIME >= lock_at_ms` | `place_wager` |
| `INVALID_OUTCOME` | Outcome index outside `0..outcome_count-1` | `place_wager` |
| `HOST_CANNOT_BET` | Caller is `room.host_id` | `place_wager` |
| `NOT_IN_ROOM` | No wallet field for this user | `place_wager` |
| `INSUFFICIENT_FUNDS` | Balance below the stake | `place_wager` |
| `ALREADY_LOCKED` | Round already `locked` (benign) | `lock_round` |
| `ROUND_TERMINAL` | Round already `resolved`/`refunded` | `lock_round` |
| `NOT_LOCKED` | Round is not `locked` | `settle_round`, `refund_round` |
| `ALREADY_RESOLVED` | Round already terminal (benign replay) | `settle_round`, `refund_round` |

Three rules the scripts must obey, each verified against Redis 7.2 on
2026-08-24:

1. **Never place `nil` inside a returned table.** Lua→Redis conversion
   truncates the reply at the first `nil`. `return {'OK', nil, 'AFTER'}`
   returns just `OK`, silently. Use `''` for an absent value.
2. **Return numbers as strings** via `tostring()`. Mixed-type arrays convert
   unevenly; uniform strings parse predictably in Go.
3. **`cjson` is available** and round-trips a flat string array exactly, which
   is what makes the `idem:{key}` cache work: `cjson.encode` the reply on
   accept, `cjson.decode` and return it verbatim on replay.

Every rejection path must mutate **nothing** — no wallet change, no pool
change, no outbox entry, no idempotency key written. Each rejection test
asserts this explicitly.

---

## Test harness conventions

Integration tests talk to a **real Redis**. There is no fake, and no
build tag: `//go:build integration` would exclude these from `go test ./...`,
which is precisely how a concurrency suite rots unnoticed.

- Address comes from `REDIS_ADDR`, defaulting to `localhost:6379`.
- **Unreachable Redis is a test failure, never a skip.** A suite that reports
  PASS while proving nothing is worse than a red one.
- Tests run against **DB 15**, never DB 0, so a run cannot destroy local dev
  state. `FLUSHALL` is forbidden; `FLUSHDB` on 15 is permitted in `TestMain`.
- Every test namespaces its keys with a unique prefix derived from
  `t.Name()`, so tests remain independent under `-race` and parallelism.
- `make test` brings Redis up and waits for health before running Go, so the
  normal path cannot be red for the wrong reason.

---

## Task 1: Dependency, configuration, key schema, and the test harness

Nothing in this phase can be tested until Redis is reachable from Go and every
key has exactly one definition. This task ends with a client that connects, a
key module the rest of the phase builds on, and a `make test` that cannot
produce a false green.

**Files:**
- Modify: `backend/go.mod`, create `backend/go.sum`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Create: `backend/internal/redisstore/keys.go`
- Create: `backend/internal/redisstore/keys_test.go`
- Create: `backend/internal/redisstore/client.go`
- Create: `backend/internal/redisstore/testmain_test.go`
- Modify: `Makefile`, `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  ```go
  // package config
  type Config struct {
      Port      int
      Env       string
      LogLevel  string
      RedisAddr string // default "localhost:6379"
      RedisDB   int    // default 0, valid 0-15
  }

  // package redisstore
  type Store struct{ /* unexported */ }
  func New(addr string, db int) (*Store, error)
  func (s *Store) Close() error
  func (s *Store) Ping(ctx context.Context) error

  const OutboxStream   = "wager-outbox"
  const PoolTotalField = "total"

  func RoomKey(roomID string) string
  func RoomWalletsKey(roomID string) string
  func RoomCodeKey(code string) string
  func RoundKey(roundID string) string
  func RoundPoolsKey(roundID string) string
  func RoundWagersKey(roundID string) string
  func RoundBettorsKey(roundID string) string
  func IdemKey(key string) string
  func WagerField(userID string, outcome int) string
  func ParseWagerField(field string) (userID string, outcome int, err error)
  ```

### Setup steps (one commit, no test cycle — this is scaffolding)

- [ ] **Step 1: Add the dependency at the pinned version**

```bash
cd backend
go get github.com/redis/go-redis/v9@v9.18.0
go mod tidy
```

Verify `go.mod` reads `github.com/redis/go-redis/v9 v9.18.0` and that the Go
directive is still `go 1.22.10`. If `go mod tidy` tries to raise the Go
version, the wrong `go-redis` version was resolved — see Global Constraints.

- [ ] **Step 2: Make `make test` guarantee Redis is up**

In the root `Makefile`, change the `test` target so it starts Redis and waits
for the container to report healthy before running Go tests. Add a
`test-unit` target that runs Go alone, for the case where Redis is already up
and the Docker round trip is unwanted. Both run `go test ./... -race -cover`.

- [ ] **Step 3: Give CI a Redis service**

In `.github/workflows/ci.yml`, add a `services:` block to the `backend` job
running `redis:7.2-alpine`, port `6379:6379`, with the health options
`--health-cmd "redis-cli ping" --health-interval 5s --health-timeout 3s
--health-retries 5`. Set `REDIS_ADDR: localhost:6379` in the Test step's
environment. Remove the "No go.sum yet" comment under `actions/setup-go` —
caching now applies.

- [ ] **Step 4: Verify and commit**

Run: `make lint && make build` — expect both clean.

```bash
git add backend/go.mod backend/go.sum Makefile .github/workflows/ci.yml
git commit -m "chore: add go-redis v9.18.0 and wire Redis into test and CI"
```

**Checkpoint 1: config exposes the Redis address and database index**

- [ ] **Step 1: Write a failing test**

Extend the existing table-driven test in `config_test.go`. Cases:
- No `REDIS_ADDR`, no `REDIS_DB` → `RedisAddr == "localhost:6379"`, `RedisDB == 0`
- `REDIS_ADDR=redis:6379` → `RedisAddr == "redis:6379"`
- `REDIS_ADDR=""` (set but empty) → error mentioning `REDIS_ADDR`
- `REDIS_DB=15` → `RedisDB == 15`
- `REDIS_DB=16` → error naming the valid range `0-15`
- `REDIS_DB=-1` → error naming the valid range `0-15`
- `REDIS_DB=notanumber` → error mentioning `REDIS_DB`

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/config/ -run TestLoad -v`
Expected: FAIL — compile error, `cfg.RedisAddr` and `cfg.RedisDB` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: add `RedisAddr string` and `RedisDB int` to `Config`, defaulted in
`Load` to `"localhost:6379"` and `0`. Follow the file's existing pattern
exactly — `lookup`, then validate, then assign — and return errors in the same
`config: KEY %q ...` style as `PORT` and `ENV`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/config/ -cover`
Expected: PASS, coverage still 100%.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config/config.go backend/internal/config/config_test.go
git commit -m "feat: load Redis address and database index from the environment"
```

**Checkpoint 2: every Redis key has exactly one construction site**

- [ ] **Step 1: Write a failing test**

Create `keys_test.go`. Table-driven, asserting the exact literal produced:

| Call | Expected |
|---|---|
| `RoomKey("r1")` | `room:r1` |
| `RoomWalletsKey("r1")` | `room:r1:wallets` |
| `RoomCodeKey("WXYZ")` | `code:WXYZ` |
| `RoundKey("n1")` | `round:n1` |
| `RoundPoolsKey("n1")` | `round:n1:pools` |
| `RoundWagersKey("n1")` | `round:n1:wagers` |
| `RoundBettorsKey("n1")` | `round:n1:bettors` |
| `IdemKey("abc")` | `idem:abc` |
| `WagerField("u1", 2)` | `u1:2` |

Plus `ParseWagerField`:
- `"u1:2"` → `("u1", 2, nil)`
- `"11111111-2222-3333-4444-555555555555:0"` → that UUID, `0`, `nil`
- `"nocolon"` → error
- `"u1:notanint"` → error
- `"u1:"` → error

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run TestKeys -v`
Expected: FAIL — no non-test Go files in the package; every symbol undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: `keys.go` with the signatures from Interfaces above, plus the
`OutboxStream` and `PoolTotalField` constants. `ParseWagerField` splits on the
**last** colon, so a user ID containing a colon cannot corrupt the outcome
index; it returns a wrapped error naming the malformed field.

Add a package doc comment stating that this file is the only place in the
codebase permitted to construct a Redis key, and that the schema it encodes
is the parent plan §4 table plus Amendment A2.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/redisstore/ -run TestKeys -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/redisstore/keys.go backend/internal/redisstore/keys_test.go
git commit -m "feat: define the Redis key schema in one place"
```

**Checkpoint 3: a Store connects to Redis, and the harness fails loudly without it**

- [ ] **Step 1: Write a failing test**

Create `testmain_test.go` with the shared harness:

- `TestMain` dials `REDIS_ADDR` (default `localhost:6379`) against **DB 15**,
  `PING`s it, and on any error calls `log.Fatalf` with a message naming the
  address and telling the reader to run `make up`. It then `FLUSHDB` (DB 15
  only — never `FLUSHALL`) and runs the suite.
- `newTestStore(t *testing.T) *Store` returns a Store bound to DB 15 with a
  per-test outbox stream name, registering `t.Cleanup` to close it.
- `testID(t *testing.T, kind string) string` returns a collision-free
  identifier derived from `t.Name()` and an atomic counter, so no two tests
  touch the same room or round keys.

Then write `TestStorePing`: a Store built by `newTestStore` answers `Ping`
with no error.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run TestStorePing -v`
Expected: FAIL — compile error, `New`, `Store`, `Ping`, `Close` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: `client.go` defines `Store` wrapping a `*redis.Client` and the
outbox stream name. `New(addr string, db int) (*Store, error)` constructs the
client and returns an error if the initial `PING` fails, so an unreachable
Redis surfaces at construction rather than at first use — matching the
fail-fast posture `internal/config` already takes.

`Store` carries the outbox stream name as a field because `wager-outbox` is
the one key in the schema not namespaced by a room or round ID; without this,
concurrent tests would read each other's events. It defaults to
`OutboxStream`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/redisstore/ -v`
Expected: PASS. Then stop Redis (`docker compose stop redis`) and re-run —
expected: **FAIL** with the "run `make up`" message, not a skip. Restart it
with `make up` before continuing.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/redisstore/client.go backend/internal/redisstore/testmain_test.go
git commit -m "feat: add Redis store constructor and integration test harness"
```

---

## Task 2: Room and round state

The wager scripts read four structures that must already exist. This task
writes them, in the shapes the key schema fixes. Per Amendment A4 these are
the real creators, not fixtures — Phase 3's `internal/room` and Phase 4's
`internal/round` wrap them.

**Files:**
- Create: `backend/internal/redisstore/room.go`, `room_test.go`
- Create: `backend/internal/redisstore/round.go`, `round_test.go`

**Interfaces:**
- Consumes: `Store`, all key builders (Task 1); `domain.Tokens`,
  `domain.RoundStatus`, `domain.ValidateBuyIn`, `domain.ValidateOutcomeCount`.
- Produces:
  ```go
  type Room struct {
      ID        string
      HostID    string
      BuyIn     domain.Tokens
      Status    string
      CreatedAt time.Time
  }
  type Round struct {
      ID              string
      RoomID          string
      Status          domain.RoundStatus
      LockAtMS        int64
      OutcomeCount    int
      ResolvedOutcome int // -1 when unresolved
  }

  func (s *Store) CreateRoom(ctx context.Context, roomID, code, hostID string, buyIn domain.Tokens) error
  func (s *Store) RoomByCode(ctx context.Context, code string) (string, error)
  func (s *Store) Room(ctx context.Context, roomID string) (Room, error)
  func (s *Store) JoinRoom(ctx context.Context, roomID, userID string, balance domain.Tokens) error
  func (s *Store) Balance(ctx context.Context, roomID, userID string) (domain.Tokens, error)
  func (s *Store) PlayerCount(ctx context.Context, roomID string) (int, error)

  func (s *Store) CreateRound(ctx context.Context, roundID, roomID string, outcomeCount int, lockAt time.Time) error
  func (s *Store) Round(ctx context.Context, roundID string) (Round, error)
  func (s *Store) Pools(ctx context.Context, roundID string) (pools []domain.Tokens, total domain.Tokens, err error)
  ```

**Checkpoint 1: creating a room writes the room hash and its code mapping**

- [ ] **Step 1: Write a failing test**

`TestCreateRoom`: `CreateRoom(ctx, "room1", "WXYZ", "host1", 500)` then assert
- `HGETALL room:room1` yields `host_id=host1`, `buy_in=500`, `status=open`,
  and a `created_at` parsable as a Unix millisecond timestamp
- `GET code:WXYZ` yields `room1`
- `RoomByCode(ctx, "WXYZ")` returns `"room1"`
- `Room(ctx, "room1")` returns a `Room` with those values and `BuyIn` typed
  `domain.Tokens(500)`
- `RoomByCode(ctx, "NOPE")` returns an error satisfying
  `errors.Is(err, ErrNotFound)`
- `CreateRoom` with `buyIn` of `50` returns an error satisfying
  `errors.Is(err, domain.ErrInvalidBuyIn)` and writes **nothing** (assert
  `EXISTS room:...` is 0)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run TestCreateRoom -v`
Expected: FAIL — compile error, `CreateRoom`/`Room`/`RoomByCode`/`ErrNotFound`
undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: `CreateRoom` validates the buy-in through `domain.ValidateBuyIn`
before touching Redis, then writes the room hash and the code mapping in a
single `TxPipeline` so a room can never exist without its code lookup. Amounts
are written as base-10 integers; `created_at` as Unix milliseconds.
`ErrNotFound` is a new sentinel in `errors.go`, returned when a key or field is
absent — `redis.Nil` must not escape the package.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/redisstore/ -run TestCreateRoom -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/redisstore/room.go backend/internal/redisstore/room_test.go backend/internal/redisstore/errors.go
git commit -m "feat: create rooms with a code lookup in Redis"
```

**Checkpoint 2: joining a room materializes a session wallet**

- [ ] **Step 1: Write a failing test**

`TestJoinRoom`, against a room created with buy-in 500 and host `host1`:
- `JoinRoom(ctx, roomID, "u1", 500)` → `HGET room:{id}:wallets u1` is `500`,
  and `Balance(ctx, roomID, "u1")` returns `domain.Tokens(500)`
- `Balance` for a user who never joined → `errors.Is(err, ErrNotFound)`
- `JoinRoom` with balance `0` → error satisfying
  `errors.Is(err, domain.ErrInvalidStake)`, nothing written
- After the host and two players are seeded, `PlayerCount(ctx, roomID)`
  returns `2` — the host is present in the wallets hash but excluded from the
  count, because the host cannot wager and the "2/5 players" denominator
  counts only those who can (spec §4)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run TestJoinRoom -v`
Expected: FAIL — `JoinRoom`, `Balance`, `PlayerCount` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: `JoinRoom` rejects a non-positive balance, then `HSET`s the wallet
field. `PlayerCount` reads `HLEN room:{roomID}:wallets` and subtracts one for
the host, floored at zero. The caller decides the session balance — that is
`domain.GuestSessionBalance` or `domain.AccountSessionBalance`, and this layer
must not re-derive it.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/redisstore/ -run TestJoinRoom -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/redisstore/room.go backend/internal/redisstore/room_test.go
git commit -m "feat: materialize session wallets on room join"
```

**Checkpoint 3: creating a round writes its hash and zeroed pools**

- [ ] **Step 1: Write a failing test**

`TestCreateRound`:
- `CreateRound(ctx, "n1", "room1", 3, lockAt)` where `lockAt` is 30s ahead →
  `HGETALL round:n1` yields `room_id=room1`, `status=open`,
  `outcome_count=3`, and `lock_at_ms` equal to `lockAt.UnixMilli()`
- `HGETALL round:n1:pools` yields `0=0`, `1=0`, `2=0`, `total=0` — exactly
  four fields, so `Pools` never has to distinguish "no pool" from "zero pool"
- `Round(ctx, "n1")` returns `Status: domain.RoundOpen`,
  `OutcomeCount: 3`, `ResolvedOutcome: -1`
- `Pools(ctx, "n1")` returns `[]domain.Tokens{0,0,0}` and total `0`
- `CreateRound` with `outcomeCount` of `1` and of `5` → error satisfying
  `errors.Is(err, domain.ErrInvalidOutcomeCount)`, nothing written
- `Round(ctx, "missing")` → `errors.Is(err, ErrNotFound)`

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run TestCreateRound -v`
Expected: FAIL — `CreateRound`, `Round`, `Pools`, `Round` struct undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: `CreateRound` validates through `domain.ValidateOutcomeCount`, then
writes the round hash and pre-zeroes every pool field plus `total` in one
`TxPipeline`. `Round` maps the stored status string directly onto
`domain.RoundStatus` — they are the same strings by construction (see
`round.go`'s doc comment in `internal/domain`), so no translation table is
needed. `ResolvedOutcome` is `-1` when the `resolved_outcome` field is absent.
`Pools` reads the hash once and returns outcome pools in index order.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/redisstore/ -run TestCreateRound -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/redisstore/round.go backend/internal/redisstore/round_test.go
git commit -m "feat: create rounds with pre-zeroed outcome pools"
```

---

## Task 3: `place_wager.lua` — the accept path

The hot path, and the one operation whose atomicity the whole design rests on.
Build the accept path first with no guards at all; Task 4 adds each rejection
as its own RED→GREEN cycle.

**Files:**
- Create: `backend/scripts/lua/embed.go`
- Create: `backend/scripts/lua/place_wager.lua`
- Create: `backend/internal/redisstore/wager.go`, `wager_test.go`
- Modify: `backend/internal/redisstore/errors.go`

**Interfaces:**
- Consumes: `Store`, key builders, `CreateRoom`/`JoinRoom`/`CreateRound` (Tasks 1–2).
- Produces:
  ```go
  // package lua  (backend/scripts/lua)
  var PlaceWager   string // //go:embed place_wager.lua
  var LockRound    string
  var SettleRound  string
  var RefundRound  string

  // package redisstore
  type WagerResult struct {
      Balance     domain.Tokens   // the wagerer's balance after the debit
      Pools       []domain.Tokens // per-outcome totals, index order
      Total       domain.Tokens   // sum of all pools
      BettorCount int             // distinct players who have wagered (A2)
  }

  func (s *Store) PlaceWager(ctx context.Context, req WagerRequest) (WagerResult, error)

  type WagerRequest struct {
      RoomID, RoundID, UserID string
      Outcome                 int
      Amount                  domain.Tokens
      IdempotencyKey          string
  }
  ```

**`place_wager.lua` signature** — fixed now, extended by Task 4 with guards only:

```
KEYS[1] room:{roomID}            KEYS[5] round:{roundID}:wagers
KEYS[2] room:{roomID}:wallets    KEYS[6] round:{roundID}:bettors
KEYS[3] round:{roundID}          KEYS[7] idem:{idempotencyKey}
KEYS[4] round:{roundID}:pools    KEYS[8] <outbox stream>

ARGV[1] userID   ARGV[2] outcomeIdx   ARGV[3] amount
ARGV[4] idempotencyKey              ARGV[5] roomID   ARGV[6] roundID

reply on accept:
  {'OK', balance, bettorCount, total, pool_0, pool_1, ... pool_{n-1}}
  -- every element a string; pool count equals the round's outcome_count
```

**Checkpoint 1: an accepted wager debits, pools, records, counts, and emits — atomically**

- [ ] **Step 1: Write a failing test**

`TestPlaceWager_Accept`. Arrange: a room with host `host1` and buy-in 500, a
round with 3 outcomes and `lockAt` 30 seconds in the future, player `u1`
joined with 500.

Act and assert, `PlaceWager` with `Outcome: 1, Amount: 200`:
- returns `Balance: 300`, `Pools: []domain.Tokens{0, 200, 0}`, `Total: 200`,
  `BettorCount: 1`
- `HGET room:{id}:wallets u1` is `300`
- `HGETALL round:{id}:pools` is `0=0, 1=200, 2=0, total=200`
- `HGET round:{id}:wagers u1:1` is `200`
- `SCARD round:{id}:bettors` is `1`
- `XLEN <test outbox>` is `1`, and the entry's fields include
  `type=wager_placed`, `user=u1`, `outcome=1`, `amount=200`, `balance=300`,
  and the idempotency key

Then, in the same test, a **second, different** wager by `u1` — `Outcome: 1,
Amount: 100`, a fresh idempotency key — asserts that repeat wagers accumulate
rather than replace:
- returns `Balance: 200`, `Pools: {0, 300, 0}`, `Total: 300`, `BettorCount: 1`
  (still one distinct bettor — the count tracks players, not wagers, per A2)
- `HGET round:{id}:wagers u1:1` is `300`
- `XLEN` is `2`

And a wager by a **second** player `u2` (joined with 500) on outcome 0 for 50
moves `BettorCount` to `2` and `Total` to `350`.

> These three cases share one checkpoint because `HINCRBY` satisfies all of
> them the moment it is written — a separate "repeat wagers sum" checkpoint
> would pass before its implementation existed, which is not a RED→GREEN
> cycle. See `writing-plans` § Bite-Sized Task Granularity.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run TestPlaceWager_Accept -v`
Expected: FAIL — compile error, `PlaceWager`, `WagerRequest`, `WagerResult`
undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract, `place_wager.lua` — **no guards yet**, accept unconditionally:
read `outcome_count` from KEYS[3]; `HINCRBY` the wallet by `-amount`; `HINCRBY`
the outcome field and `total` in the pools hash by `+amount`; `HINCRBY` the
wager field (built as `userID:outcomeIdx`) by `+amount`; `SADD` the user to
bettors; `XADD` the event to KEYS[8]; read the pools back and return the reply
shape above. Every returned element via `tostring()`; no `nil` may appear in
the table (see Lua conventions).

Contract, `embed.go`: `package lua`, one `//go:embed` per script into an
exported `string`.

Contract, `wager.go`: a package-level `redis.NewScript(lua.PlaceWager)` — go-redis
runs `EVALSHA` and falls back to `EVAL` on `NOSCRIPT` automatically, so scripts
need no explicit loading step. `PlaceWager` assembles the eight keys through
`keys.go` builders only, runs the script, and parses the flat string reply into
`WagerResult`. A reply whose first element is not `OK` is routed through the
status-code mapper in `errors.go`; Task 4 fills that mapper in, so for now an
unrecognized code returns a wrapped generic error naming the code.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/redisstore/ -run TestPlaceWager_Accept -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/scripts/lua backend/internal/redisstore/wager.go backend/internal/redisstore/wager_test.go backend/internal/redisstore/errors.go
git commit -m "feat: place a wager atomically with pool, bettor, and outbox updates"
```

**Checkpoint 2: a replayed idempotency key returns the cached reply and mutates nothing**

- [ ] **Step 1: Write a failing test**

`TestPlaceWager_IdempotentReplay`. Arrange as above, place a wager of 200 on
outcome 1 with key `k1`, capture the result. Act: call `PlaceWager` again with
**the identical request, same key `k1`**.

Assert:
- the second call returns a `WagerResult` deep-equal to the first
- `HGET room:{id}:wallets u1` is still `300` — debited once
- `HGET round:{id}:pools 1` is still `200`
- `SCARD round:{id}:bettors` is still `1`
- `XLEN <test outbox>` is still `1` — **no duplicate outbox event**, which is
  what keeps at-least-once relay to Kafka from becoming at-least-twice ledger
  writes
- `TTL idem:k1` is greater than `0` and at most `86400`

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run TestPlaceWager_IdempotentReplay -v`
Expected: FAIL — the wallet reads `100` and `XLEN` is `2`; the script debits
twice because it has no idempotency check.

- [ ] **Step 3: Implement to satisfy the test**

Contract: as the script's **first** action, `GET KEYS[7]`; if present,
`return cjson.decode(cached)` immediately, before any mutation. On the accept
path, after building the reply, `SET KEYS[7]` to `cjson.encode(reply)` with
`EX 86400`.

Verified 2026-08-24 on Redis 7.2: `cjson` round-trips a flat array of strings
exactly, and the decoded table converts to the same multi-bulk reply.

Note for later phases: a cached reply carries the pools *as they were* at the
original wager, so a replay arriving after other players have wagered returns
stale pool figures. That is correct — it is the original transaction's result.
Live odds are broadcast separately (Phase 4) and are never sourced from a
replayed reply.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/redisstore/ -run TestPlaceWager -v`
Expected: PASS — both wager tests.

- [ ] **Step 5: Commit**

```bash
git add backend/scripts/lua/place_wager.lua backend/internal/redisstore/wager_test.go
git commit -m "feat: dedupe wagers on idempotency key with a cached reply"
```

---

## Task 4: `place_wager.lua` — the rejection paths

Six guards, added in the precedence order the parent plan §5 fixes:
idempotency, lockout, outcome range, host, membership, funds. Each is a
distinct branch that does not exist before its checkpoint, so each test is a
genuine RED.

**Every checkpoint in this task asserts the same three things in addition to
the returned error**: the wallet is unchanged, the pools hash is unchanged,
and `XLEN` on the outbox is unchanged. A guard that rejects but leaks a
mutation is the exact failure mode this phase exists to rule out.

**Files:**
- Modify: `backend/scripts/lua/place_wager.lua`
- Modify: `backend/internal/redisstore/errors.go`, `wager_test.go`

**Interfaces:**
- Produces:
  ```go
  var (
      ErrPoolLocked    = errors.New("redisstore: round is locked")
      ErrHostCannotBet = errors.New("redisstore: host cannot wager in their own room")
      ErrNotInRoom     = errors.New("redisstore: user has no wallet in this room")
      ErrNotFound      = errors.New("redisstore: key or field not found") // Task 2
  )
  ```
  `INVALID_OUTCOME` maps to `domain.ErrInvalidOutcome` and `INSUFFICIENT_FUNDS`
  to `domain.ErrInsufficientFunds` — both already exist in Phase 1's domain, and
  a second sentinel for the same condition would let call sites diverge.
  `POOL_LOCKED`, `HOST_CANNOT_BET` and `NOT_IN_ROOM` are new here rather than in
  `internal/domain`, because no domain function produces them and that package
  deliberately carries no error without a producer (see its `errors.go`).

**Checkpoint 1: a round whose status is not `open` rejects with `POOL_LOCKED`**

- [ ] Step 1: Write a failing test — `TestPlaceWager_RejectsLockedStatus`.
  Arrange a valid room, round, and funded player, then `HSET round:{id} status
  locked`. Act: `PlaceWager` of 200 on outcome 1, `lock_at_ms` still in the
  future. Assert `errors.Is(err, ErrPoolLocked)`, and the three no-mutation
  assertions.
- [ ] Step 2: Run — expect FAIL: the wager is accepted, `err` is nil, balance
  is 300.
- [ ] Step 3: Implement — after the idempotency check, read `status` from
  KEYS[3]; if it is not `open`, `return {'POOL_LOCKED'}`. Map the code to
  `ErrPoolLocked` in the wrapper.
- [ ] Step 4: Run — expect PASS
- [ ] Step 5: Commit — `fix: reject wagers on a round that is not open`

**Checkpoint 2: lockout is judged by Redis's own clock, not the caller's**

This is the checkpoint that makes spec §4's "no client-latency exploit"
guarantee structural. Read it carefully.

- [ ] Step 1: Write a failing test — `TestPlaceWager_RejectsAfterLockInstant`.
  Arrange a round whose `status` is still literally `open` but whose
  `lock_at_ms` is **1000 ms in the past**. Act: `PlaceWager`. Assert
  `errors.Is(err, ErrPoolLocked)` plus the three no-mutation assertions.

  Add a second case in the same test: `lock_at_ms` 30 seconds in the future,
  status `open` → accepted. This pins that the guard tests the clock rather
  than rejecting everything.

  The wrapper takes no timestamp parameter and must never gain one — there is
  deliberately no way for a caller to influence this decision.
- [ ] Step 2: Run — expect FAIL: the past-lockout wager is accepted, because
  the script reads `status` but never consults the clock.
- [ ] Step 3: Implement — in the same guard, compute the current instant from
  `redis.call('TIME')`, which returns `{seconds, microseconds}`:
  `local t = redis.call('TIME')`
  `local nowMs = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)`
  Reject with `POOL_LOCKED` when `nowMs >= tonumber(lock_at_ms)`. Verified
  working under Redis 7.2 on 2026-08-24. Redis 7 replicates scripts by
  effects, so a non-deterministic `TIME` call inside a script is safe.
- [ ] Step 4: Run — expect PASS
- [ ] Step 5: Commit — `feat: enforce lockout against the Redis clock`

**Checkpoint 3: an outcome index the round does not have rejects with `INVALID_OUTCOME`**

- [ ] Step 1: Write a failing test — `TestPlaceWager_RejectsInvalidOutcome`,
  table-driven over a round with `outcome_count` 3: outcome `3`, `4`, and `-1`.
  Assert `errors.Is(err, domain.ErrInvalidOutcome)` and the three no-mutation
  assertions — including that no stray pool field such as `3` was created.
- [ ] Step 2: Run — expect FAIL: `HINCRBY` happily creates a pool field `3`,
  so the wager is accepted and the pools hash grows a field the round never had.
- [ ] Step 3: Implement — reject when the index is `< 0` or
  `>= outcome_count`. Map to `domain.ErrInvalidOutcome`.
- [ ] Step 4: Run — expect PASS
- [ ] Step 5: Commit — `fix: reject wagers on an outcome the round does not have`

**Checkpoint 4: the host cannot wager in their own room**

- [ ] Step 1: Write a failing test — `TestPlaceWager_RejectsHost`. Arrange a
  room whose `host_id` is `host1`, with `host1` present in the wallets hash and
  funded (the host holds a wallet for room bookkeeping; the guard, not the
  absence of a wallet, is what stops them). Act: `PlaceWager` as `host1`.
  Assert `errors.Is(err, ErrHostCannotBet)` and the three no-mutation
  assertions. Add a control case: a non-host player in the same room is
  accepted.
- [ ] Step 2: Run — expect FAIL: the host's wager is accepted and their wallet
  is debited.
- [ ] Step 3: Implement — `HGET KEYS[1] host_id`; if it equals `ARGV[1]`,
  `return {'HOST_CANNOT_BET'}`. Map to `ErrHostCannotBet`.
- [ ] Step 4: Run — expect PASS
- [ ] Step 5: Commit — `feat: block the host from wagering in their own room`

**Checkpoint 5: a user with no wallet in the room rejects with `NOT_IN_ROOM`**

- [ ] Step 1: Write a failing test — `TestPlaceWager_RejectsNonMember`. Act as
  user `stranger`, never joined. Assert `errors.Is(err, ErrNotInRoom)` and the
  three no-mutation assertions, plus that `HEXISTS room:{id}:wallets stranger`
  is still `0`.
- [ ] Step 2: Run — expect FAIL: `HINCRBY` on a missing field creates it, so
  `stranger` ends up with a balance of `-200` — a minted negative wallet.
- [ ] Step 3: Implement — `HGET KEYS[2] ARGV[1]`; on a false/absent value,
  `return {'NOT_IN_ROOM'}`. Map to `ErrNotInRoom`.
- [ ] Step 4: Run — expect PASS
- [ ] Step 5: Commit — `fix: reject wagers from users with no wallet in the room`

**Checkpoint 6: a stake above the wallet rejects with `INSUFFICIENT_FUNDS`, and precedence holds**

- [ ] Step 1: Write a failing test — `TestPlaceWager_RejectsInsufficientFunds`,
  over a player funded with 500: stake `501` rejects, stake `500` is accepted
  and leaves a balance of exactly `0`. Assert
  `errors.Is(err, domain.ErrInsufficientFunds)` and the three no-mutation
  assertions on the rejecting case.

  Then add `TestPlaceWager_GuardPrecedence`, pinning the parent plan §5 order
  where two guards could both fire:
  - replayed key **and** locked round → the cached reply, not `POOL_LOCKED`
  - locked round **and** invalid outcome → `ErrPoolLocked`
  - invalid outcome **and** caller is host → `domain.ErrInvalidOutcome`
  - caller is host **and** stake exceeds balance → `ErrHostCannotBet`
  - non-member **and** stake exceeds any plausible balance → `ErrNotInRoom`
- [ ] Step 2: Run — expect FAIL: the 501 stake is accepted and the wallet goes
  to `-1`. (`TestPlaceWager_GuardPrecedence` will pass once the last guard
  lands — it pins ordering established by Checkpoints 1–5 and is folded in here
  rather than given its own checkpoint, since it can never fail first.)
- [ ] Step 3: Implement — reject when the stored balance is less than the
  stake. Map to `domain.ErrInsufficientFunds`. Confirm the guard sequence in
  the finished script reads: idempotency → status/clock → outcome range → host
  → membership → funds.
- [ ] Step 4: Run — `cd backend && go test ./internal/redisstore/ -v` — expect
  PASS across the whole package.
- [ ] Step 5: Commit — `fix: reject wagers that exceed the session wallet`

---

## Task 5: `lock_round.lua` — the `open → locked` transition

Amendment A3. A status compare-and-set, and the precondition every settlement
path checks.

**Files:** create `backend/scripts/lua/lock_round.lua`; modify
`backend/internal/redisstore/round.go`, `round_test.go`, `errors.go`.

**Interfaces:**
- Produces:
  ```go
  func (s *Store) LockRound(ctx context.Context, roundID string) error
  var ErrRoundTerminal = errors.New("redisstore: round is already terminal")
  ```
  Locking an already-locked round returns `nil`, not an error: the timer that
  fires this may retry, and a second lock is the state the caller wanted.
  Locking a resolved or refunded round returns `ErrRoundTerminal`.

```
KEYS[1] round:{roundID}
reply: {'OK'} | {'ALREADY_LOCKED'} | {'ROUND_TERMINAL', status}
```

**Checkpoint 1: an open round locks**

- [ ] Step 1: Write a failing test — `TestLockRound`. Arrange an open round.
  Act: `LockRound`. Assert `err` is nil, `HGET round:{id} status` is `locked`,
  and `Round(ctx, id).Status` is `domain.RoundLocked`. Then assert that a
  `PlaceWager` against that round now returns `ErrPoolLocked` — the point of
  the transition.
- [ ] Step 2: Run — expect FAIL: `LockRound` undefined.
- [ ] Step 3: Implement — script sets `status` to `locked` and returns `{'OK'}`.
  Wrapper maps `OK` to `nil`.
- [ ] Step 4: Run — expect PASS
- [ ] Step 5: Commit — `feat: lock a round to freeze its pools`

**Checkpoint 2: locking an already-locked round is a benign no-op**

- [ ] Step 1: Write a failing test — `TestLockRound_AlreadyLocked`. Lock a
  round, then lock it again. Assert the second call returns `nil` and the
  status is still `locked`.
- [ ] Step 2: Run — expect FAIL: the script returns `{'OK'}` unconditionally,
  so the test asserting a distinct `ALREADY_LOCKED` reply fails. Assert on the
  raw reply code as well as the mapped error so this checkpoint has something
  to fail on.
- [ ] Step 3: Implement — read `status`; when it is `locked`, return
  `{'ALREADY_LOCKED'}` without writing. Wrapper maps it to `nil`.
- [ ] Step 4: Run — expect PASS
- [ ] Step 5: Commit — `feat: make locking an already-locked round a no-op`

**Checkpoint 3: a terminal round cannot be reopened by locking**

- [ ] Step 1: Write a failing test — `TestLockRound_Terminal`, table-driven over
  `resolved` and `refunded`. Act: `LockRound`. Assert
  `errors.Is(err, ErrRoundTerminal)` and that `HGET round:{id} status` is
  **unchanged** — a resolved round that could be relocked would be settleable
  twice.
- [ ] Step 2: Run — expect FAIL: the round is silently relocked, status becomes
  `locked`, `err` is nil.
- [ ] Step 3: Implement — when the status is neither `open` nor `locked`,
  return `{'ROUND_TERMINAL', status}` without writing. Map to
  `ErrRoundTerminal`.
- [ ] Step 4: Run — expect PASS
- [ ] Step 5: Commit — `fix: refuse to relock a resolved or refunded round`

---

## Task 6: `settle_round.lua` — applying a computed settlement

Amendment A1: `domain.Settle` computes, the script applies. The wrapper is the
seam between them.

**Files:** create `backend/scripts/lua/settle_round.lua`,
`backend/internal/redisstore/settle.go`, `settle_test.go`.

**Interfaces:**
- Consumes: `domain.Settle`, `domain.Settlement`, `domain.Stake`,
  `ParseWagerField`, `Round`, `LockRound`.
- Produces:
  ```go
  func (s *Store) ReadStakes(ctx context.Context, roundID string) ([]domain.Stake, error)
  func (s *Store) SettleRound(ctx context.Context, roundID string, winningOutcome int, idempotencyKey string) (domain.Settlement, error)
  var ErrNotLocked       = errors.New("redisstore: round is not locked")
  var ErrAlreadySettled  = errors.New("redisstore: round is already settled")
  ```

```
KEYS[1] round:{roundID}   KEYS[2] room:{roomID}:wallets   KEYS[3] <outbox>
ARGV[1] terminalStatus ('resolved' | 'refunded')
ARGV[2] resolvedOutcome  ('' when refunded — never nil, see Lua conventions)
ARGV[3] dust             ARGV[4] idempotencyKey   ARGV[5] roundID
ARGV[6..] alternating userID, amount

reply: {'OK', creditedCount} | {'NOT_LOCKED', status} | {'ALREADY_RESOLVED', status}
```

**Checkpoint 1: stakes read back from Redis are deterministic and correctly typed**

- [ ] Step 1: Write a failing test — `TestReadStakes`. Arrange a round with
  wagers `u2:1 = 300`, `u1:0 = 100`, `u1:2 = 50`, `u3:1 = 75` written directly
  to the hash. Act: `ReadStakes`. Assert the returned slice is **exactly**
  `[{u1,0,100}, {u1,2,50}, {u2,1,300}, {u3,1,75}]` — sorted by user ID, then
  by outcome index. Assert an empty wagers hash returns an empty, non-nil
  slice and no error. Assert a malformed field (`HSET ... "garbage" 10`)
  returns an error naming the field.
- [ ] Step 2: Run — expect FAIL: `ReadStakes` undefined.
- [ ] Step 3: Implement — `HGETALL` the wagers hash, split each field with
  `ParseWagerField`, parse amounts as `int64` into `domain.Tokens`, then
  **sort** by `(UserID, Outcome)`.

  The sort is required, not cosmetic. `HGETALL` returns fields in unspecified
  order, and `domain.Settle` emits `Settlement.Results` in the order players
  first appear in its input. Without sorting, settling the same round twice
  could produce the reveal rows in different orders. This narrows Phase 1's
  documented "order they first staked" to "ascending by user ID" for anything
  sourced from Redis; `domain.Settle` itself is unchanged.
- [ ] Step 4: Run — expect PASS
- [ ] Step 5: Commit — `feat: read round stakes from Redis in deterministic order`

**Checkpoint 2: settling a locked round credits every payout and marks it resolved**

- [ ] Step 1: Write a failing test — `TestSettleRound`. Arrange: room buy-in
  500, players `u1`, `u2`, `u3` each joined with 500, a 3-outcome round.
  `u1` wagers 100 on outcome 0, `u2` wagers 300 on outcome 1, `u3` wagers 100
  on outcome 0. Lock the round. Act: `SettleRound(ctx, roundID, 0, "s1")`.

  Total 500, winning pool 200. `u1` receives `floor(100*500/200) = 250`,
  `u3` receives `250`, dust `0`.

  Assert:
  - the returned `domain.Settlement` has `Payouts` of `u1:250, u3:250`,
    `Dust: 0`, `Refunded: false`, and `Results` with `u1` net `+150`,
    `u2` net `-300`, `u3` net `+150`
  - `HGET room:{id}:wallets u1` is `650` (500 − 100 + 250)
  - `HGET room:{id}:wallets u2` is `200` — the loser is **not** credited
  - `HGET room:{id}:wallets u3` is `650`
  - `HGET round:{id} status` is `resolved`, `resolved_outcome` is `0`
  - `XLEN` grew by exactly `1`, with `type=round_settled`, the dust, and the
    winning outcome in the entry
  - `Σ wallets + Σ pools == 1500`, the tokens the room started with
- [ ] Step 2: Run — expect FAIL: `SettleRound` undefined.
- [ ] Step 3: Implement — the wrapper reads the round (for `room_id` and
  `outcome_count`), calls `ReadStakes`, calls
  `domain.Settle(stakes, winningOutcome, outcomeCount)`, then runs the script
  with the resulting payouts flattened into ARGV. The script **CAS**es
  `locked → resolved`, `HINCRBY`s each wallet by its payout, writes
  `resolved_outcome`, and `XADD`s one settlement event. Return the
  `domain.Settlement` unchanged to the caller — it is the reveal payload
  Phase 4 broadcasts.

  No guards beyond the CAS yet; Checkpoints 3–5 add them.
- [ ] Step 4: Run — expect PASS
- [ ] Step 5: Commit — `feat: settle a locked round by applying computed payouts`

**Checkpoint 3: settling twice credits once**

- [ ] Step 1: Write a failing test — `TestSettleRound_Idempotent`. Settle as
  above, capture balances, then call `SettleRound` again with the same
  arguments. Assert the second call returns `errors.Is(err, ErrAlreadySettled)`,
  every wallet is byte-for-byte unchanged, and `XLEN` did not grow.
- [ ] Step 2: Run — expect FAIL: every winner is credited a second time —
  `u1` reads `900` instead of `650`, minting 500 tokens from nothing.
- [ ] Step 3: Implement — before any mutation, read `status`; when it is
  `resolved` or `refunded`, return `{'ALREADY_RESOLVED', status}`. Map to
  `ErrAlreadySettled`.
- [ ] Step 4: Run — expect PASS
- [ ] Step 5: Commit — `fix: make round settlement idempotent`

**Checkpoint 4: an unlocked round cannot be settled**

- [ ] Step 1: Write a failing test — `TestSettleRound_RequiresLock`. Arrange
  wagers on a round left `open`. Act: `SettleRound`. Assert
  `errors.Is(err, ErrNotLocked)`, no wallet changed, status still `open`,
  `XLEN` unchanged.
- [ ] Step 2: Run — expect FAIL: the open round is settled, which is the race
  Amendment A1 depends on being impossible — new wagers could still arrive
  between `ReadStakes` and the credit.
- [ ] Step 3: Implement — reject with `{'NOT_LOCKED', status}` when the status
  is not `locked`. Have the wrapper check the round status **before** calling
  `domain.Settle`, so an unlocked round fails without computing a settlement it
  will not apply; the script keeps its own check regardless, because the
  wrapper's read is not atomic with it.
- [ ] Step 4: Run — expect PASS
- [ ] Step 5: Commit — `fix: refuse to settle a round that is not locked`

**Checkpoint 5: nobody backed the winner — every stake refunded, round marked `refunded`**

- [ ] Step 1: Write a failing test — `TestSettleRound_NobodyBackedWinner`.
  Arrange: `u1` wagers 100 on outcome 0, `u2` wagers 300 on outcome 1, round
  locked, 3 outcomes. Act: `SettleRound(ctx, roundID, 2, "s1")` — outcome 2
  has an empty pool.

  Assert:
  - `Settlement.Refunded` is `true`, `Dust` is `0`, and every player's `Net`
    is `0`
  - `u1`'s wallet is back to `500`, `u2`'s wallet is back to `500`
  - `HGET round:{id} status` is `refunded`, **not** `resolved`
  - `HEXISTS round:{id} resolved_outcome` is `0` — a refunded round never
    records a winning outcome
  - `Σ wallets == 1000`, exactly what the players started with
- [ ] Step 2: Run — expect FAIL: the round is marked `resolved` and
  `resolved_outcome` is written as `2`, because the wrapper always passes the
  resolved terminal status.
- [ ] Step 3: Implement — the wrapper derives `terminalStatus` from
  `Settlement.Refunded` and passes an empty string for `resolvedOutcome` on the
  refunded path. The script writes `resolved_outcome` only when that argument
  is non-empty, and tags the outbox event `type=round_refunded`. Recall that an
  empty string, never `nil`, must be used — a `nil` in a Lua table truncates
  the reply.
- [ ] Step 4: Run — `cd backend && go test ./internal/redisstore/ -v` — expect PASS
- [ ] Step 5: Commit — `feat: refund every stake when nobody backed the winner`

---

## Task 7: `refund_round.lua` — the timeout and disconnect path

Spec §4: if a round is still unresolved 60 seconds after lockout, every wager
is refunded. Unlike settlement there is nothing to compute — refunding is the
identity function on stakes — so this script reads the wagers hash inside its
own atomic unit rather than taking amounts from Go (Amendment A1).

**Files:** create `backend/scripts/lua/refund_round.lua`; modify
`backend/internal/redisstore/settle.go`, `settle_test.go`.

**Interfaces:**
- Produces: `func (s *Store) RefundRound(ctx context.Context, roundID, idempotencyKey string) (domain.Tokens, error)`
  — returns the total refunded.

```
KEYS[1] round:{roundID}  KEYS[2] room:{roomID}:wallets
KEYS[3] round:{roundID}:wagers   KEYS[4] <outbox>
ARGV[1] idempotencyKey   ARGV[2] roundID
reply: {'OK', totalRefunded} | {'NOT_LOCKED', status} | {'ALREADY_RESOLVED', status}
```

**Checkpoint 1: every stake goes back to whoever placed it**

- [ ] Step 1: Write a failing test — `TestRefundRound`. Arrange `u1` wagering
  100 on outcome 0 and 50 on outcome 2, `u2` wagering 300 on outcome 1, all
  from wallets of 500; lock the round. Act: `RefundRound`.

  Assert: returns `domain.Tokens(450)`; `u1`'s wallet is `500` and `u2`'s is
  `500`; `HGET round:{id} status` is `refunded`; `HEXISTS round:{id}
  resolved_outcome` is `0`; `XLEN` grew by exactly `1` with
  `type=round_refunded`; and `Σ wallets == 1000`. Note `u1`'s two stakes on
  different outcomes must **both** come back — a refund that walked only one
  field per player would silently keep 50 tokens.
- [ ] Step 2: Run — expect FAIL: `RefundRound` undefined.
- [ ] Step 3: Implement — `HGETALL` the wagers hash (a flat
  `field, value, field, value` array inside Lua — verified 2026-08-24), split
  each field on its last colon to recover the user ID, `HINCRBY` that wallet by
  the staked amount, accumulate the total, CAS the status to `refunded`, and
  `XADD` one event.
- [ ] Step 4: Run — expect PASS
- [ ] Step 5: Commit — `feat: refund every stake on the round timeout path`

**Checkpoint 2: refunding twice credits once**

- [ ] Step 1: Write a failing test — `TestRefundRound_Idempotent`. Refund, then
  refund again. Assert the second call returns
  `errors.Is(err, ErrAlreadySettled)`, wallets are unchanged, `XLEN` did not
  grow. Add a case asserting a round already `resolved` also rejects with
  `ErrAlreadySettled` — a settled round must never be refundable on top of its
  payouts.
- [ ] Step 2: Run — expect FAIL: every stake is credited a second time,
  minting 450 tokens.
- [ ] Step 3: Implement — reject when the status is already terminal, before
  any mutation.
- [ ] Step 4: Run — expect PASS
- [ ] Step 5: Commit — `fix: make round refund idempotent`

**Checkpoint 3: an unlocked round cannot be refunded**

- [ ] Step 1: Write a failing test — `TestRefundRound_RequiresLock`. Arrange
  wagers on a round left `open`. Assert `errors.Is(err, ErrNotLocked)`, wallets
  unchanged, status still `open`, `XLEN` unchanged — otherwise a refund could
  race a wager still in flight.
- [ ] Step 2: Run — expect FAIL: the open round is refunded.
- [ ] Step 3: Implement — reject with `{'NOT_LOCKED', status}` unless the
  status is `locked`.
- [ ] Step 4: Run — expect PASS
- [ ] Step 5: Commit — `fix: refuse to refund a round that is not locked`

---

## Task 8: Concurrency and conservation suite

This is the phase's headline deliverable (parent plan §9): *"N goroutines
racing a single wallet, asserting zero double-spend and exact token
conservation."*

> **This task has no RED→GREEN cycles, and that is deliberate.** These suites
> verify properties that Tasks 3–7 already implement; a correct implementation
> means they pass the moment they are written. Every step below says
> **expect PASS**, and there is no "run it to see it fail" step to
> mis-follow.
>
> **If one of these fails, you have found a real defect. Stop and report it —
> do not weaken the assertion to make it green.** That inversion is the
> single most likely way this task goes wrong.
>
> Run every suite with `-race`. Without it, a data race in the Go wrapper
> stays invisible and the suite proves less than it appears to.

**Files:** create `backend/internal/redisstore/concurrency_test.go`.

**Interfaces:** consumes everything built so far. Produces no new exported API.

**Verification 1: N goroutines racing one wallet never double-spend**

- [ ] **Step 1: Write the suite**

`TestConcurrent_NoDoubleSpend`. Arrange a player funded with exactly `1000` in
a 3-outcome round with a distant lockout. Act: launch **100 goroutines**, each
placing a wager of `50` with its own unique idempotency key, released together
from a `sync.WaitGroup` so they contend.

Assert:
- exactly **20** calls return no error — `1000 / 50`, not one more
- exactly **80** return `errors.Is(err, domain.ErrInsufficientFunds)`
- no call returns any other error
- the final wallet balance is exactly `0`, never negative
- `HGET round:{id}:pools total` is exactly `1000`
- `XLEN` on the outbox is exactly `20` — one event per accepted wager, so the
  ledger will learn of exactly what Redis did

- [ ] **Step 2: Run**

Run: `cd backend && go test ./internal/redisstore/ -run TestConcurrent_NoDoubleSpend -race -count=3 -v`
Expected: PASS, all three runs. `-count=3` is not optional: a race that
reproduces one time in three is still a race.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/redisstore/concurrency_test.go
git commit -m "test: prove concurrent wagers cannot double-spend a wallet"
```

**Verification 2: tokens are conserved under mixed concurrent load**

- [ ] **Step 1: Write the suite**

`TestConcurrent_TokenConservation`. Arrange 5 players funded with `500` each —
`2500` tokens in the room. Act: **200 goroutines**, each picking a
pseudo-random player, outcome, and amount in `1..100` (seeded deterministically
so a failure reproduces), each with a unique idempotency key. Accept both
success and `ErrInsufficientFunds` as valid outcomes; fail on any other error.

Assert, after all goroutines finish:
- `Σ (all wallet balances) + (pools total) == 2500` — **exactly**, the
  invariant that makes double-spend structurally impossible
- every individual wallet balance is `>= 0`
- `pools total == Σ (individual outcome pools)`
- `Σ (wagers hash values) == pools total` — the per-wager record and the
  aggregate pool cannot disagree
- `XLEN` equals the number of successful calls

- [ ] **Step 2: Run**

Run: `cd backend && go test ./internal/redisstore/ -run TestConcurrent_TokenConservation -race -count=3 -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add backend/internal/redisstore/concurrency_test.go
git commit -m "test: prove token conservation under concurrent mixed wagers"
```

**Verification 3: a racing idempotency key debits exactly once**

- [ ] **Step 1: Write the suite**

`TestConcurrent_IdempotencyRace`. Arrange a player funded with `500`. Act:
**50 goroutines** issuing the byte-identical request — same user, outcome,
amount `200`, and **the same idempotency key** — released together.

Assert:
- all 50 calls return no error
- all 50 return the identical `WagerResult` (`Balance: 300`, same pools)
- the wallet is `300` — debited exactly once, not 50 times
- `HGET round:{id}:wagers u1:1` is `200`
- `XLEN` is exactly `1` — the property that keeps at-least-once Kafka relay
  from producing 50 ledger entries for one wager

This is the strongest test of the idempotency design, because the check and the
write must be atomic with each other: a `GET` followed by a separate `SET`
would let several goroutines through the gap.

- [ ] **Step 2: Run**

Run: `cd backend && go test ./internal/redisstore/ -run TestConcurrent_IdempotencyRace -race -count=3 -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add backend/internal/redisstore/concurrency_test.go
git commit -m "test: prove a racing idempotency key debits exactly once"
```

**Verification 4: a full round conserves tokens end to end**

- [ ] **Step 1: Write the suite**

`TestFullRound_TokenConservation`. Arrange 4 players funded with `500` each —
`2000` in the room. Act, in order: 30 concurrent wagers across 3 outcomes →
`LockRound` → `SettleRound` on a winning outcome that has a non-empty pool.

Assert:
- `Σ (all wallet balances) + Settlement.Dust == 2000` — the flooring remainder
  is the only place tokens may sit outside a wallet, and it is accounted for,
  not lost (parent plan §5)
- `Settlement.Dust >= 0` and is less than the number of winning stakes
- `Σ (PlayerResult.Net) == -Settlement.Dust` — the same invariant Phase 1's
  fuzz test asserts in pure Go, now holding across a real Redis round trip
- the round status is `resolved` and `pools total` is unchanged by settlement

Add a second case running the same shape but resolving to an **empty** outcome,
asserting `Σ wallets == 2000` exactly, dust `0`, status `refunded`.

- [ ] **Step 2: Run**

Run: `cd backend && go test ./internal/redisstore/ -race -count=2 -v`
Expected: PASS — the entire package.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/redisstore/concurrency_test.go
git commit -m "test: prove token conservation across a full settled round"
```

---

## Task 9: Coverage, documentation amendments, and close-out

**Files:** modify `CLAUDE.md`,
`docs/plans/2026-08-21-implementation-plan.md`,
`docs/specs/2026-08-21-callit-design.md`.

**Checkpoint 1: coverage meets the floor and nothing regressed**

- [ ] **Step 1: Measure**

```bash
cd backend
go test ./... -race -coverprofile=/tmp/cover.out
go tool cover -func=/tmp/cover.out | tail -20
```

Expected: `internal/redisstore` at **80% or above**; `internal/domain` still
`100.0%`; `internal/config` still `100.0%`; `cmd/api` `0%` (expected — thin
wiring, per the parent plan's note on `main.go`).

If `redisstore` falls short, the gap is almost always an unexercised error
path in a wrapper — a `redis.Nil` branch or a parse failure. Add the missing
case as a normal RED→GREEN checkpoint and commit it separately rather than
folding it in here.

- [ ] **Step 2: Verify the whole toolchain**

```bash
make lint && make build && make test
```
Expected: all clean. `gofmt -l .` must print nothing — CI fails on any output.

- [ ] **Step 3: Commit only if something changed**

If Steps 1–2 required no edits, skip the commit rather than making an empty one.

**Checkpoint 2: fold this phase's amendments back into the committed docs**

The amendments at the top of this plan are decisions, and decisions belong in
the spec and the parent plan, not only in a phase plan that later readers may
never open. This mirrors how Phase 1 closed out its A1–A3.

- [ ] **Step 1: Amend the parent plan** (`docs/plans/2026-08-21-implementation-plan.md`)

- §4 key schema table: add the `round:{roundID}:bettors` SET row (A2). Add a
  note that brace placeholders are substitutions, not Cluster hash tags.
- §5 `settle_round.lua`: replace the "computes `floor(a * total / pool_W)`"
  description with the A1 flow — Go computes via `domain.Settle`, the script
  applies — and state why (single source of money math; `Settlement.Results`
  is needed in Go regardless). Keep the `pool_W == 0` and dust paragraphs;
  they still describe the behavior, just implemented in Go.
- §5: add `lock_round.lua` as a fourth script (A3).
- §9 Phase 3 row: note that room and round writers already exist in
  `internal/redisstore`, that Phase 3's `internal/room` wraps them rather than
  writing hashes directly, and that short-code generation is still Phase 3's
  (A4). Note the rate limiter is Phase 3's to build (A5).

- [ ] **Step 2: Amend the spec** (`docs/specs/2026-08-21-callit-design.md`)

§4's anonymity section already specifies the "2/5 players" counter. Add one
sentence recording how it is counted: a `round:{roundID}:bettors` set,
`SCARD` over `HLEN` of the room wallets minus the host — so a future reader
knows why a player's second wager does not move it.

- [ ] **Step 3: Update `CLAUDE.md`**

- Repository Layout: retag `internal/redisstore/` and `scripts/lua/` from
  "(Phase 2)" to "exists", and add `internal/room`/`internal/round` notes
  pointing at A4.
- Stack: record `github.com/redis/go-redis/v9 v9.18.0` as the first external
  dependency, **with the Go 1.22 ceiling and why** — v9.19.0+ requires Go
  1.24. This is the single most likely thing for a future session to break by
  running `go get -u`.
- Build & Test: document that `make test` now starts Redis and that the
  integration tests **fail** rather than skip when it is unreachable; note
  they run against DB 15.
- Critical Invariants: add that the money math is not duplicated in Lua (A1),
  and that `keys.go` is the only place a Redis key may be constructed.
- Testing: update the live coverage figures.
- Installed Tooling: `redis-patterns` is now in use; note that
  `postgres-patterns` / `database-migrations` come with Phase 5 and
  `api-design` with Phase 3.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md docs/plans/2026-08-21-implementation-plan.md docs/specs/2026-08-21-callit-design.md
git commit -m "docs: fold Phase 2 amendments into the spec, plan, and CLAUDE.md"
```

**Checkpoint 3: confirm the branch is green and verified**

- [ ] **Step 1: Full verification from a clean state**

```bash
make down && make up
cd backend && go test ./... -race -cover -count=1
```
Expected: all packages PASS against a freshly started Redis. The `make down`
matters — it proves the suite does not depend on state left by earlier runs.

- [ ] **Step 2: Confirm commit granularity held**

```bash
git log --oneline dev..HEAD | wc -l
```
Expected: **31–32 commits**, made up of:

| Source | Commits |
|---|---|
| Task 1 setup (dependency, Makefile, CI) | 1 |
| Checkpoints, Tasks 1–7 (3+3+2+6+3+5+3) | 25 |
| Verification suites, Task 8 | 4 |
| Task 9 Checkpoint 2 (doc amendments) | 1 |
| Task 9 Checkpoint 1 (only if coverage needed work) | 0–1 |

A number far below this means checkpoints were batched — the exact regression
the branch-per-phase convention exists to prevent.

```bash
git log --oneline dev..HEAD | grep -c '^[a-f0-9]* \(feat\|fix\|test\|docs\|chore\|refactor\):'
```
Expected: equal to the total — every commit follows `type: description`.

- [ ] **Step 3: Report and hand off**

Report to the user: the checkpoint count, the coverage figures, and any
amendment that was made beyond A1–A5 during execution.

Then hand off to the **`finishing-a-development-branch`** skill, which verifies
tests and presents the integration options. This plan stops at *"branch is
green and verified"* — the merge decision is the user's, and belongs to that
skill rather than to this document.

---

## Self-Review

**Spec coverage.** Spec §5 steps 1–4 are covered: the atomic script (Tasks
3–4), the `TIME`-based lockout (Task 4 CP2), the outbox `XADD` inside the same
unit (Task 3 CP1), and idempotency (Task 3 CP2). Step 3's odds broadcast and
step 5's Kafka consumer are Phase 4 and Phase 5 respectively — out of scope
here, and `domain.Multipliers` already exists to serve the former. Spec §4's
host-cannot-bet rule is Task 4 CP4; the anonymity counter is A2 and Task 3 CP1;
the 60-second auto-refund is Task 7 (the *timer* that fires it is Phase 4).
Spec §7's latency targets are not asserted here — they need k6 and a tuned
environment, which is Phase 7.

**Deliberately out of scope**, each with a reason recorded above: the shared
rate limiter (A5, Phase 3), short-code generation (A4, Phase 3), the outbox
relay and Kafka producer (Phase 5, though this phase writes the stream it
reads), and round countdown timers (Phase 4).

**Type consistency.** `domain.Tokens` is used for every amount crossing the
boundary; no wrapper returns a bare `int64`. `domain.RoundStatus` values are
the literal strings stored in Redis, so `Round.Status` needs no mapping table.
`WagerResult.Pools` and `domain.Multipliers`'s `pools` parameter are both
`[]domain.Tokens` in index order, so Phase 4 can pass one to the other
directly. Errors: `INVALID_OUTCOME` and `INSUFFICIENT_FUNDS` reuse Phase 1's
`domain.ErrInvalidOutcome` and `domain.ErrInsufficientFunds`; the seven new
sentinels (`ErrNotFound`, `ErrPoolLocked`, `ErrHostCannotBet`, `ErrNotInRoom`,
`ErrRoundTerminal`, `ErrNotLocked`, `ErrAlreadySettled`) all live in
`redisstore/errors.go` because no domain function produces them.

**Facts verified live against the running stack on 2026-08-24**, rather than
assumed: `go-redis` v9.18.0 is the newest release that builds under Go 1.22
(v9.19.0+ declare `go 1.24`); `redis.call('TIME')` works inside a script and
rejects correctly on a past lockout while mutating nothing; `cjson` is present
under Lua 5.1 and round-trips a flat string array exactly; `HGETALL` inside Lua
yields a flat `field, value, ...` array; `SADD`/`SCARD` give the distinct-bettor
count; and a `nil` inside a returned Lua table silently truncates the reply at
that point.
