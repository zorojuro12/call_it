# Phase 4b — Round Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make CallIt playable end to end — a host opens a prediction round over the socket, players wager against a live pari-mutuel pool, a server-side timer locks it, the host resolves it, and every stake and result is revealed at once.

**Architecture:** All round control travels over the Phase 4a WebSocket transport (Amendment D1). `internal/round` owns round lifecycle and the two server-side timers; `internal/wager` owns validate → Lua → broadcast. Neither writes Redis directly — both call `internal/redisstore`'s existing, tested writers. Settlement math stays in `internal/domain.Settle`; nothing recomputes a payout.

**Tech Stack:** Go 1.22.10 · existing `internal/redisstore` Lua wrappers (`PlaceWager`, `LockRound`, `SettleRound`, `RefundRound`) · `internal/domain` (`Settle`, `Multipliers`, `ValidateStake`, `ApplySessionResult`) · the `internal/ws` transport from Phase 4a. No Kafka, no PostgreSQL — those are Phase 5.

**Spec:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md) §4 (gameplay and round lifecycle), §5 (write path), §3 (session-end persistence). Parent plan: [`docs/plans/2026-08-21-implementation-plan.md`](2026-08-21-implementation-plan.md) §5 (Lua contracts), §8 (economy constants).

**Depends on:** [`docs/plans/2026-08-26-phase-4a-ws-transport.md`](2026-08-26-phase-4a-ws-transport.md), merged into `dev`.

**Plan budget:** 31 checkpoints, target ≤60 lines/checkpoint.

---

## ⚠ Revision required before execution

This plan was drafted in the **same session as Phase 4a's plan, before 4a was executed.** It consumes three interfaces that do not exist yet and are specified only on paper:

- `ws.MessageHandler` — the `func(c *Client, e Envelope)` seam (4a Task 4)
- `ws.Room.Broadcast(payload []byte)` and `ws.Client.Send(payload []byte)` (4a Tasks 2 and 4)
- `ws.Hub.Join(roomID string, c *Client) *Room` (4a Task 5)

**Before starting Task 1, diff those three against what 4a actually landed** (`go doc ./internal/ws`) and fix any drift in this plan's Interfaces blocks. If 4a's execution changed a signature, this plan is stale at exactly those points and nowhere else — the tasks below never reach into 4a's transport internals.

---

## Amendment D1 — round control travels over the socket, not REST

Phase 3 put room creation and joining behind REST. Round control — open, wager, resolve — goes over the WebSocket instead.

Three reasons, in order of weight: the wager path has a **<15 ms p99 target** (spec §7) that a fresh HTTP request per wager works against; the host's identity is **already verified on the socket** from the room-scoped JWT, so a REST route would re-verify the same claim to reach the same room; and every one of these actions **produces a broadcast**, so handling them where the `*Room` already lives avoids `httpapi` reaching into the hub to publish.

Room lifecycle stays REST — creating and joining a room happen before any socket exists, so they have no connection to travel over.

## Amendment D2 — a room needs an index to its current round

`round:{roundID}` records its `room_id`, but nothing maps the other way, and finding a room's open round would mean scanning every round key. Three paths in this phase need that lookup: routing an inbound wager to the right round, refusing a second concurrent round, and the auto-refund timer confirming what it is refunding.

**Add `room:{roomID}:round` — STRING → current `roundID`**, set when a round opens and deleted when it reaches a terminal state. Key built by a new `RoomRoundKey` builder in `keys.go`, per the invariant that `keys.go` is the only place a Redis key may be constructed.

## Amendment D3 — the opening session stake must be persisted

`CLAUDE.md`'s invariant: *"A session's opening stake never debits an account holder's persistent balance. Only the net delta at session end does (Phase 4)."* Computing that delta needs `ApplySessionResult(accountBalance, sessionStart, sessionEnd)` — and `sessionStart` exists nowhere after `JoinRoom` returns it. `room:{roomID}:wallets` holds only the *current* balance, which moves on every wager.

**Add `room:{roomID}:opening` — HASH `userID` → the effective balance granted at join.** Written by `JoinRoom` in the same transaction as the wallet, so a wallet can never exist without its opening stake.

## Amendment D4 — the round hash carries its question and outcome labels

`round:{roundID}` currently holds `outcome_count` but not the question text or the outcome labels, and spec §4 has the host typing both. Holding them only in server memory would leave a player who connects mid-round unable to render what they are betting on.

**Add `question` (STRING) and `outcomes` (JSON array of strings) to the `round:{roundID}` hash.** `Store.CreateRound` gains both parameters and `Round` gains both fields. This changes an existing, tested signature — Task 1 CP1 updates its callers and tests in the same checkpoint.

---

## Global Constraints

- **Go 1.22.10.** No new external dependencies in this phase — everything needed is already in `go.mod`.
- **`internal/domain` stays free of I/O**, and **settlement math is not duplicated.** `domain.Settle` is reached only through `redisstore.SettleRound`. Never recompute a payout, a multiplier, or dust anywhere in `internal/round` or `internal/wager`.
- **Lockout is decided by Redis's `TIME`, never by Go.** The server-side timer in Task 3 decides *when to ask*; `place_wager.lua` decides whether a wager is late. A timer that fires early or late changes nothing about correctness — do not add a Go-side timestamp comparison to "help".
- **The host cannot wager.** Already enforced inside `place_wager.lua` and surfaced as `redisstore.ErrHostCannotBet`. Do not add a second Go-side check that could drift from it.
- **Wagers stay anonymous until terminal.** `odds_updated` carries pool totals, multipliers, and counts only. The first and only per-player reveal is `round_resolved`, built from `domain.Settlement.Results`. Any payload carrying a named stake before that violates the invariant.
- **Every wager carries a UUIDv4 `idempotency_key`, supplied by the client.** It is the dedupe identity in Lua and the `UNIQUE` constraint in Phase 5's ledger. Never generate one server-side to "fix" a missing field — reject the message instead, or a client retry becomes a second wager.
- **All amounts are integer token units** (`domain.Tokens`). Multipliers become `float64` only in the outbound `odds_updated` payload.
- **`-p 1` is load-bearing.** This phase adds integration suites in `internal/round` and `internal/wager` that share Redis DB 15 with four existing packages.
- **`export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin` before any Go command**, and `make up` before any checkpoint whose tests touch Redis (Tasks 1–8, 11).
- Checkpoint test commands are package-scoped; the full suite runs only at task boundaries.
- Commit format `type: description`. One checkpoint, one commit.

---

## File Structure

| File | Responsibility |
|---|---|
| `backend/internal/redisstore/keys.go` | **Modify** — `RoomRoundKey`, `RoomOpeningKey` builders |
| `backend/internal/redisstore/round.go` | **Modify** — question/outcomes on the round hash; current-round index set and clear |
| `backend/internal/redisstore/room.go` | **Modify** — `JoinRoom` writes the opening-stake hash; `OpeningStake` reader |
| `backend/internal/round/protocol.go` | Round command payloads and event payloads |
| `backend/internal/round/service.go` | Create, Resolve, host authorization, round-in-progress guard |
| `backend/internal/round/timer.go` | Server-side lock timer and the 60-second auto-refund fallback |
| `backend/internal/round/session.go` | Session-end persistence (`ApplySessionResult`) |
| `backend/internal/wager/service.go` | Validate → `PlaceWager` → odds broadcast |
| `backend/internal/ws/router.go` | The `MessageHandler` that routes inbound message types to the two services |
| `backend/cmd/callit-cli/main.go` | CLI client — join a room, play a round end to end |
| `backend/internal/httpapi/ws_handlers.go` | **Modify** — pass the real router instead of 4a's nil seam |
| `backend/cmd/api/main.go` | **Modify** — construct the round/wager services and the router |

---

## Task 1: Redis schema additions

**Files:**
- Modify: `backend/internal/redisstore/keys.go`, `round.go`, `room.go`, and their `_test.go` files

**Interfaces — Produces:**
```go
func RoomRoundKey(roomID string) string   // "room:{roomID}:round"
func RoomOpeningKey(roomID string) string // "room:{roomID}:opening"

// CreateRound gains question and outcomes (Amendment D4).
func (s *Store) CreateRound(ctx context.Context, roundID, roomID, question string,
    outcomes []string, lockAt time.Time) error

// Round gains Question and Outcomes; OutcomeCount stays and equals len(Outcomes).
type Round struct {
    ID, RoomID, Question string
    Outcomes             []string
    Status               domain.RoundStatus
    LockAtMS             int64
    OutcomeCount         int
    ResolvedOutcome      int
}

func (s *Store) CurrentRound(ctx context.Context, roomID string) (string, error) // ErrNotFound when none
func (s *Store) ClearCurrentRound(ctx context.Context, roomID string) error
func (s *Store) OpeningStake(ctx context.Context, roomID, userID string) (domain.Tokens, error)
```

`CreateRound` no longer takes `outcomeCount` — it derives it from `len(outcomes)` and validates via the existing `domain.ValidateOutcomeCount`. Update every existing caller and test; find them with `grep -rn "CreateRound(" internal/ cmd/`.

**Checkpoint 1: a round remembers its question and outcome labels**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `CreateRound(ctx, "rd1", "rm1", "Will he clutch the 1v2?", []string{"Yes","No","Trade"}, lockAt)` then `Round(ctx, "rd1")` returns `Question == "Will he clutch the 1v2?"`, `Outcomes` deep-equal to the three labels in order, and `OutcomeCount == 3`. `CreateRound` with `[]string{"Only one"}` returns `domain.ErrInvalidOutcomeCount` and writes nothing (`Round` then returns `ErrNotFound`).

Run: `make up && cd backend && go test ./internal/redisstore/ -race -count=1 -p 1 -run TestCreateRound`
Expected: FAIL — `CreateRound` does not accept a question or outcomes; the package will not compile against the new call.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `CreateRound` HSETs `question` and a `json.Marshal`ed `outcomes` alongside the existing fields, in the same transaction that pre-zeroes the pools. `Round` unmarshals `outcomes` and sets `OutcomeCount` from its length.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/redisstore/ -race -count=1 -p 1 && \
  git add internal/redisstore/round.go internal/redisstore/round_test.go && \
  git commit -m "feat: persist a round's question and outcome labels"
```

Expected: PASS, then one commit.

**Checkpoint 2: a room indexes its current round**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `CurrentRound(ctx, "rm1")` on a room with no round returns `ErrNotFound`. After `CreateRound(..., "rd1", "rm1", ...)`, it returns `"rd1"`. `RoomRoundKey("rm1") == "room:rm1:round"`. Creating a second round for `"rm1"` while `"rd1"` is indexed **overwrites** the index — `CreateRound` is a low-level writer and enforces no policy; refusing a concurrent round is Task 2 CP3's job at the service layer.

Run: `cd backend && go test ./internal/redisstore/ -race -count=1 -p 1 -run "TestCurrentRound|TestKeys"`
Expected: FAIL — `RoomRoundKey` and `CurrentRound` undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add `RoomRoundKey` to `keys.go`. `CreateRound` SETs it inside its existing transaction. `CurrentRound` GETs it, wrapping `redis.Nil` into `ErrNotFound` per this package's convention that `redis.Nil` never escapes.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/redisstore/ -race -count=1 -p 1 && \
  git add internal/redisstore/keys.go internal/redisstore/round.go internal/redisstore/*_test.go && \
  git commit -m "feat: index a room's current round"
```

Expected: PASS, then one commit.

**Checkpoint 3: a terminal round clears the index**

- [ ] **Step 1: Write the failing test, then run it**

Spec: with `"rd1"` indexed for `"rm1"`, `ClearCurrentRound(ctx, "rm1")` → `CurrentRound` returns `ErrNotFound`. Calling it again on an already-cleared room returns `nil`, not an error — the resolve path and the refund timer can both reach it for the same round, and the loser of that race must not report a failure.

Run: `cd backend && go test ./internal/redisstore/ -race -count=1 -p 1 -run TestClearCurrentRound`
Expected: FAIL — `ClearCurrentRound` undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `ClearCurrentRound` is `DEL` on `RoomRoundKey(roomID)`, returning `nil` regardless of whether the key existed.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/redisstore/ -race -count=1 -p 1 && \
  git add internal/redisstore/round.go internal/redisstore/round_test.go && \
  git commit -m "feat: clear the current-round index idempotently"
```

Expected: PASS, then one commit.

**Checkpoint 4: joining records the opening session stake**

- [ ] **Step 1: Write the failing test, then run it**

Spec: create room `"rm1"` with buy-in 1000. `JoinRoom(ctx, "rm1", "u1", 600)` returns effective `600`; `OpeningStake(ctx, "rm1", "u1")` returns `600`, and `Balance(ctx, "rm1", "u1")` also returns `600`. Now mutate the wallet by placing a wager of 100 — `Balance` becomes `500` while `OpeningStake` **stays 600**. That divergence is the whole point of the hash. `OpeningStake` for a user who never joined returns `ErrNotFound`.

Run: `cd backend && go test ./internal/redisstore/ -race -count=1 -p 1 -run TestOpeningStake`
Expected: FAIL — `OpeningStake` and `RoomOpeningKey` undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add `RoomOpeningKey` to `keys.go`. `JoinRoom` HSETs the effective balance into it in the same transaction as the wallet write, so a wallet without an opening stake is unrepresentable. `OpeningStake` HGETs it, wrapping a missing field into `ErrNotFound`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/redisstore/ -race -count=1 -p 1 && \
  git add internal/redisstore/keys.go internal/redisstore/room.go internal/redisstore/*_test.go && \
  git commit -m "feat: record each player's opening session stake at join"
```

Expected: PASS, then one commit.

**Task boundary:** `make test && make lint && make build`

---

## Task 2: Round service — opening a round

**Files:**
- Create: `backend/internal/round/protocol.go`, `service.go`, `service_test.go`, `testmain_test.go`

**Interfaces — Produces:**
```go
type Spec struct {
    Question string
    Outcomes []string
    LockIn   time.Duration
}

type Opened struct {
    RoundID  string
    Question string
    Outcomes []string
    LockAtMS int64
}

// Broadcaster is how this package reaches the room's connected clients
// without importing internal/ws. internal/ws satisfies it in Task 9.
type Broadcaster interface {
    Broadcast(roomID string, payload []byte)
}

type Service struct{ /* store, broadcaster, clock */ }
func NewService(store *redisstore.Store, b Broadcaster) *Service
func (s *Service) Open(ctx context.Context, roomID, callerID string, spec Spec) (Opened, error)

var ErrNotHost         = errors.New("round: only the room host may control rounds")
var ErrRoundInProgress = errors.New("round: a round is already open in this room")
var ErrInvalidSpec     = errors.New("round: round specification is invalid")

const MinLockIn = 3 * time.Second
const MaxLockIn = 120 * time.Second
```

**Checkpoint 1: the host opens a round and the room is told**

- [ ] **Step 1: Write the failing test, then run it**

Spec: room `"rm1"` created with host `"host1"`. `Open(ctx, "rm1", "host1", Spec{Question: "Clutch?", Outcomes: []string{"Yes","No"}, LockIn: 10 * time.Second})` returns `Opened` with a non-empty UUIDv4 `RoundID`, the question and outcomes echoed, and `LockAtMS` between 9 and 11 seconds from now. `redisstore.CurrentRound(ctx, "rm1")` equals that `RoundID`, and `Round(ctx, RoundID).Status == domain.RoundOpen`.

The injected `Broadcaster` (a test double recording calls) received exactly one broadcast for `"rm1"`, decoding to `Envelope{Type: "round_opened"}` whose data carries the round ID, question, outcomes, and `lock_at_ms`.

Run: `make up && cd backend && go test ./internal/round/ -race -count=1 -p 1 -run TestOpen`
Expected: FAIL — package `round` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Open` reads the room, generates a UUIDv4 round ID, computes `lockAt = now + spec.LockIn`, calls `store.CreateRound`, then broadcasts. Reuse `internal/ws`'s `Encode` — do **not** write a second envelope encoder. Order matters: persist before broadcasting, so no client ever learns of a round Redis does not have.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -race -count=1 -p 1 && \
  git add internal/round/ && \
  git commit -m "feat: open a round and announce it to the room"
```

Expected: PASS, then one commit.

**Checkpoint 2: only the host may open a round**

- [ ] **Step 1: Write the failing test, then run it**

Spec: room `"rm1"` hosted by `"host1"`, with `"u2"` joined. `Open(ctx, "rm1", "u2", validSpec)` returns `ErrNotHost`. Nothing was written — `CurrentRound(ctx, "rm1")` returns `ErrNotFound` — and the broadcaster recorded **zero** calls.

Run: `cd backend && go test ./internal/round/ -race -count=1 -p 1 -run TestOpenNotHost`
Expected: FAIL — any caller currently opens a round.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Open` compares `callerID` against `room.HostID` from `store.Room` and returns `ErrNotHost` before generating an ID or writing anything.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -race -count=1 -p 1 && \
  git add internal/round/service.go internal/round/service_test.go && \
  git commit -m "feat: restrict round opening to the room host"
```

Expected: PASS, then one commit.

**Checkpoint 3: a malformed spec, or a second concurrent round, is refused**

- [ ] **Step 1: Write the failing test, then run it**

Spec: table-driven over `Open` by the legitimate host, each case returning an error, writing nothing, and broadcasting nothing —
| case | spec | expected |
|---|---|---|
| empty question | `Question: "   "` (whitespace only) | `ErrInvalidSpec` |
| too few outcomes | `Outcomes: []string{"Yes"}` | `ErrInvalidSpec` |
| too many outcomes | 5 outcomes | `ErrInvalidSpec` |
| blank outcome label | `[]string{"Yes", "  "}` | `ErrInvalidSpec` |
| lock window too short | `LockIn: 1 * time.Second` | `ErrInvalidSpec` |
| lock window too long | `LockIn: 5 * time.Minute` | `ErrInvalidSpec` |

Then a separate case: after a successful `Open`, a second `Open` on the same room returns `ErrRoundInProgress`, and `CurrentRound` still points at the **first** round.

The outcome-count bounds are spec §4's "2-4 custom outcome options" and are already encoded in `domain.ValidateOutcomeCount` — call it rather than re-deriving 2 and 4 here.

Run: `cd backend && go test ./internal/round/ -race -count=1 -p 1 -run "TestOpenInvalid|TestOpenConcurrent"`
Expected: FAIL — no validation exists and a second open overwrites the index.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: validate in `Open` before any write — trim the question and each outcome label, reject empties, call `domain.ValidateOutcomeCount(len(spec.Outcomes))`, and bound `LockIn` to `[MinLockIn, MaxLockIn]`. Then `CurrentRound`: if it returns a round ID whose `Round(...).Status` is non-terminal, return `ErrRoundInProgress`; a terminal one is stale and may be replaced.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -race -count=1 -p 1 && \
  git add internal/round/service.go internal/round/service_test.go && \
  git commit -m "feat: validate round specs and refuse concurrent rounds"
```

Expected: PASS, then one commit.

**Task boundary:** `make test && make lint && make build`

---

## Task 3: Server-side lock timer

**Files:**
- Create: `backend/internal/round/timer.go`, `timer_test.go`
- Modify: `backend/internal/round/service.go`

**Interfaces — Produces:**
```go
// watch runs one round's whole server-side clock: lock at lockAt, then
// auto-refund RefundGrace later if still unresolved. Started by Open.
func (s *Service) watch(ctx context.Context, roomID, roundID string, lockAt time.Time)

const RefundGrace = 60 * time.Second
```

`Service` gains a `refundGrace time.Duration` field defaulting to `RefundGrace`, settable by tests. Tests must not sleep 60 real seconds.

**Checkpoint 1: the timer locks the round at its lock instant**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `Open` with `LockIn: 100 * time.Millisecond`. Within 2 seconds, `Round(ctx, roundID).Status == domain.RoundLocked`, and the broadcaster recorded a `round_locked` event for that room carrying the round ID. Poll for the status rather than sleeping a fixed interval.

Run: `make up && cd backend && go test ./internal/round/ -race -count=1 -p 1 -run TestTimerLocks`
Expected: FAIL — the round stays `open` forever; nothing calls `LockRound`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Open` ends with `go s.watch(ctx, roomID, roundID, lockAt)` — note this deliberately uses a background context, not the request's, so a disconnecting host does not cancel the round's clock. `watch` sleeps until `lockAt` via `time.NewTimer`, calls `store.LockRound`, tolerates `ErrRoundTerminal` as a benign race (a round resolved before its lock instant needs no lock), and broadcasts `round_locked` only when the lock actually succeeded.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -race -count=1 -p 1 && \
  git add internal/round/timer.go internal/round/service.go internal/round/timer_test.go && \
  git commit -m "feat: lock a round on a server-side timer"
```

Expected: PASS, then one commit.

**Checkpoint 2: a locked round refuses further wagers**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `Open` with `LockIn: 100 * time.Millisecond`; a joined player wagers 50 **before** the lock and succeeds. Wait for status `locked`. A second `PlaceWager` with a fresh idempotency key returns `redisstore.ErrPoolLocked`, and the player's `Balance` is unchanged from after the first wager.

This checkpoint exists to prove the timer and `place_wager.lua`'s own `TIME` check agree in practice — the lockout guarantee is spec §4's client-latency defence and is worth one integration test rather than trusting two mechanisms to line up.

Run: `cd backend && go test ./internal/round/ -race -count=1 -p 1 -run TestLockedRejectsWagers`
Expected: FAIL if the timer never locks; PASS is only meaningful once CP1 is green — if this passes before CP1's implementation, the round was already being rejected for a different reason (check the round exists and the player joined) and the test is not measuring what it claims.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: no new production code — this is a verification checkpoint over CP1's implementation plus existing Lua. If it fails, the defect is in CP1's timer or in the round's `lock_at_ms`, not here.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -race -count=1 -p 1 && \
  git add internal/round/timer_test.go && \
  git commit -m "test: verify a timer-locked round rejects late wagers"
```

Expected: PASS, then one commit.

**Checkpoint 3: locking is not attempted on an already-terminal round**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `Open` with `LockIn: 2 * time.Second`. Immediately lock and settle the round by hand through the store (`LockRound`, then `SettleRound` with a winning outcome). Wait past the 2-second mark. The round's status is still the terminal one settlement produced — **not** `locked` — and the broadcaster recorded **no** `round_locked` event.

Run: `cd backend && go test ./internal/round/ -race -count=1 -p 1 -run TestTimerSkipsTerminal`
Expected: FAIL — the timer broadcasts `round_locked` unconditionally, so a `round_locked` arrives after the round already resolved.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: in `watch`, branch on `LockRound`'s error — `errors.Is(err, redisstore.ErrRoundTerminal)` returns from `watch` without broadcasting and without arming the refund phase. Any other error is logged and also returns; a round whose lock failed for an unknown reason must not then be auto-refunded on a guess.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -race -count=1 -p 1 && \
  git add internal/round/timer.go internal/round/timer_test.go && \
  git commit -m "fix: do not lock or announce an already-terminal round"
```

Expected: PASS, then one commit.

**Task boundary:** `make test && make lint && make build`

---

## Task 4: Wager service

**Files:**
- Create: `backend/internal/wager/service.go`, `service_test.go`, `testmain_test.go`

**Interfaces — Produces:**
```go
type Request struct {
    RoomID, RoundID, UserID string
    Outcome                 int
    Amount                  domain.Tokens
    IdempotencyKey          string
}

type Accepted struct {
    Balance     domain.Tokens
    Pools       []domain.Tokens
    Total       domain.Tokens
    Multipliers []float64
    Bettors     int
    Players     int
}

type Service struct{ /* store, broadcaster */ }
func NewService(store *redisstore.Store, b round.Broadcaster) *Service
func (s *Service) Place(ctx context.Context, req Request) (Accepted, error)

var ErrNoActiveRound  = errors.New("wager: the room has no open round")
var ErrBadIdempotency = errors.New("wager: idempotency key must be a UUIDv4")

const Scope = "wager"
const Limit = 20
const Window = 10 * time.Second
```

**Checkpoint 1: an accepted wager returns the wagerer's new state**

- [ ] **Step 1: Write the failing test, then run it**

Spec: room `"rm1"` buy-in 1000, host `"host1"`, player `"u1"` joined with 1000, round open with 2 outcomes. `Place(ctx, Request{RoomID:"rm1", UserID:"u1", Outcome:0, Amount:200, IdempotencyKey: <fresh uuid>})` — note `RoundID` is left empty and resolved from the room's index — returns `Accepted{Balance: 800, Pools: []Tokens{200, 0}, Total: 200, Bettors: 1, Players: 1}` with `Multipliers` equal to `domain.Multipliers(200, []Tokens{200, 0})`.

`Players` comes from `store.PlayerCount`, which already excludes the host — assert it is 1, not 2, since `"host1"` cannot wager.

Run: `make up && cd backend && go test ./internal/wager/ -race -count=1 -p 1 -run TestPlace`
Expected: FAIL — package `wager` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Place` resolves `RoundID` from `store.CurrentRound` when the request leaves it empty, calls `store.PlaceWager`, then builds `Accepted` from the returned `WagerResult` plus `domain.Multipliers` and `store.PlayerCount`. No Go-side balance or lockout check — the Lua script is the authority.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/wager/ -race -count=1 -p 1 && \
  git add internal/wager/ && \
  git commit -m "feat: place a wager and report the wagerer's new state"
```

Expected: PASS, then one commit.

**Checkpoint 2: rejected wagers surface the store's own sentinels unchanged**

- [ ] **Step 1: Write the failing test, then run it**

Spec: table-driven, each asserting with `errors.Is` and that the wagerer's `Balance` is untouched —
| case | expected error |
|---|---|
| the host wagers in their own room | `redisstore.ErrHostCannotBet` |
| a user who never joined wagers | `redisstore.ErrNotInRoom` |
| amount exceeds the session wallet | `domain.ErrInsufficientFunds` |
| outcome index 5 on a 2-outcome round | `domain.ErrInvalidOutcome` |
| the room has no open round | `ErrNoActiveRound` |
| amount is 0 | `domain.ErrInvalidStake` (via `domain.ValidateStake`) |
| amount is negative | `domain.ErrInvalidStake` |

Confirm the exact sentinel names in `internal/domain/errors.go` before writing the table — the two stake errors are named there, and this plan must not invent a name.

Run: `cd backend && go test ./internal/wager/ -race -count=1 -p 1 -run TestPlaceRejects`
Expected: FAIL — `ErrNoActiveRound` is unhandled and the zero/negative cases reach Lua rather than being caught in Go.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Place` returns `ErrNoActiveRound` when `CurrentRound` gives `ErrNotFound`, and calls `domain.ValidateStake(req.Amount, <session balance from store.Balance>)` before reaching Lua so a zero or negative stake never costs a round trip. Every other error is `store.PlaceWager`'s, returned **unwrapped in identity** — wrap for context with `%w`, never translate one sentinel into another.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/wager/ -race -count=1 -p 1 && \
  git add internal/wager/service.go internal/wager/service_test.go && \
  git commit -m "feat: reject invalid wagers with the store's own sentinels"
```

Expected: PASS, then one commit.

**Checkpoint 3: a replayed idempotency key does not double-spend**

- [ ] **Step 1: Write the failing test, then run it**

Spec: player with 1000 places 200 with key `K` → `Balance` 800. Place the **identical** request with the same key `K` again → returns `Accepted` with `Balance` still 800, `Total` still 200, and `Bettors` still 1. The pools did not grow. Then place 200 with a *different* key → `Balance` 600, `Total` 400, `Bettors` still 1 (a repeat wagerer never moves the distinct-bettor count).

Also assert a non-UUIDv4 key (`"abc"`) returns `ErrBadIdempotency` before any Redis call.

Run: `cd backend && go test ./internal/wager/ -race -count=1 -p 1 -run TestPlaceIdempotent`
Expected: FAIL — `ErrBadIdempotency` is not implemented. The replay case itself should pass on `place_wager.lua`'s existing dedupe; if it does **not**, the defect is in Phase 2's script, not here — stop and report rather than working around it.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Place` validates the key with `uuid.Parse` and a `Version() == 4` check, returning `ErrBadIdempotency` on failure. Dedupe itself is entirely `place_wager.lua`'s — add no Go-side cache.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/wager/ -race -count=1 -p 1 && \
  git add internal/wager/service.go internal/wager/service_test.go && \
  git commit -m "feat: require a UUIDv4 idempotency key on every wager"
```

Expected: PASS, then one commit.

**Checkpoint 4: wager placement is rate limited per user**

- [ ] **Step 1: Write the failing test, then run it**

Spec: with `Limit` 20 per `Window`, a single user placing 21 wagers in immediate succession has the 21st rejected. The rejection is a distinct error carrying a retry hint — reuse `internal/httpapi`'s existing pattern for surfacing `redisstore.Decision.RetryAfter` rather than inventing a second shape; check how `apiThrottle` does it. A **different** user's 21st wager in the same window still succeeds, proving the limiter is keyed by user, not globally.

Run: `cd backend && go test ./internal/wager/ -race -count=1 -p 1 -run TestPlaceRateLimited`
Expected: FAIL — no limiter is consulted; all 21 succeed.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Place` calls `store.Allow(ctx, Scope, req.UserID, Limit, Window)` first and returns the throttle error when denied. This is the parent plan's Phase 4 note — the wager throttle's call site is this phase's to wire, and `rate_limit.lua` / `Store.Allow` already exist. Per `CLAUDE.md`, do **not** fork a second limiter.

Denied attempts must not be recorded (the script already handles this), and a denied wager must not consume the idempotency key.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/wager/ -race -count=1 -p 1 && \
  git add internal/wager/service.go internal/wager/service_test.go && \
  git commit -m "feat: throttle wager placement with the shared rate limiter"
```

Expected: PASS, then one commit.

**Task boundary:** `make test && make lint && make build`

---

## Task 5: Live odds broadcast

**Files:**
- Modify: `backend/internal/wager/service.go`, `service_test.go`

**Interfaces — Produces:**
```go
type OddsEvent struct {
    RoundID     string    `json:"round_id"`
    Pools       []int64   `json:"pools"`
    Total       int64     `json:"total"`
    Multipliers []float64 `json:"multipliers"`
    Bettors     int       `json:"bettors"`
    Players     int       `json:"players"`
}
```

**Checkpoint 1: an accepted wager broadcasts anonymous odds to the room**

- [ ] **Step 1: Write the failing test, then run it**

Spec: after a successful `Place` of 200 on outcome 0 by `"u1"`, the broadcaster recorded exactly one call for `"rm1"` decoding to `Envelope{Type: "odds_updated"}` with `OddsEvent{Pools: []int64{200, 0}, Total: 200, Bettors: 1, Players: 1}` and multipliers matching `domain.Multipliers`.

**Then assert the anonymity invariant directly:** marshal the broadcast payload to a string and assert it contains neither `"u1"` nor `"200"` in any field other than the pool and total figures — concretely, unmarshal into `map[string]any` and assert the key set is exactly `round_id, pools, total, multipliers, bettors, players`. A future field carrying a user ID would fail this immediately. This is `CLAUDE.md`'s "no payload from any phase may carry per-user wager data before the round resolves", asserted rather than assumed.

Run: `cd backend && go test ./internal/wager/ -race -count=1 -p 1 -run TestPlaceBroadcastsOdds`
Expected: FAIL — nothing is broadcast.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: after building `Accepted`, `Place` encodes an `OddsEvent` and calls `broadcast.Broadcast(req.RoomID, payload)`. Broadcast **after** the Lua call succeeds, never before — an announced wager that then failed would desynchronize every client's odds from Redis.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/wager/ -race -count=1 -p 1 && \
  git add internal/wager/service.go internal/wager/service_test.go && \
  git commit -m "feat: broadcast anonymous live odds on each accepted wager"
```

Expected: PASS, then one commit.

**Checkpoint 2: a rejected or replayed wager does not move the room's odds**

- [ ] **Step 1: Write the failing test, then run it**

Spec: two cases, each asserting the broadcaster's recorded call count.
- A rejected wager (host wagering in their own room) → **zero** broadcasts.
- A replayed idempotency key → the first `Place` broadcasts once; the replay broadcasts **again**, but with byte-identical payload to the first. Assert equality of the two payloads.

Re-broadcasting an unchanged payload on replay is deliberate: a client that retried because it never saw the first response still needs an answer, and an identical payload is idempotent for every other client in the room.

Run: `cd backend && go test ./internal/wager/ -race -count=1 -p 1 -run TestPlaceBroadcastSuppressed`
Expected: FAIL — the rejection path currently broadcasts before checking the error.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: the broadcast sits after the error check, on the success path only. No special-casing of the replay — the Lua script returns the cached result, so the same `Accepted` produces the same bytes naturally.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/wager/ -race -count=1 -p 1 && \
  git add internal/wager/service.go internal/wager/service_test.go && \
  git commit -m "fix: suppress the odds broadcast for rejected wagers"
```

Expected: PASS, then one commit.

**Task boundary:** `make test && make lint && make build`

---

## Task 6: Host resolution and the reveal

**Files:**
- Modify: `backend/internal/round/service.go`, `service_test.go`

**Interfaces — Produces:**
```go
type ResultRow struct {
    UserID      string `json:"user_id"`
    DisplayName string `json:"display_name"`
    Staked      int64  `json:"staked"`
    Returned    int64  `json:"returned"`
    Net         int64  `json:"net"`
}
type ResolvedEvent struct {
    RoundID        string      `json:"round_id"`
    WinningOutcome int         `json:"winning_outcome"`
    Results        []ResultRow `json:"results"`
    Dust           int64       `json:"dust"`
    Refunded       bool        `json:"refunded"`
}

func (s *Service) Resolve(ctx context.Context, roomID, callerID string,
    winningOutcome int) (domain.Settlement, error)
```

`DisplayName` is not in `domain.PlayerResult`. Resolve it from the room's connected clients via a new `Broadcaster` method — extend the interface to `Names(roomID string) map[string]string` and have `internal/ws` satisfy it from `Room.Members()` in Task 9. A player who disconnected before resolution has no name available; fall back to their user ID rather than dropping the row, since their payout is real either way.

**Checkpoint 1: the host resolves a locked round and everyone learns every result**

- [ ] **Step 1: Write the failing test, then run it**

Spec: round with 2 outcomes; `"u1"` stakes 100 on outcome 0, `"u2"` stakes 100 on outcome 1. Lock it. `Resolve(ctx, "rm1", "host1", 0)` returns a `domain.Settlement` where `"u1"` is paid 200 and `Dust` is 0.

The broadcaster recorded one `round_resolved` event whose `Results` has **two** rows — `"u1"` with `Staked:100, Returned:200, Net:100` and `"u2"` with `Staked:100, Returned:0, Net:-100`. The loser appears; this event is the first and only moment per-player stakes are revealed. `CurrentRound(ctx, "rm1")` now returns `ErrNotFound`.

Run: `make up && cd backend && go test ./internal/round/ -race -count=1 -p 1 -run TestResolve`
Expected: FAIL — `Resolve` undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Resolve` checks host identity, reads `CurrentRound`, generates a UUIDv4 idempotency key, calls `store.SettleRound(ctx, roundID, winningOutcome, key)` — which internally runs `domain.Settle` and applies it via Lua — then `ClearCurrentRound`, then broadcasts. Map `Settlement.Results` to `ResultRow`s. Compute nothing: `Net` and `Returned` come straight from `domain.PlayerResult`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -race -count=1 -p 1 && \
  git add internal/round/service.go internal/round/service_test.go && \
  git commit -m "feat: resolve a round and reveal every player's result"
```

Expected: PASS, then one commit.

**Checkpoint 2: resolution is refused when it would be unsafe**

- [ ] **Step 1: Write the failing test, then run it**

Spec: table-driven, each asserting the error and that no `round_resolved` was broadcast —
| case | expected error |
|---|---|
| a non-host calls Resolve | `ErrNotHost` |
| the room has no current round | `ErrNoActiveRound` |
| the round is still open (not yet locked) | `redisstore.ErrNotLocked` |
| the round already resolved | `redisstore.ErrAlreadySettled` |
| winning outcome 7 on a 2-outcome round | `domain.ErrInvalidOutcome` |

The still-open case matters most: settling a round that is still accepting wagers would race stakes arriving between `domain.Settle`'s read and Lua's write. `settle_round.lua` already refuses it — this test proves the service does not work around it.

Run: `cd backend && go test ./internal/round/ -race -count=1 -p 1 -run TestResolveRejects`
Expected: FAIL — `ErrNoActiveRound` and the outcome-range check are unimplemented.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: host check first, then `CurrentRound` → `ErrNoActiveRound` on `ErrNotFound`, then `domain.ValidateOutcomeIndex(winningOutcome, round.OutcomeCount)`. Remaining errors come from `SettleRound` and are wrapped with `%w`, never translated. On any error, do not clear the current-round index — a failed resolve must leave the round resolvable again.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -race -count=1 -p 1 && \
  git add internal/round/service.go internal/round/service_test.go && \
  git commit -m "feat: refuse unsafe round resolutions"
```

Expected: PASS, then one commit.

**Checkpoint 3: a round nobody won refunds everyone**

- [ ] **Step 1: Write the failing test, then run it**

Spec: 3-outcome round; `"u1"` stakes 100 on outcome 0, `"u2"` stakes 150 on outcome 0. Nobody backs outcome 2. Lock, then `Resolve(ctx, "rm1", "host1", 2)`.

The returned `Settlement` has `Refunded == true`. `"u1"`'s room wallet is back to its pre-wager value and so is `"u2"`'s. The `round_resolved` event carries `Refunded: true` and rows where every `Net` is 0 and each `Returned` equals that player's `Staked`.

This is parent plan §5's `pool_W == 0` edge case. `domain.Settle` already implements it at 100% coverage — this checkpoint proves the service surfaces it rather than reimplementing it.

Run: `cd backend && go test ./internal/round/ -race -count=1 -p 1 -run TestResolveNobodyWon`
Expected: PASS immediately is possible here, since `domain.Settle` and `settle_round.lua` already handle it and CP1's mapping is generic. **If it passes on first run, verify it is a genuine cycle** by temporarily hardcoding `Refunded: false` in the event mapping, confirming the test fails, then restoring — the same procedure Phase 3 used for its two anticipated-pass checkpoints. Record which you observed in the commit body.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Refunded` is copied from `Settlement.Refunded` into the event. No branch, no special path — that is the point.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -race -count=1 -p 1 && \
  git add internal/round/service.go internal/round/service_test.go && \
  git commit -m "test: verify a round nobody won refunds every stake"
```

Expected: PASS, then one commit.

**Task boundary:** `make test && make lint && make build`

---

## Task 7: Auto-refund fallback

**Files:**
- Modify: `backend/internal/round/timer.go`, `timer_test.go`

**Checkpoint 1: an unresolved round auto-refunds after the grace period**

- [ ] **Step 1: Write the failing test, then run it**

Spec: service constructed with `refundGrace` of 300 ms. `Open` with `LockIn: 100 * time.Millisecond`; `"u1"` stakes 100 and `"u2"` stakes 150. Let the timer lock it and let the grace elapse without resolving.

Within 3 seconds: `Round(...).Status == domain.RoundRefunded`, both players' room wallets are back to their pre-wager values, and the broadcaster recorded a `round_refunded` event carrying the round ID and the total refunded (250). `CurrentRound(ctx, "rm1")` returns `ErrNotFound`.

This is spec §4's host-disconnect path — the host vanishing must not strand everyone's tokens in a pool forever.

Run: `make up && cd backend && go test ./internal/round/ -race -count=1 -p 1 -run TestAutoRefund`
Expected: FAIL — `watch` returns after locking; the round sits locked forever.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: after a successful lock, `watch` waits `s.refundGrace`, re-reads the round, and returns without acting if its status is already terminal. Otherwise it calls `store.RefundRound` with a fresh UUIDv4 key, calls `ClearCurrentRound`, and broadcasts `round_refunded`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -race -count=1 -p 1 && \
  git add internal/round/timer.go internal/round/timer_test.go && \
  git commit -m "feat: auto-refund a round left unresolved past the grace period"
```

Expected: PASS, then one commit.

**Checkpoint 2: a resolved round is never auto-refunded**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `refundGrace` 300 ms, `LockIn` 100 ms. Stake, let it lock, then `Resolve` immediately. Wait past the grace period.

The round's status stays whatever settlement produced, the winner's wallet still holds their payout (it was **not** clawed back by a refund), and the broadcaster recorded **no** `round_refunded` event. Assert the winner's exact balance — a double-credit here is the worst possible bug in this phase, and asserting only the status would miss it.

Run: `cd backend && go test ./internal/round/ -race -count=1 -p 1 -run TestNoRefundAfterResolve`
Expected: FAIL — the grace branch refunds unconditionally.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: the status re-read added in CP1 is the guard. Belt and braces: also tolerate `redisstore.ErrAlreadySettled` from `RefundRound` as a benign no-op rather than an error, since the round could resolve in the window between the re-read and the call. Both layers are wanted — the read avoids the call, the error tolerance survives the race.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -race -count=1 -p 1 && \
  git add internal/round/timer.go internal/round/timer_test.go && \
  git commit -m "fix: never auto-refund a round that already resolved"
```

Expected: PASS, then one commit.

**Checkpoint 3: shutdown stops every in-flight round timer**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `Open` a round with `LockIn: 5 * time.Second` against a service built with a cancellable context. Cancel it. Within 1 second the round's status is still `open` — no lock was attempted — and no event was broadcast. Assert with `goleak`-style discipline instead if preferred, but the observable assertion above is sufficient and needs no new dependency.

Without this, `cmd/api` exiting would leave round goroutines mid-sleep and a `make test` run would leak one goroutine per round created.

Run: `cd backend && go test ./internal/round/ -race -count=1 -p 1 -run TestTimerCancelled`
Expected: FAIL — `watch` sleeps on a bare timer and ignores context cancellation.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Service` holds a base `context.Context` supplied to `NewService` (add the parameter; update callers). Both waits in `watch` become `select { case <-timer.C: case <-ctx.Done(): return }`, with `timer.Stop()` deferred.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -race -count=1 -p 1 && \
  git add internal/round/timer.go internal/round/service.go internal/round/*_test.go && \
  git commit -m "feat: cancel in-flight round timers on shutdown"
```

Expected: PASS, then one commit.

**Task boundary:** `make test && make lint && make build`

---

## Task 8: Session-end persistence

**Files:**
- Create: `backend/internal/round/session.go`, `session_test.go`

**Interfaces — Produces:**
```go
// EndSession folds a departing account holder's net session result into
// their persistent balance. A guest has no persistent balance and is a
// no-op returning (0, nil).
func (s *Service) EndSession(ctx context.Context, roomID, userID string,
    guest bool) (newBalance domain.Tokens, err error)
```

**Checkpoint 1: an account holder's net session result carries to their account**

- [ ] **Step 1: Write the failing test, then run it**

Spec: account `"u1"` with persistent balance 5000 joins room `"rm1"` (buy-in 1000) — `AccountSessionBalance` grants 1000, and `OpeningStake` records 1000. Through wagering their room wallet reaches 1600.

`EndSession(ctx, "rm1", "u1", false)` returns `5600` — the persistent balance plus the **net** 600, not the session's 1600 — and `store.User(ctx, "u1").Balance` is 5600. This is spec §3's "net profit/loss, not final balance", and `domain.ApplySessionResult` is the only thing permitted to compute it.

Second case: a session ending at 400 against an opening 1000 gives `5000 - 600 = 4400`.

Run: `make up && cd backend && go test ./internal/round/ -race -count=1 -p 1 -run TestEndSession`
Expected: FAIL — `EndSession` undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: read the persistent balance from `store.User`, the opening stake from `store.OpeningStake`, and the current wallet from `store.Balance`; call `domain.ApplySessionResult(accountBalance, opening, current)`; persist the result. Do not reuse `TopUpBalance` — that sets a floor-style target and would refuse to write a *decrease*. Add a plain `SetBalance(ctx, userID string, balance domain.Tokens) error` to `redisstore` in this checkpoint.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -race -count=1 -p 1 && \
  git add internal/round/session.go internal/round/session_test.go \
          internal/redisstore/user.go internal/redisstore/user_test.go && \
  git commit -m "feat: fold a session's net result into the persistent balance"
```

Expected: PASS, then one commit.

**Checkpoint 2: guests and absent players are no-ops**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `EndSession(ctx, "rm1", "guest1", true)` on a joined guest returns `(0, nil)` and writes no `user:{id}` hash — assert `store.User(ctx, "guest1")` still returns `ErrNotFound`. A guest has no persistent identity at all, so there is nothing to fold into.

Then `EndSession(ctx, "rm1", "u-never-joined", false)` returns `redisstore.ErrNotFound` and mutates nothing.

Run: `cd backend && go test ./internal/round/ -race -count=1 -p 1 -run TestEndSessionGuest`
Expected: FAIL — the guest path reads `store.User` and errors, or worse, creates a hash.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `if guest { return 0, nil }` as the first statement, before any read.

**Known limitation to record in the commit body:** `EndSession` fires on socket disconnect, so a player who drops and reconnects ends their session and starts a new one at the room's buy-in. Reconnect-with-session-resume is deferred to Phase 7 hardening; it needs a grace window this phase does not have.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -race -count=1 -p 1 && \
  git add internal/round/session.go internal/round/session_test.go && \
  git commit -m "feat: skip session settlement for guests"
```

Expected: PASS, then one commit.

**Task boundary:** `make test && make lint && make build`

---

## Task 9: Message routing

**Files:**
- Create: `backend/internal/ws/router.go`, `router_test.go`
- Modify: `backend/internal/ws/hub.go` (satisfy `round.Broadcaster`)

**Interfaces — Produces:**
```go
// Hub satisfies round.Broadcaster.
func (h *Hub) Broadcast(roomID string, payload []byte)
func (h *Hub) Names(roomID string) map[string]string

type Router struct{ /* rounds, wagers, hub */ }
func NewRouter(rounds *round.Service, wagers *wager.Service) *Router
func (r *Router) Handle(c *Client, e Envelope) // a ws.MessageHandler
```

Import direction: `ws` imports `round` and `wager`; neither imports `ws`. `round.Broadcaster` is defined in `round` and satisfied by `*Hub` — that is why `round` does not need the `ws` import, and it must stay that way or the build cycles.

**Checkpoint 1: inbound message types reach the right service**

- [ ] **Step 1: Write the failing test, then run it**

Spec: table-driven over `Handle` with a stub `round.Service`/`wager.Service` (interfaces declared in `router.go` and satisfied by the real services) —
| inbound type | routed to | with |
|---|---|---|
| `create_round` | `rounds.Open` | the client's `UserID` as caller, the payload's question/outcomes/lock_in_ms |
| `place_wager` | `wagers.Place` | the client's `UserID`, the payload's outcome/amount/idempotency_key |
| `resolve_round` | `rounds.Resolve` | the client's `UserID`, the payload's winning_outcome |

In every case the room ID comes from the **client's verified token claim**, never from the message payload. Assert this explicitly: send a `place_wager` whose payload includes `"room_id": "someone-elses-room"` and assert the service was called with the client's own room.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestRouterDispatch`
Expected: FAIL — `NewRouter` undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Handle` switches on `e.Type`, unmarshals `e.Data` into the matching payload struct, and calls the service with `c.UserID` and the client's room. `Client` gains a `RoomID` field set by `Handler` from the claims in Phase 4a's Task 6 — add it if 4a did not.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/router.go internal/ws/router_test.go internal/ws/client.go && \
  git commit -m "feat: route inbound socket messages to the round and wager services"
```

Expected: PASS, then one commit.

**Checkpoint 2: a service error answers the sender privately**

- [ ] **Step 1: Write the failing test, then run it**

Spec: stub `wagers.Place` to return `redisstore.ErrHostCannotBet`. `Handle` a `place_wager`. The client's own `send` channel receives one `Envelope{Type: TypeError}` with `ErrorEvent{Code: "host_cannot_bet"}`, and the hub broadcast **nothing** — an error belongs to the sender, not the room.

Table the code mapping: `ErrHostCannotBet`→`host_cannot_bet`, `ErrPoolLocked`→`pool_locked`, `ErrNotInRoom`→`not_in_room`, `domain.ErrInsufficientFunds`→`insufficient_funds`, `domain.ErrInvalidOutcome`→`invalid_outcome`, `round.ErrNotHost`→`not_host`, `round.ErrRoundInProgress`→`round_in_progress`, `wager.ErrNoActiveRound`→`no_active_round`, `wager.ErrBadIdempotency`→`bad_idempotency_key`. Anything unrecognized → `internal_error` with a generic message and the real error logged, never returned.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestRouterErrors`
Expected: FAIL — errors are currently dropped.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: one `errorCode(err error) string` helper using `errors.Is` in the table's order. The generic fallback is deliberate — leaking an internal error string to a client is the information disclosure `internal/httpapi`'s error envelope already avoids in Phase 3.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/router.go internal/ws/router_test.go && \
  git commit -m "feat: answer socket errors privately with a stable code"
```

Expected: PASS, then one commit.

**Checkpoint 3: the hub broadcasts by room ID and names its members**

- [ ] **Step 1: Write the failing test, then run it**

Spec: two clients in `"r1"`, one in `"r2"`. `hub.Broadcast("r1", payload)` → both `"r1"` clients' `send` channels receive it, the `"r2"` client's does not. `hub.Broadcast("nonexistent", payload)` does not panic. `hub.Names("r1")` returns `map[string]string{"u1":"Ada","u2":"Grace"}`; `Names("nonexistent")` returns an empty non-nil map.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestHubBroadcastByID`
Expected: FAIL — `Hub.Broadcast` and `Hub.Names` undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: both are hub commands with reply channels, looking the room up in the registry and delegating to `Room.Broadcast` / `Room.Members`. A missing room is a silent no-op for `Broadcast` — a round settling just as its last player left is normal, not an error.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/hub.go internal/ws/hub_test.go && \
  git commit -m "feat: broadcast and resolve display names by room ID"
```

Expected: PASS, then one commit.

**Task boundary:** `make test && make lint && make build`

---

## Task 10: CLI client

**Files:**
- Create: `backend/cmd/callit-cli/main.go`

This is the parent plan's "playable end to end from a CLI client". It is a demo vehicle, not a product surface — `cmd/` wiring carries no unit tests here, consistent with `cmd/api`'s documented 0% coverage.

**Checkpoint 1: the CLI plays a full round against a live server**

- [ ] **Step 1: Write the failing test, then run it**

Spec: an end-to-end test in `internal/ws/e2e_test.go` — not in `cmd/` — driving the *library* paths the CLI uses, against a real `httptest` server with a real hub, real services, and real Redis:

1. Host registers and creates a room over REST; two players join by code over REST.
2. All three open sockets with their room-scoped tokens.
3. Host sends `create_round` with 2 outcomes and `lock_in_ms: 500`. All three receive `round_opened`.
4. Player A wagers 100 on outcome 0; player B wagers 200 on outcome 1. Each wager produces an `odds_updated` all three receive, with `bettors` reaching 2 and `players` equal to 2.
5. All three receive `round_locked` within 2 seconds.
6. Host sends `resolve_round` with `winning_outcome: 0`. All three receive `round_resolved` with A's `Net` of +200 and B's of -200.
7. Assert token conservation: A's wallet + B's wallet + `Dust` equals their combined opening stakes.

Step 7 is the phase's real acceptance test. Everything before it is setup.

Run: `make up && cd backend && go test ./internal/ws/ -race -count=1 -p 1 -run TestEndToEndRound`
Expected: FAIL — the full path has never run together; expect the first failure at whichever wiring seam is weakest.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: fix whatever the e2e run exposes. Add no new behavior — every piece is built by Tasks 1–9, and this checkpoint exists to prove they compose. If it demands a genuinely new capability, that is a plan defect: stop and report it rather than growing the task.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 -p 1 && \
  git add internal/ws/e2e_test.go && \
  git commit -m "test: play a full round end to end over the socket"
```

Expected: PASS, then one commit.

**Checkpoint 2: the CLI binary drives that same flow interactively**

- [ ] **Step 1: Write the failing test, then run it**

Spec: no automated test — this checkpoint's verification is manual and its evidence is a transcript. Build the binary and run it against a live server:

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make up && cd backend && \
  go build -o /tmp/callit-cli ./cmd/callit-cli && \
  JWT_SECRET=$(openssl rand -hex 32) go run ./cmd/api
```

Then, in two more shells, `/tmp/callit-cli --host` and `/tmp/callit-cli --join <code>`.

Expected: FAIL — `cmd/callit-cli` does not exist, so the build fails.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: flags `--addr` (default `localhost:8080`), `--host` (register, create a room, print the code) or `--join <code>` (join as a guest with a prompted display name). After connecting: print every inbound event in a readable line; accept `round <question> | <outcome> | <outcome>` from the host, `bet <outcome-index> <amount>` from a player (generating a fresh UUIDv4 per bet), and `resolve <outcome-index>` from the host. Keep it under 250 lines — it is a demo harness.

Paste the session transcript into the commit body as the evidence for this checkpoint.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go build ./... && go vet ./... && \
  git add cmd/callit-cli/main.go && \
  git commit -m "feat: add a CLI client that plays a full round"
```

Expected: build and vet clean, then one commit.

**Task boundary:** `make test && make lint && make build`

---

## Task 11: Wiring and close-out

**Files:**
- Modify: `backend/internal/httpapi/ws_handlers.go`, `backend/cmd/api/main.go`
- Modify: `docs/plans/2026-08-21-implementation-plan.md`, `docs/specs/2026-08-21-callit-design.md`

**Checkpoint 1: the running server serves the real router**

- [ ] **Step 1: Write the failing test, then run it**

Spec: in `internal/httpapi`, build a mux whose `Deps` carry a hub, a round service, and a wager service. Dial the socket with a room-scoped token and send `{"type":"place_wager", ...}`. The reply is an `error` envelope with code `no_active_round` — **not** `unknown_type`. That single code difference is the whole proof that Phase 4a's nil seam has been replaced by the real router.

Run: `make up && cd backend && go test ./internal/httpapi/ -race -count=1 -p 1 -run TestSocketRoutesToServices`
Expected: FAIL — the reply is `unknown_type`; `Deps` has no round or wager service.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Deps` gains `Rounds *round.Service` and `Wagers *wager.Service`. `registerWSRoutes` builds `ws.NewRouter(d.Rounds, d.Wagers)` and passes its `Handle` as the `MessageHandler` in place of 4a's `nil`. `cmd/api/main.go` constructs both services with the hub as their `Broadcaster` and a cancellable context, wires the session-end callback into the hub's disconnect path, and cancels that context before `hub.Shutdown()`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/httpapi/ -race -count=1 -p 1 && \
  git add internal/httpapi/ws_handlers.go internal/httpapi/*_test.go cmd/api/main.go && \
  git commit -m "feat: serve the real message router from the process mux"
```

Expected: PASS, then one commit.

**Checkpoint 2: close out the phase and the MVP**

- [ ] **Step 1: Verify the whole branch is green**

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  make test && make lint && make build
```

Expected: PASS on all three. Then coverage: `cd backend && go test ./... -coverpkg=./... -count=1 -p 1`. `internal/round` and `internal/wager` must clear 80%; use the `-coverpkg` profile, not the per-package figure, per `CLAUDE.md`'s note on that measurement artifact.

**Run the `security-reviewer` agent** against `internal/round`, `internal/wager`, and `internal/ws` before closing out — this phase handles money movement and a new authenticated transport, which is a `.claude/rules/ecc/common/code-review.md` mandatory trigger. Fix CRITICAL and HIGH findings before the final commit; record LOW findings and their disposition.

- [ ] **Step 2: Record outcomes, then commit**

Contract:
- `docs/plans/2026-08-21-implementation-plan.md` §9: mark 4a and 4b complete. Update §4's key-schema table with `room:{roomID}:round` and `room:{roomID}:opening` (Amendments D2/D3) and the round hash's new `question`/`outcomes` fields (D4). Note that §12's first acceptance box — "Phases 0–4 complete, producing an end-to-end playable round" — is now satisfied, with Task 10 CP1 as the evidence.
- `docs/specs/2026-08-21-callit-design.md` §4: record that round control travels over the socket (Amendment D1), and the reconnect limitation from Task 8 CP2.
- Append the **Measured** table to this plan: plan lines, checkpoints, lines/checkpoint, commits landed vs. the 31 planned, and how many checkpoints had to be un-batched. Compare against Phase 4a's row — two phases under `writing-plans-tuned` is enough evidence to decide whether Overrides A–C merge into `writing-plans` or the experiment is deleted.

```bash
git add docs/plans/2026-08-21-implementation-plan.md docs/specs/2026-08-21-callit-design.md \
        docs/plans/2026-08-26-phase-4b-round-lifecycle.md && \
  git commit -m "docs: close out Phase 4b and the MVP acceptance criteria"
```

Expected: one commit. **The branch is green and verified — stop here.** `executing-plans` Step 3 hands off to `finishing-a-development-branch`; do not merge from this plan.

---

## Self-Review

**1. Spec coverage.** §4: host-authored question and 2–4 outcomes (T2 CP1/CP3), host-cannot-wager (T4 CP2, enforced in existing Lua), server-side lockout (T3 CP1/CP2), host manual resolution with pari-mutuel settlement (T6 CP1), host-disconnect 60-second auto-refund (T7 CP1), anonymity until terminal (T5 CP1 asserts the payload's key set; T6 CP1 is the reveal), the N/M progress counter (T4 CP1 via `BettorCount`/`PlayerCount`). §5: wager over authenticated socket with idempotency key (T4 CP3), atomic Lua write with outbox `XADD` (existing, unchanged). §3: net-delta session persistence with the floor at 0 (T8 CP1), guests have no persistent balance (T8 CP2). §7's <15 ms wager target motivates D1 but is measured in Phase 7, not here.

**2. Placeholder scan.** No "TBD" or "handle errors appropriately". Two places deliberately send the executor to read existing code rather than restating it, and both name the exact file: `domain.ErrInvalidStake`'s precise sentinel name (T4 CP2 → `internal/domain/errors.go`) and the throttle-error shape (T4 CP4 → `httpapi`'s `apiThrottle`). Those are lookups, not gaps — inventing either name here is what would create a gap.

**3. Type consistency.** `Broadcaster` is declared once in `round` and satisfied by `*ws.Hub` (T9 CP3); `wager` consumes the same interface, so there is one broadcast surface, not two. `redisstore` sentinels are passed through unwrapped in identity by both services, so the router's `errorCode` table (T9 CP2) matches exactly what T4 CP2 and T6 CP2 assert. `domain.Tokens` is the type everywhere internally, narrowing to `int64` only in JSON payload structs. `CreateRound`'s signature changes once, in T1 CP1, and every later reference uses the new one.

**4. Checkpoint realness.** Three checkpoints could pass on first run and each says so with a specific procedure: T3 CP2 (verification over T3 CP1's implementation — the plan states what a premature pass means), T6 CP3 (`domain.Settle` already handles the no-winner case — the plan prescribes the disable-and-confirm procedure Phase 3 used, and requires recording which was observed), and T10 CP1 (composition of parts already built). Naming them in advance is the Phase 3 lesson applied; T3 CP2 and T10 CP1 are the *interaction* pattern Phase 3's retrospective flagged as not yet named in `writing-plans`.

**5. Where this plan stops.** At "the branch is green and verified" (T11 CP2). No merge, push, or PR step appears.
