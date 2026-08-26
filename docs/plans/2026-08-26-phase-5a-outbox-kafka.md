# Phase 5a — Outbox → Kafka + Ledger Schema Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every money movement durably readable outside Redis — events
relayed from the `wager-outbox` stream into Kafka, and a PostgreSQL ledger
schema that structurally refuses to hold an unbalanced transaction.

**Architecture:** Three separable pieces that meet at a wire format. The Lua
scripts already `XADD` into `wager-outbox` atomically with each balance
mutation; this phase enriches the settlement and refund payloads so a ledger
can be written from an event alone, defines `internal/events` as the typed
schema over those entries, and adds `cmd/relay` — a separate binary reading
the stream through a Redis consumer group and producing to Kafka, acking only
after the produce is confirmed (at-least-once). In parallel, `migrations/`
lands the double-entry schema from the parent plan §6, whose balance
invariant is enforced by a `DEFERRABLE INITIALLY DEFERRED` constraint trigger
inside PostgreSQL rather than by application code. **No consumer is built
here** — nothing reads the Kafka topics or writes a ledger row until Phase 5b.

**Tech Stack:** Go 1.22.10 · Redis 7.2 Streams (consumer groups) · Kafka 3.7
KRaft (`segmentio/kafka-go`) · PostgreSQL 16 (`jackc/pgx/v5`) ·
`golang-migrate/migrate/v4` as an embedded library.

**Spec:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md)
· parent plan [`docs/plans/2026-08-21-implementation-plan.md`](2026-08-21-implementation-plan.md)
§1 (outbox amendment), §4 (key schema), §5 (Lua contracts), §6 (ledger
schema), §7 (Kafka topology).

## Global Constraints

- **Go directive stays `go 1.22.10`.** Pin exactly: `jackc/pgx/v5@v5.7.4`,
  `segmentio/kafka-go@v0.4.48`, `golang-migrate/migrate/v4@v4.18.2`. Every
  newer release of all three declares `go >= 1.23` and `go get` will silently
  rewrite the module directive, breaking CI. Verified 2026-08-26 —
  `docs/project-history.md`. **Never run `go get -u`.** After any `go get`,
  confirm `grep '^go ' backend/go.mod` still reads `go 1.22.10`.
- **`go` may be off PATH in non-interactive shells.** Prefix any session that
  runs Go with `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin`.
- **All amounts are integer token units.** No floats in Redis, in an event
  payload, or in the ledger. When decoding JSON, decode into typed structs
  with `int64`/`domain.Tokens` fields — never `interface{}` or `map[string]any`,
  which turn every number into `float64` and silently lose precision above
  2^53.
- **`internal/domain` stays free of I/O.** Nothing in this phase adds an
  import to it.
- **The WebSocket server never writes PostgreSQL directly.** `cmd/relay` is a
  separate binary for exactly this reason; do not run it as a goroutine inside
  `cmd/api`.
- **`internal/redisstore/keys.go` is the only place a Redis key is constructed.**
  New keys (the consumer group's name is not a key, but the stream is) go
  through its builders.
- **Every event carries the `idempotency_key` already minted on the wager
  path.** Do not mint a second one in the relay — at-least-once delivery is
  made safe downstream by that key's `UNIQUE` constraint, and a relay-minted
  key would defeat it by making every redelivery look new.
- **80% coverage minimum**, TDD RED→GREEN, AAA structure, table-driven tests.
  `-p 1` is load-bearing on `go test ./...` and must not be dropped.
- **Commit format:** `type: description`. One checkpoint, one commit, chained
  behind the verifying test with `&&`.

---

## Amendments to the parent plan

These are resolved here, in this document, rather than left to execution.
Task 6 CP3 writes them back into the parent plan.

### Amendment E1 — settlement and refund outbox payloads carry per-user detail

**Problem.** The parent plan §5 specifies that each script emits "one outbox
event" but never fixed the payload. As built in Phase 2, `settle_round.lua`
emits `type, round_id, dust, winning_outcome, idempotency_key` and
`refund_round.lua` emits `type, round_id, total, idempotency_key`. Neither
carries per-user amounts, and neither carries `room_id`.

Both omissions are fatal to Phase 5b. A double-entry settlement transaction is
*debit `round_pool` by the total staked; credit each winner's `user_wallet` by
their payout; credit `system_dust` by the dust* — so without per-user payouts
there are no credit lines to write, and the ledger worker cannot recover them,
because the Redis wagers hash is the very state being settled and may be gone
by the time the event is consumed. `room_id` is separately required as the
Kafka partition key (§7), which is what buys per-room ordering.

**Resolution.** Both events gain `room_id`, `total`, and a `payouts` field
holding a JSON array of `{"user_id": string, "amount": int}`. The
`wager_placed` payload is already complete and does not change.

**Go authors the JSON, Lua echoes it.** `settle_round.lua` keeps its existing
alternating `userID, amount` ARGV tail for the `HINCRBY` credits and receives
the JSON as one additional ARGV that it writes into the `XADD` verbatim. The
apparent redundancy is deliberate and cannot drift: both are derived from the
same `settlement.Payouts` slice inside one function call. The alternative —
having Lua build the JSON with `cjson` from the ARGV it already walks — would
put wire-format knowledge inside a script that cannot be unit-tested, for no
gain.

### Amendment E2 — `refund_round.lua` takes amounts from Go, like settlement

**Problem.** E1 requires the refund event to carry per-user amounts, but
`refund_round.lua` derives them itself via `HGETALL` on the wagers hash
(parent plan §5: "refunding is the identity function on stakes — so this
script reads the wagers hash inside its own atomic unit"). To emit them it
would have to build JSON in Lua, which E1 rejects.

**Resolution.** Refund becomes symmetric with settlement. `Store.RefundRound`
reads stakes with the existing `ReadStakes`, aggregates them per user in Go,
and passes both the alternating ARGV and the JSON. The script drops its
`HGETALL` loop and its hand-rolled colon-scan field parser, becoming an apply-
only script like `settle_round.lua`. `KEYS[3]` (the wagers key) is no longer
needed.

This moves *toward* the standing invariant that money math is not duplicated
in Lua, and removes the last script that derives a balance movement from state
it reads itself. It supersedes the parent plan §5's `refund_round.lua`
paragraph.

**Safety — this is the part that matters.** Reading stakes in Go opens a
read-then-write window the old script did not have. Settlement is safe from
the identical window only because the round is already `locked` before Go
reads, and `place_wager.lua` rejects every wager on a locked round, so the
hash cannot grow in between. Refund gets the same guarantee the same way:
`RefundRound` must check `round.Status == domain.RoundLocked` **in Go, before
calling `ReadStakes`**, returning `ErrNotLocked` otherwise — mirroring what
`SettleRound` already does at `settle.go:69-76`. Without that check the
sequence *read stakes on an open round → round locks → script applies a stale
snapshot* would silently fail to refund any wager placed in the gap. The
script keeps its own status CAS regardless, since the Go read is not atomic
with it.

### Amendment E3 — Kafka topics are created explicitly, not auto-created

`docker-compose.yml` sets `KAFKA_AUTO_CREATE_TOPICS_ENABLE: "true"`, which
would create `wagers-placed` and `rounds-settled` on first produce with the
broker default of **one** partition. The parent plan §7 specifies **six**, and
partition count cannot be lowered later. `cmd/relay` therefore creates both
topics idempotently at startup with the specified partition count, rather than
relying on auto-creation.

---

## File Structure

**Created:**

| Path | Responsibility |
|---|---|
| `backend/migrations/0001_ledger_schema.up.sql` | Double-entry schema: three tables, the balance trigger, the uniqueness and positivity constraints |
| `backend/migrations/0001_ledger_schema.down.sql` | Exact reversal |
| `backend/migrations/embed.go` | `//go:embed *.sql` — mirrors `scripts/lua/embed.go` |
| `backend/internal/migrate/migrate.go` | `Up`/`Down` over the embedded FS; no CLI dependency |
| `backend/internal/migrate/testmain_test.go` | Creates and drops the scratch test database |
| `backend/internal/migrate/schema_test.go` | Schema shape, trigger, constraints |
| `backend/cmd/migrate/main.go` | Thin `main` — `make migrate` runs this |
| `backend/internal/events/event.go` | Typed event structs and the `Event` interface |
| `backend/internal/events/decode.go` | Redis stream entry → typed event |
| `backend/internal/events/kafka.go` | `Producer` interface + `KafkaProducer` |
| `backend/internal/events/*_test.go` | Codec tests (no infrastructure) and one Kafka integration test |
| `backend/cmd/relay/main.go` | Wiring only |
| `backend/internal/relay/relay.go` | The read → produce → ack loop |
| `backend/internal/relay/*_test.go` | Loop behavior against a fake producer |

**Modified:**

| Path | Change |
|---|---|
| `backend/scripts/lua/settle_round.lua` | Add `room_id`, `total`, `payouts` to the `XADD` (E1) |
| `backend/scripts/lua/refund_round.lua` | Apply-only; take amounts from Go (E2) |
| `backend/internal/redisstore/settle.go` | Build the payouts JSON; Go-side locked check on refund |
| `backend/internal/redisstore/keys.go` | `OutboxGroup` constant |
| `backend/internal/config/config.go` | `LoadRelay` and `LoadMigrate` |
| `backend/go.mod` / `go.sum` | Three pinned dependencies |
| `Makefile` | Real `migrate` target |
| `.github/workflows/ci.yml` | PostgreSQL and Kafka |
| `docs/plans/2026-08-21-implementation-plan.md` | Amendments E1–E3 |
| `CLAUDE.md` | Pinned-dependency list |

**Deliberately not created:** `internal/ledger`, `cmd/ledger-worker`. Those
are Phase 5b. A relay that also wrote ledger rows would collapse the split.

---

## Task 1: Ledger schema and migrations

No Kafka, no Redis. Pure PostgreSQL — the piece most independently verifiable,
which is why it goes first.

**Files:**
- Create: `backend/migrations/0001_ledger_schema.up.sql`, `backend/migrations/0001_ledger_schema.down.sql`, `backend/migrations/embed.go`, `backend/internal/migrate/migrate.go`, `backend/cmd/migrate/main.go`
- Test: `backend/internal/migrate/testmain_test.go`, `backend/internal/migrate/schema_test.go`
- Modify: `backend/go.mod`, `Makefile`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `package migrations` with `//go:embed *.sql` exposing `var FS embed.FS`
  - `func migrate.Up(ctx context.Context, dsn string) error`
  - `func migrate.Down(ctx context.Context, dsn string) error`
  - Both are idempotent: `Up` on an already-migrated database returns nil, not `ErrNoChange`.

**Test database convention.** Mirrors the Redis DB-15 rule: integration tests
here **fail rather than skip** when PostgreSQL is unreachable. `TestMain`
connects to the maintenance database using `POSTGRES_DSN` (default
`postgres://callit:callit@localhost:5432/callit?sslmode=disable`), issues
`DROP DATABASE IF EXISTS callit_test` then `CREATE DATABASE callit_test`, and
every test in the package runs against `callit_test`. Never the dev database.

**Editing `0001` in place is correct here, and only here.** Checkpoints 3–5
each add one enforced rule to the same file rather than adding `0002`, `0003`,
`0004`. This migration has never been applied outside a scratch database that
`TestMain` drops and recreates, so there is no deployed state to preserve. The
`database-migrations` skill's rule — never edit an applied migration — takes
effect the first time this runs against an environment anyone else uses, which
in this project is Phase 5b onward. Say so in a comment at the top of the file.

**Checkpoint 1: `Up` creates the three tables**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestUpCreatesLedgerTables`. After `migrate.Up(ctx, testDSN)`, query
`information_schema.tables` for `table_schema = 'public'`. Expect exactly the
tables `accounts`, `transactions`, `ledger_entries` present (a
`schema_migrations` bookkeeping table will also exist — assert the three are
present rather than asserting the total count). Then assert column presence per
parent plan §6 by querying `information_schema.columns`:
- `accounts`: `id`, `kind`, `user_id`, `room_id`, `created_at`
- `transactions`: `id`, `idempotency_key`, `kind`, `room_id`, `round_id`, `occurred_at`
- `ledger_entries`: `id`, `transaction_id`, `account_id`, `direction`, `amount`

Types: `id` columns `uuid`; `kind` and `direction` `text`; `amount` `bigint`
(integer token units — never `numeric`, never a float type); timestamps
`timestamptz`; `user_id`/`room_id`/`round_id` nullable, everything else `NOT
NULL`. `accounts.kind` is constrained to `user_wallet | room_escrow |
round_pool | system_mint | system_dust`; `ledger_entries.direction` to
`debit | credit`.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/migrate/ -run TestUpCreatesLedgerTables -count=1 -race`
Expected: FAIL — `package github.com/zorojuro12/call_it/backend/internal/migrate is not in std` (the package does not exist yet).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add the three pinned dependencies. `migrations/embed.go` declares
`package migrations` and `//go:embed *.sql` into `var FS embed.FS`.
`migrate.Up` builds a `golang-migrate` instance from an `iofs` source over
`migrations.FS` and a `postgres` database driver over the DSN, calls `Up`, and
translates `migrate.ErrNoChange` into `nil` so re-running is a no-op rather
than an error. `0001_ledger_schema.up.sql` creates the three tables with the
columns and constraints above; `.down.sql` drops them in reverse dependency
order.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/migrate/ -run TestUpCreatesLedgerTables -count=1 -race && \
  grep -q '^go 1.22.10$' go.mod && \
  git add go.mod go.sum migrations/ internal/migrate/ && \
  git commit -m "feat: add the double-entry ledger schema and an embedded migration runner"
```

Expected: PASS, then one commit. The `grep` guards the Go directive — if it
fails, a dependency rewrote it and the commit must not land.

**Checkpoint 2: `Down` reverses the migration**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestDownRemovesLedgerTables`. Arrange: `Up`. Act: `Down`. Assert: none
of `accounts`, `transactions`, `ledger_entries` appear in
`information_schema.tables`. Then assert `Up` succeeds again afterward, proving
the down migration leaves a re-migratable database rather than a wedged one.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/migrate/ -run TestDownRemovesLedgerTables -count=1 -race`
Expected: FAIL — `migrate.Down` is undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `migrate.Down(ctx, dsn) error` mirrors `Up`, calling the library's
`Down`, translating `ErrNoChange` to `nil`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/migrate/ -count=1 -race && \
  git add internal/migrate/ migrations/ && \
  git commit -m "feat: support rolling the ledger schema back"
```

Expected: PASS, then one commit.

**Checkpoint 3: the deferred trigger rejects an unbalanced transaction at COMMIT**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestUnbalancedTransactionRejectedAtCommit`. Arrange: `Up`; insert two
`accounts` rows and one `transactions` row. Act: `BEGIN`; insert a single
`ledger_entries` row (`direction = 'debit'`, `amount = 100`) — that INSERT must
**succeed**, proving the check is deferred and not immediate — then `COMMIT`.
Assert: the `COMMIT` returns an error whose message contains
`transaction is not balanced`, and that after the failure `ledger_entries` holds
no rows for that transaction.

Second case in the same test, table-driven: a *balanced* pair (`debit` 100 and
`credit` 100 against the same `transaction_id`) inserted and committed in one
transaction succeeds, and both rows are readable afterward.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/migrate/ -run TestUnbalancedTransactionRejectedAtCommit -count=1 -race`
Expected: FAIL — the single-sided COMMIT succeeds, because no trigger exists yet.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add to `0001_ledger_schema.up.sql` a function and a constraint
trigger. The trigger must be `CONSTRAINT ... DEFERRABLE INITIALLY DEFERRED`, so
it fires at commit rather than per statement — an immediate trigger would
reject the first leg of every legitimate two-leg entry. Pin the exact shape:

```sql
CREATE FUNCTION assert_transaction_balanced() RETURNS trigger AS $$
DECLARE
  net bigint;
BEGIN
  SELECT COALESCE(SUM(CASE WHEN direction = 'debit' THEN amount ELSE -amount END), 0)
    INTO net
    FROM ledger_entries
   WHERE transaction_id = NEW.transaction_id;
  IF net <> 0 THEN
    RAISE EXCEPTION 'transaction is not balanced: % has net %', NEW.transaction_id, net;
  END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER ledger_entries_balanced
  AFTER INSERT ON ledger_entries
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION assert_transaction_balanced();
```

The `.down.sql` drops the trigger and function.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/migrate/ -count=1 -race && \
  git add migrations/ internal/migrate/ && \
  git commit -m "feat: enforce double-entry balance with a deferred constraint trigger"
```

Expected: PASS, then one commit.

**Checkpoint 4: `idempotency_key` is unique**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestDuplicateIdempotencyKeyRejected`. Arrange: `Up`. Act: insert two
`transactions` rows carrying the same `idempotency_key` and different `id`s.
Assert: the second insert fails with a `unique_violation` (SQLSTATE `23505`),
and exactly one row remains. This is the constraint that makes at-least-once
Kafka delivery safe in 5b — a replayed event violates it and is skipped.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/migrate/ -run TestDuplicateIdempotencyKeyRejected -count=1 -race`
Expected: FAIL — both inserts succeed; no unique constraint exists yet.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add `UNIQUE` to `transactions.idempotency_key` in
`0001_ledger_schema.up.sql`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/migrate/ -count=1 -race && \
  git add migrations/ internal/migrate/ && \
  git commit -m "feat: make transactions.idempotency_key unique"
```

Expected: PASS, then one commit.

**Checkpoint 5: entry amounts must be strictly positive**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestNonPositiveAmountRejected`, table-driven over `amount` values `0`
and `-1`. Each insert (inside its own transaction, with an account and
transaction row already present) must fail with a `check_violation` (SQLSTATE
`23514`). Direction is carried by the `direction` column, so a negative amount
would be a second, contradictory way to express a credit.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/migrate/ -run TestNonPositiveAmountRejected -count=1 -race`
Expected: FAIL — both inserts succeed; no CHECK constraint exists yet.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add `CHECK (amount > 0)` to `ledger_entries.amount`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/migrate/ -count=1 -race && \
  git add migrations/ internal/migrate/ && \
  git commit -m "feat: constrain ledger entry amounts to be strictly positive"
```

Expected: PASS, then one commit.

**Checkpoint 6: `cmd/migrate` and `make migrate`**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestLoadMigrateRequiresDSN` in `internal/config`. `config.LoadMigrate`
takes a `config.LookupFunc` and returns `MigrateConfig{PostgresDSN, LogLevel}`.
Table-driven: `POSTGRES_DSN` unset → error containing `POSTGRES_DSN is
required`; set to empty string → the same error; set to a non-empty value →
that value in the struct, no error. It must **not** require `JWT_SECRET` — a
migration runner has no business demanding a signing key.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/config/ -run TestLoadMigrateRequiresDSN -count=1 -race`
Expected: FAIL — `config.LoadMigrate` is undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add `MigrateConfig` and `LoadMigrate` to `internal/config`, following
the existing `Load`'s fail-fast style. `cmd/migrate/main.go` calls
`LoadMigrate(os.LookupEnv)`, then `migrate.Up` unless `argv[1] == "down"`, in
which case `migrate.Down`; it logs the outcome via `slog` and exits non-zero on
error. Replace the `Makefile`'s stub `migrate` target with one that runs
`cd backend && go run ./cmd/migrate`, keeping the default DSN documented in the
target's comment.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/config/ ./internal/migrate/ -count=1 -race && go build ./... && \
  git add internal/config/ cmd/migrate/ ../Makefile && \
  git commit -m "feat: add a migrate command and wire the make target"
```

Expected: PASS, then one commit.

**Task 1 boundary — full suite:**

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  docker compose up -d redis postgres && cd backend && \
  go test ./... -race -cover -p 1 -count=1
```

---

## Task 2: Enrich the outbox payloads (Amendments E1, E2)

**Files:**
- Modify: `backend/scripts/lua/settle_round.lua`, `backend/scripts/lua/refund_round.lua`, `backend/internal/redisstore/settle.go`
- Test: `backend/internal/redisstore/settle_test.go`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces — the outbox wire format every later task decodes. Field values are
  Redis stream strings throughout:

  | Event `type` | Fields |
  |---|---|
  | `wager_placed` | `user`, `outcome`, `amount`, `balance`, `idempotency_key`, `room_id`, `round_id` *(unchanged)* |
  | `round_settled` | `round_id`, `room_id`, `dust`, `total`, `winning_outcome`, `payouts`, `idempotency_key` |
  | `round_refunded` | `round_id`, `room_id`, `dust`, `total`, `winning_outcome` (empty), `payouts`, `idempotency_key` |

  `payouts` is a JSON array of objects with exactly the keys `user_id`
  (string) and `amount` (integer): `[{"user_id":"u1","amount":250}]`. An
  empty settlement emits `[]`, never an empty string and never `null`.
  `total` is the sum of all stakes on the round. `Σ payout amounts + dust ==
  total` holds for both event types.

- Also produces: `Store.RefundRound` gains the error `ErrNotLocked` on a
  non-locked round (the sentinel already exists in the package and is already
  returned by `SettleRound`).

**Checkpoint 1: the settlement event carries `room_id`, `total`, and `payouts`**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestSettleEventCarriesPayoutDetail`. Arrange: create a room and a round
with 2 outcomes; two users wager — `userA` 100 on outcome 0, `userB` 300 on
outcome 1; lock the round. Act: `SettleRound(ctx, roundID, 1, idemKey)`. Assert
by `XRANGE` over the store's outbox stream that exactly one entry was added and
that its fields are:
- `type` = `round_settled`
- `round_id` = the round's ID, `room_id` = the room's ID
- `total` = `400`
- `winning_outcome` = `1`
- `dust` = the settlement's dust as a decimal string
- `payouts` parses as JSON into `[]struct{UserID string; Amount int64}` with
  one element, `{userB, 400}`

Then assert the conservation invariant directly on the event: the sum of the
decoded payout amounts plus the parsed `dust` equals the parsed `total`.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/redisstore/ -run TestSettleEventCarriesPayoutDetail -count=1 -race`
Expected: FAIL — the entry has no `room_id`, `total`, or `payouts` field.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: in `Store.SettleRound`, marshal `settlement.Payouts` into the JSON
shape above using a dedicated unexported struct with `json` tags (not
`domain.Payout` directly — the domain type must not grow wire-format tags).
Compute `total` by summing the stakes already read for `domain.Settle`. Pass
`roomID`, `total`, and the JSON as new ARGV *before* the existing alternating
payout tail, so the tail stays open-ended; update `settle_round.lua`'s ARGV
index comments and its `while ARGV[i] ~= nil` start index to match, and add the
three fields to its `XADD`. Marshal an empty slice as `[]`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/redisstore/ -count=1 -race && \
  git add internal/redisstore/ scripts/lua/settle_round.lua && \
  git commit -m "feat: carry room, total, and per-user payouts on the settlement event"
```

Expected: PASS, then one commit.

**Checkpoint 2: `RefundRound` refuses a round that is not locked**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestRefundRoundRequiresLockedStatus`, table-driven over the non-locked
statuses. Arrange: a round left `open`; a second round already `resolved`. Act:
`RefundRound` on each. Assert: the `open` round returns an error satisfying
`errors.Is(err, ErrNotLocked)`; the `resolved` round returns
`errors.Is(err, ErrAlreadySettled)`. In both cases assert no outbox entry was
added and no wallet balance moved.

This is the guard E2's safety argument rests on, and it must exist **before**
the refund path starts reading stakes in Go.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/redisstore/ -run TestRefundRoundRequiresLockedStatus -count=1 -race`
Expected: FAIL — the `open` case currently reaches the script, which returns
`NOT_LOCKED`, so the mapped error is produced by the script rather than by the
Go guard. Confirm the failure is the *absence of the Go-side guard* by
asserting, in this test, that the error is returned without the script running
— assert `XLEN` of the outbox is unchanged **and** that the round's status
field was not read-modified. If the existing script mapping already produces
`ErrNotLocked` for the `open` case, that single assertion cannot RED: in that
event, keep only the `resolved` case here and move the guard's proof to
Checkpoint 3, where a stale-snapshot test can observe it.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: in `Store.RefundRound`, after the existing `s.Round` read, switch on
`round.Status` exactly as `SettleRound` does at `settle.go:69-76` — `resolved`
or `refunded` → `ErrAlreadySettled`; `locked` → proceed; anything else →
`ErrNotLocked`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/redisstore/ -count=1 -race && \
  git add internal/redisstore/settle.go internal/redisstore/settle_test.go && \
  git commit -m "fix: reject refunds on non-locked rounds before reading stakes"
```

Expected: PASS, then one commit.

**Checkpoint 3: the refund event carries `room_id`, `total`, and `payouts`**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestRefundEventCarriesPayoutDetail`. Arrange: a room and a locked round
with `userA` staking 100 on outcome 0 and 50 on outcome 1, and `userB` staking
200 on outcome 0. Act: `RefundRound`. Assert the single outbox entry has
`type` = `round_refunded`, `room_id` set, `total` = `350`, `dust` = `0`,
`winning_outcome` = `""`, and `payouts` decoding to exactly two elements —
`{userA, 150}` and `{userB, 200}`. **`userA`'s two stakes aggregate into one
payout entry**, because a ledger credit line is per account, not per stake.
Assert the returned `domain.Tokens` total is still `350` and that both wallets
were credited by their aggregate.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/redisstore/ -run TestRefundEventCarriesPayoutDetail -count=1 -race`
Expected: FAIL — the entry has no `room_id`, `total`, or `payouts` field.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Store.RefundRound` calls `ReadStakes`, aggregates amounts per
`UserID` into a deterministic slice ordered by ascending user ID (matching
`ReadStakes`'s existing sort rationale — the same round refunded twice must
produce identical output), marshals it with the same JSON helper Checkpoint 1
introduced, and passes `roomID`, `total`, the JSON, and the alternating
`userID, amount` tail. `refund_round.lua` drops its `HGETALL` loop, its
colon-scan parser, and `KEYS[3]`; it becomes an apply-only script that credits
from ARGV, `HSET`s status `refunded`, and `XADD`s the enriched payload. Keep
its status CAS unchanged. Update its header comment to record that amounts now
come from Go, citing Amendment E2.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/redisstore/ -count=1 -race && \
  git add internal/redisstore/ scripts/lua/refund_round.lua && \
  git commit -m "feat: take refund amounts from Go and carry them on the event"
```

Expected: PASS, then one commit.

**Task 2 boundary — full suite.** The end-to-end round test in `internal/ws`
exercises both scripts; it must stay green.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  docker compose up -d redis postgres && cd backend && \
  go test ./... -race -cover -p 1 -count=1
```

---

## Task 3: `internal/events` — typed schema over the outbox

Pure decoding and encoding. No Redis, no Kafka, no Postgres — every test here
runs with nothing running.

**Files:**
- Create: `backend/internal/events/event.go`, `backend/internal/events/decode.go`
- Test: `backend/internal/events/decode_test.go`

**Interfaces:**
- Consumes: the wire format from Task 2's Produces table.
- Produces:
  - `type Payout struct { UserID string \`json:"user_id"\`; Amount int64 \`json:"amount"\` }`
  - `type WagerPlaced struct { RoomID, RoundID, UserID, IdempotencyKey string; Outcome int; Amount, Balance int64 }`
  - `type RoundSettled struct { RoomID, RoundID, IdempotencyKey string; WinningOutcome int; Total, Dust int64; Payouts []Payout; Refunded bool }`
  - `type Event interface { Topic() string; PartitionKey() string; Key() string }` — `Key()` returns the idempotency key.
  - `func Decode(fields map[string]string) (Event, error)`
  - `var ErrUnknownEventType = errors.New("events: unknown event type")`

  One struct serves both `round_settled` and `round_refunded`, distinguished by
  `Refunded`. They carry identical fields and produce identical ledger shapes
  in 5b; two structs would be two copies of one thing.

**Checkpoint 1: decode a `wager_placed` entry**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestDecodeWagerPlaced`. Input: the exact field map
`{"type":"wager_placed","user":"u1","outcome":"1","amount":"100","balance":"900","idempotency_key":"idem-1","room_id":"r1","round_id":"rd1"}`.
Expect a `WagerPlaced` with `UserID` `u1`, `Outcome` `1`, `Amount` `100`,
`Balance` `900`, and the three IDs set. Table-driven malformed cases, each
expecting an error mentioning the offending field name: `amount` not an
integer; `outcome` not an integer; `room_id` absent.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/events/ -run TestDecodeWagerPlaced -count=1 -race`
Expected: FAIL — package `internal/events` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Decode` switches on `fields["type"]`, parsing every numeric field
with `strconv.ParseInt(..., 10, 64)` and returning a wrapped error naming the
field on failure. A missing required field is an error, not a zero value.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/events/ -count=1 -race && \
  git add internal/events/ && \
  git commit -m "feat: decode wager-placed outbox entries into typed events"
```

Expected: PASS, then one commit.

**Checkpoint 2: decode `round_settled`, including the payouts JSON**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestDecodeRoundSettled`. Input fields
`{"type":"round_settled","round_id":"rd1","room_id":"r1","dust":"2","total":"400","winning_outcome":"1","payouts":"[{\"user_id\":\"u2\",\"amount\":398}]","idempotency_key":"idem-2"}`.
Expect `RoundSettled{RoomID:"r1", RoundID:"rd1", WinningOutcome:1, Total:400,
Dust:2, Payouts:[{u2,398}], Refunded:false}`.

Include a precision case that would pass under `float64` decoding and fail
under correct decoding only if done wrong: a payout amount of `9007199254740993`
(2^53 + 1) must round-trip exactly. This pins the Global Constraint about
never decoding numbers through `interface{}`.

Malformed cases: `payouts` not valid JSON → error; `payouts` absent → error;
`dust` not an integer → error.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/events/ -run TestDecodeRoundSettled -count=1 -race`
Expected: FAIL — `Decode` returns `ErrUnknownEventType` for `round_settled`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add the `round_settled` case, unmarshalling `payouts` into
`[]Payout`. `Refunded` is false. An empty array decodes to an empty, non-nil
slice.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/events/ -count=1 -race && \
  git add internal/events/ && \
  git commit -m "feat: decode settlement events and their payout arrays"
```

Expected: PASS, then one commit.

**Checkpoint 3: decode `round_refunded`**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestDecodeRoundRefunded`. Same shape as Checkpoint 2 but
`type` = `round_refunded`, `winning_outcome` = `""`, `dust` = `"0"`. Expect
`Refunded: true` and `WinningOutcome: -1` — the sentinel for "no winning
outcome", chosen because `0` is a valid outcome index and would be
indistinguishable from a real outcome-0 win. Assert that decoding a refund
whose `winning_outcome` is non-empty returns an error, since that combination
is contradictory.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/events/ -run TestDecodeRoundRefunded -count=1 -race`
Expected: FAIL — `Decode` returns `ErrUnknownEventType` for `round_refunded`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add the `round_refunded` case, sharing the settled decoder and
setting `Refunded: true`, `WinningOutcome: -1`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/events/ -count=1 -race && \
  git add internal/events/ && \
  git commit -m "feat: decode refund events"
```

Expected: PASS, then one commit.

**Checkpoint 4: unknown and missing event types are rejected**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestDecodeUnknownType`, table-driven: `type` = `"nonsense"`; `type`
absent entirely; an empty field map. Each must return an error satisfying
`errors.Is(err, ErrUnknownEventType)` and a nil `Event`. A relay that silently
dropped an entry it did not recognise would lose money movements without
signalling, so this must be an error rather than a skip.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/events/ -run TestDecodeUnknownType -count=1 -race`
Expected: FAIL — the sentinel `ErrUnknownEventType` is not yet exported, or the
absent-`type` case returns a nil error.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: export `ErrUnknownEventType`; the `default` branch wraps it with the
observed type string.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/events/ -count=1 -race && \
  git add internal/events/ && \
  git commit -m "feat: reject unrecognised outbox entry types"
```

Expected: PASS, then one commit.

**Checkpoint 5: topic and partition-key routing**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestEventRouting`, table-driven over all three event types. Assert
`Topic()` is `wagers-placed` for `WagerPlaced` and `rounds-settled` for both
settled and refunded (a refund is a settlement outcome, and routing it
elsewhere would break the per-room ordering guarantee that settlements must
follow the wagers they settle). Assert `PartitionKey()` returns the event's
`RoomID` for all three — parent plan §7 keys by `room_id` — and `Key()`
returns the idempotency key.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/events/ -run TestEventRouting -count=1 -race`
Expected: FAIL — `Topic`, `PartitionKey`, and `Key` are undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: implement the three `Event` methods on both structs. Declare the
topic names as exported constants `TopicWagersPlaced` and `TopicRoundsSettled`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/events/ -count=1 -race && \
  git add internal/events/ && \
  git commit -m "feat: route events to topics and partition by room"
```

Expected: PASS, then one commit.

**Task 3 boundary — full suite** (same command as Task 1's boundary).

---

## Task 4: `internal/relay` — the read → produce → ack loop

The durability-critical logic, tested against a fake producer so every case
including failure is reachable without a broker.

**Files:**
- Create: `backend/internal/relay/relay.go`
- Test: `backend/internal/relay/relay_test.go`, `backend/internal/relay/testmain_test.go`
- Modify: `backend/internal/redisstore/keys.go`

**Interfaces:**
- Consumes: `events.Decode`, `events.Event`, `redisstore.OutboxStream`.
- Produces:
  - `redisstore.OutboxGroup = "relay"` (constant in `keys.go`)
  - `type Producer interface { Produce(ctx context.Context, evs []events.Event) error }`
  - `func New(client *redis.Client, stream string, group, consumer string, p Producer) *Relay`
  - `func (r *Relay) EnsureGroup(ctx context.Context) error`
  - `func (r *Relay) Once(ctx context.Context, count int64, block time.Duration) (relayed int, err error)`
  - `func (r *Relay) Recover(ctx context.Context, count int64) (relayed int, err error)`
  - `func (r *Relay) Run(ctx context.Context) error`

  `Once` is the unit under test: one `XREADGROUP` → decode → `Produce` →
  `XACK` cycle. `Run` is a loop over `Once` that returns nil on
  `context.Canceled`.

**Test harness.** Same conventions as `internal/redisstore`: Redis DB 15,
`TestMain` pings and `FLUSHDB`s, failing rather than skipping when Redis is
unreachable, and each test uses a uniquely-named stream and group so tests do
not read each other's entries.

**Checkpoint 1: the consumer group is created idempotently**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestEnsureGroupIsIdempotent`. Act: call `EnsureGroup` twice against a
stream name that does not exist yet. Assert: both calls return nil; `XINFO
GROUPS` reports exactly one group with the configured name. The second call
must swallow Redis's `BUSYGROUP` error specifically and return any other error
unchanged. The group must be created with `MKSTREAM` and start id `0`, not
`$` — starting at `$` would skip every entry already written by a running
API process, silently losing money movements that predate the relay's first
start.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/relay/ -run TestEnsureGroupIsIdempotent -count=1 -race`
Expected: FAIL — package `internal/relay` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `EnsureGroup` calls `XGroupCreateMkStream(ctx, stream, group, "0")`
and returns nil when the error string contains `BUSYGROUP`. Add `OutboxGroup`
to `keys.go` with a comment explaining the `0`-vs-`$` choice.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/relay/ -count=1 -race && \
  git add internal/relay/ internal/redisstore/keys.go && \
  git commit -m "feat: create the outbox consumer group idempotently from the stream start"
```

Expected: PASS, then one commit.

**Checkpoint 2: a batch is produced and then acked**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestOnceProducesThenAcks`. Arrange: `EnsureGroup`; `XADD` three entries
— one `wager_placed`, one `round_settled`, one `round_refunded`, each with the
full field set from Task 2's Produces table. Use a fake producer recording
every call. Act: `Once(ctx, 10, time.Second)`. Assert: returns `relayed == 3,
err == nil`; the fake received **one** `Produce` call carrying all three events
in stream order (batching matters — one call per entry would multiply broker
round trips by the wager rate); and `XPENDING` on the group reports zero
pending entries, proving the ack happened.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/relay/ -run TestOnceProducesThenAcks -count=1 -race`
Expected: FAIL — `Once` is undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Once` issues `XReadGroup` with the `>` id, decodes each entry via
`events.Decode`, calls `Produce` once with the whole batch, and only on a nil
error `XACK`s every id in the batch. An empty read (block timeout) returns
`0, nil`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/relay/ -count=1 -race && \
  git add internal/relay/ && \
  git commit -m "feat: relay a batch of outbox entries and ack after the produce"
```

Expected: PASS, then one commit.

**Checkpoint 3: a produce failure leaves the batch unacked**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestOnceDoesNotAckOnProduceFailure`. Arrange: two entries; a fake
producer returning `errors.New("broker down")`. Act: `Once`. Assert: returns a
non-nil error wrapping the producer's; `XPENDING` reports **2** pending
entries. Then swap the fake to succeed and call `Recover`: assert both entries
are delivered and pending drops to zero. This is the at-least-once guarantee —
an ack before a confirmed produce would turn a broker outage into permanent
data loss, which is exactly the crash window the outbox exists to close.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/relay/ -run TestOnceDoesNotAckOnProduceFailure -count=1 -race`
Expected: FAIL — `Recover` is undefined (and, if `Once` acks unconditionally,
the pending count is 0 rather than 2).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Once` returns before acking when `Produce` errors. `Recover` is
`Once`'s logic with the `0` id instead of `>`, re-reading this consumer's own
pending entries.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/relay/ -count=1 -race && \
  git add internal/relay/ && \
  git commit -m "feat: keep entries pending when the produce fails, and recover them"
```

Expected: PASS, then one commit.

**Checkpoint 4: an undecodable entry stops the relay rather than being skipped**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestOnceHaltsOnUndecodableEntry`. Arrange: `XADD` one valid
`wager_placed` followed by one entry with `type` `garbage`. Act: `Once`.
Assert: returns an error satisfying `errors.Is(err, events.ErrUnknownEventType)`;
the fake producer received **no** call; `XPENDING` reports 2. Nothing is acked
and nothing is produced — a partially-relayed batch would be worse than none,
since the acked prefix could not be replayed. Halting surfaces a schema
mismatch loudly instead of silently dropping a money movement.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/relay/ -run TestOnceHaltsOnUndecodableEntry -count=1 -race`
Expected: FAIL — decoding errors are currently unhandled, so the call either
panics or relays only the valid entry.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Once` decodes the whole batch before producing any of it, returning
the first decode error wrapped with the offending stream id.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/relay/ -count=1 -race && \
  git add internal/relay/ && \
  git commit -m "feat: halt the relay on an undecodable entry instead of dropping it"
```

Expected: PASS, then one commit.

**Checkpoint 5: `Run` recovers pending work before reading new entries**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestRunRecoversBeforeNewWork`. Arrange: leave one entry pending by
running `Once` against a failing producer; then `XADD` a second, newer entry;
swap in a recording, succeeding producer. Act: `Run` with a context cancelled
once two events have been recorded. Assert: `Run` returns nil (a cancelled
context is a clean shutdown, not an error), and the recorded events arrive
**oldest first** — the recovered entry before the new one. Ordering is the
point: `rounds-settled` must never reach Kafka ahead of the `wagers-placed`
entries it settles, and a relay that read new work before draining its pending
list would invert exactly that.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/relay/ -run TestRunRecoversBeforeNewWork -count=1 -race`
Expected: FAIL — `Run` is undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Run` calls `EnsureGroup`, then `Recover` until it returns 0, then
loops on `Once`. It returns nil when the context is cancelled and the wrapped
error otherwise.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/relay/ -count=1 -race && \
  git add internal/relay/ && \
  git commit -m "feat: drain pending entries before new ones on relay startup"
```

Expected: PASS, then one commit.

**Task 4 boundary — full suite** (same command as Task 1's boundary).

---

## Task 5: The Kafka producer

**Files:**
- Create: `backend/internal/events/kafka.go`
- Test: `backend/internal/events/kafka_test.go`

**Interfaces:**
- Consumes: `events.Event`, `relay.Producer` (satisfied structurally).
- Produces:
  - `func NewKafkaProducer(brokers []string) *KafkaProducer`
  - `func (p *KafkaProducer) EnsureTopics(ctx context.Context, partitions int) error`
  - `func (p *KafkaProducer) Produce(ctx context.Context, evs []Event) error`
  - `func (p *KafkaProducer) Close() error`
  - `const Partitions = 6`

**Harness.** These tests need a real broker and **fail rather than skip**
without one, per the project's standing rule. `KAFKA_BROKERS` (default
`localhost:9092`) selects it; `TestMain` dials and calls `t.Fatal` with
`run \`make up-full\` and retry` if unreachable. Each test uses a
uniquely-suffixed topic name so runs do not read each other's messages.

**Checkpoint 1: topics are created with the specified partition count**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestEnsureTopicsCreatesPartitions`. Act: `EnsureTopics(ctx, 6)` against
uniquely-named topics, twice. Assert: both calls return nil (idempotent — a
`TOPIC_ALREADY_EXISTS` response is not an error), and a metadata read reports
6 partitions for each. Amendment E3: relying on broker auto-creation would
yield 1 partition, and partition count cannot be lowered afterward.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/events/ -run TestEnsureTopicsCreatesPartitions -count=1 -race`
Expected: FAIL — `NewKafkaProducer` and `EnsureTopics` are undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `EnsureTopics` uses `kafka.Client.CreateTopics` with
`ReplicationFactor: 1` (single-node local broker), treating an already-exists
response as success.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/events/ -count=1 -race && \
  git add internal/events/ && \
  git commit -m "feat: create Kafka topics with the specified partition count"
```

Expected: PASS, then one commit.

**Checkpoint 2: events reach Kafka on the right topic, keyed by room**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestProduceRoundTrip`. Arrange: `EnsureTopics`; a `WagerPlaced` for room
`r1` and a `RoundSettled` for the same room. Act: `Produce` with both. Assert
by reading each topic back with a `kafka.Reader`: the wager lands on
`wagers-placed` and the settlement on `rounds-settled`; each message's `Key` is
the room ID as bytes; each message's value unmarshals back to the same field
values, with the settlement's payouts intact. Assert both messages carry the
same partition, since they share a room key — that co-location is what makes
per-room ordering real rather than nominal.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/events/ -run TestProduceRoundTrip -count=1 -race`
Expected: FAIL — `Produce` is undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Produce` marshals each event to JSON, builds one
`kafka.Message` per event with `Topic` from `Topic()` and `Key` from
`PartitionKey()`, and writes them in one `WriteMessages` call. The writer uses
`kafka.Hash` as its balancer so an identical key always lands on one partition,
and `RequiredAcks: kafka.RequireAll` — anything weaker would let the relay ack
a Redis entry against a write the broker can still lose, reopening the crash
window the outbox exists to close.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/events/ -count=1 -race && \
  git add internal/events/ && \
  git commit -m "feat: produce events to Kafka keyed by room with full acks"
```

Expected: PASS, then one commit.

**Task 5 boundary — full suite, Kafka included:**

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  docker compose --profile full up -d && cd backend && \
  go test ./... -race -cover -p 1 -count=1
```

---

## Task 6: Wiring, CI, and close-out

**Files:**
- Create: `backend/cmd/relay/main.go`
- Modify: `backend/internal/config/config.go`, `.github/workflows/ci.yml`, `docs/plans/2026-08-21-implementation-plan.md`, `CLAUDE.md`
- Test: `backend/internal/config/config_test.go`

**Interfaces:**
- Consumes: `relay.New`, `relay.Run`, `events.NewKafkaProducer`, `events.EnsureTopics`, `redisstore.OutboxStream`, `redisstore.OutboxGroup`.
- Produces: `config.RelayConfig{RedisAddr, RedisDB, KafkaBrokers []string, LogLevel, Env}`, `config.LoadRelay(lookup) (RelayConfig, error)`.

**Checkpoint 1: `LoadRelay` validates the relay's own surface**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestLoadRelay`, table-driven. Defaults with an empty environment:
`RedisAddr` `localhost:6379`, `RedisDB` `0`, `KafkaBrokers`
`["localhost:9092"]`, `LogLevel` `info`, `Env` `development`. `KAFKA_BROKERS`
= `"a:9092,b:9092"` splits on commas into two entries; `""` → error containing
`KAFKA_BROKERS must not be empty`; `"a:9092,,b:9092"` → error naming the empty
element, since a silently-dropped broker would degrade availability invisibly.
Invalid `REDIS_DB` and `LOG_LEVEL` values reject exactly as `Load` already
does. **`LoadRelay` must succeed with no `JWT_SECRET` set** — assert this
explicitly; the relay never issues or verifies a token, and requiring the
secret would hand a non-auth binary a credential it has no use for.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/config/ -run TestLoadRelay -count=1 -race`
Expected: FAIL — `config.LoadRelay` is undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add `RelayConfig` and `LoadRelay`, reusing the existing validation
helpers and `validLogLevels`/`validEnvs` maps. Update `Config`'s doc comment,
which currently says Postgres and Kafka fields are "added in the phase that
introduces that integration" — that phase is this one.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/config/ -count=1 -race && \
  git add internal/config/ && \
  git commit -m "feat: add the relay's configuration surface"
```

Expected: PASS, then one commit.

**Checkpoint 2: `cmd/relay` wires and shuts down cleanly**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestRelayBinaryBuilds` in `internal/relay` — a test asserting the
binary compiles is not meaningful, so make the observable behavior graceful
shutdown instead. `TestRunReturnsOnContextCancel`: arrange a `Relay` over an
empty stream with a no-op producer; act by calling `Run` with a context
cancelled after 50ms; assert it returns nil within 1s rather than blocking on
`XREADGROUP`'s block timeout. Pin the block interval at 1 second or less so a
`SIGTERM` is not held for longer than a deploy is willing to wait.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/relay/ -run TestRunReturnsOnContextCancel -count=1 -race`
Expected: FAIL — `Run` blocks past the deadline, because `XREADGROUP`'s block
duration is not bounded by the context.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Run` checks `ctx.Err()` between iterations and passes a block
duration no greater than 1s. `cmd/relay/main.go` mirrors `cmd/api/main.go`'s
structure: `LoadRelay`, `slog` setup, `redisstore`-style Redis client,
`NewKafkaProducer`, `EnsureTopics(ctx, events.Partitions)`,
`relay.New(...)`, `signal.NotifyContext` for SIGINT/SIGTERM, `Run`, and
deferred `Close` on both the Redis client and the producer.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/relay/ -count=1 -race && go build ./... && \
  git add internal/relay/ cmd/relay/ && \
  git commit -m "feat: add the relay binary with graceful shutdown"
```

Expected: PASS, then one commit.

**Checkpoint 3: CI runs the new integration suites**

- [ ] **Step 1: Write the failing test, then run it**

Spec: no Go test — the observable signal is CI's own behavior, verified
locally by running exactly what CI will run against a clean service set.
Arrange: `docker compose down -v` then `docker compose --profile full up -d`.
Act: run the full suite plus vet and gofmt exactly as `.github/workflows/ci.yml`
would. Assert: all green. Before editing the workflow, run it once with only
`redis` up and confirm the Postgres and Kafka suites **fail** rather than skip
— that failure is the evidence the new tests are actually wired into CI's
signal, and it is what this checkpoint's RED step demonstrates.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  docker compose down -v && docker compose up -d redis && cd backend && \
  go test ./internal/migrate/ ./internal/events/ -count=1 -race
```
Expected: FAIL — `internal/migrate` cannot reach PostgreSQL and
`internal/events` cannot reach Kafka, both with the fatal messages their
`TestMain`s specify.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add a `postgres:16-alpine` service to `.github/workflows/ci.yml`
matching `docker-compose.yml`'s credentials and health check, and bring Kafka
up with a workflow step running `docker compose --profile full up -d kafka`
followed by a wait on its health status — GitHub Actions' `services:` block
makes KRaft's listener configuration awkward, and reusing the compose file
keeps CI and local dev on one definition. Add `POSTGRES_DSN` and
`KAFKA_BROKERS` to the test step's `env`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  docker compose --profile full up -d && cd backend && \
  go vet ./... && test -z "$(gofmt -l .)" && \
  go test ./... -race -cover -p 1 -count=1 && \
  git add ../.github/workflows/ci.yml && \
  git commit -m "ci: run the PostgreSQL and Kafka integration suites"
```

Expected: PASS, then one commit.

**Checkpoint 4: record the amendments and close the phase**

- [ ] **Step 1: Write the failing test, then run it**

Spec: no test — documentation. Verify the current state instead: run
`go test ./... -race -cover -p 1 -count=1 -coverpkg=./...` and record the
per-package coverage figures, confirming `internal/events`, `internal/relay`,
and `internal/migrate` each clear the 80% floor. If any is short, add the
missing cases as their own checkpoint before proceeding — do not lower the bar
and do not pad with tests that re-exercise covered lines.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && docker compose --profile full up -d && cd backend && go test ./... -race -cover -p 1 -count=1 -coverpkg=./...`
Expected: PASS, with the three new packages at or above 80%.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: write Amendments E1, E2, and E3 into the parent plan — E1/E2 replace
§5's `settle_round.lua` and `refund_round.lua` payload and sourcing
descriptions, E3 goes next to §7's topology table. Add the three pinned
dependencies and their walls to `CLAUDE.md`'s Stack section alongside the
existing go-redis and x/crypto pins. Add the `wager-outbox` consumer group to
§4's key-schema table. Record the phase's measured `tok/CP` against the
pre-registered `< 4.6M` bar (Phase 2 is the control, not Phase 4b — 5a is
plumbing and comparing it to 4b's money-wiring would flatter the result) in a
journal entry via the `journal` skill.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  docker compose --profile full up -d && cd backend && \
  go test ./... -race -cover -p 1 -count=1 && \
  git add ../docs/ ../CLAUDE.md ../journal/ && \
  git commit -m "docs: record Phase 5a amendments and close out the phase"
```

Expected: PASS, then one commit. **Branch is green and verified — stop here.**
`executing-plans` hands off to `finishing-a-development-branch` for the merge;
this plan does not merge, push, or open a PR.

---

## Self-Review

**1. Spec coverage.** Parent plan §9's Phase 5a row names six deliverables:
relay binary (Task 4 + Task 6 CP2), `wagers-placed`/`rounds-settled` producers
(Task 5), `internal/events` schemas (Task 3), PostgreSQL migrations (Task 1
CP1–CP2, CP6), ledger schema (Task 1 CP1, CP4, CP5), deferred constraint
trigger (Task 1 CP3). §7's topology — both topics, `room_id` keys, 6
partitions — is covered by Task 3 CP5 and Task 5 CP1–CP2. §6's schema shape is
Task 1. §1's outbox amendment is Task 2. The `ledger-writer` consumer group in
§7 is deliberately unimplemented: no consumer exists until 5b.

**2. Gaps found and closed while reviewing.** The outbox payload gap (E1) and
its refund knock-on (E2) were not in the parent plan at all and would have
blocked 5b; both are now resolved in-document with the read-then-write hazard
named explicitly. Topic auto-creation (E3) would have silently produced
1-partition topics. Task 4 CP4's halt-don't-skip rule and CP5's
recover-before-new-work ordering are the two places a plausible implementation
loses or reorders money events; both now have their own checkpoints.

**3. Checkpoint falsifiability.** Every checkpoint names an observable signal
at the interface its test calls. Task 2 CP2 is the one place where the RED may
not materialise — the existing script already maps `NOT_LOCKED`, so a Go-side
guard could be black-box indistinguishable at the wrapper's return type. That
risk is called out inline with a concrete fallback (keep the `resolved` case,
move the guard's proof to CP3) rather than left to be discovered mid-execution,
which is the Phase 2 `ALREADY_LOCKED` failure mode this plan is written to
avoid. Task 6 CP3 and CP4 are documentation/CI checkpoints whose RED is a real
failing command, not a test.

**4. Type consistency.** `events.Payout` is the wire type throughout;
`domain.Payout` is never JSON-tagged. `RoundSettled` serves both terminal event
types with `Refunded` discriminating, and `WinningOutcome: -1` is the refund
sentinel in Task 3 CP3 and nowhere contradicted. `Producer` is declared in
`internal/relay` and satisfied structurally by `events.KafkaProducer`, so
`internal/events` does not import `internal/relay` and no cycle exists — the
`round`↔`ws` cycle that cost Phase 4b real time.

**5. Sizing.** 6 tasks, 22 checkpoints — against Phase 4b's 31 and Phase 3's
38. Task 1 is the largest at 6 checkpoints because a schema's constraints are
each independently falsifiable; none of its neighbours could be rejected
without rejecting it.

---

## Execution notes

**This phase is the delegation test bed.** Each task above is sized to be
handed to a subagent with only its own `Interfaces` block as context. Two
things must hold for that to be safe, both of which this plan is written to
support: the `Consumes`/`Produces` blocks are exact, and a subagent that hits
a contradiction between this plan and the code **stops and reports** rather
than redesigning around it. The amendments were resolved here, in-document,
specifically so no delegated task has to invent a contract that Phase 5b
depends on.

**Pre-registered prediction:** `tok/CP < 4.6M`, with `un-batched = 0` and
`commits/CP <= 1.10` as discipline guardrails. Phase 2 is the control — a
result between 4.6M and 6.0M is ambiguous, not a win.
