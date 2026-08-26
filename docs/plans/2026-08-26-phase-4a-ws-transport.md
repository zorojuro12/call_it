# Phase 4a — WebSocket Transport Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the room-scoped WebSocket transport — authenticated upgrade, per-room owner goroutine, client read/write pumps, heartbeat, slow-client eviction, and presence — with zero knowledge of rounds, wagers, or money.

**Architecture:** A `Hub` owns a registry of `Room`s; each `Room` is a goroutine that owns its client set and receives commands over a channel (no mutexes anywhere in this package). Each connected `Client` has a bounded send channel drained by a write pump and fed by a read pump; a client whose buffer overflows is evicted rather than allowed to stall the room's broadcast. Gameplay arrives in Phase 4b through one injected seam, `MessageHandler`, which this phase defines and defaults to an unknown-type responder.

**Tech Stack:** Go 1.22.10 · `github.com/gorilla/websocket` v1.5.3 (new dependency) · existing `internal/auth` for JWT verification. No Redis, no Kafka, no PostgreSQL on this phase's paths.

**Spec:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md) §6 (auth over the socket), §7 (<30 ms sync target). Parent plan: [`docs/plans/2026-08-21-implementation-plan.md`](2026-08-21-implementation-plan.md) §9 Phase 4 row, §10 risk row "Slow WebSocket client stalls room broadcast".

**Plan budget:** 24 checkpoints, target ≤60 lines/checkpoint.

---

## Amendment C1 — Phase 4 is split into 4a and 4b

The parent plan's §9 table has a single Phase 4 ("WebSocket hub + round lifecycle"). Scoping it produced ~13 tasks / ~38 checkpoints — the shape the parent plan's own **Phase-sizing note** warns against, written after Phase 3 landed at 2,904 lines and exhausted a full token window.

Split at the layer boundary:

- **Phase 4a (this plan)** — transport. No game knowledge. Deliverable: two clients connect to a room with real tokens, see each other join and leave, a stalled client is evicted without stalling the room, and the connection survives on heartbeat.
- **Phase 4b (next plan)** — rounds over this transport: round create, server-side lock timer, wager → live odds + bettor count, host resolve → settlement reveal, 60-second auto-refund, session-end persistence, CLI client.

The seam is `MessageHandler` (Task 4) plus `Room.Broadcast` (Task 2). 4b supplies a handler and calls Broadcast; it does not modify this package's transport internals.

**A second reason for the split, beyond plan length:** 4a is not money code. A bug here drops a connection; a bug in 4b's wager path loses tokens and breaks the 0.00% double-spend claim. Kept in one phase, both must run at money-code rigor.

**Action for the executor:** the parent plan's §9 table is updated as the final checkpoint of Task 7. Do not edit it earlier — an amendment recorded before the work lands describes an intention, not an outcome.

## Amendment C2 — the socket route carries no room in its path

`GET /api/v1/socket`, not `GET /api/v1/rooms/{code}/socket`. The room is read **solely** from the verified JWT's `RoomID` claim, which `internal/room`'s `Join`/`Create` already put there (spec §6, Amendment B5).

A path segment would introduce a second, unauthenticated source of the same fact, and every such endpoint has to answer "what if the path says room B and the token says room A?" — a check that can be forgotten. With one source there is no mismatch to check. This is a deliberate departure from the resource-oriented naming `api-design` drove in Phase 3, and it is confined to this one endpoint.

---

## Global Constraints

- **Go 1.22.10.** `gorilla/websocket` v1.5.3 declares `go 1.12` in its `go.mod` — **safe**, no toolchain-pin risk. This is *not* the go-redis / `x/crypto` situation described in `CLAUDE.md`; no version pin commentary is needed for it.
- **Never run `go mod tidy` before an import of the new package exists.** Phase 3 lost its new dependencies exactly this way (`go get` immediately followed by `tidy`), and Phase 2's journal had already warned about it. Task 1 CP1 adds the dependency in the same checkpoint that first imports it.
- **`internal/ws` uses no mutexes.** Room and hub state is owned by exactly one goroutine each and mutated only through its command channel (parent plan §9). A `sync.Mutex` appearing in this package is a design failure, not an optimization.
- **`internal/domain` stays free of I/O.** This phase does not touch it.
- **No wager data crosses this transport in this phase.** The anonymity invariant (`CLAUDE.md`) binds Phase 4. Presence events carry `user_id`, `display_name`, and a room-wide `player_count` — who is *present*, never who wagered what. That is exactly the data the "N/M players" denominator needs and is explicitly permitted.
- **All tests run with `-race`.** This package is concurrent by construction; a non-race run proves very little here.
- **Checkpoint test commands are package-scoped**, run from `backend/`. The full suite runs only at task boundaries. Never put `make test` inside a checkpoint.
- **`export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin` before any Go command** — `go` is installed user-locally and `~/.bashrc` only loads for interactive shells (`CLAUDE.md`, Known Environment Gotchas).
- **No Redis needed for Tasks 1–6.** Task 7 CP3 builds an `httpapi` mux, whose existing test helpers may require Redis; run `make up` before that checkpoint only.
- Commit format `type: description`. One checkpoint, one commit.

---

## File Structure

| File | Responsibility |
|---|---|
| `backend/internal/ws/protocol.go` | Envelope encode/decode, event type constants, event payload structs |
| `backend/internal/ws/protocol_test.go` | Envelope tests |
| `backend/internal/ws/room.go` | Per-room owner goroutine: join, leave, broadcast, eviction, empty-notification |
| `backend/internal/ws/room_test.go` | Room tests (no network — `Client` values with plain channels) |
| `backend/internal/ws/client.go` | `Conn` port, `Identity`, `ClientConfig`, `Client`, read pump, write pump |
| `backend/internal/ws/client_test.go` | Pump tests against a stub `Conn` |
| `backend/internal/ws/hub.go` | Room registry goroutine: get-or-create + join, empty-room reaping, shutdown |
| `backend/internal/ws/hub_test.go` | Hub tests |
| `backend/internal/ws/handler.go` | HTTP upgrade handler: token extraction, verification, client wiring, presence |
| `backend/internal/ws/handler_test.go` | Handshake + presence tests against a real `httptest` server |
| `backend/internal/httpapi/ws_handlers.go` | Route registration for `GET /api/v1/socket` |
| `backend/internal/httpapi/ws_handlers_test.go` | Route-is-wired test |
| `backend/internal/httpapi/health.go` | **Modify** — add `Hub` to `Deps`, call `registerWSRoutes` in `NewMux` |
| `backend/cmd/api/main.go` | **Modify** — construct the hub, pass it in `Deps`, shut it down on signal |

---

## Task 1: Message envelope

**Files:**
- Create: `backend/internal/ws/protocol.go`, `backend/internal/ws/protocol_test.go`
- Modify: `backend/go.mod`, `backend/go.sum`

**Interfaces — Produces:**
```go
type Envelope struct {
    Type string          `json:"type"`
    Data json.RawMessage `json:"data,omitempty"`
}

const (
    TypeConnected    = "connected"
    TypePlayerJoined = "player_joined"
    TypePlayerLeft   = "player_left"
    TypeError        = "error"
)

var ErrMalformed   = errors.New("ws: malformed message envelope")
var ErrMissingType = errors.New("ws: message envelope has no type")

func Encode(msgType string, data any) ([]byte, error)
func Decode(raw []byte) (Envelope, error)

type ConnectedEvent struct {
    UserID      string `json:"user_id"`
    DisplayName string `json:"display_name"`
    RoomID      string `json:"room_id"`
    Guest       bool   `json:"guest"`
}
type PresenceEvent struct {
    UserID      string `json:"user_id"`
    DisplayName string `json:"display_name"`
    PlayerCount int    `json:"player_count"`
}
type ErrorEvent struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}
```

**Checkpoint 1: an encoded envelope decodes back to the same type and payload**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `Encode(TypeConnected, ConnectedEvent{UserID: "u1", DisplayName: "Ada", RoomID: "r1", Guest: false})` returns bytes that are valid JSON with top-level keys exactly `type` and `data`. Feeding those bytes to `Decode` returns `Envelope{Type: "connected"}` whose `Data` unmarshals into a `ConnectedEvent` equal to the original. Also assert `Encode(TypeError, ErrorEvent{Code: "unknown_type", Message: "x"})` round-trips the same way — two payload shapes through one code path.

Add the dependency **in this step**, before writing the file that imports it (the test file itself imports only `encoding/json`, so the dependency is not strictly needed until Task 3 — add it now anyway so `go.mod` never sits in a state where `tidy` would strip it):

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go get github.com/gorilla/websocket@v1.5.3
```

Run: `cd backend && go test ./internal/ws/ -race -count=1`
Expected: FAIL — the package does not exist yet (`no required module provides package .../internal/ws`).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Encode` marshals `data` to JSON, wraps it in an `Envelope` with the given type, and marshals that. `Decode` unmarshals into an `Envelope`. The constants and payload structs are as declared in Interfaces above. Package doc comment states that this package holds CallIt's WebSocket transport and carries no game state.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/protocol.go internal/ws/protocol_test.go go.mod go.sum && \
  git commit -m "feat: add WebSocket message envelope encode/decode"
```

Expected: PASS, then one commit.

**Checkpoint 2: a malformed or type-less envelope is rejected**

- [ ] **Step 1: Write the failing test, then run it**

Spec: table-driven over `Decode`'s input —
| input | expected error |
|---|---|
| `[]byte("{not json")` | `ErrMalformed` |
| `[]byte("[]")` | `ErrMalformed` (a JSON array is not an envelope) |
| `[]byte(`{"data":{}}`)` | `ErrMissingType` |
| `[]byte(`{"type":"","data":{}}`)` | `ErrMissingType` |

Assert with `errors.Is`. On any error the returned `Envelope` must be the zero value.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestDecode`
Expected: FAIL — `Decode` currently returns `json.SyntaxError`, not `ErrMalformed`, and accepts an empty type.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Decode` wraps any `json.Unmarshal` failure as `fmt.Errorf("ws: decode: %w", ErrMalformed)`, then rejects an empty `Type` with `ErrMissingType`, returning `Envelope{}` in both cases.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/protocol.go internal/ws/protocol_test.go && \
  git commit -m "feat: reject malformed and type-less message envelopes"
```

Expected: PASS, then one commit.

**Task boundary:** `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make lint && make build`

---

## Task 2: Room owner goroutine

**Files:**
- Create: `backend/internal/ws/room.go`, `backend/internal/ws/room_test.go`
- Create: `backend/internal/ws/client.go` (the `Identity` and `Client` value only — pumps arrive in Tasks 3–4)

**Interfaces — Produces:**
```go
type Identity struct {
    UserID      string
    DisplayName string
    Guest       bool
}

type Client struct {
    Identity
    conn Conn        // nil is legal in this task's tests; pumps arrive in Task 3
    send chan []byte
}

func newClient(conn Conn, ident Identity, sendBuffer int) *Client

type Room struct {
    ID string
    // unexported: cmds chan any
}

func NewRoom(id string, onEmpty func(roomID string)) *Room
func (r *Room) Join(c *Client)
func (r *Room) Leave(c *Client)
func (r *Room) Broadcast(payload []byte)
func (r *Room) Members() []Identity
func (r *Room) Count() int
```

`Conn` is used by `Client` here but only exercised from Task 3 onward. Declare it in `client.go` now, in full, exactly as Task 3's Interfaces block gives it — do **not** stub it as an empty placeholder interface and widen it later, which would let Task 2 and Task 3 disagree about the same type. Nothing implements it in this task; the tests here pass `nil` for `conn`.

**Design note for the executor:** `Members`/`Count` are implemented as commands carrying a reply channel, serviced by the same `run()` goroutine that owns the client map. That is what makes a mutex unnecessary — do not add a `sync.RWMutex` "just for reads".

**Checkpoint 1: a joined client appears in the room's membership**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `NewRoom("r1", nil)` then `Join(newClient(nil, Identity{UserID: "u1", DisplayName: "Ada"}, 4))`. `Count()` returns `1`; `Members()` returns a one-element slice equal to `[]Identity{{UserID: "u1", DisplayName: "Ada"}}`. Joining the same `*Client` pointer twice still leaves `Count() == 1` (the client set is a map keyed by pointer).

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestRoom`
Expected: FAIL — `NewRoom` undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `NewRoom` allocates the command channel, starts `go r.run()`, and returns. `run()` owns `map[*Client]struct{}` and selects over commands: `joinCmd{c}`, `membersCmd{reply chan []Identity}`, `countCmd{reply chan int}`. `Members` returns identities in unspecified order — the test must sort or compare as a set.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/room.go internal/ws/room_test.go internal/ws/client.go && \
  git commit -m "feat: add per-room owner goroutine with join and membership snapshot"
```

Expected: PASS, then one commit.

**Checkpoint 2: leaving removes a member, and leaving twice is harmless**

- [ ] **Step 1: Write the failing test, then run it**

Spec: join two clients, `Leave(first)` → `Count() == 1` and `Members()` contains only the second. Then `Leave(first)` a second time → still `Count() == 1`, and the call does not panic. Then `Leave` a client that never joined → `Count()` unchanged, no panic.

The double-leave case is not defensive padding: Task 2 CP4's eviction removes a client from the map, and Task 4 CP4's read-pump close path calls `Leave` for that same client afterwards. Both will run against one connection.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestRoomLeave`
Expected: FAIL — `Leave` undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `leaveCmd{c}` deletes `c` from the map **only if present**, and closes `c.send` in the same branch. A non-member leave is a no-op that touches nothing — this is what makes closing `send` here safe from a double-close panic.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/room.go internal/ws/room_test.go && \
  git commit -m "feat: remove room members on leave, idempotently"
```

Expected: PASS, then one commit.

**Checkpoint 3: a broadcast reaches every member's send channel**

- [ ] **Step 1: Write the failing test, then run it**

Spec: join three clients with `sendBuffer` 4. `Broadcast([]byte("hello"))`. Each client's `send` channel yields exactly `[]byte("hello")` and nothing more. Broadcasting twice yields both payloads to each client in order.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestRoomBroadcast`
Expected: FAIL — `Broadcast` undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `broadcastCmd{payload}` iterates the client map and delivers `payload` to each `c.send`. Delivery is non-blocking (see CP4) — implement the `select`/`default` shape now and let CP4 pin the eviction behavior.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/room.go internal/ws/room_test.go && \
  git commit -m "feat: broadcast payloads to every room member"
```

Expected: PASS, then one commit.

**Checkpoint 4: a client whose buffer is full is evicted, and the rest still receive**

- [ ] **Step 1: Write the failing test, then run it**

Spec: this is the parent plan §10 risk row "Slow WebSocket client stalls room broadcast". Join `slow` with `sendBuffer` 1 and `fast` with `sendBuffer` 8; nothing drains `slow`. Broadcast twice.

- First broadcast fills `slow`'s single slot.
- Second broadcast finds it full → `slow` is evicted: `Count() == 1`, `Members()` contains only `fast`, and `slow.send` is closed (a second receive yields `ok == false` after draining the one buffered payload).
- `fast` receives **both** payloads — the broadcast was never blocked by `slow`.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestRoomEvicts`
Expected: FAIL — with a blocking send the test deadlocks and the run times out; with a bare `default: continue` the client is never evicted and `Count()` stays 2.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: inside `broadcastCmd`, `select { case c.send <- payload: default: evict(c) }`, where `evict` deletes `c` from the map and closes `c.send` — the same body as the `leaveCmd` present-branch, so factor it into one unexported `remove(c *Client)` helper called by both. Evicted clients are collected and removed after the range completes, never during it.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/room.go internal/ws/room_test.go && \
  git commit -m "feat: evict slow clients rather than stalling the room broadcast"
```

Expected: PASS, then one commit.

**Checkpoint 5: emptying a room notifies the owner exactly once**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `NewRoom("r1", onEmpty)` where `onEmpty` sends the room ID on a buffered channel of capacity 4. Join two clients; `Leave` the first → nothing arrives on the channel within 100 ms. `Leave` the second → `"r1"` arrives. Then `Leave` the second again → nothing further arrives (the no-op leave must not re-fire the notification). A `nil` `onEmpty` is legal and must not panic — CP1–CP4 pass `nil`.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestRoomOnEmpty`
Expected: FAIL — `onEmpty` is currently ignored, so nothing ever arrives.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `remove(c)` — the helper shared by leave and eviction — checks the map size after deleting. On a transition to zero **and** a non-nil `onEmpty`, it calls `go r.onEmpty(r.ID)`. The `go` is required, not stylistic: Task 5's hub responds to this notification by sending a command *back* to this room, and a synchronous call would deadlock the room's own `run()` goroutine against that reply.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/room.go internal/ws/room_test.go && \
  git commit -m "feat: notify the hub when a room empties"
```

Expected: PASS, then one commit.

**Task boundary:** `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make lint && make build`

---

## Task 3: Write pump

**Files:**
- Modify: `backend/internal/ws/client.go`
- Create: `backend/internal/ws/client_test.go`

**Interfaces — Produces:**
```go
type Conn interface {
    ReadMessage() (messageType int, p []byte, err error)
    WriteMessage(messageType int, data []byte) error
    WriteControl(messageType int, data []byte, deadline time.Time) error
    SetReadDeadline(t time.Time) error
    SetWriteDeadline(t time.Time) error
    SetReadLimit(limit int64)
    SetPongHandler(h func(appData string) error)
    Close() error
}

type ClientConfig struct {
    SendBuffer   int
    PingInterval time.Duration
    PongWait     time.Duration
    WriteWait    time.Duration
    MaxMessage   int64
}

func DefaultClientConfig() ClientConfig // 64, 30s, 60s, 10s, 4096

func NewClient(conn Conn, ident Identity, cfg ClientConfig) *Client
func (c *Client) WritePump()
```

`*websocket.Conn` from gorilla satisfies `Conn` as written — no adapter needed. Verify by assigning one to a `Conn` variable in `handler.go` (Task 6); do not write a compile-time assertion in `client.go`, which would force the gorilla import into a file that otherwise needs only stdlib.

**Test stub for this task:** a `stubConn` in `client_test.go` recording written messages and control frames on buffered channels, with settable error returns. It must guard its own recorded state — the pump writes from its goroutine while the test reads from another. This stub is test-local; it is the one place in this package where a mutex is correct, because it is not room or hub state.

**Checkpoint 1: queued payloads are written in order as text messages**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `NewClient(stub, Identity{UserID:"u1"}, cfg)` with `SendBuffer` 4, then `go c.WritePump()`. Push `[]byte("a")` then `[]byte("b")` onto `c.send`. The stub records exactly two `WriteMessage` calls, both with `messageType == websocket.TextMessage`, payloads `"a"` then `"b"` in that order. Each write is preceded by a `SetWriteDeadline` call with a deadline in the future.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestWritePump`
Expected: FAIL — `WritePump` undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `WritePump` ranges over `c.send`; for each payload it calls `SetWriteDeadline(time.Now().Add(cfg.WriteWait))` then `WriteMessage(websocket.TextMessage, payload)`. `NewClient` stores `cfg` on the client and allocates `send` with capacity `cfg.SendBuffer`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/client.go internal/ws/client_test.go && \
  git commit -m "feat: add client write pump"
```

Expected: PASS, then one commit.

**Checkpoint 2: the pump pings on an interval**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `cfg.PingInterval = 20 * time.Millisecond`, nothing pushed onto `send`. Within 200 ms the stub records **at least two** `WriteControl` calls with `messageType == websocket.PingMessage` and a deadline in the future. Assert "at least two", never an exact count — an exact count makes the test a timing flake on a loaded CI runner.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestWritePumpPings`
Expected: FAIL — the pump only ranges over `send`, so no control frame is ever written.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: replace the `range` with a `select` over `c.send` and `ticker.C` from `time.NewTicker(cfg.PingInterval)`, `defer ticker.Stop()`. The ticker branch calls `WriteControl(websocket.PingMessage, nil, time.Now().Add(cfg.WriteWait))`. Receiving from a closed `c.send` must still exit the loop — use the two-value receive form.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/client.go internal/ws/client_test.go && \
  git commit -m "feat: send heartbeat pings from the write pump"
```

Expected: PASS, then one commit.

**Checkpoint 3: closing the send channel stops the pump and closes the connection**

- [ ] **Step 1: Write the failing test, then run it**

Spec: start the pump, then `close(c.send)`. Within 200 ms the stub records exactly one `Close()` call, and `WritePump` has returned (assert via a `done` channel closed by the goroutine that called `WritePump`). No further `WriteMessage` calls are recorded after the close.

This is the path Task 2's eviction takes: the room closes `send`, and that must tear the connection down.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestWritePumpClosesConn`
Expected: FAIL — the pump returns but never calls `Close()`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `defer c.conn.Close()` as the first statement of `WritePump`. On a closed `send` the pump sends a `websocket.CloseMessage` control frame (best-effort, error ignored) and returns.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/client.go internal/ws/client_test.go && \
  git commit -m "feat: close the connection when the write pump stops"
```

Expected: PASS, then one commit.

**Checkpoint 4: a write failure terminates the pump**

- [ ] **Step 1: Write the failing test, then run it**

Spec: configure the stub so `WriteMessage` returns `errors.New("broken pipe")` on its first call. Start the pump, push one payload. Within 200 ms `WritePump` has returned and `Close()` was called exactly once. Push a second payload afterwards — the stub records no second `WriteMessage`.

Without this the pump would spin on a dead socket, writing every broadcast into an error for the life of the process.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestWritePumpWriteError`
Expected: FAIL — the error return is currently discarded and the pump keeps looping.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: both `WriteMessage` and `SetWriteDeadline` error returns cause an immediate `return` from `WritePump` (the deferred `Close` runs). A failed ping `WriteControl` returns too.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/client.go internal/ws/client_test.go && \
  git commit -m "feat: terminate the write pump on write failure"
```

Expected: PASS, then one commit.

**Task boundary:** `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make lint && make build`

---

## Task 4: Read pump and heartbeat

**Files:**
- Modify: `backend/internal/ws/client.go`, `backend/internal/ws/client_test.go`

**Interfaces — Produces:**
```go
// MessageHandler is the seam Phase 4b fills. A nil handler is legal and
// makes every inbound message an unknown-type error reply.
type MessageHandler func(c *Client, e Envelope)

func (c *Client) ReadPump(handle MessageHandler, onClose func())
func (c *Client) Send(payload []byte) // non-blocking; drops when the buffer is full
```

**Checkpoint 1: decoded envelopes reach the handler**

- [ ] **Step 1: Write the failing test, then run it**

Spec: stub `ReadMessage` yields `[]byte(`{"type":"place_wager","data":{"amount":50}}`)` once, then blocks until the test closes it and returns `io.EOF`. `go c.ReadPump(handler, nil)` where `handler` records its arguments. The handler is called exactly once with the same `*Client` and an `Envelope` whose `Type` is `"place_wager"` and whose `Data` unmarshals to `map[string]any{"amount": float64(50)}`.

`place_wager` is used here only as a realistic type string — this phase implements no wager behavior.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestReadPumpDispatch`
Expected: FAIL — `ReadPump` undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `ReadPump` loops on `c.conn.ReadMessage()`; on success it calls `Decode`, and on a nil error calls `handle(c, env)` when `handle != nil`. Any `ReadMessage` error breaks the loop.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/client.go internal/ws/client_test.go && \
  git commit -m "feat: add client read pump dispatching to a message handler"
```

Expected: PASS, then one commit.

**Checkpoint 2: an undecodable or unhandled message answers with an error event, privately**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `SendBuffer` 4, `handle` nil. Feed two messages then EOF: `[]byte("{not json")` and `[]byte(`{"type":"nonsense"}`)`.

- `c.send` yields exactly two payloads. Decoding each gives `Type == TypeError`, with `Data` unmarshalling to `ErrorEvent{Code: "malformed"}` and `ErrorEvent{Code: "unknown_type"}` respectively. `Message` is non-empty in both.
- The pump does **not** return after the first bad message — a client sending garbage is not disconnected.
- Nothing is broadcast: the reply reaches only this client's own `send`, which is why `Send` exists rather than routing the reply through the room.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestReadPumpErrorReply`
Expected: FAIL — decode errors are currently swallowed and no reply is produced.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: on `Decode` error, `c.Send(Encode(TypeError, ErrorEvent{Code: "malformed", Message: err.Error()}))` and `continue`. With a nil `handle`, reply `Code: "unknown_type"`, `Message: "unsupported message type: " + env.Type` and `continue`. `Send` is `select { case c.send <- payload: default: }` — dropping is correct here, since a client too backed up to receive an error reply is already being evicted by the room.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/client.go internal/ws/client_test.go && \
  git commit -m "feat: reply with an error event for malformed and unknown messages"
```

Expected: PASS, then one commit.

**Checkpoint 3: a pong extends the read deadline**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `cfg.PongWait = 5 * time.Second`. Start the pump. Assert the stub recorded a `SetReadLimit(cfg.MaxMessage)` call and at least one `SetReadDeadline`. Capture the pong handler the pump installed via `SetPongHandler`, invoke it with `""`, and assert a **further** `SetReadDeadline` was recorded whose deadline is at least 4 seconds in the future.

This is the whole heartbeat: Task 3 CP2 sends pings, the peer answers with pongs, and each pong buys another `PongWait` before the read deadline kills a silent connection.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestReadPumpPong`
Expected: FAIL — no read limit, deadline, or pong handler is installed.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: before the read loop, `ReadPump` calls `SetReadLimit(cfg.MaxMessage)`, `SetReadDeadline(now + PongWait)`, and `SetPongHandler(func(string) error { return c.conn.SetReadDeadline(time.Now().Add(cfg.PongWait)) })`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/client.go internal/ws/client_test.go && \
  git commit -m "feat: extend the read deadline on pong"
```

Expected: PASS, then one commit.

**Checkpoint 4: a read failure runs the close callback exactly once**

- [ ] **Step 1: Write the failing test, then run it**

Spec: stub `ReadMessage` returns `io.ErrUnexpectedEOF` immediately. `go c.ReadPump(nil, onClose)` where `onClose` increments a counter. Within 200 ms `ReadPump` has returned, `onClose` ran exactly once, and `Close()` was called on the conn. A nil `onClose` must not panic.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestReadPumpOnClose`
Expected: FAIL — `onClose` is currently ignored.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `defer func() { c.conn.Close(); if onClose != nil { onClose() } }()` at the top of `ReadPump`. Close before the callback — Task 7 CP2's callback broadcasts `player_left`, and the socket should already be down when the room learns of it.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/client.go internal/ws/client_test.go && \
  git commit -m "feat: run the close callback when the read pump ends"
```

Expected: PASS, then one commit.

**Task boundary:** `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make lint && make build`

---

## Task 5: Hub

**Files:**
- Create: `backend/internal/ws/hub.go`, `backend/internal/ws/hub_test.go`

**Interfaces — Produces:**
```go
func NewHub() *Hub
func (h *Hub) Join(roomID string, c *Client) *Room // get-or-create, then join — one atomic hub command
func (h *Hub) RoomCount() int
func (h *Hub) Shutdown()
```

**Design note for the executor:** `Join` is deliberately one call rather than a `Room(id)` getter followed by `room.Join(c)`. Between those two calls the room could be reaped for being empty, and the caller would then join a dead room whose goroutine has exited. Folding both into a single hub command closes that window by construction — do not split it back apart for a "nicer" API.

**Checkpoint 1: rooms are created on demand and reused by ID**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `h := NewHub()`. `roomA := h.Join("r1", c1)` → `roomA.ID == "r1"`, `h.RoomCount() == 1`, `roomA.Count() == 1`. `roomB := h.Join("r1", c2)` → `roomB == roomA` (same pointer), `h.RoomCount()` still `1`, `roomA.Count() == 2`. `roomC := h.Join("r2", c3)` → `roomC != roomA`, `h.RoomCount() == 2`.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestHubJoin`
Expected: FAIL — `NewHub` undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `NewHub` starts `go h.run()` owning `map[string]*Room`. `joinCmd{roomID, client, reply chan *Room}` looks up the room, creates it via `NewRoom(roomID, h.notifyEmpty)` when absent, calls `room.Join(client)`, and replies with the room. `notifyEmpty` sends the room ID on `h.empty`, which `run()` also selects on.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/hub.go internal/ws/hub_test.go && \
  git commit -m "feat: add hub registry with get-or-create room join"
```

Expected: PASS, then one commit.

**Checkpoint 2: an emptied room is reaped from the registry**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `h.Join("r1", c1)` then `h.Join("r1", c2)` → `RoomCount() == 1`. `room.Leave(c1)` → within 200 ms `RoomCount()` is still `1`. `room.Leave(c2)` → within 200 ms `RoomCount() == 0`. Poll with a short interval rather than a single fixed sleep; the notification is asynchronous by design (Task 2 CP5).

Then `h.Join("r1", c3)` → `RoomCount() == 1` and the returned room is a **different** pointer from the first. A reaped room is gone, not revived.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestHubReaps`
Expected: FAIL — `h.empty` is never acted on, so `RoomCount()` stays 1 forever.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `run()`'s `h.empty` branch calls `room.close()`, which sends a `closeCmd{reply chan bool}` to the room. The room replies `true` and returns from `run()` only when its client map is empty, `false` otherwise. The hub deletes the map entry only on `true` — a client that rejoined between the notification and the close keeps its room alive.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/hub.go internal/ws/room.go internal/ws/hub_test.go && \
  git commit -m "feat: reap empty rooms from the hub registry"
```

Expected: PASS, then one commit.

**Checkpoint 3: shutdown disconnects every client in every room**

- [ ] **Step 1: Write the failing test, then run it**

Spec: join `c1` to `"r1"` and `c2` to `"r2"`. `h.Shutdown()`. Afterwards `RoomCount() == 0`, and both `c1.send` and `c2.send` are closed (receive yields `ok == false`). `Shutdown` returns only once that is true — the test asserts immediately after it returns, with no sleep.

The no-sleep assertion is the point: `cmd/api`'s graceful shutdown needs a synchronous guarantee, not a hope.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestHubShutdown`
Expected: FAIL — `Shutdown` undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `shutdownCmd{reply chan struct{}}`. `run()` iterates every room, sends each a `shutdownCmd` that removes and closes every client's `send` then returns from the room's `run()`, clears the map, replies, and returns from the hub's `run()`. A `Join` after `Shutdown` is out of scope for this phase — the process is exiting.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/hub.go internal/ws/room.go internal/ws/hub_test.go && \
  git commit -m "feat: disconnect all clients on hub shutdown"
```

Expected: PASS, then one commit.

**Task boundary:** `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make lint && make build`

---

## Task 6: Authenticated upgrade handshake

**Files:**
- Create: `backend/internal/ws/handler.go`, `backend/internal/ws/handler_test.go`

**Interfaces — Produces:**
```go
func Handler(hub *Hub, issuer *auth.Issuer, cfg ClientConfig, onMessage MessageHandler) http.HandlerFunc
```

**Consumes:** `auth.Issuer.Verify(tokenString string) (auth.Claims, error)` and `auth.Claims{UserID, DisplayName, RoomID, Guest}` — both already exist and are tested (Phase 3). `Verify` already pins HS256 and rejects `alg: none`; do not add a second algorithm check here.

**Test setup for this task:** a real `httptest.NewServer(Handler(...))` dialled with `websocket.DefaultDialer`. Build tokens with a real `auth.NewIssuer([]byte(strings.Repeat("k", 32)), time.Hour)`. Convert the `http://` URL to `ws://` with `strings.Replace`.

**Checkpoint 1: a valid room-scoped token upgrades and receives a connected event**

- [ ] **Step 1: Write the failing test, then run it**

Spec: issue a token for `Claims{UserID: "u1", DisplayName: "Ada", RoomID: "r1", Guest: false}`. Dial with `?token=<jwt>`; assert the handshake response is `101`. The first message read from the socket decodes to `Envelope{Type: TypeConnected}` whose `Data` unmarshals to `ConnectedEvent{UserID: "u1", DisplayName: "Ada", RoomID: "r1", Guest: false}`. `hub.RoomCount() == 1`.

Repeat the same assertions with the token supplied as `Authorization: Bearer <jwt>` and no query parameter. Both must work: browsers cannot set headers on a WebSocket handshake, so the query form is required for Phase 6; a CLI client can use either.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestHandlerUpgrade`
Expected: FAIL — `Handler` undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: extract the token from `Authorization: Bearer` first, else the `token` query parameter. `issuer.Verify` → `Claims`. Upgrade with a package-level `websocket.Upgrader{}` (gorilla's default `CheckOrigin` rejects cross-origin browser requests and permits requests with no `Origin` header, which is what a CLI client sends — Phase 6 will supply a real origin allowlist and this plan deliberately does not guess at it). Then: build the client with `NewClient`, `hub.Join(claims.RoomID, c)`, `c.Send(Encode(TypeConnected, ...))`, `go c.WritePump()`, `go c.ReadPump(onMessage, ...)`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/handler.go internal/ws/handler_test.go && \
  git commit -m "feat: add authenticated WebSocket upgrade handler"
```

Expected: PASS, then one commit.

**Checkpoint 2: a missing or invalid token is refused before upgrading**

- [ ] **Step 1: Write the failing test, then run it**

Spec: table-driven, each case dialling and asserting the dial fails with HTTP `401` and `hub.RoomCount() == 0` —
| case | token |
|---|---|
| absent | no header, no query parameter |
| garbage | `?token=not-a-jwt` |
| wrong secret | a token signed by a second issuer with a different 32-byte secret |
| expired | a token from an issuer with `ttl` of `-1 * time.Hour` |

`websocket.DefaultDialer.Dial` returns `websocket.ErrBadHandshake` plus the `*http.Response`; assert on `resp.StatusCode`.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestHandlerRejects`
Expected: FAIL — a bad token currently upgrades anyway, or panics on the zero `Claims`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: an empty token or any `Verify` error → `http.Error(w, "unauthorized", http.StatusUnauthorized)` and `return`, before `Upgrader.Upgrade` is reached. The response body is the generic string in every case — never echo the verification error, which would distinguish "expired" from "bad signature" for an attacker.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/handler.go internal/ws/handler_test.go && \
  git commit -m "feat: refuse WebSocket upgrade without a valid token"
```

Expected: PASS, then one commit.

**Checkpoint 3: an account-scoped token cannot open a room socket**

- [ ] **Step 1: Write the failing test, then run it**

Spec: issue a valid token for `Claims{UserID: "u1", DisplayName: "Ada", RoomID: ""}` — exactly what `POST /api/v1/auth/login` returns. Dial with it. The dial fails with HTTP `403` (not 401 — the caller authenticated correctly, they simply have no room), and `hub.RoomCount() == 0`.

Without this guard an empty `RoomID` would create and join a room literally named `""`, silently pooling every account-scoped connection in the process into one shared room.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestHandlerRequiresRoom`
Expected: FAIL — the handler joins room `""` and returns 101.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: after a successful `Verify`, `if claims.RoomID == ""` → `http.Error(w, "token is not scoped to a room", http.StatusForbidden)` and `return`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/handler.go internal/ws/handler_test.go && \
  git commit -m "feat: require a room-scoped token to open a socket"
```

Expected: PASS, then one commit.

**Task boundary:** `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make lint && make build`

---

## Task 7: Presence and wiring

**Files:**
- Modify: `backend/internal/ws/handler.go`, `backend/internal/ws/handler_test.go`
- Create: `backend/internal/httpapi/ws_handlers.go`, `backend/internal/httpapi/ws_handlers_test.go`
- Modify: `backend/internal/httpapi/health.go` (the `Deps` struct and `NewMux`), `backend/cmd/api/main.go`
- Modify: `docs/plans/2026-08-21-implementation-plan.md` (§9 table — final checkpoint only)

**Checkpoint 1: a joining client is announced to the room**

- [ ] **Step 1: Write the failing test, then run it**

Spec: one `httptest` server, one hub. Client A dials with a token for room `"r1"` and reads its `connected` event. Client B then dials with a different `UserID`/`DisplayName` for the same room.

A's next message decodes to `Envelope{Type: TypePlayerJoined}` with `Data` unmarshalling to `PresenceEvent{UserID: "u2", DisplayName: "Grace", PlayerCount: 2}`.

B receives its own `connected` event first, and then also receives the same `player_joined` event — the broadcast goes to the whole room including the newcomer. A client distinguishes its own arrival by comparing `UserID` against its `connected` event. This is a deliberate simplification: a broadcast-except-sender variant would be a second code path used once.

Set a read deadline on the test connections so a missing broadcast fails the test instead of hanging it.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestPresenceJoin`
Expected: FAIL — nothing is broadcast on join; A's read deadline expires.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: in `Handler`, after `hub.Join` and after sending `connected`, broadcast `Encode(TypePlayerJoined, PresenceEvent{UserID, DisplayName, PlayerCount: room.Count()})` via `room.Broadcast`. Order matters: `connected` is queued on the client's own buffer before the broadcast, so it always arrives first.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/handler.go internal/ws/handler_test.go && \
  git commit -m "feat: broadcast player_joined on connect"
```

Expected: PASS, then one commit.

**Checkpoint 2: a departing client is announced to the survivors**

- [ ] **Step 1: Write the failing test, then run it**

Spec: A and B both connected to `"r1"` (drain both sockets through the `player_joined` events). Close B's connection. A's next message decodes to `Envelope{Type: TypePlayerLeft}` with `PresenceEvent{UserID: "u2", DisplayName: "Grace", PlayerCount: 1}` — the count is taken **after** removal. Then close A; within 500 ms `hub.RoomCount() == 0`, proving the leave path feeds Task 5 CP2's reaping.

Run: `cd backend && go test ./internal/ws/ -race -count=1 -run TestPresenceLeave`
Expected: FAIL — nothing is broadcast on disconnect; A's read deadline expires.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: the `onClose` callback passed to `ReadPump` calls `room.Leave(c)` **then** `room.Broadcast(Encode(TypePlayerLeft, PresenceEvent{..., PlayerCount: room.Count()}))`. Leaving first is what keeps the departing client out of the count and out of the broadcast; it is also why Task 2 CP2 made `Leave` idempotent, since eviction may already have removed this client.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -race -count=1 && \
  git add internal/ws/handler.go internal/ws/handler_test.go && \
  git commit -m "feat: broadcast player_left on disconnect"
```

Expected: PASS, then one commit.

**Checkpoint 3: the socket route is served by the process mux**

- [ ] **Step 1: Write the failing test, then run it**

Spec: in `internal/httpapi`, build `NewMux(Deps{Issuer: issuer, Hub: ws.NewHub(), ...})`, serve it with `httptest.NewServer`, and dial `ws://…/api/v1/socket?token=<room-scoped jwt>`. Assert the handshake returns `101` and the first message decodes to `ws.TypeConnected`. Dial without a token → `401`.

Existing `httpapi` tests construct `Deps` without a `Hub`; find them with `grep -rn 'Deps{' internal/httpapi/*_test.go` and add `Hub: ws.NewHub()` to each. Registration must not be made conditional on a nil hub — a route that silently disappears depending on how `Deps` was built is exactly the kind of quiet failure `.claude/rules/ecc/common/coding-style.md` forbids.

Run `make up` first — this package's `TestMain` needs Redis.

Run: `cd backend && go test ./internal/httpapi/ -race -count=1 -p 1 -run TestWSRoute`
Expected: FAIL — `Deps` has no `Hub` field; once added, the dial returns `404`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract:
- `Deps` gains `Hub *ws.Hub`. `NewMux` calls `registerWSRoutes(mux, d)`.
- `ws_handlers.go`: `mux.Handle("GET /api/v1/socket", ws.Handler(d.Hub, d.Issuer, ws.DefaultClientConfig(), nil))`. The nil `MessageHandler` is Phase 4b's seam — add a comment saying so, so it does not read as an oversight.
- `cmd/api/main.go`: construct `hub := ws.NewHub()` beside the other services, pass it in `Deps`, and call `hub.Shutdown()` after `server.Shutdown` returns.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/httpapi/ -race -count=1 -p 1 && \
  git add internal/httpapi/ws_handlers.go internal/httpapi/ws_handlers_test.go \
          internal/httpapi/health.go internal/httpapi/*_test.go cmd/api/main.go && \
  git commit -m "feat: serve the WebSocket route from the process mux"
```

Expected: PASS, then one commit.

**Checkpoint 4: record the phase split and close out**

- [ ] **Step 1: Verify the whole branch is green**

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  make test && make lint && make build
```

Expected: PASS on all three. Also capture coverage for the new package:
`cd backend && go test ./internal/ws/ -cover -count=1` — expect ≥80% per `.claude/rules/ecc/common/testing.md`. `handler.go`'s upgrade-failure branches are the likely gap; if the figure is short, check `go test ./... -coverpkg=./...` before adding tests, per `CLAUDE.md`'s note on per-package coverage artifacts.

- [ ] **Step 2: Amend the parent plan, then commit**

Contract: in `docs/plans/2026-08-21-implementation-plan.md` §9, split the Phase 4 row into **4a — WebSocket transport** (deliverable: authenticated room socket, presence, heartbeat, slow-client eviction; depends on 3) and **4b — Round lifecycle** (deliverable: rounds, wagers, live odds, settlement reveal, auto-refund, CLI client; depends on 4a). Renumber nothing else — Phases 5–8 keep their numbers and their `4` dependencies become `4b`. Add a one-paragraph note recording that the split was made under the §9 Phase-sizing recommendation, with this plan's path, and that it is the first phase planned under `writing-plans-tuned`.

Then record the experiment's outcome in this plan's own file: append a short "Measured" table with plan lines, checkpoints, lines/checkpoint, commits landed vs. the 24 planned, and how many checkpoints had to be un-batched into separate verify and commit steps. That last number decides whether `writing-plans-tuned` merges into `writing-plans` or gets deleted.

```bash
git add docs/plans/2026-08-21-implementation-plan.md docs/plans/2026-08-26-phase-4a-ws-transport.md && \
  git commit -m "docs: split Phase 4 into 4a and 4b in the parent plan"
```

Expected: one commit. **The branch is green and verified — stop here.** `executing-plans` Step 3 hands off to `finishing-a-development-branch` for the merge; do not merge from this plan.

---

## Self-Review

**1. Spec coverage.** Parent plan §9's Phase 4 row, transport half: per-room owner goroutine over a channel with no mutexes (Task 2), client read/write pumps (Tasks 3–4), ping/pong heartbeat (Task 3 CP2 + Task 4 CP3), slow-client eviction (Task 2 CP4). Spec §6, JWT presented at connection and verified without a per-message lookup (Task 6) — identity comes from the token's own claims per Amendment B5, no Redis read on the socket path. §10's slow-client risk row is Task 2 CP4 specifically. Deferred to 4b by Amendment C1 and *not* gaps here: lock timer, auto-refund, wager path, CLI client.

**2. Placeholder scan.** No "TBD", no "handle errors appropriately". Every checkpoint names exact inputs and exact expected outputs or error values. Two deliberate deferrals are named rather than vague: the `Upgrader.CheckOrigin` allowlist (Phase 6, when real origins exist) and `MessageHandler` (Phase 4b, wired as an explicit nil with a comment).

**3. Type consistency.** `Identity` is one type used by `Client`, `Room.Members`, and the presence events. `Conn` is declared once in Task 3's Interfaces and referenced by Task 2. `ClientConfig` flows `DefaultClientConfig` → `Handler` → `NewClient` unchanged. `PresenceEvent` serves both `player_joined` and `player_left`. `Room.Count()` is the single source of `PlayerCount` in both. `hub.Join(roomID, c) *Room` is the only room-acquisition call, matching Task 5's design note.

**4. Checkpoint realness.** Every checkpoint names an observable signal at the interface its test calls, per `writing-plans`' observable-signal rule. The two that warranted a second look: Task 2 CP4 (eviction is visible as `Count()` dropping *and* `send` closing — not merely as an internal map mutation) and Task 5 CP2 (reaping is visible as `RoomCount()` dropping *and* a new pointer on rejoin, which distinguishes a reaped room from a merely-emptied one). Both are genuinely falsifiable.

**5. Where this plan stops.** At "the branch is green and verified" (Task 7 CP4). No merge, push, or PR step appears anywhere.

## Measured

Recorded at Task 7 CP4 close-out, for the `writing-plans-tuned` experiment
this plan's split (Amendment C1) opted into first.

| Metric | Value |
|---|---|
| Plan lines | 899 |
| Checkpoints (plan body) | 25 — the header's "24 checkpoints" budget undercounted by one; Task 7 CP4 (close-out) is a 25th checkpoint the header total didn't include |
| Lines/checkpoint | 899 / 25 ≈ 36 — under the header's own ≤60 target |
| Commits landed | 25 (24 `feat` + 1 `docs` for this close-out), a clean 1:1 with checkpoints — no checkpoint merged or split across commits, unlike Phase 2 (28 landed vs. 31–32 estimated) or Phase 3 (~49 vs. ~44) |
| Checkpoints un-batched into separate verify/commit steps | 0 — every checkpoint's "Step 2: Implement, then verify-and-commit in one command" ran as written, first try, no splitting into a separate verify pass and a separate commit pass |
| Checkpoints that passed immediately (prior checkpoint's implementation already satisfied the contract) | 5 of 25 — Task 1 CP2, Task 3 CP2/CP3/CP4, Task 6 CP2. Each was verified genuine per the observable-signal rule by disabling the relevant guard, confirming the test failed, then restoring it — all 5 confirmed real, none vacuous |
| Real defects found during execution (plan text vs. what actually had to be built) | 1 — Task 5 CP2's literal contract (delete the room from the registry directly on the empty notification) would let a client that rejoins between notification and reap land in an orphaned room while the hub spins up a second, empty one under the same ID. Implemented the close-confirmation round-trip the plan's own design note called for instead (`Room.close()` replies `false` if a member re-joined, hub only deletes on `true`) — this also required *not* returning from the hub's `run()` on `Shutdown`, since `RoomCount()` is called immediately after `Shutdown()` returns in Task 5 CP3's own test and would otherwise deadlock forever |

**Verdict for `writing-plans-tuned` vs. `writing-plans`:** the zero-un-batching
result is the number this experiment was designed to produce. On this one
data point, the tuned format's combined verify-and-commit step held up
under real execution without needing to fall back to the untuned two-step
form. One phase isn't enough to merge the variant back into `writing-plans`
outright — that call belongs to whoever runs the next `writing-plans-tuned`
phase and compares.
