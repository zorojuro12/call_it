# Phase 5b — Double-Entry Ledger Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the Kafka events Phase 5a durably produces into double-entry
PostgreSQL ledger rows, and prove — with a test that races real concurrent
wagers through Redis → Kafka → PostgreSQL — that the ledger reconciles
exactly against Redis with zero token leakage.

**Architecture:** A `cmd/ledger-worker` binary consumes `wagers-placed` and
`rounds-settled` under the `ledger-writer` group, decodes each message to a
typed `events.Event`, maps it through a **pure** function to a balanced
double-entry `ledger.Transaction`, and batch-writes it inside one PostgreSQL
transaction whose deferred constraint trigger re-checks the balance at
COMMIT. Replay is absorbed by `transactions.idempotency_key`'s UNIQUE
constraint, so at-least-once Kafka delivery is safe without a second identity
path. The worker mirrors `internal/relay`'s shape exactly: `Once` for one
fetch→write→commit cycle, `Run` for the loop, and the Kafka offset is never
committed ahead of a durable PostgreSQL write.

**Tech Stack:** Go 1.22.10 · `segmentio/kafka-go` v0.4.48 (`Reader` with
`GroupTopics` + manual `CommitMessages`) · `jackc/pgx/v5` v5.7.4 (`pgxpool`)
· `golang-migrate/migrate/v4` v4.18.2 · PostgreSQL 16 · Redis 7.2 ·
Kafka 3.7 KRaft.

**No new dependencies.** `pgxpool` ships inside the already-pinned
`jackc/pgx/v5`; `kafka.Reader` ships inside the already-pinned
`segmentio/kafka-go`. Every `go get` in this phase should be unnecessary — if
one seems required, stop and re-read `CLAUDE.md`'s Stack section before
running it.

**Spec:** `docs/specs/2026-08-21-callit-design.md` (§2 stack, §4 lockout) and
`docs/plans/2026-08-21-implementation-plan.md` §6 (PostgreSQL double-entry
schema), §7 (Kafka topology), §9 (phase 5b row). Both travel with this plan;
read them alongside it.

**Branch:** `git checkout -b phase-5b-ledger dev`

---

## Global Constraints

Every task's requirements implicitly include this section. Values copied
verbatim from `CLAUDE.md` and the parent plan.

- **Go 1.22.10.** Never run `go get -u`. If any `go get` is unavoidable, pin
  **every** target explicitly including subpackages (`@vX.Y.Z` on each) — a
  version-less `go get` on a multi-package module rewrote `go.mod`'s
  directive to `1.24.0` in Phase 5a.
- **All amounts are integer token units.** No floats reach Redis, Kafka, or
  PostgreSQL. `ledger_entries.amount` is `bigint` with `CHECK (amount > 0)`.
- **The WebSocket server never writes PostgreSQL directly.** `cmd/api` gains
  no PostgreSQL or Kafka import in this phase. `cmd/ledger-worker` is a
  separate binary for exactly this reason — running it as a goroutine inside
  the API process would satisfy the written invariant while reintroducing the
  coupling it exists to prevent.
- **Every wager carries a UUIDv4 `idempotency_key`, minted once on the wager
  path.** The ledger dedupes on it via the `UNIQUE` constraint and mints no
  second identity path.
- **Payout flooring's remainder goes to `system_dust`.** Never let dust
  silently vanish.
- **`internal/domain` stays free of I/O** and is untouched by this phase.
- **`internal/redisstore/keys.go` is the only place a Redis key may be
  constructed.**
- **Settlement math is not duplicated.** `domain.Settle` already computed
  payouts and dust; the events carry them. The ledger applies, it does not
  recompute — the one arithmetic check it performs is
  `total == Σpayouts + dust`, which is a *verification* of the event, not a
  second implementation of the formula.
- **Testing:** TDD (RED → GREEN → IMPROVE), AAA structure, table-driven
  where there are cases. 80% minimum coverage on every new package, judged
  from `go test ./... -coverpkg=./...`, never the per-package figure.
  `cmd/*` at 0% is expected.
- **`-p 1` is load-bearing.** Integration packages share Redis DB 15 and the
  `callit_test` PostgreSQL database, and each `TestMain` flushes/recreates.
  Never drop it.
- **Integration tests fail rather than skip** when Redis, PostgreSQL, or
  Kafka is unreachable. A suite whose purpose is proving zero double-spend
  must not report PASS while executing nothing.
- **PostgreSQL scratch database is `callit_test`**, created by `TestMain`.
  Redis tests use **DB 15**, never DB 0.
- **Commit format:** `type: description` — `feat`, `fix`, `docs`, `test`,
  `chore`, `refactor`, `perf`, `ci`. Commit at every checkpoint.
- **Shell PATH gotcha:** non-interactive shells need
  `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin` before any `go`
  command. If `go` reports "not found", that is why — do not `sudo`.
- **Bring the stack up first:** `make up-full` (or `make test`, which brings
  it up and waits on all three healthchecks).

---

## Design Decisions Fixed Before Execution

These are settled here so no task has to re-derive them. Each states why,
because each has a plausible-looking alternative that is wrong.

### D1. Sign convention: `credit` = tokens in, `debit` = tokens out

Every account in this ledger holds tokens. An account's balance is
`Σ credits − Σ debits`. Every transaction satisfies `Σ debits == Σ credits`,
which is exactly the invariant `assert_transaction_balanced()` enforces at
COMMIT — tokens are neither created nor destroyed by a transfer.

This is deliberately *not* the classical accounting convention where a debit
increases an asset account. Restating that convention here would mean every
reader has to hold "is a user wallet an asset or a liability?" in their head
to know which way a wager moves the balance. One rule — money in is a credit
— removes the question entirely, and the database enforces the same equality
either way.

### D2. The ledger records outbox movements only; the opening stake is not one

The only money movements that reach Kafka are those emitted by
`place_wager.lua`, `settle_round.lua`, and `refund_round.lua`. Joining a room
is **not** one of them: `Store.JoinRoom` is a Go pipeline
(`backend/internal/redisstore/room.go:114`), not a Lua script, and it emits
no outbox entry.

Therefore a user's `user_wallet` ledger balance is their **net session
delta**, not their absolute holding:

```
ledger_balance(user, room) = −Σ wagers + Σ payouts
                           = redis_wallet(user, room) − opening_stake(user, room)
```

The reconciliation test asserts exactly that identity. It does **not** assert
`redis_wallet == ledger_balance`, which is false by the opening stake.

The alternative — emitting a `system_mint → user_wallet` grant on join so the
two match directly — was rejected as out of scope: `JoinRoom` would have to
become a Lua script to make the grant atomic with the outbox `XADD`, which
means rewriting a Phase 4 write path to improve a Phase 5b assertion. Record
it as a Phase 7 candidate; do not build it here.

Consequence: the `system_mint` and `room_escrow` account kinds go **unused**
in 5b. That is expected, not an oversight — the schema declares them for the
phase that mints.

### D3. `round_pool` accounts are keyed by room, not round

`accounts` has `user_id` and `room_id` columns and no `round_id` (parent plan
§6). Do not add one. A room has at most one non-terminal round at a time —
`room:{roomID}:round` indexes exactly one — so a room-scoped pool account is
per-round in practice, and it returns to **zero** once each round settles
(`Σ wagered == total`, and `total == Σ payouts + dust`). Per-round auditing
is still available through `transactions.round_id`, which every transaction
carries.

If concurrent rounds within one room are ever introduced, this needs a
`round_id` column. Note it; do not pre-build it.

### D4. Cross-topic ordering does not affect ledger correctness

Parent plan §7 keys both topics by `room_id` for per-room ordering, but
`wagers-placed` and `rounds-settled` are separate topics — Kafka provides
**no** ordering guarantee between them. A settlement can therefore be
consumed before the wagers it settles.

This is safe here because **every transaction is internally balanced**. A
settlement written before its wagers drives `round_pool` transiently
negative; the wagers then bring it back to zero. No transaction is ever
rejected, and no final balance differs. Ordering affects only the transient
sign of the pool account.

The consequence for tests: the reconciliation test must wait until the worker
has consumed *both* topics to completion before asserting, not merely until
it has seen the settlement.

### D5. Account IDs are deterministic UUIDv5, so provisioning is one statement

`accounts.id` is generated as `uuid.NewSHA1(accountNamespace, []byte(key))`
where `key` is `"user_wallet:"+userID`, `"round_pool:"+roomID`, or
`"system_dust:"`. Insertion is then a single
`INSERT ... ON CONFLICT (id) DO NOTHING` against the primary key — no lookup
round trip per account per batch, which matters at thousands of wagers, and
account IDs are reproducible from their identity for auditing.

Migration `0002` adds partial unique indexes on the natural keys anyway, as
the enforcing constraint: the deterministic ID makes the common path fast,
the index makes a drifted ID impossible rather than merely unlikely.

Namespace constant, fixed here so it never changes:
`9b1d4f6a-3c2e-4a58-8f7b-1e0d2c3b4a59`.

### D6. Migration `0002`, not an edit to `0001`

`0001_ledger_schema.up.sql`'s own header comment says editing it in place is
correct "only here… The database-migrations skill's rule against editing an
applied migration takes effect the moment this runs anywhere else — Phase 5b
onward." This is Phase 5b. Add `0002`.

### D7. Explicit JSON tags on the Kafka wire format, added before anything consumes it

`events.KafkaProducer.Produce` marshals each event with `json.Marshal(ev)`,
and neither `WagerPlaced` nor `RoundSettled` carries JSON tags — so the wire
format is currently Go field names (`"RoomID"`, `"IdempotencyKey"`), decided
implicitly by struct field spelling. Renaming a Go field would silently
change a money wire format.

Task 1 pins it: explicit `snake_case` tags, asserted byte-for-byte. Nothing
consumes these topics yet and no deployment exists, so this is free now and
expensive later. Recorded as **Amendment F1** against parent plan §7.

Note one intentional divergence: the Redis outbox field for the wagering user
is `user` (`place_wager.lua`), but the Kafka field is `user_id`, matching
`Payout`'s existing `user_id` tag. The Kafka format is internally consistent;
the Redis stream format is unchanged and stays as it is.

---

## File Structure

**Create:**

| File | Responsibility |
|---|---|
| `backend/migrations/0002_ledger_indexes.up.sql` | Partial unique indexes on account natural keys; indexes on `ledger_entries(transaction_id)` and `(account_id)` |
| `backend/migrations/0002_ledger_indexes.down.sql` | Drops all four indexes |
| `backend/internal/events/message.go` | `DecodeMessage(topic, value)` — the Kafka-side wire boundary, with validation |
| `backend/internal/events/consumer.go` | `KafkaConsumer` — `Reader` over both topics under one group, manual offset commit |
| `backend/internal/ledger/account.go` | `AccountKind`, `Direction`, `AccountRef`, deterministic `AccountRef.ID()` |
| `backend/internal/ledger/mapping.go` | **Pure** `TransactionFor(events.Event) (Transaction, error)` — no I/O |
| `backend/internal/ledger/repo.go` | `Repo` over `pgxpool` — `WriteBatch` plus the four room-scoped read methods |
| `backend/internal/ledger/worker.go` | `Worker.Once` / `Worker.Run` — fetch → map → write → commit |
| `backend/cmd/ledger-worker/main.go` | Thin wiring: config → pool → consumer → worker → graceful shutdown |

**Modify:**

| File | Change |
|---|---|
| `backend/internal/events/event.go` | Add JSON tags to `WagerPlaced` and `RoundSettled` (D7) |
| `backend/internal/config/config.go` | Add `LedgerConfig` + `LoadLedger` |
| `Makefile` | Add a `ledger-worker` run target next to `migrate` |
| `CLAUDE.md` | Repository Layout note for `internal/ledger`; anything Task 6 finds stale |
| `docs/plans/2026-08-21-implementation-plan.md` | Amendments F1–F3 |
| `docs/project-history.md` | Phase 5b security review + accepted coverage gaps |

**Test files:** `backend/internal/events/message_test.go`,
`backend/internal/events/consumer_test.go`,
`backend/internal/ledger/testmain_test.go`,
`backend/internal/ledger/mapping_test.go`,
`backend/internal/ledger/repo_test.go`,
`backend/internal/ledger/worker_test.go`,
`backend/internal/ledger/reconcile_test.go`,
`backend/internal/config/ledger_config_test.go`.

**A trap worth stating once:** `accounts.user_id`, `accounts.room_id`,
`transactions.id`, `transactions.room_id`, and `transactions.round_id` are
all PostgreSQL `uuid` columns. `redisstore`'s test helper `testID()` produces
strings like `room-TestFoo-3`, which are **not** valid UUIDs and will fail
insertion. Every ID that reaches PostgreSQL in a test must come from
`uuid.NewString()`. `transactions.idempotency_key` is `text` and has no such
constraint.

---

## Task 1: Kafka wire format — tags, decoding, validation

**Files:**
- Modify: `backend/internal/events/event.go` (add JSON tags to `WagerPlaced` and `RoundSettled`)
- Create: `backend/internal/events/message.go`
- Test: `backend/internal/events/message_test.go`

**Interfaces:**
- Consumes: `events.WagerPlaced`, `events.RoundSettled`, `events.Payout`,
  `events.TopicWagersPlaced`, `events.TopicRoundsSettled`,
  `events.ErrUnknownEventType` — all existing.
- Produces:
  - `func DecodeMessage(topic string, value []byte) (Event, error)`
  - `var ErrInvalidEvent = errors.New("events: invalid event")`

**Checkpoint 1: both event types marshal to explicit snake_case JSON**

- [ ] **Step 1: Write the failing test, then run it**

Spec: two cases, exact-string assertions on `json.Marshal`'s output.

`WagerPlaced{RoomID: "r1", RoundID: "rd1", UserID: "u1", IdempotencyKey: "k1", Outcome: 1, Amount: 50, Balance: 950}` marshals to exactly:

```
{"room_id":"r1","round_id":"rd1","user_id":"u1","idempotency_key":"k1","outcome":1,"amount":50,"balance":950}
```

`RoundSettled{RoomID: "r1", RoundID: "rd1", IdempotencyKey: "k2", WinningOutcome: 1, Total: 100, Dust: 2, Payouts: []Payout{{UserID: "u1", Amount: 98}}, Refunded: false}` marshals to exactly:

```
{"room_id":"r1","round_id":"rd1","idempotency_key":"k2","winning_outcome":1,"total":100,"dust":2,"payouts":[{"user_id":"u1","amount":98}],"refunded":false}
```

Run: `cd backend && go test ./internal/events/ -run TestEventJSONWireFormat -count=1`
Expected: FAIL — current output uses Go field names (`{"RoomID":"r1",...}`), so the string comparison fails on the very first key.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add `json:"..."` tags to every field of `WagerPlaced` and
`RoundSettled` in declaration order, using exactly the names above. Do not
reorder fields — Go marshals in declaration order and the assertion pins it.
Leave `Payout`'s existing tags alone. Add a comment on each struct recording
that this is the Kafka wire format and that renaming a field is a wire break
(D7).

```bash
cd backend && go test ./internal/events/ -run TestEventJSONWireFormat -count=1 && \
  git add internal/events/event.go internal/events/message_test.go && \
  git commit -m "feat: pin the Kafka wire format with explicit JSON tags"
```

Expected: PASS, then one commit.

**Checkpoint 2: DecodeMessage round-trips both types, routing by topic**

- [ ] **Step 1: Write the failing test, then run it**

Spec: marshal each of the two structs from Checkpoint 1, pass the bytes to
`DecodeMessage` with the matching topic, and assert the returned `Event` is
`reflect.DeepEqual` to the original struct value.

- `DecodeMessage(TopicWagersPlaced, wagerJSON)` → the `WagerPlaced` value, nil error.
- `DecodeMessage(TopicRoundsSettled, settledJSON)` → the `RoundSettled` value, nil error.
- `DecodeMessage(TopicRoundsSettled, refundJSON)` where the payload has `"refunded":true`, `"winning_outcome":-1`, `"dust":0` → the `RoundSettled` value with `Refunded: true`, nil error.

Run: `cd backend && go test ./internal/events/ -run TestDecodeMessage -count=1`
Expected: FAIL — `undefined: DecodeMessage` (compile error).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `DecodeMessage(topic string, value []byte) (Event, error)` switches
on `topic`, unmarshals into the corresponding concrete type, and returns it
as an `Event`. `TopicWagersPlaced → WagerPlaced`, `TopicRoundsSettled →
RoundSettled`. Package doc comment on `message.go` must state that the
concrete type is determined by the topic alone, because the wire payload
carries no type discriminator — `Refunded` distinguishes a refund from a
resolution *within* `RoundSettled`, it does not distinguish the two structs.

```bash
cd backend && go test ./internal/events/ -run TestDecodeMessage -count=1 && \
  git add internal/events/message.go internal/events/message_test.go && \
  git commit -m "feat: decode Kafka messages into typed events by topic"
```

Expected: PASS, then one commit.

**Checkpoint 3: an unrecognised topic is an error, never a skip**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `DecodeMessage("some-other-topic", []byte("{}"))` returns a nil `Event`
and an error satisfying `errors.Is(err, ErrUnknownEventType)` whose message
contains the topic name `some-other-topic`.

Run: `cd backend && go test ./internal/events/ -run TestDecodeMessageUnknownTopic -count=1`
Expected: FAIL — the default branch currently does not exist, so the switch
falls through and returns a nil error.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `DecodeMessage`'s `default` branch returns
`fmt.Errorf("%w: topic %q", ErrUnknownEventType, topic)`. Reuse the existing
sentinel rather than adding a second one — a consumer that silently dropped
an unrecognised topic would lose a money movement without signalling, the
same reason `Decode` already returns this error.

```bash
cd backend && go test ./internal/events/ -run TestDecodeMessage -count=1 && \
  git add internal/events/message.go internal/events/message_test.go && \
  git commit -m "feat: reject messages from an unrecognised topic"
```

Expected: PASS, then one commit.

**Checkpoint 4: unknown JSON fields are rejected, not ignored**

- [ ] **Step 1: Write the failing test, then run it**

Spec: decode this payload on `TopicWagersPlaced` — valid in every respect
except the trailing field:

```json
{"room_id":"r1","round_id":"rd1","user_id":"u1","idempotency_key":"k1","outcome":1,"amount":50,"balance":950,"surprise":7}
```

`DecodeMessage` must return a nil `Event` and a non-nil error whose message
contains `surprise`.

Rationale to put in the test's comment: this is the drift guard for a money
wire format. A producer that renamed `amount` to `stake` would otherwise
decode to `Amount: 0` and write a zero-token ledger entry rather than
failing loudly.

Run: `cd backend && go test ./internal/events/ -run TestDecodeMessageRejectsUnknownField -count=1`
Expected: FAIL — `encoding/json`'s default behaviour ignores unknown fields, so decoding succeeds and the error is nil.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: decode with `json.NewDecoder(bytes.NewReader(value))` and
`dec.DisallowUnknownFields()` rather than `json.Unmarshal`. Wrap the
resulting error with the topic name for context.

```bash
cd backend && go test ./internal/events/ -run TestDecodeMessage -count=1 && \
  git add internal/events/message.go internal/events/message_test.go && \
  git commit -m "feat: reject Kafka messages carrying unknown fields"
```

Expected: PASS, then one commit.

**Checkpoint 5: money-invalid payloads are rejected at the wire boundary**

- [ ] **Step 1: Write the failing test, then run it**

Spec: table-driven. Every case decodes cleanly as JSON and must still be
rejected with an error satisfying `errors.Is(err, ErrInvalidEvent)` whose
message names the offending field.

On `TopicWagersPlaced`:

| case | payload change from the valid wager | why |
|---|---|---|
| empty room_id | `"room_id":""` | a ledger row keyed to no room is unauditable |
| empty round_id | `"round_id":""` | same |
| empty user_id | `"user_id":""` | a debit with no account holder |
| empty idempotency_key | `"idempotency_key":""` | destroys replay safety |
| zero amount | `"amount":0` | `ledger_entries.amount` has `CHECK (amount > 0)`; reject at the boundary with a named field, not as an opaque database error |
| negative amount | `"amount":-50` | same |
| negative outcome | `"outcome":-1` | not a valid outcome index |
| negative balance | `"balance":-1` | `place_wager.lua` rejects insufficient funds, so a negative post-wager balance means corruption upstream |

On `TopicRoundsSettled` (base: the valid settled payload from Checkpoint 1):

| case | payload change | why |
|---|---|---|
| empty room_id | `"room_id":""` | as above |
| empty round_id | `"round_id":""` | as above |
| empty idempotency_key | `"idempotency_key":""` | as above |
| negative total | `"total":-1` | |
| negative dust | `"dust":-1` | |
| zero payout amount | `"payouts":[{"user_id":"u1","amount":0}]` | violates `CHECK (amount > 0)` |
| empty payout user | `"payouts":[{"user_id":"","amount":98}]` | credit with no account holder |
| resolved with no outcome | `"refunded":false,"winning_outcome":-1` | `-1` is the refund sentinel |
| refund carrying an outcome | `"refunded":true,"winning_outcome":1` | both Lua paths emit `winning_outcome:''` → `-1` for a refund |
| refund carrying dust | `"refunded":true,"winning_outcome":-1,"dust":1` | `domain.Settle`'s refund path returns every stake in full, so a refund strands no dust |

Also assert the two positive controls still pass: an empty `"payouts":[]`
with `"total":0,"dust":0` is **valid** (a round that locked with no wagers
and then settled), and the valid payloads from Checkpoint 2 still decode.

Run: `cd backend && go test ./internal/events/ -run TestDecodeMessageValidation -count=1`
Expected: FAIL — no validation exists, so every case decodes successfully with a nil error.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add unexported `validate()` methods (or equivalent helper
functions) invoked by `DecodeMessage` after unmarshalling, returning
`fmt.Errorf("%w: <field> ...", ErrInvalidEvent, ...)` naming the field.
Declare `ErrInvalidEvent` in `message.go`. Validation belongs here, at the
wire boundary, not in the mapping layer — a missing required field must
surface as an error rather than a silently-substituted zero value, exactly as
`decode.go` already documents for the Redis-side decode.

```bash
cd backend && go test ./internal/events/ -count=1 && \
  git add internal/events/message.go internal/events/message_test.go && \
  git commit -m "feat: validate decoded events at the Kafka wire boundary"
```

Expected: PASS, then one commit.

**Task 1 boundary — full suite**

```bash
cd backend && go vet ./... && gofmt -l . && go test ./... -race -cover -p 1 -count=1
```

Expected: PASS, no `gofmt` output, no vet findings.

---

## Task 2: Event → ledger transaction mapping (pure, no I/O)

**Files:**
- Create: `backend/internal/ledger/account.go`
- Create: `backend/internal/ledger/mapping.go`
- Test: `backend/internal/ledger/mapping_test.go`

**Interfaces:**
- Consumes: `events.Event`, `events.WagerPlaced`, `events.RoundSettled`,
  `events.Payout`, `events.ErrUnknownEventType`.
- Produces:
  ```go
  type AccountKind string
  const (
      KindUserWallet AccountKind = "user_wallet"
      KindRoomEscrow AccountKind = "room_escrow"
      KindRoundPool  AccountKind = "round_pool"
      KindSystemMint AccountKind = "system_mint"
      KindSystemDust AccountKind = "system_dust"
  )

  type Direction string
  const (
      Debit  Direction = "debit"
      Credit Direction = "credit"
  )

  type AccountRef struct {
      Kind   AccountKind
      UserID string // "" when the kind does not scope by user
      RoomID string // "" when the kind does not scope by room
  }

  // ID is the deterministic UUIDv5 this account is stored under (D5).
  func (a AccountRef) ID() uuid.UUID

  type Entry struct {
      Account   AccountRef
      Direction Direction
      Amount    int64
  }

  type Transaction struct {
      IdempotencyKey string
      Kind           string // "wager" | "settlement" | "refund"
      RoomID         string
      RoundID        string
      Entries        []Entry
  }

  var ErrUnbalanced = errors.New("ledger: transaction is not balanced")

  func TransactionFor(ev events.Event) (Transaction, error)
  ```

This file must not import `pgx`, `kafka-go`, or `redis`. It is the phase's
one pure surface and its tests must run with nothing up.

**Checkpoint 1: a wager maps to a two-entry transaction**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TransactionFor(events.WagerPlaced{RoomID: "<roomUUID>", RoundID: "<roundUUID>", UserID: "<userUUID>", IdempotencyKey: "k1", Outcome: 1, Amount: 50, Balance: 950})` returns, with a nil error:

```
Transaction{
  IdempotencyKey: "k1",
  Kind:           "wager",
  RoomID:         "<roomUUID>",
  RoundID:        "<roundUUID>",
  Entries: []Entry{
    {Account: AccountRef{Kind: KindUserWallet, UserID: "<userUUID>"}, Direction: Debit,  Amount: 50},
    {Account: AccountRef{Kind: KindRoundPool,  RoomID: "<roomUUID>"}, Direction: Credit, Amount: 50},
  },
}
```

Entry order is part of the assertion — deterministic output makes a failing
diff readable. Assert with `reflect.DeepEqual`.

Also assert in the same checkpoint that `AccountRef{Kind: KindUserWallet, UserID: "u"}.ID()` is stable across two calls and differs from `AccountRef{Kind: KindRoundPool, RoomID: "u"}.ID()` — the kind must be part of the hashed key, or a user and a room sharing an ID string would collide onto one account.

Run: `cd backend && go test ./internal/ledger/ -run TestTransactionForWager -count=1`
Expected: FAIL — the `ledger` package does not exist (build error).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: create `account.go` with the types above.
`AccountRef.ID()` returns `uuid.NewSHA1(accountNamespace, []byte(string(a.Kind)+":"+a.UserID+":"+a.RoomID))` where `accountNamespace = uuid.MustParse("9b1d4f6a-3c2e-4a58-8f7b-1e0d2c3b4a59")`.
Create `mapping.go` with `TransactionFor` handling the `events.WagerPlaced`
case per the spec above. Document D1's sign convention on the `Direction`
type: credit is tokens in, debit is tokens out, balance is
`Σ credits − Σ debits`.

```bash
cd backend && go test ./internal/ledger/ -run TestTransactionForWager -count=1 && \
  git add internal/ledger/account.go internal/ledger/mapping.go internal/ledger/mapping_test.go && \
  git commit -m "feat: map a placed wager to a balanced ledger transaction"
```

Expected: PASS, then one commit.

**Checkpoint 2: a resolved settlement maps to pool debit, payout credits, dust credit**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TransactionFor(events.RoundSettled{RoomID: "<roomUUID>", RoundID: "<roundUUID>", IdempotencyKey: "k2", WinningOutcome: 1, Total: 100, Dust: 2, Payouts: []events.Payout{{UserID: "<uA>", Amount: 60}, {UserID: "<uB>", Amount: 38}}, Refunded: false})` returns, with a nil error:

```
Transaction{
  IdempotencyKey: "k2",
  Kind:           "settlement",
  RoomID:         "<roomUUID>",
  RoundID:        "<roundUUID>",
  Entries: []Entry{
    {Account: AccountRef{Kind: KindRoundPool,  RoomID: "<roomUUID>"}, Direction: Debit,  Amount: 100},
    {Account: AccountRef{Kind: KindUserWallet, UserID: "<uA>"},       Direction: Credit, Amount: 60},
    {Account: AccountRef{Kind: KindUserWallet, UserID: "<uB>"},       Direction: Credit, Amount: 38},
    {Account: AccountRef{Kind: KindSystemDust},                       Direction: Credit, Amount: 2},
  },
}
```

Order: pool debit first, payouts in event order, dust last.

Run: `cd backend && go test ./internal/ledger/ -run TestTransactionForSettlement -count=1`
Expected: FAIL — `TransactionFor` has no `events.RoundSettled` case, so it falls through and returns `ErrUnknownEventType`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add the `events.RoundSettled` case, emitting entries in the order
above. `Kind` is `"settlement"` when `Refunded` is false.

```bash
cd backend && go test ./internal/ledger/ -run TestTransactionFor -count=1 && \
  git add internal/ledger/mapping.go internal/ledger/mapping_test.go && \
  git commit -m "feat: map a resolved settlement to pool, payout, and dust entries"
```

Expected: PASS, then one commit.

**Checkpoint 3: zero dust produces no dust entry**

- [ ] **Step 1: Write the failing test, then run it**

Spec: the Checkpoint 2 event with `Total: 100, Dust: 0, Payouts: [{<uA>, 60}, {<uB>, 40}]` returns a transaction with exactly **three** entries — pool debit 100, credit 60, credit 40 — and **no** `KindSystemDust` entry.

Rationale for the test comment: `ledger_entries.amount` is
`CHECK (amount > 0)`, so a zero-amount dust entry is not merely redundant, it
is rejected by the database. An exactly-divisible round is the common case,
not an edge case.

Run: `cd backend && go test ./internal/ledger/ -run TestTransactionForZeroDust -count=1`
Expected: FAIL — the implementation appends the dust entry unconditionally, producing four entries with a zero-amount fourth.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: append the dust entry only when `ev.Dust > 0`.

```bash
cd backend && go test ./internal/ledger/ -run TestTransactionFor -count=1 && \
  git add internal/ledger/mapping.go internal/ledger/mapping_test.go && \
  git commit -m "feat: omit the dust entry when a round divides exactly"
```

Expected: PASS, then one commit.

**Checkpoint 4: a refund maps with kind "refund"**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TransactionFor(events.RoundSettled{RoomID: "<roomUUID>", RoundID: "<roundUUID>", IdempotencyKey: "k3", WinningOutcome: -1, Total: 100, Dust: 0, Payouts: []events.Payout{{UserID: "<uA>", Amount: 60}, {UserID: "<uB>", Amount: 40}}, Refunded: true})` returns a transaction with `Kind: "refund"` and the same three-entry shape as Checkpoint 3.

`Kind` distinguishes the two in the ledger because the money shape is
identical but the reason is not — an auditor reading `transactions` must be
able to tell a resolved round from a refunded one without joining anything.

Run: `cd backend && go test ./internal/ledger/ -run TestTransactionForRefund -count=1`
Expected: FAIL — `Kind` is unconditionally `"settlement"`, so the assertion on `"refund"` fails.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Kind` is `"refund"` when `ev.Refunded` is true, `"settlement"` otherwise.

```bash
cd backend && go test ./internal/ledger/ -run TestTransactionFor -count=1 && \
  git add internal/ledger/mapping.go internal/ledger/mapping_test.go && \
  git commit -m "feat: record a refunded round under its own transaction kind"
```

Expected: PASS, then one commit.

**Checkpoint 5: a zero-total settlement maps to a transaction with no entries**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TransactionFor(events.RoundSettled{RoomID: "<roomUUID>", RoundID: "<roundUUID>", IdempotencyKey: "k4", WinningOutcome: -1, Total: 0, Dust: 0, Payouts: []events.Payout{}, Refunded: true})` returns, with a nil error, a `Transaction` with `Kind: "refund"` and `len(Entries) == 0`.

This is reachable, not hypothetical: a round that locks with no wagers and
then hits the 60-second auto-refund produces exactly this event. The
transaction row is still written so that every terminal event has exactly one
row and replay dedupes uniformly; it simply has no legs.

Run: `cd backend && go test ./internal/ledger/ -run TestTransactionForEmptyRound -count=1`
Expected: FAIL — the pool debit is appended unconditionally, producing one entry with `Amount: 0`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: append the pool debit only when `ev.Total > 0`. Comment the
zero-entry case explicitly so nobody later "fixes" it into an error.

```bash
cd backend && go test ./internal/ledger/ -run TestTransactionFor -count=1 && \
  git add internal/ledger/mapping.go internal/ledger/mapping_test.go && \
  git commit -m "feat: map a settled round with no wagers to an empty transaction"
```

Expected: PASS, then one commit.

**Checkpoint 6: an arithmetically inconsistent event is rejected**

- [ ] **Step 1: Write the failing test, then run it**

Spec: two cases, both returning an error satisfying `errors.Is(err, ErrUnbalanced)` and a zero `Transaction`:

- `Total: 100, Dust: 2, Payouts: [{<uA>, 60}]` — `Σpayouts + dust = 62 ≠ 100`.
- `Total: 100, Dust: 0, Payouts: [{<uA>, 60}, {<uB>, 50}]` — `110 ≠ 100`.

And one positive control: `Total: 100, Dust: 2, Payouts: [{<uA>, 60}, {<uB>, 38}]` returns nil error.

Also assert `TransactionFor` on an `events.Event` implementation that is
neither type returns `ErrUnknownEventType`. (Define a tiny local stub type in
the test satisfying the three-method interface.)

Rationale for the test comment: `domain.Settle` guarantees
`Σ payouts + dust == Σ stakes` with a fuzz test, so a violation here means
the event was corrupted between Redis and this consumer. The deferred
trigger would also reject it at COMMIT, but as an opaque database error
naming a transaction ID; failing here names the arithmetic. This is a
verification of the event, not a second implementation of the payout formula
— do not compute payouts here.

Run: `cd backend && go test ./internal/ledger/ -run TestTransactionForUnbalanced -count=1`
Expected: FAIL — no balance check exists, so the unbalanced cases return a transaction and a nil error.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: in the `events.RoundSettled` case, before building entries, compute
`sum := ev.Dust; for _, p := range ev.Payouts { sum += p.Amount }` and return
`Transaction{}, fmt.Errorf("%w: round %s total %d but payouts+dust %d", ErrUnbalanced, ev.RoundID, ev.Total, sum)` when `sum != ev.Total`.
Confirm the `default` branch already returns `ErrUnknownEventType`; add it if
Checkpoint 1 did not.

```bash
cd backend && go test ./internal/ledger/ -count=1 && \
  git add internal/ledger/mapping.go internal/ledger/mapping_test.go && \
  git commit -m "feat: reject a settlement whose payouts and dust miss its total"
```

Expected: PASS, then one commit.

**Task 2 boundary — full suite**

```bash
cd backend && go vet ./... && gofmt -l . && go test ./... -race -cover -p 1 -count=1
```

Expected: PASS. `internal/ledger` should read close to 100% at this point — it is still pure.

---

## Task 3: PostgreSQL repository

**Files:**
- Create: `backend/migrations/0002_ledger_indexes.up.sql`
- Create: `backend/migrations/0002_ledger_indexes.down.sql`
- Create: `backend/internal/ledger/repo.go`
- Create: `backend/internal/ledger/testmain_test.go`
- Test: `backend/internal/ledger/repo_test.go`

**Interfaces:**
- Consumes: `ledger.Transaction`, `ledger.Entry`, `ledger.AccountRef`,
  `ledger.Direction` (Task 2); `migrate.Up`, `migrate.Down`
  (`backend/internal/migrate`); `pgxpool.Pool`.
- Produces:
  ```go
  type Repo struct { /* pool *pgxpool.Pool */ }

  func New(pool *pgxpool.Pool) *Repo

  // WriteBatch persists every transaction and its entries inside one
  // PostgreSQL transaction, skipping any whose idempotency_key is already
  // present. Returns how many were newly written.
  func (r *Repo) WriteBatch(ctx context.Context, txns []Transaction) (written int, err error)

  // WalletBalancesForRoom returns Σcredits − Σdebits per user_wallet
  // account, restricted to transactions belonging to roomID.
  func (r *Repo) WalletBalancesForRoom(ctx context.Context, roomID string) (map[string]int64, error)

  // PoolBalance returns Σcredits − Σdebits for a room's round_pool account.
  func (r *Repo) PoolBalance(ctx context.Context, roomID string) (int64, error)

  // DustForRoom returns the dust credited by transactions belonging to roomID.
  func (r *Repo) DustForRoom(ctx context.Context, roomID string) (int64, error)

  // TransactionCount returns how many transactions belong to roomID.
  func (r *Repo) TransactionCount(ctx context.Context, roomID string) (int, error)
  ```

`TestMain` for this package: connect to `POSTGRES_DSN` (default
`postgres://callit:callit@localhost:5432/callit?sslmode=disable`), `DROP
DATABASE IF EXISTS callit_test` then `CREATE DATABASE callit_test`, set a
package-level `testDSN` pointing at it, and run `migrate.Up(ctx, testDSN)`.
Mirror `backend/internal/migrate/testmain_test.go` including its
`replaceDBName` helper and its `log.Fatalf` on an unreachable database —
fail, never skip. Task 6 extends this same `TestMain` with Redis and Kafka
probes; write it so that extension is additive.

**Checkpoint 1: migration 0002 adds the identity and lookup indexes**

- [ ] **Step 1: Write the failing test, then run it**

Spec: with `migrate.Up` applied to `callit_test`, query
`pg_indexes` for `schemaname = 'public'` and assert all four index names are
present:

- `accounts_user_wallet_key`
- `accounts_round_pool_key`
- `accounts_system_singleton_key`
- `ledger_entries_transaction_id_idx`
- `ledger_entries_account_id_idx`

Then assert the identity constraint actually bites: insert
`accounts (id, kind, user_id)` twice with **different** `id` values and the
same `user_id` under `kind = 'user_wallet'`, and require the second insert to
fail with a `*pgconn.PgError` whose `Code` is `23505` (unique violation).

Run: `cd backend && go test ./internal/ledger/ -run TestMigration0002 -count=1`
Expected: FAIL — no `0002` migration exists, so none of the indexes are present.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `0002_ledger_indexes.up.sql` contains exactly:

```sql
-- Account identity. Phase 5b derives accounts.id deterministically from
-- (kind, user_id/room_id) so provisioning is a single ON CONFLICT (id) DO
-- NOTHING with no lookup round trip. These partial unique indexes are the
-- enforcing constraint behind that convention: a drifted or hand-written id
-- for an identity that already exists is rejected rather than silently
-- creating a second account that splits one holder's balance in two.
CREATE UNIQUE INDEX accounts_user_wallet_key
    ON accounts (user_id) WHERE kind = 'user_wallet';
CREATE UNIQUE INDEX accounts_round_pool_key
    ON accounts (room_id) WHERE kind = 'round_pool';
CREATE UNIQUE INDEX accounts_system_singleton_key
    ON accounts (kind) WHERE kind IN ('system_mint', 'system_dust');

-- assert_transaction_balanced() runs per inserted row and looks entries up
-- by transaction_id. Without this index that lookup is a sequential scan of
-- the whole ledger_entries table on every entry insert — quadratic over a
-- load test of thousands of wagers, which is precisely the run the flagship
-- reconciliation test performs.
CREATE INDEX ledger_entries_transaction_id_idx ON ledger_entries (transaction_id);

-- Balance queries aggregate a single account's entries.
CREATE INDEX ledger_entries_account_id_idx ON ledger_entries (account_id);
```

`0002_ledger_indexes.down.sql` drops all five in reverse order with
`DROP INDEX IF EXISTS`.

Note the file naming convention is `NNNN_name.up.sql` / `.down.sql`
(`CLAUDE.md` Repository Layout) and `migrations/embed.go`'s `//go:embed
*.sql` picks new files up with no change.

```bash
cd backend && go test ./internal/ledger/ ./internal/migrate/ -count=1 && \
  git add migrations/0002_ledger_indexes.up.sql migrations/0002_ledger_indexes.down.sql internal/ledger/testmain_test.go internal/ledger/repo_test.go && \
  git commit -m "feat: add ledger identity and lookup indexes in migration 0002"
```

Expected: PASS, then one commit.

**Checkpoint 2: WriteBatch persists a transaction and its entries**

- [ ] **Step 1: Write the failing test, then run it**

Spec: build a wager transaction via `TransactionFor` for a fresh
`uuid.NewString()` room, round, and user with `Amount: 50`, call
`repo.WriteBatch(ctx, []Transaction{txn})`, and assert:

- `written == 1`, nil error.
- `TransactionCount(ctx, roomID) == 1`.
- `WalletBalancesForRoom(ctx, roomID)` is `map[string]int64{userID: -50}` — a debit of 50 under D1's sign convention.
- `PoolBalance(ctx, roomID) == 50`.

Run: `cd backend && go test ./internal/ledger/ -run TestWriteBatch -count=1`
Expected: FAIL — `undefined: New` / `undefined: Repo` (compile error).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `WriteBatch` opens one `pgx` transaction via `pool.Begin`, and for
each `Transaction`:

1. `INSERT INTO transactions (id, idempotency_key, kind, room_id, round_id) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (idempotency_key) DO NOTHING RETURNING id` with `id = uuid.New()`. If no row comes back, this key is already applied — skip its entries entirely and move on without counting it.
2. For each `Entry`: `INSERT INTO accounts (id, kind, user_id, room_id) VALUES ($1,$2,$3,$4) ON CONFLICT (id) DO NOTHING` using `entry.Account.ID()`, passing `nil` (SQL NULL) for an empty `UserID`/`RoomID` rather than the empty string — `uuid` columns reject `''`.
3. For each `Entry`: `INSERT INTO ledger_entries (id, transaction_id, account_id, direction, amount) VALUES ($1,$2,$3,$4,$5)`.
4. Increment `written`.

Commit once at the end; `defer tx.Rollback(ctx)` for the error path. The
deferred constraint trigger fires at COMMIT, so a balance violation surfaces
from `tx.Commit`, not from an entry insert — handle the error there.

The four read methods are one `SELECT` each, no N+1:

```sql
-- WalletBalancesForRoom
SELECT a.user_id::text,
       SUM(CASE WHEN e.direction = 'credit' THEN e.amount ELSE -e.amount END)
  FROM ledger_entries e
  JOIN accounts     a ON a.id = e.account_id
  JOIN transactions t ON t.id = e.transaction_id
 WHERE a.kind = 'user_wallet' AND t.room_id = $1
 GROUP BY a.user_id;

-- PoolBalance  (same shape; a.kind = 'round_pool' AND a.room_id = $1,
--               no transactions join needed, COALESCE(...,0))
-- DustForRoom  (a.kind = 'system_dust' AND t.room_id = $1, COALESCE(...,0))
-- TransactionCount  SELECT count(*) FROM transactions WHERE room_id = $1
```

`PoolBalance` and `DustForRoom` must `COALESCE` to `0` so an unknown room
returns `0, nil` rather than a scan error.

```bash
cd backend && go test ./internal/ledger/ -count=1 && \
  git add internal/ledger/repo.go internal/ledger/repo_test.go && \
  git commit -m "feat: write a ledger transaction and read its balances back"
```

Expected: PASS, then one commit.

**Checkpoint 3: one account row per identity across many transactions**

- [ ] **Step 1: Write the failing test, then run it**

Spec: write **three** wager transactions for the same room, round, and user
(distinct `IdempotencyKey`s, `Amount: 10` each) in a single `WriteBatch`
call, then assert:

- `written == 3`.
- `SELECT count(*) FROM accounts WHERE kind = 'user_wallet' AND user_id = $1` is `1`.
- `SELECT count(*) FROM accounts WHERE kind = 'round_pool' AND room_id = $1` is `1`.
- `WalletBalancesForRoom(ctx, roomID)[userID] == -30`.

Then repeat with the three transactions split across three separate
`WriteBatch` calls and assert the same account counts — provisioning must be
idempotent across batches, not just within one.

Run: `cd backend && go test ./internal/ledger/ -run TestWriteBatchProvisionsAccountsOnce -count=1`
Expected: FAIL — depends on how Checkpoint 2 was implemented. If `ON CONFLICT (id) DO NOTHING` is already in place this may PASS immediately; if so, **that is the signal to fold this checkpoint into Checkpoint 2 rather than manufacture a failure**. Record the fold in the journal and move to Checkpoint 4.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: ensure the account insert uses `ON CONFLICT (id) DO NOTHING` and
that `AccountRef.ID()` is called once per entry, not once per transaction.

```bash
cd backend && go test ./internal/ledger/ -count=1 && \
  git add internal/ledger/repo.go internal/ledger/repo_test.go && \
  git commit -m "test: verify accounts are provisioned once per identity"
```

Expected: PASS, then one commit.

**Checkpoint 4: a replayed idempotency key writes nothing**

- [ ] **Step 1: Write the failing test, then run it**

Spec: write one wager transaction (`Amount: 50`), then call `WriteBatch` with
**the same** `Transaction` value again. Assert on the second call:

- `written == 0`, nil error — a replay is success, not failure. The consumer must be able to commit its Kafka offset on it.
- `TransactionCount(ctx, roomID)` is still `1`.
- `WalletBalancesForRoom(ctx, roomID)[userID]` is still `-50`.
- `SELECT count(*) FROM ledger_entries` scoped to that transaction is still `2` — the entries must not be duplicated onto the first transaction's row.

Then a third case: one batch containing the **same** transaction twice (a
Kafka duplicate delivered inside one fetch) returns `written == 1` and leaves
one transaction row.

Run: `cd backend && go test ./internal/ledger/ -run TestWriteBatchIsIdempotent -count=1`
Expected: FAIL unless Checkpoint 2's `ON CONFLICT (idempotency_key) DO NOTHING RETURNING id` already skipped entries on a nil return. If it PASSES, fold as described in Checkpoint 3 and record it.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: on a nil `RETURNING id`, `continue` to the next transaction without
inserting entries and without incrementing `written`. Document on
`WriteBatch` that the `UNIQUE` constraint on `idempotency_key` is what makes
at-least-once Kafka delivery safe, and that `ON CONFLICT DO NOTHING` is used
rather than catching a `23505` error so the surrounding `pgx` transaction is
never aborted mid-batch by a duplicate.

```bash
cd backend && go test ./internal/ledger/ -count=1 && \
  git add internal/ledger/repo.go internal/ledger/repo_test.go && \
  git commit -m "feat: skip a replayed transaction rather than duplicating it"
```

Expected: PASS, then one commit.

**Checkpoint 5: the database rejects an unbalanced transaction at COMMIT**

- [ ] **Step 1: Write the failing test, then run it**

Spec: hand-build a `Transaction` that bypasses Task 2's arithmetic check —
`Entries: [{user_wallet(u), Debit, 50}, {round_pool(room), Credit, 40}]` —
and call `WriteBatch`. Assert:

- a non-nil error is returned, whose message contains `not balanced` (the trigger's own `RAISE EXCEPTION` text);
- **and the whole batch rolled back**: put a second, perfectly valid transaction in the same batch and assert `TransactionCount(ctx, roomID) == 0` afterwards.

Rationale for the test comment: this is the test that proves the invariant
lives in the database rather than in application code (parent plan §6). Task
2's `ErrUnbalanced` is a fast, well-named first line; this is the one that
holds if a future caller bypasses `TransactionFor` entirely.

Run: `cd backend && go test ./internal/ledger/ -run TestWriteBatchRejectsUnbalanced -count=1`
Expected: FAIL only if `WriteBatch` mishandles the commit-time error (e.g. returns nil, or leaves the valid transaction committed). If the error already surfaces correctly and the rollback already holds, this is a genuine PASS-on-write — fold and record it as in Checkpoint 3.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: return the error from `tx.Commit(ctx)` wrapped as
`fmt.Errorf("ledger: committing batch of %d: %w", len(txns), err)`, and
return `written = 0` alongside it — nothing in a rolled-back batch was
written, and a caller that committed a Kafka offset against a non-zero count
here would lose the batch.

```bash
cd backend && go test ./internal/ledger/ -count=1 && \
  git add internal/ledger/repo.go internal/ledger/repo_test.go && \
  git commit -m "feat: roll back the whole batch when the balance trigger fires"
```

Expected: PASS, then one commit.

**Task 3 boundary — full suite**

```bash
cd backend && go vet ./... && gofmt -l . && go test ./... -race -cover -p 1 -count=1
```

Expected: PASS.

---

## Task 4: Kafka consumer and the worker loop

**Files:**
- Create: `backend/internal/events/consumer.go`
- Create: `backend/internal/ledger/worker.go`
- Test: `backend/internal/events/consumer_test.go`
- Test: `backend/internal/ledger/worker_test.go`

**Interfaces:**
- Consumes: `events.DecodeMessage` (Task 1), `ledger.TransactionFor` (Task 2),
  `ledger.Repo.WriteBatch` (Task 3), `events.TopicWagersPlaced`,
  `events.TopicRoundsSettled`, `kafka.Reader`.
- Produces:
  ```go
  // in package events
  const GroupLedgerWriter = "ledger-writer"

  type KafkaConsumer struct { /* reader *kafka.Reader */ }

  // NewKafkaConsumer joins group over the given topics. startFromBeginning
  // selects kafka.FirstOffset for a group with no committed offset.
  func NewKafkaConsumer(brokers []string, group string, topics []string, startFromBeginning bool) *KafkaConsumer

  // Fetch returns the next message without committing its offset.
  func (c *KafkaConsumer) Fetch(ctx context.Context) (kafka.Message, error)

  // Commit marks msgs consumed. Never call it before the durable write.
  func (c *KafkaConsumer) Commit(ctx context.Context, msgs ...kafka.Message) error

  func (c *KafkaConsumer) Close() error

  // in package ledger
  type Consumer interface {
      Fetch(ctx context.Context) (kafka.Message, error)
      Commit(ctx context.Context, msgs ...kafka.Message) error
  }

  type Writer interface {
      WriteBatch(ctx context.Context, txns []Transaction) (int, error)
  }

  func NewWorker(c Consumer, w Writer) *Worker

  // Once performs one fetch → map → write → commit cycle over at most
  // maxBatch messages gathered within batchWindow. Returns how many
  // messages were consumed. A batch window that elapses with no message
  // returns (0, nil).
  func (wk *Worker) Once(ctx context.Context) (int, error)

  // Run loops Once until ctx is cancelled, returning nil on cancellation.
  func (wk *Worker) Run(ctx context.Context) error
  ```

`ledger.Consumer` and `ledger.Writer` are declared in `ledger` and satisfied
structurally by `events.KafkaConsumer` and `*ledger.Repo`, so `ledger` does
not import `events`' Kafka code — the same seam `relay.Producer` uses.

Constants, mirroring `relay`'s: `maxBatch = 100`, `batchWindow = 200ms`.

**Checkpoint 1: the consumer reads both topics under one group without committing**

- [ ] **Step 1: Write the failing test, then run it**

Spec (integration, real Kafka): produce one `WagerPlaced` to
`TopicWagersPlaced` and one `RoundSettled` to `TopicRoundsSettled` via
`NewKafkaProducer` (call `EnsureTopics` first). Create a
`NewKafkaConsumer(testBrokers, <unique group>, []string{TopicWagersPlaced, TopicRoundsSettled}, true)`
and `Fetch` twice with a 30-second context. Assert that across the two
fetched messages both topics appear exactly once, and that each message's
value decodes through `DecodeMessage(msg.Topic, msg.Value)` to the event that
was produced.

Then, **without** calling `Commit`, close the consumer, open a second one
with the **same** group, and assert `Fetch` returns those same two messages
again — proving offsets are not auto-committed.

Use a unique group name per run (`fmt.Sprintf("ledger-test-%s-%d", t.Name(), counter)`) so runs never inherit each other's offsets. `internal/events`' existing `TestMain` and `testTopic` helper already establish this pattern.

Run: `cd backend && go test ./internal/events/ -run TestKafkaConsumer -count=1 -timeout 120s`
Expected: FAIL — `undefined: NewKafkaConsumer` (compile error).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `NewKafkaConsumer` builds a `kafka.Reader` with `Brokers`,
`GroupID: group`, `GroupTopics: topics`, and
`StartOffset: kafka.FirstOffset` when `startFromBeginning` is true (else
`kafka.LastOffset`). `Fetch` delegates to `Reader.FetchMessage` — **not**
`ReadMessage`, which auto-commits. `Commit` delegates to
`Reader.CommitMessages`. Document on the type that `FetchMessage` is chosen
precisely so the offset never advances ahead of the PostgreSQL write, the
same rule `relay` follows for its Redis `XACK`.

```bash
cd backend && go test ./internal/events/ -count=1 -timeout 120s && \
  git add internal/events/consumer.go internal/events/consumer_test.go && \
  git commit -m "feat: consume both ledger topics under one group without auto-commit"
```

Expected: PASS, then one commit.

**Checkpoint 2: Once writes a batch, then commits its offsets**

- [ ] **Step 1: Write the failing test, then run it**

Spec (unit, no Kafka or PostgreSQL): a fake `Consumer` returning three
canned `kafka.Message` values — two on `TopicWagersPlaced`, one on
`TopicRoundsSettled` — then blocking until its context expires; and a fake
`Writer` recording the `[]Transaction` it received. Assert:

- `Once` returns `(3, nil)`.
- The fake `Writer` was called exactly once, with three transactions in fetch order, each equal to `TransactionFor` of the corresponding event.
- The fake `Consumer` recorded a `Commit` call with all three messages, and the recorded **order** is write-then-commit: the fake `Writer` must stamp a monotonic counter that the fake `Consumer`'s `Commit` compares against, so a commit-before-write implementation fails rather than passes by coincidence.

Run: `cd backend && go test ./internal/ledger/ -run TestWorkerOnce -count=1`
Expected: FAIL — `undefined: NewWorker` (compile error).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Once` calls `Fetch` once with the caller's context (blocking), then
continues fetching under a `context.WithTimeout(ctx, batchWindow)` until
`maxBatch` messages are gathered or the deadline passes — treat
`context.DeadlineExceeded` from the windowed fetch as "batch complete", not
as an error. If the very first `Fetch` returns `ctx.Err()` because the
caller's context was cancelled, return `(0, nil)`. Map each message through
`DecodeMessage` then `TransactionFor`, call `WriteBatch` once with the whole
slice, and only then `Commit` every message. Return the message count.

```bash
cd backend && go test ./internal/ledger/ -count=1 && \
  git add internal/ledger/worker.go internal/ledger/worker_test.go && \
  git commit -m "feat: batch-write consumed events before committing offsets"
```

Expected: PASS, then one commit.

**Checkpoint 3: a failed write commits nothing**

- [ ] **Step 1: Write the failing test, then run it**

Spec: same fakes, but the fake `Writer` returns
`(0, errors.New("postgres is down"))`. Assert:

- `Once` returns a non-nil error whose message contains `postgres is down`.
- The fake `Consumer` recorded **no** `Commit` call at all.

Rationale for the test comment: this is the at-least-once guarantee. An
offset committed against a write the database rejected would lose those
wagers permanently — the same crash window the outbox exists to close,
reopened one hop later.

Run: `cd backend && go test ./internal/ledger/ -run TestWorkerOnceDoesNotCommitOnWriteFailure -count=1`
Expected: FAIL if Checkpoint 2 committed unconditionally. If Checkpoint 2 already returned early on the write error, fold and record as in Task 3 Checkpoint 3.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: return the wrapped write error immediately, before any `Commit`.

```bash
cd backend && go test ./internal/ledger/ -count=1 && \
  git add internal/ledger/worker.go internal/ledger/worker_test.go && \
  git commit -m "feat: leave offsets uncommitted when the ledger write fails"
```

Expected: PASS, then one commit.

**Checkpoint 4: an undecodable message halts the worker rather than being skipped**

- [ ] **Step 1: Write the failing test, then run it**

Spec: two cases, each with a fake `Consumer` yielding one bad message.

- A message on topic `"nonsense-topic"` → `Once` returns an error satisfying `errors.Is(err, events.ErrUnknownEventType)`.
- A message on `TopicWagersPlaced` whose value is `{"room_id":"r","round_id":"rd","user_id":"u","idempotency_key":"k","outcome":0,"amount":0,"balance":10}` → `Once` returns an error satisfying `errors.Is(err, events.ErrInvalidEvent)`.

In both cases assert the fake `Writer` was **never** called and the fake
`Consumer` recorded **no** `Commit`.

Rationale for the test comment: `relay` halts on an undecodable outbox entry
for the same reason — skipping one drops a money movement silently, and a
loud stop is recoverable while a silent gap is not.

Run: `cd backend && go test ./internal/ledger/ -run TestWorkerOnceHaltsOnUndecodable -count=1`
Expected: FAIL — the mapping error is currently unhandled or swallowed, depending on Checkpoint 2's implementation.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: return the wrapped decode/map error immediately, naming the topic
and partition/offset of the offending message so an operator can find it:
`fmt.Errorf("ledger: decoding message at %s/%d offset %d: %w", msg.Topic, msg.Partition, msg.Offset, err)`.

```bash
cd backend && go test ./internal/ledger/ -count=1 && \
  git add internal/ledger/worker.go internal/ledger/worker_test.go && \
  git commit -m "feat: halt the worker on an undecodable message"
```

Expected: PASS, then one commit.

**Checkpoint 5: Run loops until cancellation and returns nil**

- [ ] **Step 1: Write the failing test, then run it**

Spec: a fake `Consumer` that yields a message on every `Fetch` and a fake
`Writer` that counts batches. Start `Run` in a goroutine with a cancellable
context, wait until the writer has recorded at least two batches, cancel, and
assert `Run` returns `nil` within 2 seconds (select on a done channel with a
`time.After(2 * time.Second)` failure arm).

Then a second case: the fake `Writer` fails on its first call, and `Run`
returns that error rather than nil.

Run: `cd backend && go test ./internal/ledger/ -run TestWorkerRun -count=1`
Expected: FAIL — `undefined: (*Worker).Run` (compile error).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Run` loops `Once` until `ctx.Err() != nil`, returning `nil` on
cancellation — a clean shutdown, not an error — and returning any other error
immediately. Check `ctx.Err()` before returning an error from `Once` so a
cancellation racing an in-flight fetch reports clean, exactly as
`relay.Run` does.

```bash
cd backend && go test ./internal/ledger/ -count=1 && \
  git add internal/ledger/worker.go internal/ledger/worker_test.go && \
  git commit -m "feat: run the ledger worker until context cancellation"
```

Expected: PASS, then one commit.

**Task 4 boundary — full suite**

```bash
cd backend && go vet ./... && gofmt -l . && go test ./... -race -cover -p 1 -count=1 -timeout 300s
```

Expected: PASS.

---

## Task 5: Configuration and the `ledger-worker` binary

**Files:**
- Modify: `backend/internal/config/config.go`
- Create: `backend/cmd/ledger-worker/main.go`
- Modify: `Makefile`
- Test: `backend/internal/config/ledger_config_test.go`

**Interfaces:**
- Consumes: `config.LookupFunc`, `config.validLogLevels`, `config.validEnvs`;
  `events.NewKafkaConsumer`, `events.NewKafkaProducer`, `events.Partitions`,
  `events.GroupLedgerWriter`, `events.TopicWagersPlaced`,
  `events.TopicRoundsSettled`; `ledger.New`, `ledger.NewWorker`.
- Produces:
  ```go
  type LedgerConfig struct {
      PostgresDSN   string   // REQUIRED — no default
      KafkaBrokers  []string // default []string{"localhost:9092"}
      ConsumerGroup string   // default events.GroupLedgerWriter
      LogLevel      string   // default "info"
      Env           string   // default "development"
  }

  func LoadLedger(lookup LookupFunc) (LedgerConfig, error)
  ```

**Checkpoint 1: LoadLedger requires a DSN and defaults the rest**

- [ ] **Step 1: Write the failing test, then run it**

Spec: table-driven over an in-memory `LookupFunc`, matching
`migrate_config_test.go` / `relay_config_test.go`'s style.

| env | expectation |
|---|---|
| `{}` | error containing `POSTGRES_DSN is required` |
| `{POSTGRES_DSN: ""}` | same error |
| `{POSTGRES_DSN: "postgres://x"}` | `PostgresDSN: "postgres://x"`, `KafkaBrokers: ["localhost:9092"]`, `ConsumerGroup: "ledger-writer"`, `LogLevel: "info"`, `Env: "development"` |
| `+ KAFKA_BROKERS: "a:9092,b:9092"` | `KafkaBrokers: ["a:9092","b:9092"]` |
| `+ KAFKA_BROKERS: ""` | error containing `KAFKA_BROKERS must not be empty` |
| `+ KAFKA_BROKERS: "a:9092,"` | error containing `contains an empty element` |
| `+ LEDGER_GROUP: "alt"` | `ConsumerGroup: "alt"` |
| `+ LEDGER_GROUP: ""` | error containing `LEDGER_GROUP must not be empty` |
| `+ LOG_LEVEL: "debug"` | `LogLevel: "debug"` |
| `+ LOG_LEVEL: "shout"` | error containing `not one of debug\|info\|warn\|error` |
| `+ ENV: "production"` | `Env: "production"` |
| `+ ENV: "staging"` | error containing `not one of development\|production\|test` |

Also assert `LoadLedger` does **not** require `JWT_SECRET`: the third row's
environment contains no JWT key and must still succeed. The ledger worker
neither issues nor verifies a token, and handing a non-auth binary a
credential it has no use for is how credentials leak.

Run: `cd backend && go test ./internal/config/ -run TestLoadLedger -count=1`
Expected: FAIL — `undefined: LoadLedger` (compile error).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add `LedgerConfig` and `LoadLedger` to
`backend/internal/config/config.go`, reusing the existing `validLogLevels`
and `validEnvs` maps and mirroring `LoadRelay`'s broker-parsing branch
verbatim rather than writing a second parser. Document on the struct why
`JWT_SECRET` is absent, matching `RelayConfig`'s comment.

```bash
cd backend && go test ./internal/config/ -count=1 && \
  git add internal/config/config.go internal/config/ledger_config_test.go && \
  git commit -m "feat: add the ledger worker's configuration surface"
```

Expected: PASS, then one commit.

**Checkpoint 2: the ledger-worker binary wires the pieces with graceful shutdown**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `cmd/*` carries no tests by this project's standing interpretation
(thin wiring, 0% expected), so this checkpoint's RED is the build itself.

Run: `cd backend && go build ./... && go vet ./...`
Expected: FAIL — `backend/cmd/ledger-worker` does not exist, so `go build ./...` reports no such package once the `Makefile` target references it. Confirm the failure by running `cd backend && go run ./cmd/ledger-worker` and observing `package ./cmd/ledger-worker is not in std`-style output before writing the file.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `main` delegates to `run() error`, mirroring `cmd/relay/main.go`:

1. `config.LoadLedger(os.LookupEnv)`; on error, `slog.Error` and `os.Exit(1)`.
2. Build a JSON `slog` handler at `cfg.LogLevel` via the same `newLogger` helper shape `cmd/relay` uses, and `slog.SetDefault`.
3. `signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)`.
4. `events.NewKafkaProducer(cfg.KafkaBrokers)` **solely** to call `EnsureTopics(ctx, events.Partitions)`, then `Close()` it — the worker may start before any relay has ever run, and a `Reader` over a topic that does not exist would otherwise spin. `EnsureTopics` is documented idempotent, so calling it from two binaries is a no-op for the second.
5. `pgxpool.New(ctx, cfg.PostgresDSN)`, then `pool.Ping(ctx)` so a bad DSN fails at startup rather than at the first batch; `defer pool.Close()`.
6. `events.NewKafkaConsumer(cfg.KafkaBrokers, cfg.ConsumerGroup, []string{events.TopicWagersPlaced, events.TopicRoundsSettled}, true)`; `defer` its `Close()` with the error logged, matching `cmd/relay`'s deferred-close style.
7. `ledger.NewWorker(consumer, ledger.New(pool))`, log `"ledger worker starting"` with the group name, and `return worker.Run(ctx)`.

Do **not** run migrations from this binary — `cmd/migrate` owns that, and a
worker that silently migrates on start is a worker that can migrate a
production database from a stale image.

Add to the `Makefile`, next to `migrate`:

```make
# Runs the Kafka → PostgreSQL ledger writer. Requires POSTGRES_DSN — e.g.
# postgres://callit:callit@localhost:5432/callit?sslmode=disable. Apply the
# schema with `make migrate` first; this binary never migrates.
ledger-worker:
	cd backend && go run ./cmd/ledger-worker
```

and add `ledger-worker` to the `.PHONY` line.

```bash
cd backend && go build ./... && go vet ./... && gofmt -l . && go test ./... -race -cover -p 1 -count=1 -timeout 300s && \
  git add cmd/ledger-worker/main.go ../Makefile && \
  git commit -m "feat: add the ledger-worker binary with graceful shutdown"
```

Expected: PASS, then one commit.

---

## Task 6: Reconciliation and phase close-out

**Files:**
- Modify: `backend/internal/ledger/testmain_test.go` (add Redis and Kafka probes)
- Create: `backend/internal/ledger/reconcile_test.go`
- Modify: `docs/plans/2026-08-21-implementation-plan.md`
- Modify: `CLAUDE.md`
- Modify: `docs/project-history.md`
- Create: `journal/<date>_ansh_phase-5b-ledger-execution.md`

**Interfaces:**
- Consumes: everything built in Tasks 1–5, plus `redisstore.New`,
  `redisstore.Store.{CreateRoom,JoinRoom,CreateRound,PlaceWager,LockRound,SettleRound,Balance,OpeningStake}`,
  `redisstore.OutboxStream`, `redisstore.OutboxGroup`, `relay.New`,
  `events.NewKafkaProducer`, `domain.StartingBalance`.
- Produces: nothing consumed by a later task.

> **These are verification checkpoints, not RED→GREEN cycles.** Every
> component they exercise was built and unit-tested in Tasks 1–5, so each
> test is expected to **PASS the first time it runs**. This is declared here
> deliberately so the executor does not treat that PASS as an
> instruction/reality mismatch under `executing-plans`' stop-on-mismatch
> rule, and does not manufacture a failure by breaking a correct test. If one
> of these FAILS, that is a real defect in Tasks 1–5 — fix the
> implementation, never the assertion. This is the plan's one structural
> exception; every other checkpoint is a genuine RED→GREEN cycle.

**Shared fixture** (write once as a helper in `reconcile_test.go`, used by
Checkpoints 1–3):

- `TestMain` extension: after the existing PostgreSQL setup, connect to
  `REDIS_ADDR` (default `localhost:6379`) on **DB 15** and `FLUSHDB`; dial
  `KAFKA_BROKERS` (default `localhost:9092`) as a reachability probe. Both
  `log.Fatalf` on failure — fail, never skip.
- All IDs from `uuid.NewString()`. Never `testID()` — PostgreSQL's `uuid`
  columns reject it (see File Structure).
- `store, _ := redisstore.New(addr, 15)`.
- `store.CreateRoom(ctx, roomID, code, hostID, 100)` — buy-in 100.
- 8 players. For each: `opening, _ := store.JoinRoom(ctx, roomID, userID, domain.StartingBalance)`. **Do not assume `opening`'s value** — read it back with `store.OpeningStake` at assert time; the effective-stake rule is `internal/domain`'s, not this test's.
- `store.CreateRound(ctx, roundID, roomID, "q?", []string{"a", "b"}, time.Now().Add(30*time.Second))` — a lock 30 seconds out, so nothing locks mid-test.
- Each player places 5 wagers of 10 tokens on outcome `i % 2`, each with a fresh `uuid.NewString()` idempotency key. Total staked 400.
- `store.LockRound(ctx, roundID)`, then `settlement, _ := store.SettleRound(ctx, roundID, 0, uuid.NewString())`.
- Expected transactions for this room: `8*5 + 1 = 41`.

**Draining helper** (also written once):

```
drain(t, ctx, relay, worker, repo, roomID, wantTxns):
  1. Loop relay.Once(ctx, 100, 200*time.Millisecond) until it returns 0
     twice consecutively.
  2. Loop worker.Once(ctx) until repo.TransactionCount(ctx, roomID) ==
     wantTxns.
  3. Bound the whole thing with a 60-second deadline; t.Fatalf on expiry,
     reporting the counts actually reached.
```

The relay is `relay.New(client, redisstore.OutboxStream, redisstore.OutboxGroup, uuid.NewString(), events.NewKafkaProducer(brokers))` after `EnsureGroup` and `EnsureTopics`. The worker uses a **unique consumer group per run** (`"ledger-recon-" + uuid.NewString()`) reading from `FirstOffset`.

That group choice matters and is deliberate: the local Kafka topics retain
messages from earlier runs, so the worker will also consume and ledger those.
That is harmless — they dedupe on `idempotency_key` and belong to other
`room_id`s — because **every assertion below is scoped to this run's own
`roomID`**. Do not assert on unscoped totals; they are polluted by design.

**Checkpoint 1: sequential traffic reconciles per user**

- [ ] **Step 1: Write the test, then run it**

Spec: build the fixture placing all 40 wagers sequentially, drain, then for
each of the 8 players assert:

```
store.Balance(ctx, roomID, userID) − store.OpeningStake(ctx, roomID, userID)
    == WalletBalancesForRoom(ctx, roomID)[userID]
```

Every player must be present in the map — a missing key is a lost wager, not
a zero balance, so assert presence explicitly rather than relying on Go's
zero value.

Document D2 in the test's comment: the ledger records outbox movements only,
so a `user_wallet` balance is a net session delta and the opening stake is
the constant that separates it from the absolute Redis wallet.

Run: `cd backend && go test ./internal/ledger/ -run TestReconcileSequential -count=1 -timeout 300s`
Expected: **PASS** (see the note above this task).

- [ ] **Step 2: Verify and commit**

```bash
cd backend && go test ./internal/ledger/ -run TestReconcile -count=1 -timeout 300s && \
  git add internal/ledger/reconcile_test.go internal/ledger/testmain_test.go && \
  git commit -m "test: reconcile Redis wallets against the ledger for a settled round"
```

Expected: PASS, then one commit.

**Checkpoint 2: concurrent traffic reconciles exactly**

- [ ] **Step 1: Write the test, then run it**

Spec: the same fixture, but the 40 wagers are placed by 8 goroutines running
concurrently (one per player, 5 wagers each), joined with a `sync.WaitGroup`
before locking the round. Assert the same per-player identity as Checkpoint
1, plus `TransactionCount(ctx, roomID) == 41` exactly — not "at least".

This is the double-spend proof: `place_wager.lua` is atomic, the outbox
`XADD` is inside that same atomic unit, and `idempotency_key` is unique per
wager, so 40 concurrent wagers must produce exactly 40 wager transactions and
exactly the balances Redis holds. A count above 41 means a duplicate slipped
the unique constraint; below means one was lost.

Must run under `-race`.

Run: `cd backend && go test ./internal/ledger/ -run TestReconcileConcurrent -race -count=1 -timeout 300s`
Expected: **PASS**.

- [ ] **Step 2: Verify and commit**

```bash
cd backend && go test ./internal/ledger/ -run TestReconcile -race -count=1 -timeout 300s && \
  git add internal/ledger/reconcile_test.go && \
  git commit -m "test: reconcile the ledger under concurrent wager placement"
```

Expected: PASS, then one commit.

**Checkpoint 3: the pool returns to zero and dust is accounted for**

- [ ] **Step 1: Write the test, then run it**

Spec: on the concurrent fixture after draining, assert:

- `repo.PoolBalance(ctx, roomID) == 0` — every token that entered the round's pool left it.
- `repo.DustForRoom(ctx, roomID) == int64(settlement.Dust)`, using the `domain.Settlement` returned by `store.SettleRound`.
- Token conservation over this room: `Σ WalletBalancesForRoom(ctx, roomID) + PoolBalance + DustForRoom == 0`.

Document why the pool assertion has teeth: a lost `wager_placed` event leaves
the pool short by that amount, and a duplicated one leaves it long — neither
shows up in a per-user wallet comparison if the corresponding settlement
credit is also affected, but both show up here.

Run: `cd backend && go test ./internal/ledger/ -run TestReconcileConservation -count=1 -timeout 300s`
Expected: **PASS**.

- [ ] **Step 2: Verify and commit**

```bash
cd backend && go test ./internal/ledger/ -run TestReconcile -count=1 -timeout 300s && \
  git add internal/ledger/reconcile_test.go && \
  git commit -m "test: assert the round pool empties and dust is conserved"
```

Expected: PASS, then one commit.

**Checkpoint 4: a full replay changes nothing**

- [ ] **Step 1: Write the test, then run it**

Spec: after the concurrent fixture has been drained and asserted, record
`TransactionCount(ctx, roomID)` and `WalletBalancesForRoom(ctx, roomID)`.
Then build a **second** worker on a **fresh** consumer group (again from
`FirstOffset`, so it re-reads every message from the beginning of both
topics) sharing the same `Repo`, and drain it until it reports two
consecutive zero-message `Once` calls. Assert:

- `TransactionCount(ctx, roomID)` is unchanged.
- `WalletBalancesForRoom(ctx, roomID)` is `reflect.DeepEqual` to the recorded map.
- `PoolBalance(ctx, roomID)` is still `0`.

This is the at-least-once guarantee end to end: Kafka may redeliver any
message any number of times, and the `idempotency_key` UNIQUE constraint is
the single mechanism that absorbs it.

Run: `cd backend && go test ./internal/ledger/ -run TestReconcileReplay -count=1 -timeout 300s`
Expected: **PASS**.

- [ ] **Step 2: Verify and commit**

```bash
cd backend && go test ./internal/ledger/ -race -count=1 -timeout 300s && \
  git add internal/ledger/reconcile_test.go && \
  git commit -m "test: prove a full topic replay leaves the ledger unchanged"
```

Expected: PASS, then one commit.

**Checkpoint 5: security review, coverage, and documentation close-out**

- [ ] **Step 1: Run the reviews and gather the figures**

`CLAUDE.md` requires the `security-reviewer` agent before closing any phase
touching money movement. This phase does. Scope it explicitly to:

1. `internal/events/message.go` — decoding attacker-influenceable JSON: unbounded `Payouts` slices, integer overflow on `Amount`/`Total`, and whether `DisallowUnknownFields` plus validation actually closes the field-substitution path.
2. `internal/ledger/repo.go` — SQL construction (every query must be parameterised; no identifier interpolation), and whether the PostgreSQL DSN can reach a log line or an error string.
3. `cmd/ledger-worker/main.go` — the credential surface: confirm no `JWT_SECRET`, and that a bad DSN's error does not print the password.
4. The trust boundary itself — a consumer that writes money rows from a Kafka payload it did not authenticate. Record what is and is not assumed about broker access.

Then collect coverage:

```bash
cd backend && go test ./... -race -coverpkg=./... -p 1 -count=1 -timeout 600s
```

Judge `internal/ledger` and `internal/events` from the `-coverpkg` figure,
never the per-package one. Both must clear 80%. If `internal/ledger` falls
short, diagnose before adding tests: a genuinely unreachable-without-fault-
injection branch (a `pgx` connection failure mid-batch) is the same accepted
defensive-branch gap `redisstore` and `internal/migrate` already carry —
record it rather than padding. A reachable untested branch is a real gap.

- [ ] **Step 2: Write the documentation, then verify-and-commit**

Contract — five documents, all in one commit:

1. `docs/plans/2026-08-21-implementation-plan.md`:
   - **Amendment F1** next to §7's topology table: the Kafka wire format is pinned by explicit JSON tags on `events.WagerPlaced`/`RoundSettled`, with the `user` → `user_id` divergence from the Redis outbox noted (D7).
   - **Amendment F2** next to §6's schema block: migration `0002` adds the three partial unique indexes and two `ledger_entries` indexes, with the trigger's per-row `transaction_id` lookup as the reason the first of those is a correctness-at-scale requirement rather than a nicety (Task 3 CP1).
   - **Amendment F3** next to §6's flagship-correctness-test paragraph: the reconciliation identity is `redis_wallet − opening_stake == ledger_balance`, not `redis_wallet == ledger_balance`, because the opening session stake never enters the outbox (D2). Name the `system_mint`-on-join alternative and mark it a Phase 7 candidate. Note that §6's "after a k6 run" framing is satisfied here by an in-process concurrent Go load generator, since k6 arrives in Phase 7.
   - Mark the §9 Phase 5b row complete.
2. `CLAUDE.md`:
   - Repository Layout: `internal/ledger/` is "PostgreSQL double-entry repository, the pure event→transaction mapping, and the Kafka consume loop that feeds it"; add `cmd/ledger-worker/`.
   - Add a line to the "separate binaries" note covering `ledger-worker` alongside `relay`.
   - Build & Test: the new `make ledger-worker` target.
   - Add D1's sign convention to Critical Invariants: credit is tokens in, debit is tokens out, balance is `Σ credits − Σ debits`, and every transaction satisfies `Σ debits == Σ credits` — enforced by the deferred trigger, not application code.
   - Add D2 to Critical Invariants: the ledger records outbox movements only, so a `user_wallet` balance is a net session delta; do not "fix" a reconciliation by comparing it to the absolute Redis wallet.
3. `docs/project-history.md`: the Phase 5b security review findings and their dispositions, plus any accepted coverage gap with its reasoning.
4. A journal entry via the `journal` skill, covering: the checkpoints that collapsed because an earlier one already implemented them (Task 3 CPs 3–5 are the likely candidates — record which actually did), the declared verification framing of Task 6 and whether it held, and the phase's `tok/CP` if measured with `scripts/phase_compare.py` (bound both ends; include `subagents/*.jsonl`).
5. Anything Task 6's review found stale in `CLAUDE.md`.

```bash
cd backend && go vet ./... && gofmt -l . && go test ./... -race -cover -p 1 -count=1 -timeout 600s && \
  cd .. && git add CLAUDE.md docs/plans/2026-08-21-implementation-plan.md docs/project-history.md journal/ && \
  git commit -m "docs: record Phase 5b amendments and close out the phase"
```

Expected: PASS, then one commit. **The branch is now green and verified.**

---

## Where This Plan Stops

At "branch is green and verified." Merging `phase-5b-ledger` into `dev` is
**not** part of this plan — `executing-plans` Step 3 hands off to
`finishing-a-development-branch`, which verifies the tests and presents the
merge/keep menu. That decision stays with the user.

---

## Self-Review

**Spec coverage.** Parent plan §9's Phase 5b row names four deliverables:

| Deliverable | Task |
|---|---|
| `cmd/ledger-worker` consumer | Task 4 (loop), Task 5 (binary) |
| `internal/ledger` repository | Task 3 |
| idempotent replay on the `idempotency_key` unique constraint | Task 3 CP4 (unit), Task 6 CP4 (end to end) |
| Redis↔PostgreSQL reconciliation test | Task 6 CPs 1–3 |

§6's schema is applied by Task 3 CP1 (`0002`) and exercised throughout; §6's
deferred balance trigger is proven load-bearing by Task 3 CP5. §7's topology
is consumed by Task 4 CP1 and pinned by Task 1 CP1. Two things §6 asks for
are deliberately reinterpreted rather than dropped, both recorded as
amendments: the reconciliation identity carries the opening-stake term (F3),
and the load is generated in-process rather than by k6, which is Phase 7's
tooling (F3).

Nothing in §6, §7, or the 5b row is unclaimed.

**Placeholder scan.** No "TBD", no "handle edge cases", no "similar to Task
N". Every checkpoint states exact inputs and exact expected outputs or
sentinel errors. The two places that could read as vague are deliberate and
bounded: Task 6's fixture says not to assume `JoinRoom`'s returned effective
stake (the assertion reads it back instead of hardcoding a number), and Task
3 CPs 3–5 name in advance the possibility of a PASS-on-write and say exactly
what to do about it rather than leaving the executor to improvise.

**Type consistency.** Checked across tasks: `DecodeMessage(topic string,
value []byte) (Event, error)` is produced in Task 1 and consumed in Task 4
CP2 with that signature. `TransactionFor(ev events.Event) (Transaction,
error)` is produced in Task 2 and consumed in Task 4 CP2. `WriteBatch(ctx,
[]Transaction) (int, error)` is produced in Task 3 and is the shape of
`ledger.Writer` in Task 4. `AccountRef.ID()` is produced in Task 2 CP1 and
consumed by Task 3 CP2's account insert. `events.GroupLedgerWriter` is
produced in Task 4 and consumed as `LoadLedger`'s default in Task 5 CP1.
`Worker.Once(ctx) (int, error)` and `Worker.Run(ctx) error` are produced in
Task 4 and consumed by Task 5 CP2 and Task 6's drain helper. `Direction` is
`Debit`/`Credit` everywhere, matching the schema's `CHECK (direction IN
('debit', 'credit'))`. `AccountKind` values match `accounts`' `CHECK`
verbatim.

One inconsistency found and fixed while reviewing: Task 4's fake-driven
checkpoints referred to a `ledger.Consumer` interface that Task 4's
Interfaces block had not declared. It is now declared there alongside
`ledger.Writer`, with the note that both are satisfied structurally so
`ledger` never imports `events`' Kafka code — the same seam `relay.Producer`
established in Phase 5a.
