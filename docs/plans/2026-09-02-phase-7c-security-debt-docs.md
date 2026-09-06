# Phase 7c — Security Debt + Docs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the three security items this project has carried open by
design — the login-timing oracle, the disconnect-ends-a-session limitation,
and the unbounded `RoundSettled.Payouts` array — and replace the quick-start
README with one that explains the architecture, diagram included.

**Architecture:** Three independent fixes plus a doc pass, in that order.
The login fix makes the unknown-email path pay the same argon2id cost the
wrong-password path already pays, so response *time* stops distinguishing
what the response *body* already refuses to. The message-bounding fix rejects
an oversized Kafka payload before the JSON decoder allocates for it, and
caps the payout count the decoder is allowed to produce. The reconnect fix
is the largest: a disconnect no longer folds a session immediately, it starts
a grace window that a reconnect cancels — and the fold itself becomes a
once-only operation by atomically claiming the session in Redis before
crediting anything, so the "session ends twice on a rapid reconnect" MEDIUM
from Phase 4b's review closes with it. No new dependencies; no new
infrastructure.

**Tech Stack:** Go 1.26.7 · Redis 7.2 · Kafka 3.7 · PostgreSQL 16 · Mermaid
(rendered by GitHub, no toolchain) · no new Go or npm dependencies.

**Spec:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md) §3 (Identity & Account Model), §4 (the reconnect known limitation), §6 (Auth)
**Parent plan:** [`docs/plans/2026-08-21-implementation-plan.md`](2026-08-21-implementation-plan.md) §9 (row 7c), §12 (Acceptance)
**Items this phase closes:** [`docs/project-history.md`](../project-history.md) "Open by design, deferred to Phase 7 hardening" items 1, 3, and 4

---

## Global Constraints

Every task's requirements implicitly include this section.

- **`internal/domain` stays free of I/O.** Nothing in this phase adds an
  import to it.
- **All amounts are integer token units.** No floats anywhere near a balance.
- **Login gives byte-identical responses for an unknown email and a wrong
  password.** This phase extends that rule from the response body to the
  response *time*; it must not weaken the body half. Both still collapse
  into `account.ErrInvalidCredentials`.
- **One sliding-window rate limiter, every call site.** Nothing here forks a
  second one; the `auth` scope already throttles login by client IP and
  stays exactly as it is.
- **A session's opening stake never debits an account holder's persistent
  balance** — only the net delta at session end does, and
  `domain.ApplySessionResult` remains the only thing permitted to compute
  that delta.
- **Go toolchain stays at `go 1.26.7` / CI pin `1.26`**, and the five held
  dependency versions stay held (`CLAUDE.md` "Stack"). This phase adds no
  dependency and upgrades none.
- **`security-reviewer` runs before the phase closes** — this phase touches
  auth, money movement, and a network surface, all three of `CLAUDE.md`'s
  triggers.
- **Test commands need the Go PATH export** — `export
  PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin` before any `go`/`make`
  invocation (`CLAUDE.md`, Known Environment Gotchas). Every command in this
  plan already carries it.
- **`make test` needs the full stack up** (`make up-full` — Redis,
  PostgreSQL, Kafka). `internal/account`, `internal/redisstore`, and
  `internal/round` integration suites fail rather than skip without Redis
  on DB 15.

---

## Decisions This Plan Fixes

Recorded here because each is a judgment call an executor should not have to
re-make mid-task.

1. **The grace window lives in process memory, not Redis.** A pending
   session-end is a `time.Timer` plus a map entry on `round.Service`, guarded
   by a mutex. It is *not* a Redis key with a scanner. Justification: the
   WebSocket hub is already in-process — every client of one room must be
   connected to the same API instance for `Broadcast` to reach them at all —
   so an in-process grace window introduces no single-instance assumption the
   architecture doesn't already make. A Redis-backed version would be
   strictly more machinery for a property nothing currently needs. Revisit
   together with a multi-instance hub, never separately.

2. **The fold is claimed atomically before it credits.** `EndSession` deletes
   the session's wallet and opening-stake fields with a single Redis
   `HDEL` pair *before* writing the persistent balance, and treats "deleted
   nothing" as "someone already ended this session, do nothing." This is what
   makes the fold once-only under concurrency, not merely in sequence — the
   grace window narrows the double-fold race but cannot close it, because two
   disconnect paths can still race. Ordering is claim-then-credit rather than
   credit-then-claim deliberately: a crash between the two loses a session
   result, where the reverse order would mint tokens on the retry, and this
   codebase's invariants forbid minting.

3. **An unknown *user* is still an error; an already-ended *session* is not.**
   `EndSession` keeps returning `redisstore.ErrNotFound` when `store.User`
   has no such user — that is a genuine bug signal and an existing test pins
   it (`internal/round/session_test.go:162`). Only a missing *opening stake
   or wallet* — a known user with no live session — becomes a `(0, nil)`
   no-op. The two cases stay distinguishable.

4. **Guests are not scheduled at all.** `EndSession` is already a no-op for a
   guest (no persistent balance to fold into), so `ScheduleEndSession`
   returns immediately for one rather than burning a goroutine and a timer to
   reach a no-op 30 seconds later. A guest's room wallet therefore continues
   to survive indefinitely, exactly as it does today — unchanged behavior, not
   a new gap.

5. **`SessionGrace` is 30 seconds.** Long enough for a browser refresh plus a
   fresh WebSocket handshake (sub-second in practice) and a short network
   blip; short enough to sit well under `round.RefundGrace`'s 60 seconds, so a
   disconnected player's session never outlives the round-level fallback that
   would refund their stake anyway.

6. **The frontend gains no auto-reconnect in this phase.** `frontend/lib/socket.ts`
   deliberately has no retry, and its comment cites this very backend
   limitation as the reason. The limitation is what lifts here — but a
   reconnect timer with backoff is a fourth deliverable, and the parent
   plan's phase-sizing note is explicit about what bundling those does. A
   page refresh already exercises the grace window end to end (the room page
   reads its room token from `sessionStorage` on mount,
   `frontend/app/room/[code]/page.tsx:34`), so the fix delivers user-visible
   value with no frontend change. Task 8 corrects the now-stale comment;
   auto-reconnect is recorded as a Phase 8 candidate.

7. **`Register`'s `ErrEmailTaken` is out of scope.** It does reveal that an
   email is registered, but that is a deliberate UX affordance on a
   rate-limited endpoint, not one of the items this phase was scoped around.
   Left exactly as-is, and noted so a reviewer knows it was considered.

---

## File Structure

**Backend, modified:**

| File | Responsibility after this phase |
|---|---|
| `backend/internal/auth/password.go` | Adds the decoy hash and `VerifyDecoyPassword` — the constant-cost burn a caller with no stored hash performs |
| `backend/internal/account/service.go` | `Login`'s unknown-email path calls the decoy verify before returning |
| `backend/internal/events/message.go` | `DecodeMessage` bounds the raw message size; `validateRoundSettled` bounds the payout count |
| `backend/internal/redisstore/room.go` | Adds `ClearSession` — the atomic claim-and-clear of one session's Redis state |
| `backend/internal/round/session.go` | `EndSession` claims before crediting and is a no-op on an already-ended session |
| `backend/internal/round/service.go` | `Service` gains the grace-window state; `NewService` initializes it |
| `backend/internal/ws/handler.go` | `SessionEnder` becomes `Sessions`; connect resumes, disconnect schedules |
| `backend/internal/httpapi/ws_handlers.go` | Wires the widened interface |

**Backend, created:**

| File | Responsibility |
|---|---|
| `backend/internal/round/grace.go` | `ScheduleEndSession` / `ResumeSession` and the pending-end bookkeeping — kept out of `session.go` so the fold itself stays readable as pure sequence |
| `backend/internal/round/grace_test.go` | The grace window's tests |

**Docs, modified:** `README.md` (rewritten), `docs/specs/2026-08-21-callit-design.md`
(§4's known limitation), `CLAUDE.md` (one invariant), `docs/project-history.md`
(Phase 7c section), `docs/plans/2026-08-21-implementation-plan.md` (§9 row,
§12 boxes), `frontend/lib/socket.ts` (comment only).

---

### Task 1: Close the login-timing oracle

**Files:**
- Modify: `backend/internal/auth/password.go`
- Test: `backend/internal/auth/password_test.go`
- Modify: `backend/internal/account/service.go:110-133` (`Login`)
- Test: `backend/internal/account/service_test.go`

**Interfaces:**
- Consumes: `auth.HashPassword(plain string) (string, error)`,
  `auth.VerifyPassword(encoded, plain string) error`,
  `auth.ErrPasswordMismatch` — all existing.
- Produces: `auth.VerifyDecoyPassword(plain string)` — no return value. Runs
  one argon2id derivation against a decoy hash and discards the result. Its
  only purpose is the elapsed time.

**Checkpoint 1: the decoy verify performs a real argon2id derivation**

- [ ] **Step 1: Write the failing test, then run it**

Spec: in `password_test.go`, add `TestVerifyDecoyPasswordCostsAFullVerify`
asserting all of:
- `auth.VerifyDecoyPassword("anything")` compiles and returns nothing.
- Timing: measure `VerifyDecoyPassword("anything")` and, separately,
  `VerifyPassword(h, "wrong")` where `h = HashPassword("correct")`. Assert
  the decoy's elapsed time is at least half the real verify's. Take the
  median of 5 samples for each side, not a single sample.
- Parameter agreement, so the two can never silently drift apart: the decoy
  hash must be encoded with the package's current parameters. Assert this
  through the exported surface only — `VerifyPassword(decoy, "x")` must
  return `ErrPasswordMismatch`, never `ErrMalformedHash`, which proves the
  decoy is a well-formed PHC string this package can parse. To reach the
  decoy from the test, expose it as an unexported package-level accessor
  `decoyHash() string` (same package, so the test may call it).

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/auth/ -run TestVerifyDecoyPasswordCostsAFullVerify -count=1 -race
```
Expected: FAIL to compile — `undefined: auth.VerifyDecoyPassword` and
`undefined: decoyHash`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: in `password.go`, add

```go
var decoyHash = sync.OnceValue(func() string { ... })
func VerifyDecoyPassword(plain string)
```

`decoyHash` derives, once and lazily, `HashPassword` of a freshly generated
random 32-byte value read from `crypto/rand` and never retained — so no
password can ever verify against it and no constant needs explaining. It is
computed rather than hardcoded specifically so that raising `argon2Memory` /
`argon2Time` later re-costs the decoy automatically; a committed constant
would keep burning the old parameters' cost and quietly reopen the gap this
checkpoint closes. If `HashPassword` or the random read fails, panic — a
process that cannot produce a decoy cannot serve logins without a timing
oracle, and this runs once at first use, not per request.

`VerifyDecoyPassword(plain)` is `_ = VerifyPassword(decoyHash(), plain)`.
Document that the discarded error is the point: the function is called for
its cost, and returning something would invite a caller to branch on it.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/auth/ -count=1 -race && \
  git add internal/auth/password.go internal/auth/password_test.go && \
  git commit -m "feat: add a constant-cost decoy password verify"
```

Expected: PASS, then one commit.

**Checkpoint 2: an unknown email costs the same as a wrong password**

- [ ] **Step 1: Write the failing test, then run it**

Spec: in `service_test.go`, add `TestLoginUnknownEmailCostsSameAsWrongPassword`
(an integration test — it uses the real store, like the rest of that file).

Arrange: register one account, e.g. `known@example.com` / a valid password.
Act: measure two medians over 5 samples each —
- `svc.Login(ctx, "known@example.com", "wrong-password-entirely")`
- `svc.Login(ctx, "no-such-user@example.com", "wrong-password-entirely")`

Assert:
- Both calls return an error satisfying `errors.Is(err, ErrInvalidCredentials)`
  — the body-level rule must not regress while the timing one lands.
- `medianUnknown >= medianWrong/2`. The ratio, not an absolute bound: both
  paths scale together with machine load, so the ratio stays stable where an
  absolute millisecond threshold would not. Before the fix the gap is roughly
  two orders of magnitude (one argon2id derivation vs. one Redis miss), so a
  2× threshold has enormous margin in both directions.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/account/ -run TestLoginUnknownEmailCostsSameAsWrongPassword -count=1 -race
```
Expected: FAIL — the unknown-email median is a fraction of the wrong-password
median (the miss path returns before reaching argon2id at all), so the ratio
assertion reports a value far below `medianWrong/2`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: in `Login`, inside the `errors.Is(err, redisstore.ErrNotFound)`
branch, call `auth.VerifyDecoyPassword(password)` immediately before
returning `ErrInvalidCredentials`. Leave the `slog.Debug` line and the
returned sentinel exactly as they are. Update the doc comment on `Login` to
state that the miss path burns an equivalent verify, and why — a future
reader deleting that call as dead code is the failure mode this comment
exists to prevent.

Do **not** add the decoy call to the malformed-hash path: that path already
runs a real `VerifyPassword` and so already pays the cost.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/account/ ./internal/auth/ -count=1 -race && \
  git add internal/account/service.go internal/account/service_test.go && \
  git commit -m "fix: pay argon2id's cost on the unknown-email login path"
```

Expected: PASS, then one commit.

**Task boundary — full suite:**

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make test
```
Expected: PASS.

---

### Task 2: Bound an untrusted Kafka message

**Files:**
- Modify: `backend/internal/events/message.go`
- Test: `backend/internal/events/message_test.go`

**Interfaces:**
- Consumes: `events.DecodeMessage(topic string, value []byte) (Event, error)`,
  `events.ErrInvalidEvent` — both existing.
- Produces: `events.MaxMessageBytes = 1 << 20` (int) and
  `events.MaxPayouts = 10_000` (int), both exported consts. Violations of
  either wrap `ErrInvalidEvent`, matching every other validation failure in
  this file, so `cmd/ledger-worker`'s existing error handling needs no change.

**Checkpoint 1: an oversized message is rejected before it is decoded**

- [ ] **Step 1: Write the failing test, then run it**

Spec: in `message_test.go`, add `TestDecodeMessageRejectsOversizedValue`,
table-driven over both topics (`TopicWagersPlaced`, `TopicRoundsSettled`).

For each: build a `value` of exactly `MaxMessageBytes + 1` bytes. It does not
need to be valid JSON — the whole point is that the size check runs before
any parsing — so a byte slice of that length filled with `'x'` is the case to
use. Assert `DecodeMessage(topic, value)` returns a nil `Event` and an error
satisfying `errors.Is(err, ErrInvalidEvent)`, whose message names both the
actual length and the limit.

Add a second case per topic at exactly `MaxMessageBytes` bytes of the same
filler, asserting the error is *not* the size error — it is a JSON parse
failure (still `ErrInvalidEvent`, but its text must not mention the limit).
This pins the boundary as inclusive rather than off-by-one.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/events/ -run TestDecodeMessageRejectsOversizedValue -count=1 -race
```
Expected: FAIL to compile — `undefined: MaxMessageBytes`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: declare `MaxMessageBytes = 1 << 20` with a comment stating what
bounds it: `internal/events/consumer.go` sets the Kafka reader's `MaxBytes`
to `10e6`, so without this check a single message may be ten megabytes of
decoder input. One mebibyte is above any message this system produces — the
largest legitimate `rounds-settled` payload is one payout per player, and
`MaxPayouts` worth of them encodes to roughly 600 KB — and it is the first
bound an attacker-supplied message meets.

Add, as the first statement of `DecodeMessage`, before the topic switch:

```go
if len(value) > MaxMessageBytes { return nil, fmt.Errorf("%w: message is %d bytes, limit %d", ErrInvalidEvent, len(value), MaxMessageBytes) }
```

Placement before the switch is load-bearing: it must precede the
`json.NewDecoder` construction for either topic, since bounding the
allocation is the entire purpose. An unknown topic still falls through to
`ErrUnknownEventType` as before.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/events/ -count=1 -race && \
  git add internal/events/message.go internal/events/message_test.go && \
  git commit -m "fix: bound a Kafka message's size before decoding it"
```

Expected: PASS, then one commit.

**Checkpoint 2: a settlement with too many payouts is rejected**

- [ ] **Step 1: Write the failing test, then run it**

Spec: in `message_test.go`, add `TestDecodeMessageRejectsExcessivePayouts`.

Arrange: build a valid `RoundSettled` — non-empty `room_id`, `round_id`,
`idempotency_key`, `refunded: false`, `winning_outcome: 0`, `dust: 0`,
`total` equal to the payout sum — carrying `MaxPayouts + 1` payouts, each
with a distinct non-empty `user_id` and `amount: 1`. Marshal it and assert
the encoded length is under `MaxMessageBytes` (if it is not, the test would
be re-proving Checkpoint 1 instead of this checkpoint — assert it explicitly
so a future change to either constant fails loudly rather than silently
retargeting the test).

Assert `DecodeMessage(TopicRoundsSettled, value)` returns a nil `Event` and
an error satisfying `errors.Is(err, ErrInvalidEvent)` naming the count and
the limit.

Add a second case with exactly `MaxPayouts` payouts, asserting `err == nil`
and that the decoded event's payout slice has length `MaxPayouts` — the
boundary is inclusive.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/events/ -run TestDecodeMessageRejectsExcessivePayouts -count=1 -race
```
Expected: FAIL — the over-limit case decodes successfully and returns a nil
error, so the `errors.Is` assertion fails.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: declare `MaxPayouts = 10_000` with a comment stating the bound's
basis: a settlement carries at most one payout per player who backed the
winning outcome, a watch-party room holds tens of players, and Phase 7a/7b's
load harness drove a few hundred — so this is three orders of magnitude of
headroom, sized to bound the ledger lines one message can create
(`ledger.TransactionFor` emits roughly two per payout) rather than to
constrain any real room.

In `validateRoundSettled`, add the length check **before** the existing
`for _, p := range s.Payouts` loop, so an over-long array is rejected without
first walking it. Error text names `len(s.Payouts)` and `MaxPayouts`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/events/ ./internal/ledger/ -count=1 -race && \
  git add internal/events/message.go internal/events/message_test.go && \
  git commit -m "fix: cap the payout count a settlement message may carry"
```

Expected: PASS, then one commit.

**Task boundary — full suite:**

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make test
```
Expected: PASS.

---

### Task 3: `Store.ClearSession` — claim and clear one session atomically

**Files:**
- Modify: `backend/internal/redisstore/room.go`
- Test: `backend/internal/redisstore/room_test.go`

**Interfaces:**
- Consumes: `redisstore.RoomWalletsKey(roomID)`, `redisstore.RoomOpeningKey(roomID)`
  — the only permitted way to build these keys (`CLAUDE.md`: `keys.go` is the
  single definition of the key schema).
- Produces: `func (s *Store) ClearSession(ctx context.Context, roomID, userID string) (claimed bool, err error)`.
  Deletes the user's field from both the wallets hash and the opening-stake
  hash. `claimed` reports whether the *opening-stake* field existed — that is
  the claim token, and exactly one concurrent caller can observe `true`.

**Checkpoint 1: `ClearSession` removes both fields and reports the claim once**

- [ ] **Step 1: Write the failing test, then run it**

Spec: in `room_test.go`, add `TestClearSession` with these cases against a
room created by `CreateRoom` and joined by `JoinRoom(roomID, userID, 1000)`:

1. *a live session is claimed and cleared* — `ClearSession(ctx, roomID, userID)`
   returns `(true, nil)`; afterwards `Balance(ctx, roomID, userID)` returns an
   error satisfying `errors.Is(err, ErrNotFound)`, and
   `OpeningStake(ctx, roomID, userID)` likewise.
2. *a second call claims nothing* — calling `ClearSession` again on the same
   pair returns `(false, nil)` and no error.
3. *another member's session is untouched* — with two users joined, clearing
   the first leaves `Balance(ctx, roomID, other)` returning `1000` and
   `OpeningStake(ctx, roomID, other)` returning `1000`.
4. *a user who never joined claims nothing* — `ClearSession` for an
   unrelated user ID returns `(false, nil)`.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/redisstore/ -run TestClearSession -count=1 -race
```
Expected: FAIL to compile — `store.ClearSession undefined`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: implement `ClearSession` next to `JoinRoom` in `room.go` — the two
are the bookends of a session's Redis state and should be read together.

Issue both deletions in one `TxPipelined` call:
`HDel(ctx, RoomOpeningKey(roomID), userID)` and
`HDel(ctx, RoomWalletsKey(roomID), userID)`. Read the opening-stake `HDEL`'s
integer reply: `1` means this call claimed the session, `0` means it was
already gone. Return that as `claimed`.

The opening stake is the claim token rather than the wallet because a wallet
field can be *resurrected* after a session ends — `settle_round.lua` credits a
winner with `HINCRBY`, which recreates a deleted field — while nothing but
`JoinRoom` ever writes an opening stake. Document that in the method comment;
it is the non-obvious half of this design.

Wrap any pipeline error as
`fmt.Errorf("redisstore: clear session for %s in room %s: %w", userID, roomID, err)`,
matching the file's existing error style.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/redisstore/ -count=1 -race && \
  git add internal/redisstore/room.go internal/redisstore/room_test.go && \
  git commit -m "feat: add ClearSession to claim and clear a room session"
```

Expected: PASS, then one commit.

**Task boundary — full suite:**

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make test
```
Expected: PASS.

---

### Task 4: `EndSession` folds exactly once

**Files:**
- Modify: `backend/internal/round/session.go`
- Test: `backend/internal/round/session_test.go`

**Interfaces:**
- Consumes: `Store.ClearSession(ctx, roomID, userID) (bool, error)` from Task 3;
  `domain.ApplySessionResult(persistent, opening, current domain.Tokens) domain.Tokens`
  — existing, still the only thing permitted to compute the delta.
- Produces: `(*Service).EndSession(ctx, roomID, userID string, guest bool) (domain.Tokens, error)`
  — signature unchanged; behavior gains the claim and the no-op.

**Checkpoint 1: a completed fold clears the session it folded**

- [ ] **Step 1: Write the failing test, then run it**

Spec: in `session_test.go`, add `TestEndSessionClearsTheSession`.

Arrange: create a user with persistent balance `5000`; create a room with
buy-in `1000`; `JoinRoom` the user at `1000`; drive the wallet somewhere
other than the opening stake (open a round, place a wager of `400`, lock,
and resolve it against the user's outcome — or, more cheaply, follow whatever
existing helper `TestEndSession` uses to arrive at a moved wallet; the value
just has to differ from the opening stake).

Act: `svc.EndSession(ctx, roomID, userID, false)`.

Assert:
- the returned balance equals `domain.ApplySessionResult(5000, 1000, current)`
  for the wallet value that stood before the call (unchanged behavior — this
  half must not regress);
- `store.Balance(ctx, roomID, userID)` now returns an error satisfying
  `errors.Is(err, redisstore.ErrNotFound)`;
- `store.OpeningStake(ctx, roomID, userID)` likewise.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -run TestEndSessionClearsTheSession -count=1 -race
```
Expected: FAIL — the fold happens, but both reads still succeed and return
the pre-call values, so the two `ErrNotFound` assertions fail.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: rewrite `EndSession`'s body to this order, keeping the guest
short-circuit exactly where it is:

1. `guest` → `return 0, nil`.
2. `store.User` → on error, return it unchanged (Decision 3: an unknown user
   stays an error, and `session_test.go:162` pins it).
3. `store.OpeningStake` → on error, return it unchanged for now (Checkpoint 2
   changes this).
4. `store.Balance` → on error, return it unchanged for now.
5. `store.ClearSession` → on error, return it. On `claimed == false`, return
   `(0, nil)`.
6. `domain.ApplySessionResult`, then `store.SetBalance`, then return.

Step 5 sits before step 6 and after steps 3–4 on purpose: the reads must
happen before the claim (there is nothing left to read afterward), and the
credit must happen after it (Decision 2 — claim-then-credit loses a result on
a crash, credit-then-claim mints tokens on the retry).

Replace the "Known limitation" paragraph in the doc comment: it currently
says reconnect-with-resume is deferred to Phase 7. State instead that the
fold is once-only by claim, that the session's Redis state is cleared with
it, and that a rejoin after a fold therefore starts a genuinely new session
at the room buy-in. Note the one accepted race explicitly: the wallet is read
in step 4 and claimed in step 5, so a wager landing between them is not
folded — reachable only if a second live socket for the same user places a
wager during the grace window, and closing it would mean moving the whole
fold into Lua for a case the grace window already makes vanishingly rare.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -count=1 -race && \
  git add internal/round/session.go internal/round/session_test.go && \
  git commit -m "fix: clear a session's Redis state when its result is folded"
```

Expected: PASS, then one commit.

**Checkpoint 2: a second fold for the same session credits nothing**

- [ ] **Step 1: Write the failing test, then run it**

Spec: in `session_test.go`, add `TestEndSessionIsIdempotent`.

Arrange: as in Checkpoint 1 — a user at persistent `5000`, joined at `1000`,
wallet moved to some value `w != 1000`.

Act: call `svc.EndSession(ctx, roomID, userID, false)` twice in sequence.

Assert:
- the first call returns `domain.ApplySessionResult(5000, 1000, w)` with a nil
  error;
- the second call returns `(0, nil)` — no error, nothing credited;
- `store.User(ctx, userID).Balance` still equals the first call's return
  value — the account was folded once, not twice.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -run TestEndSessionIsIdempotent -count=1 -race
```
Expected: FAIL — after Checkpoint 1 the second call reaches step 3 and
returns the wrapped `ErrNotFound` from `OpeningStake`, so the "second call
returns `(0, nil)`" assertion fails on a non-nil error.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: in steps 3 and 4, treat `errors.Is(err, redisstore.ErrNotFound)`
as "this known user has no live session here" and `return 0, nil`. Any other
error still propagates. Step 2's `store.User` error handling is explicitly
*not* given this treatment — that asymmetry is Decision 3 and deserves one
line of comment saying so, since it is the kind of thing a later reader
"tidies" into consistency.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -count=1 -race && \
  git add internal/round/session.go internal/round/session_test.go && \
  git commit -m "fix: make EndSession a no-op on an already-ended session"
```

Expected: PASS, then one commit.

**Task boundary — full suite:**

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make test
```
Expected: PASS.

---

### Task 5: The disconnect grace window

**Files:**
- Create: `backend/internal/round/grace.go`
- Create: `backend/internal/round/grace_test.go`
- Modify: `backend/internal/round/service.go:22-36` (`Service` struct and `NewService`)

**Interfaces:**
- Consumes: `(*Service).EndSession(ctx, roomID, userID string, guest bool) (domain.Tokens, error)`
  from Task 4; `Service.ctx` — the base context every server-side timer in this
  package already runs against (`service.go:29-33`).
- Produces:
  - `round.SessionGrace = 30 * time.Second` (exported const)
  - `func (s *Service) ScheduleEndSession(roomID, userID string, guest bool)`
    — starts (or restarts) the grace window; returns immediately.
  - `func (s *Service) ResumeSession(roomID, userID string)` — cancels a
    pending end for that pair if one exists; a no-op otherwise.
  - `Service` gains unexported fields: `sessionGrace time.Duration`,
    `mu sync.Mutex`, `pending map[string]chan struct{}`.

**Checkpoint 1: a scheduled end folds the session once the window elapses**

- [ ] **Step 1: Write the failing test, then run it**

Spec: in `grace_test.go`, add `TestScheduleEndSessionFoldsAfterTheWindow`.

Arrange: a user at persistent `5000`, a room with buy-in `1000`, `JoinRoom`
at `1000`, wallet moved to a value `w != 1000` (same shape as Task 4's
tests). Build the service with `NewService`, then set `svc.sessionGrace =
200 * time.Millisecond` — the same test seam `timer_test.go:189` already uses
for `refundGrace`.

Act: `svc.ScheduleEndSession(roomID, userID, false)`, then sleep 500 ms.

Assert:
- `store.User(ctx, userID).Balance == domain.ApplySessionResult(5000, 1000, w)`;
- `store.Balance(ctx, roomID, userID)` returns `ErrNotFound` — the session was
  cleared, so the fold really ran through `EndSession`.

Add a second case in the same file, `TestScheduleEndSessionSkipsGuests`: call
`ScheduleEndSession(roomID, guestID, true)` and assert that after the same
sleep the guest's room wallet still reads `1000` — a guest is never scheduled
(Decision 4).

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -run 'TestScheduleEndSession' -count=1 -race
```
Expected: FAIL to compile — `svc.ScheduleEndSession undefined` and
`svc.sessionGrace undefined`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract:

In `service.go`, add the three fields to `Service` and initialize them in
`NewService`: `sessionGrace: SessionGrace` and
`pending: make(map[string]chan struct{})`. `mu` needs no initialization.

In `grace.go`:

- `SessionGrace = 30 * time.Second`, with a comment carrying Decision 5's
  reasoning (covers a refresh and a brief blip; sits under
  `RefundGrace`'s 60 s).
- `sessionKey(roomID, userID string) string` returning `roomID + "\x00" + userID`
  — a NUL separator so no ID content can forge a collision.
- `ScheduleEndSession(roomID, userID string, guest bool)`:
  - `if guest { return }`.
  - Lock; if an entry exists for the key, close its channel (cancelling the
    previous window) and replace it with a fresh `done := make(chan struct{})`;
    store it; unlock.
  - `go` a func that `select`s on `time.NewTimer(s.sessionGrace).C`,
    `<-done`, and `<-s.ctx.Done()`. On `done` or a cancelled base context,
    return without folding. On the timer, re-lock and confirm
    `s.pending[key] == done` before proceeding — if it was replaced, that
    replacement owns the fold and this goroutine returns. If it matches,
    `delete` the entry, unlock, then call
    `s.EndSession(s.ctx, roomID, userID, false)` and log any error with the
    same `log.Printf` shape `timer.go` uses. `defer timer.Stop()`.

The identity re-check under the lock is what makes a
schedule→resume→schedule sequence safe; without it a stale goroutine can fold
a session a live connection is still using.

- Run the whole package under `-race` (the command below does): the pending
  map is touched from the HTTP goroutine and the timer goroutine both.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -count=1 -race && \
  git add internal/round/grace.go internal/round/grace_test.go internal/round/service.go && \
  git commit -m "feat: fold a disconnected session after a grace window"
```

Expected: PASS, then one commit.

**Checkpoint 2: a resume inside the window cancels the fold**

- [ ] **Step 1: Write the failing test, then run it**

Spec: in `grace_test.go`, add `TestResumeSessionCancelsAPendingEnd`.

Arrange: identical to Checkpoint 1 — persistent `5000`, joined at `1000`,
wallet at `w != 1000`, `svc.sessionGrace = 200 * time.Millisecond`.

Act: `svc.ScheduleEndSession(roomID, userID, false)`; sleep 50 ms;
`svc.ResumeSession(roomID, userID)`; sleep 500 ms (well past the window).

Assert:
- `store.User(ctx, userID).Balance == 5000` — untouched, no fold ran;
- `store.Balance(ctx, roomID, userID) == w` — the session survived intact,
  which is the whole point of the feature;
- `store.OpeningStake(ctx, roomID, userID) == 1000`.

Add a second case, `TestResumeSessionWithNothingPending`: call
`ResumeSession` for a user with no scheduled end and assert it neither panics
nor changes any balance — a reconnect that was never preceded by a disconnect
is the ordinary first-connect case and must be harmless.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -run TestResumeSession -count=1 -race
```
Expected: FAIL to compile — `svc.ResumeSession undefined`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `ResumeSession(roomID, userID string)` locks, looks up
`sessionKey(roomID, userID)`, and if an entry exists closes its channel and
deletes it, then unlocks. Absent entry: return without doing anything. No
return value — nothing in the system branches on whether an end was pending
(Decision 6's frontend is not told, and the ws layer calls this
unconditionally on connect).

Document that this is safe to call for a guest too, so the caller needs no
guest branch on the connect path.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/round/ -count=1 -race && \
  git add internal/round/grace.go internal/round/grace_test.go && \
  git commit -m "feat: cancel a pending session end when a player reconnects"
```

Expected: PASS, then one commit.

**Task boundary — full suite:**

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make test
```
Expected: PASS.

---

### Task 6: Wire the grace window into the socket

**Files:**
- Modify: `backend/internal/ws/handler.go:40-46` (the interface),
  `:98` (connect path), `:132-144` (disconnect callback)
- Test: `backend/internal/ws/handler_test.go`
- Modify: `backend/internal/httpapi/ws_handlers.go:17-41`

**Interfaces:**
- Consumes: `round.Service`'s `ResumeSession` and `ScheduleEndSession` from
  Task 5 — satisfied structurally, `internal/ws` still imports no
  `internal/round`.
- Produces: `ws.Sessions`, replacing `ws.SessionEnder`:

```go
type Sessions interface {
    ResumeSession(roomID, userID string)
    ScheduleEndSession(roomID, userID string, guest bool)
}
```

  `Handler`'s parameter list is otherwise unchanged, and a `nil` value still
  disables both calls — every existing `handler_test.go` call site passes
  `nil` positionally and keeps compiling untouched.

**Checkpoint 1: a disconnect schedules an end instead of folding one**

- [ ] **Step 1: Write the failing test, then run it**

Spec: in `handler_test.go`, add a `fakeSessions` recorder in the test file —
a struct with a mutex, a `[]string` of resumed keys and a `[]string` of
scheduled keys (formatted `roomID + "/" + userID`, plus the guest flag for
scheduled), implementing both `Sessions` methods.

Add `TestHandlerSchedulesSessionEndOnDisconnect`: stand up
`httptest.NewServer(Handler(hub, issuer, DefaultClientConfig(), nil, fake))`,
connect one client with a room-scoped token for a known user, then close the
client connection and wait for the handler's close callback to run (follow
whatever synchronization the neighboring disconnect tests in this file
already use — e.g. polling `room.Count()` down to zero with a deadline;
do not add a bare `time.Sleep` if the file has an established helper).

Assert `fake`'s scheduled slice contains exactly one entry, for the token's
room and user ID, with `guest` matching the token's claim.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -run TestHandlerSchedulesSessionEndOnDisconnect -count=1 -race
```
Expected: FAIL to compile — `fakeSessions` does not implement
`ws.SessionEnder` (it has no `EndSession` method), and `ws.Sessions` is
undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: rename the `SessionEnder` interface to `Sessions` and replace its
single method with `ScheduleEndSession(roomID, userID string, guest bool)`
only — `ResumeSession` arrives in Checkpoint 2, so this checkpoint's test
compiles against a one-method interface and the fake's extra method is
harmless. Update the parameter type on `Handler` and its doc comment.

In the `ReadPump` close callback, replace the
`sessions.EndSession(context.Background(), ...)` block with
`sessions.ScheduleEndSession(claims.RoomID, claims.UserID, claims.Guest)`.
The `context` import may become unused in `handler.go` — remove it if so, and
note that `Handler` no longer needs a context at all because the fold now
runs against `round.Service`'s base context (Task 5), which is the correct
owner: a fold must not be cancelled by the request that is ending.

Update `httpapi/ws_handlers.go`: `var sessions ws.Sessions`, and rewrite the
typed-nil comment above it to name the new interface (the gotcha it describes
is unchanged and still applies).

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ ./internal/httpapi/ -count=1 -race && \
  git add internal/ws/handler.go internal/ws/handler_test.go internal/httpapi/ws_handlers.go && \
  git commit -m "refactor: schedule a session end on disconnect instead of folding"
```

Expected: PASS, then one commit.

**Checkpoint 2: a connect resumes any pending end**

- [ ] **Step 1: Write the failing test, then run it**

Spec: add `TestHandlerResumesSessionOnConnect` using the same `fakeSessions`.
Connect one client with a room-scoped token and, once the connection is
established (assert on the `connected` frame the handler already sends, so
the check has a real happens-before edge rather than a sleep), assert `fake`'s
resumed slice contains exactly one entry for that room and user.

Add a second case in the same test: connect, disconnect, then connect again
with the same token, and assert the resumed slice has two entries and the
scheduled slice one — the reconnect ordering the whole feature exists to
support.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ -run TestHandlerResumesSessionOnConnect -count=1 -race
```
Expected: FAIL to compile — `fakeSessions` is assigned to a `Sessions`
interface that has no `ResumeSession` method, so the test's assertion on
`fake.resumed` compiles but the handler never populates it; if written
against the interface it fails at the empty-slice assertion instead.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add `ResumeSession(roomID, userID string)` to the `Sessions`
interface. In `Handler`, call
`sessions.ResumeSession(claims.RoomID, claims.UserID)` immediately after the
successful `upgrader.Upgrade` and **before** `hub.Join` — as early as the
connection is real, so the window between a reconnect and the cancel is as
small as it can be. Keep it inside the existing `sessions != nil` guard
pattern.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/ws/ ./internal/httpapi/ ./internal/round/ -count=1 -race && \
  git add internal/ws/handler.go internal/ws/handler_test.go && \
  git commit -m "feat: cancel a pending session end when a socket reconnects"
```

Expected: PASS, then one commit.

**Task boundary — full suite, plus a manual end-to-end check:**

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make test
```
Expected: PASS.

Then verify the feature by hand, because no automated test in this repo puts
a real browser through a refresh: `make up`, start the API, `make fe-dev`,
create a room in one browser, join with a second, place a wager so the
wallet moves off the buy-in, then reload the joiner's page. The reloaded page
must show the moved balance, not the buy-in. Record the observed before/after
balances in the journal entry (Task 8) — this is the phase's user-visible
deliverable and deserves stated evidence rather than an assumption.

---

### Task 7: README with an architecture diagram

**Files:**
- Modify: `README.md` (substantial rewrite — the current file is quick-start only)

**Interfaces:** None — documentation.

**Checkpoint 1: the README explains the system and diagrams it**

This checkpoint has no RED→GREEN cycle: it is prose, and there is no failing
test to write for it. It is a single commit at the end of the task rather
than a two-step checkpoint — the one place in this plan where that is
correct, because inventing an assertion here would be theater.

- [ ] **Step 1: Write the README, then verify and commit**

Content, in this order. Keep the existing environment-variable sections
verbatim — they are accurate and hard-won — and build around them.

1. **What CallIt is** — two or three sentences, from the spec's §1: a host
   runs short prediction rounds during a group watch party, participants
   wager virtual tokens, a pari-mutuel engine settles when the host resolves.
   Link the spec and the implementation plan.

2. **Architecture diagram** — a Mermaid `flowchart LR` in a ```mermaid fence
   (GitHub renders it; no toolchain, no image to keep in sync). It must show
   the real write path, because that path is the project's central design
   decision:

   - `Browser (Next.js)` → `cmd/api` over both REST and WebSocket.
   - `cmd/api` → `Redis` labelled with the atomic Lua write (balance mutation
     + `XADD` to `wager-outbox`, one atomic unit).
   - `Redis` → `cmd/relay` reading the outbox stream.
   - `cmd/relay` → `Kafka` (`wagers-placed`, `rounds-settled`).
   - `Kafka` → `cmd/ledger-worker` → `PostgreSQL` (double-entry ledger).
   - `cmd/api` → `metrics listener` as a side node, marked optional.
   - A styled note or subgraph boundary making the invariant visible: the API
     process never writes PostgreSQL. Draw no edge from `cmd/api` to
     `PostgreSQL` — the diagram's job here is to make the missing edge
     conspicuous.

3. **Why the write path is shaped that way** — one short paragraph: the
   transactional outbox closes the crash window where Redis could debit a
   wallet while the ledger never learned of it. Link `CLAUDE.md`'s Critical
   Invariants rather than restating them.

4. **Running it** — a table of the five binaries under `backend/cmd/` (`api`,
   `relay`, `ledger-worker`, `migrate`, `callit-cli`), each with its `make`
   target and one line on what it does and what it needs running first
   (`make migrate` before `make ledger-worker`, and that `ledger-worker`
   never migrates). Then the existing backend and frontend quick-starts.

5. **Environment variables** — the existing `JWT_SECRET`,
   `CORS_ALLOWED_ORIGINS`, `METRICS_ADDR`, `NEXT_PUBLIC_API_BASE_URL`
   sections, carried over unchanged, plus `JWT_TTL`, `REDIS_ADDR`,
   `POSTGRES_DSN`, and `KAFKA_BROKERS` with their defaults from `CLAUDE.md`'s
   Build & Test section.

6. **Testing and load** — `make test` (brings the full stack up),
   `make test-unit`, the frontend targets, and `make loadtest` /
   `make loadtest-api` with a pointer to `loadtest/README.md` and the phase
   7a/7b baseline reports under `docs/reports/`.

7. **Security posture** — a short, honest section: argon2id password hashing;
   HS256-pinned JWT verification that rejects `alg: none`; one sliding-window
   rate limiter across the refill, auth, api, and ws-connect scopes;
   login responses identical in body *and* time (this phase); and the one
   deployment-level caveat, stated plainly — local dev runs Kafka PLAINTEXT
   with no ACLs, and broker access is equivalent to ledger-write access, so
   broker restrictions must be in place before any shared deployment.

Verify the Mermaid block parses before committing — paste-free check:
confirm the fence is exactly ```mermaid, every node id is referenced by at
least one edge, and no node label contains an unescaped `(`, `)`, or `"`
outside quotes (the three things that break GitHub's renderer silently).

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd /home/chikara/projects/call_it && \
  git add README.md && \
  git commit -m "docs: rewrite the README around an architecture diagram"
```

Expected: one commit.

---

### Task 8: Close the phase out

**Files:**
- Modify: `docs/specs/2026-08-21-callit-design.md` (§4's known-limitation bullet)
- Modify: `CLAUDE.md` (Critical Invariants — one new entry)
- Modify: `frontend/lib/socket.ts:38-43` (comment only)
- Modify: `docs/project-history.md` (a Phase 7c section; amend the
  "Open by design" list)
- Modify: `docs/plans/2026-08-21-implementation-plan.md` (§9 row 7c → ✅; §12 boxes)
- Create: `journal/YYYY-MM-DD_HHMM_<name>_phase-7c-execution.md`

**Interfaces:** None — documentation and verification.

**Checkpoint 1: the docs match the code, and the phase is verified**

No RED→GREEN cycle, same reasoning as Task 7. One commit for the doc set, and
the verification steps that gate it.

- [ ] **Step 1: Update every doc this phase invalidated**

1. **Spec §4** — the "Known limitation: no reconnect-with-session-resume"
   bullet is now false. Replace it with a statement of the implemented
   behavior: a disconnect starts a 30-second grace window, a reconnect inside
   it resumes the session intact, and a session that does expire is folded
   exactly once and cleared. Keep the bullet in the same place so §4's
   structure is unchanged, and note that auto-reconnect on the client is not
   implemented — a page reload is what exercises resume today.

2. **`CLAUDE.md` Critical Invariants** — add one entry, phrased like its
   neighbors: *a session's result is folded exactly once, and the fold claims
   the session before it credits.* State that `Store.ClearSession`'s
   opening-stake `HDEL` reply is the claim token, that the wallet cannot serve
   as one because `settle_round.lua` can resurrect a deleted wallet field via
   `HINCRBY`, and that reordering to credit-then-claim would mint tokens on a
   retry. Also amend the existing invariant that mentions the reconciliation
   identity to say the identity applies to a *live* session — a folded session
   has no Redis wallet left to compare.

3. **`frontend/lib/socket.ts`** — the comment at lines 38-43 says reconnecting
   "would re-fire the backend's `EndSession` cycle and silently reset the
   player to the room buy-in." That is no longer true. Rewrite it: the
   backend now holds a 30-second grace window, so a reconnect resumes the
   session; a reconnect timer with backoff is deliberately still not
   implemented here (Phase 8 candidate), and 6a's closed-status surface is
   unchanged. Comment only — no behavior change.

4. **`docs/project-history.md`** — add a `### Phase 7c` section recording what
   each of the three items became, and edit the "Open by design, deferred to
   Phase 7 hardening" list: items 1, 3, and 4 are now closed (say where), item
   2 (room-code modulo bias) stays permanently accepted with its existing
   reasoning. Record the `security-reviewer` findings from step 2 below.

5. **Parent plan** — mark §9's row 7c ✅, and settle §12's remaining boxes with
   evidence rather than optimism: check "Concurrency suite proves zero
   double-spend under contention" against
   `internal/redisstore/concurrency_test.go` and name the test; check "Test
   coverage meets the project's 80% minimum" against the figure step 2
   produces; check "Security review run against the auth and wager-placement
   paths" against step 2's run. If any of the three does not actually hold,
   leave it unchecked and say why — an unchecked box is a finding, not a
   failure to tidy.

6. **Journal entry** — via the `journal` skill, including the manual
   refresh-test numbers from Task 6's task boundary.

- [ ] **Step 2: Verify, then commit**

Run the security review first, since its findings may change what the docs
say:

- Launch the `security-reviewer` agent over this phase's diff
  (`git diff dev...HEAD`), scoped to the three surfaces it touches: the login
  path (`internal/auth`, `internal/account`), the session-fold money path
  (`internal/round`, `internal/redisstore`), and the Kafka decode boundary
  (`internal/events`). Fix anything CRITICAL or HIGH before proceeding;
  record MEDIUM/LOW findings and their disposition in `project-history.md`.

Then the numbers:

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make lint && make test && \
  cd backend && go test ./... -coverpkg=./... -count=1 -p 1 2>&1 | tail -30
```
Expected: `go vet` and `gofmt -l` clean, full suite PASS, and a total coverage
figure at or above 80% — judged from `-coverpkg=./...`, never the per-package
column (`CLAUDE.md`, Testing). `internal/domain` must still read 100%.

Frontend, because Task 8 edits a `.ts` file:

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make fe-lint && make fe-test
```
Expected: PASS.

```bash
cd /home/chikara/projects/call_it && \
  git add README.md CLAUDE.md docs/ frontend/lib/socket.ts journal/ && \
  git commit -m "docs: close the three deferred security items and record Phase 7c"
```

Expected: one commit. The branch is now green and verified.

**Stop here.** Merging is not this plan's to do — `executing-plans` Step 3
hands off to `finishing-a-development-branch`, which owns the merge decision.

---

## Self-Review

**1. Spec coverage.** The three items from `docs/project-history.md`'s
"Open by design" list map to Task 1 (login timing), Tasks 3–6 (reconnect ends
a session), and Task 2 (`RoundSettled.Payouts` cap). Item 2 (room-code modulo
bias) is permanently accepted, not deferred work, and Task 8 step 4 records
that explicitly so a later reader does not go looking for its fix. The parent
plan's 7c row also names "the README with an architecture diagram" — Task 7.
Spec §4's known-limitation bullet is invalidated by Tasks 3–6 and updated in
Task 8. No requirement in the 7c row is unassigned.

**2. Placeholder scan.** No "TBD", no "add appropriate error handling", no
"similar to Task N". Every checkpoint names exact inputs and exact expected
errors or values. The two doc tasks (7, 8) deliberately carry no RED→GREEN
cycle and say so in place of faking one — that is the honest shape for prose,
and both still end in a specific, verifiable commit.

**3. Type consistency.** `ClearSession` returns `(claimed bool, err error)` in
Task 3 and is consumed under that exact signature in Task 4.
`ScheduleEndSession(roomID, userID string, guest bool)` and
`ResumeSession(roomID, userID string)` are declared in Task 5 and consumed
under identical signatures by the `ws.Sessions` interface in Task 6.
`VerifyDecoyPassword(plain string)` returns nothing in both Task 1
checkpoints. `MaxMessageBytes` and `MaxPayouts` are both exported ints in
`internal/events`. `EndSession`'s signature is unchanged throughout.

**4. Delegation eligibility.** Skipped — this plan runs fully inline. Task 2
would qualify as mechanical, but it is one task of eight and the rest are the
phase's flagship correctness work (a money-fold made once-only, a timing
oracle closed); splitting one task out to a cold subagent would cost more
context handoff than it saves.

**One gap accepted and stated rather than closed:** a payout credited to a
player who has already departed lands in a resurrected Redis wallet field with
no opening stake beside it, and is never folded into their persistent balance.
This is today's behavior exactly — the fold already misses a post-departure
payout — so this phase neither fixes nor worsens it. Fixing it means deciding
what settlement owes an absent player, which is a product question, not a
security one. Recorded in Task 8's `project-history.md` entry as a known
limitation, and a Phase 8 candidate.
