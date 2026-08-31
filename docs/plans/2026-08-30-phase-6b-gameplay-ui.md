# Phase 6b — Gameplay UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Delegation:** Tasks 4–9 are delegated, one subagent per task, via the
`delegating-plan-tasks` skill — each is a presentational component or a
self-contained utility built against a contract this plan states in full.
Tasks 1, 2, 3, and 10 are executed inline: Task 1 changes a JWT claim set and
the socket's disclosure surface, Tasks 2–3 are the phase's flagship
correctness work (the state machine that must not reconstruct per-user wager
data, and the balance arithmetic), and Task 10 is acceptance evidence plus the
security review.

**Goal:** Turn the Phase 6a room shell into a playable round — the host opens
and resolves rounds, participants wager, everyone watches live odds, a lockout
countdown, an aggregate bettors counter, and a settlement reveal, with Web
Audio cues at the three phase changes.

**Architecture:** All gameplay state for a room lives in one pure reducer
(`lib/roundState.ts`) driven by the socket events Phase 4b already broadcasts.
The reducer is the client-side counterpart of `internal/domain` — no I/O, no
React, unit-testable with nothing running. Components are presentational: they
receive a `RoundState` slice and emit intent callbacks; the room page is the
only place that owns a socket and translates callbacks into `socket.send`.
Three small backend gaps that block the UI are closed first, in Task 1.

**Tech Stack:** Next.js App Router (client components), React 19, TypeScript
`strict: true`, Tailwind, Vitest + React Testing Library, Playwright, Web Audio
API. Backend changes are Go 1.22.10 in `internal/auth`, `internal/room`,
`internal/wager`, and `internal/ws`.

**Spec:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md)
— §4 (Gameplay & Round Lifecycle) is the section this plan implements; §3
supplies the wallet and partial-buy-in rules the wager pad enforces; §2 names
the Web Audio API as part of the frontend stack.

**Parent plan:** [`docs/plans/2026-08-21-implementation-plan.md`](2026-08-21-implementation-plan.md) §9, Phase 6b row.

## Global Constraints

- **Wagers stay anonymous until the round is terminal.** No component, reducer
  branch, or socket handler may derive or display who backed which outcome, or
  for how much, before `round_resolved` or `round_refunded`. The reveal is
  `ResolvedEvent.Results` and nothing earlier. (CLAUDE.md; spec §4.)
- **The only permitted in-round progress signal is the aggregate bettors
  count** — "N/M players have placed their bets". `M` is `OddsEvent.players`
  used **verbatim**: `redisstore.PlayerCount` is already `HLEN wallets - 1`
  (`internal/redisstore/room.go:187`), so the host is *already* excluded. Do
  not subtract the host again.
- **All amounts are integer token units.** Odds/multipliers are the only
  floats, and only at the presentation layer.
- **Lockout is enforced server-side.** The countdown is display-only;
  `round_locked` is the authority. `lock_at_ms` is an absolute server
  `UnixMilli` (`internal/round/service.go:83,92`), so a skewed browser clock
  makes the countdown wrong but never changes what is accepted.
- **Never run `go get -u`**, and pin every `go get` target explicitly. This
  phase adds no new dependencies, frontend or backend.
- **`internal/domain` stays free of I/O**, and settlement math is never
  recomputed — not in Lua, and not in TypeScript. The client displays
  `ResolvedEvent`; it never re-derives a payout.
- Coverage floors: backend 80% (`internal/domain` 100%); frontend 80% via
  `vitest run --coverage` over `lib/**` and `components/**`, with `app/**`
  excluded.
- Go tool calls need the PATH export, **quoted** — this WSL2 environment's
  inherited Windows `PATH` contains spaces:
  `export PATH="$PATH:$HOME/.local/go/bin:$HOME/go/bin"`.

---

## Backend Contract This Plan Consumes

Read once before Task 1; every later task depends on these exact shapes.

**Inbound (client → server), `internal/ws/router.go`:**

| Type | Payload |
|---|---|
| `create_round` | `{question: string, outcomes: string[], lock_in_ms: number}` |
| `place_wager` | `{outcome: number, amount: number, idempotency_key: string}` |
| `resolve_round` | `{winning_outcome: number}` |

**Outbound (server → client):**

| Type | Payload | Source |
|---|---|---|
| `connected` | `{user_id, display_name, room_id, guest}` | `internal/ws/protocol.go` |
| `player_joined` / `player_left` | `{user_id, display_name, player_count}` | `internal/ws/protocol.go` |
| `round_opened` | `{round_id, question, outcomes[], lock_at_ms}` | `round.Opened` |
| `odds_updated` | `{round_id, pools[], total, multipliers[], bettors, players}` | `wager.OddsEvent` |
| `round_locked` | `{round_id}` | `round.LockedEvent` |
| `round_resolved` | `{round_id, winning_outcome, results[], dust, refunded}` | `round.ResolvedEvent` |
| `round_refunded` | `{round_id, total}` | `round.RefundedEvent` |
| `error` | `{code, message}` | `internal/ws/protocol.go`, private to sender |

`results[]` rows are `{user_id, display_name, staked, returned, net}`.

**Two distinct refund paths — do not conflate them:**
- `round_resolved` with `refunded: true` — nobody backed the winning outcome,
  so every stake is returned. `results[]` is still populated.
- `round_refunded` — the host-disconnect fallback: the round locked and went
  unresolved for 60 seconds. Carries only `round_id` and `total`, with **no
  per-player rows at all**.

**Validation bounds the host console must respect** (server rejects otherwise):
`MinOutcomes = 2`, `MaxOutcomes = 4` (`internal/domain/round.go:54-55`);
`MinLockIn = 3s`, `MaxLockIn = 120s` (`internal/round/service.go:16-17`);
question must be non-empty after trimming.

**Error codes** `replyServiceError` can send: `host_cannot_bet`, `pool_locked`,
`not_in_room`, `insufficient_funds`, `invalid_outcome`, `not_host`,
`round_in_progress`, `no_active_round`, `bad_idempotency_key`, `rate_limited`,
`malformed`, `unknown_type`, `internal_error`, plus `invalid_spec` added by
Task 1 CP3.

---

## File Structure

**Backend (Task 1 only):**
- Modify `backend/internal/auth/token.go` — add `Host` to `Claims`, issue and parse it.
- Modify `backend/internal/room/service.go:81,147` — set `Host` at the two room-token issue sites.
- Modify `backend/internal/ws/protocol.go` — `Host` on `ConnectedEvent`; new `TypeWagerAccepted` + `WagerAcceptedEvent`.
- Modify `backend/internal/ws/handler.go:100` — populate `Host` from claims.
- Modify `backend/internal/ws/router.go` — send `wager_accepted`; map `invalid_spec`.
- Modify `backend/internal/wager/service.go` — add `RoundID` to `Accepted`.

**Frontend:**
- Modify `frontend/lib/protocol.ts` — the 6b wire types. Types only, no logic.
- Create `frontend/lib/roundState.ts` — the pure reducer. **The correctness core of this phase.**
- Create `frontend/lib/countdown.ts` — pure remaining-time math plus the ticking hook.
- Create `frontend/lib/audio.ts` — Web Audio cues behind an injectable context factory.
- Create `frontend/components/OddsBoard.tsx` — pools, multipliers, bettors counter.
- Create `frontend/components/WagerPad.tsx` — outcome selection and stake entry.
- Create `frontend/components/HostConsole.tsx` — open-round form and resolve picker.
- Create `frontend/components/SettlementReveal.tsx` — the terminal-state reveal.
- Modify `frontend/app/room/[code]/page.tsx` — compose all of the above.
- Modify `frontend/e2e/join.spec.ts` or create `frontend/e2e/round.spec.ts` — the full-round acceptance test.

Components stay presentational so they are testable without a socket and
reusable if the room page is ever split. The reducer holds every rule that
could leak wager data or miscount money, in one file with no React import.

---

### Task 1: Backend — the three socket-contract gaps 6b needs

**Executed inline** — this changes a JWT claim set and adds a new disclosure to
the socket, both of which Task 10's security review examines.

**Files:**
- Modify: `backend/internal/auth/token.go`
- Modify: `backend/internal/room/service.go:81,147`
- Modify: `backend/internal/ws/protocol.go`
- Modify: `backend/internal/ws/handler.go:100-105`
- Modify: `backend/internal/ws/router.go`
- Modify: `backend/internal/wager/service.go`
- Test: `backend/internal/auth/token_test.go`, `backend/internal/ws/handler_test.go`, `backend/internal/ws/router_test.go`

**Interfaces:**
- Consumes: `auth.Claims{UserID, DisplayName, RoomID, Guest}`; `wager.Accepted{Balance, Pools, Total, Multipliers, Bettors, Players}`; `Client.Send(payload []byte)`.
- Produces:
  - `auth.Claims` gains `Host bool` (JSON claim key `"host"`).
  - `ws.ConnectedEvent` gains a `Host bool` field with the JSON tag `host`.
  - `ws.TypeWagerAccepted = "wager_accepted"`, plus:

```go
type WagerAcceptedEvent struct {
	RoundID string `json:"round_id"`
	Outcome int    `json:"outcome"`
	Amount  int64  `json:"amount"`
	Balance int64  `json:"balance"`
}
```

  - `wager.Accepted` gains `RoundID string`.
  - Router error code `"invalid_spec"` for `round.ErrInvalidSpec`.

**Why these three, and why now.** The 6a room page renders identically for host
and player, has no way to learn a player's balance after a wager, and turns a
bad round spec into "an internal error occurred". Each is a prerequisite of a
6b deliverable and none is verifiable without a browser client — the same
reasoning that put CORS in 6a as Task 1 rather than a micro-phase.

**Checkpoint 1: the socket tells a client whether it is the host**

- [ ] **Step 1: Write the failing test, then run it**

Two tests.

In `internal/auth/token_test.go`: issue a token from
`Claims{UserID: "u1", DisplayName: "Ann", RoomID: "r1", Guest: false, Host: true}`,
verify it, assert the returned `Claims.Host == true`. A second case with
`Host: false` round-trips to `false`.

In `internal/ws/handler_test.go`: connect a client whose room token carries
`Host: true`, read the first envelope, assert it is type `connected` and its
decoded `data.host == true`. A second client with `Host: false` gets
`data.host == false`.

Run: `cd backend && export PATH="$PATH:$HOME/.local/go/bin:$HOME/go/bin" && go test ./internal/auth/ ./internal/ws/ -run 'Host' -count=1 -race`
Expected: FAIL — `Claims` has no field `Host` (compile error), and
`ConnectedEvent` has no field `Host`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `auth.Claims` gains `Host bool`, written to the `"host"` map claim on
issue and read back on parse with the same `claims["host"].(bool)` guarded
type-assertion pattern the existing `guest` claim uses. `room.Service` issues
`Host: true` at `service.go:81` (room creation — the caller is the host by
construction) and `Host: false` at `service.go:147` (join). `ws.ConnectedEvent`
gains a `Host bool` field tagged `json:"host"`, populated from `claims.Host`
at `handler.go:100`. The claim is advisory-for-rendering only: `round.Service`
keeps re-checking `rm.HostID != callerID` against Redis, which stays the sole
authority for host-gated actions.

```bash
cd backend && export PATH="$PATH:$HOME/.local/go/bin:$HOME/go/bin" && \
  go test ./internal/auth/ ./internal/ws/ ./internal/room/ -count=1 -race && \
  git add internal/auth/token.go internal/auth/token_test.go internal/room/service.go internal/ws/protocol.go internal/ws/handler.go internal/ws/handler_test.go && \
  git commit -m "feat: tell a connected client whether it hosts the room"
```

Expected: PASS, then one commit.

**Checkpoint 2: a placed wager privately reports the placer's new balance**

- [ ] **Step 1: Write the failing test, then run it**

In `internal/ws/router_test.go`, using the existing test doubles: a player
sends `place_wager` with `{outcome: 0, amount: 100, idempotency_key: <uuid>}`
against a stubbed `wagerService` returning
`wager.Accepted{RoundID: "rd1", Balance: 900, ...}`. Assert the sending client
receives exactly one envelope of type `wager_accepted` whose data is
`{round_id: "rd1", outcome: 0, amount: 100, balance: 900}`.

A second case: when `Place` returns an error, assert the client receives an
`error` envelope and **no** `wager_accepted`.

Run: `cd backend && export PATH="$PATH:$HOME/.local/go/bin:$HOME/go/bin" && go test ./internal/ws/ -run 'WagerAccepted' -count=1 -race`
Expected: FAIL — `TypeWagerAccepted` is undefined and the router currently
discards `Place`'s first return value (`_, err := r.wagers.Place(...)`).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `wager.Accepted` gains `RoundID string`, set from the `roundID` local
already in scope in `Service.Place`. The router binds the result instead of
discarding it and, on success, sends `TypeWagerAccepted` with
`WagerAcceptedEvent{RoundID: accepted.RoundID, Outcome: p.Outcome, Amount: p.Amount, Balance: int64(accepted.Balance)}`
to the sender **only**, via `c.Send` — the same private-reply path `replyError`
uses.

This does not weaken the anonymity invariant: it discloses a player's own
stake and own balance to that player alone, over their own connection. Nothing
is broadcast, and no other client learns of it. The room-wide `odds_updated`
broadcast is unchanged and still carries pool totals only.

```bash
cd backend && export PATH="$PATH:$HOME/.local/go/bin:$HOME/go/bin" && \
  go test ./internal/ws/ ./internal/wager/ -count=1 -race && \
  git add internal/ws/protocol.go internal/ws/router.go internal/ws/router_test.go internal/wager/service.go && \
  git commit -m "feat: privately confirm a wager with the placer's new balance"
```

Expected: PASS, then one commit.

**Checkpoint 3: a bad round spec gets an actionable error code**

- [ ] **Step 1: Write the failing test, then run it**

In `internal/ws/router_test.go`: a host sends `create_round` against a stubbed
`roundService` returning `round.ErrInvalidSpec`. Assert the client receives an
`error` envelope with `code == "invalid_spec"`.

Run: `cd backend && export PATH="$PATH:$HOME/.local/go/bin:$HOME/go/bin" && go test ./internal/ws/ -run 'InvalidSpec' -count=1 -race`
Expected: FAIL — got `code == "internal_error"`, want `"invalid_spec"`.
`replyServiceError` has no `round.ErrInvalidSpec` branch, so it falls through
to the default.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add `case errors.Is(err, round.ErrInvalidSpec): r.replyError(c, "invalid_spec", err.Error())`
to `replyServiceError`, positioned with the other `round.*` branches.

```bash
cd backend && export PATH="$PATH:$HOME/.local/go/bin:$HOME/go/bin" && \
  go test ./internal/ws/ -count=1 -race && \
  git add internal/ws/router.go internal/ws/router_test.go && \
  git commit -m "fix: report an invalid round spec as invalid_spec, not internal_error"
```

Expected: PASS, then one commit.

**Task boundary:**

```bash
cd backend && export PATH="$PATH:$HOME/.local/go/bin:$HOME/go/bin" && \
  go vet ./... && gofmt -l . && go build ./... && go test ./... -race -cover -p 1 -count=1
```

Expected: PASS, and `gofmt -l .` prints nothing.

---

### Task 2: Protocol types and the reducer's phase transitions

**Executed inline** — this file is the phase's correctness core and Task 3
continues in it.

**Files:**
- Modify: `frontend/lib/protocol.ts`
- Create: `frontend/lib/roundState.ts`
- Test: `frontend/lib/roundState.test.ts`

**Interfaces:**
- Consumes: the wire shapes in "Backend Contract" above.
- Produces (every later frontend task depends on these exact names):

```ts
// lib/protocol.ts — additions, types only
export type RoundOpenedEvent = { round_id: string; question: string; outcomes: string[]; lock_at_ms: number };
export type OddsUpdatedEvent = { round_id: string; pools: number[]; total: number; multipliers: number[]; bettors: number; players: number };
export type RoundLockedEvent = { round_id: string };
export type ResultRow = { user_id: string; display_name: string; staked: number; returned: number; net: number };
export type RoundResolvedEvent = { round_id: string; winning_outcome: number; results: ResultRow[]; dust: number; refunded: boolean };
export type RoundRefundedEvent = { round_id: string; total: number };
export type WagerAcceptedEvent = { round_id: string; outcome: number; amount: number; balance: number };
// ConnectedEvent gains: host: boolean

// lib/roundState.ts
export type Phase = "idle" | "open" | "locked" | "revealed";

export type RoundState = {
  phase: Phase;
  self_id: string | null;
  is_host: boolean;
  round_id: string | null;
  question: string | null;
  outcomes: string[];
  lock_at_ms: number | null;
  pools: number[];
  total: number;
  multipliers: number[];
  bettors: number;
  players: number;
  balance: number;
  balance_at_open: number | null;
  my_stake: number;
  results: ResultRow[] | null;
  dust: number;
  refunded: boolean;
  refund_total: number | null;
};

export type RoundAction =
  | { type: "connected"; data: ConnectedEvent }
  | { type: "round_opened"; data: RoundOpenedEvent }
  | { type: "odds_updated"; data: OddsUpdatedEvent }
  | { type: "round_locked"; data: RoundLockedEvent }
  | { type: "wager_accepted"; data: WagerAcceptedEvent }
  | { type: "round_resolved"; data: RoundResolvedEvent }
  | { type: "round_refunded"; data: RoundRefundedEvent };

export function initialRoundState(balance: number): RoundState;
export function reduceRound(state: RoundState, action: RoundAction): RoundState;
```

`reduceRound` is pure and returns a **new** object on every change (never
mutates `state`), per the immutability rule in
`.claude/rules/ecc/common/coding-style.md`.

**Stale-event guard, applied by every round-scoped action** (`odds_updated`,
`round_locked`, `wager_accepted`, `round_resolved`, `round_refunded`): if
`state.round_id !== null && action.data.round_id !== state.round_id`, return
`state` unchanged. This drops a late broadcast from a previous round rather
than corrupting the current one.

**Checkpoint 1: `initialRoundState` and `connected` establish identity**

- [ ] **Step 1: Write the failing test, then run it**

`initialRoundState(1000)` returns `phase: "idle"`, `balance: 1000`,
`self_id: null`, `is_host: false`, `round_id: null`, `outcomes: []`,
`pools: []`, `total: 0`, `multipliers: []`, `bettors: 0`, `players: 0`,
`balance_at_open: null`, `my_stake: 0`, `results: null`, `dust: 0`,
`refunded: false`, `refund_total: null`.

`reduceRound(initialRoundState(1000), {type: "connected", data: {user_id: "u1", display_name: "Ann", room_id: "r1", guest: false, host: true}})`
returns a state with `self_id: "u1"` and `is_host: true`, every other field
unchanged, and is **not** the same object reference as the input.

Run: `cd frontend && npx vitest run lib/roundState.test.ts`
Expected: FAIL — `lib/roundState.ts` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `initialRoundState(balance)` returns the literal above.
`reduceRound` handles `connected` by returning
`{...state, self_id: data.user_id, is_host: data.host}`.

```bash
cd frontend && npx vitest run lib/roundState.test.ts && \
  git add lib/protocol.ts lib/roundState.ts lib/roundState.test.ts && \
  git commit -m "feat: establish room identity in the round state reducer"
```

Expected: PASS, then one commit.

**Checkpoint 2: `round_opened` starts a round and snapshots the balance**

- [ ] **Step 1: Write the failing test, then run it**

From a state with `balance: 1000` and `self_id: "u1"`, applying
`{type: "round_opened", data: {round_id: "rd1", question: "Next goal?", outcomes: ["Home", "Away"], lock_at_ms: 1735000000000}}`
returns: `phase: "open"`, `round_id: "rd1"`, `question: "Next goal?"`,
`outcomes: ["Home", "Away"]`, `lock_at_ms: 1735000000000`, `pools: [0, 0]`
(one zero per outcome), `total: 0`, `multipliers: []`, `bettors: 0`,
`balance_at_open: 1000`, `my_stake: 0`, `results: null`, `dust: 0`,
`refunded: false`, `refund_total: null`. `balance` stays `1000`.

A second case proves a *new* round clears the previous one's reveal: from a
state with `phase: "revealed"`, `results: [<one row>]`, `refunded: true`,
`round_id: "rd1"`, applying `round_opened` for `round_id: "rd2"` returns
`phase: "open"`, `results: null`, `refunded: false`, `round_id: "rd2"`.
(The stale-event guard deliberately does not apply to `round_opened` — a new
round always supersedes.)

Run: `cd frontend && npx vitest run lib/roundState.test.ts`
Expected: FAIL — no `round_opened` case; state comes back unchanged with
`phase: "idle"`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `round_opened` resets every round-scoped field, sets `phase: "open"`,
sizes `pools` as `new Array(outcomes.length).fill(0)`, and records
`balance_at_open: state.balance`. That snapshot is what Task 3's resolve and
refund arithmetic anchors on.

```bash
cd frontend && npx vitest run lib/roundState.test.ts && \
  git add lib/roundState.ts lib/roundState.test.ts && \
  git commit -m "feat: open a round and snapshot the balance it starts from"
```

Expected: PASS, then one commit.

**Checkpoint 3: `odds_updated` records pool totals only**

- [ ] **Step 1: Write the failing test, then run it**

From an open round `rd1`, applying
`{type: "odds_updated", data: {round_id: "rd1", pools: [300, 100], total: 400, multipliers: [1.333, 4], bettors: 2, players: 5}}`
returns `pools: [300, 100]`, `total: 400`, `multipliers: [1.333, 4]`,
`bettors: 2`, `players: 5`, with `phase` still `"open"` and `balance`,
`my_stake`, and `balance_at_open` untouched.

Stale guard: the same state, given `odds_updated` with `round_id: "rd0"`,
returns the **identical object reference** (`toBe`, not `toEqual`).

Run: `cd frontend && npx vitest run lib/roundState.test.ts`
Expected: FAIL — no `odds_updated` case; `pools` stays `[0, 0]`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `odds_updated` copies exactly the six aggregate fields through, after
the stale guard. It must not touch `balance`, `my_stake`, or any per-user
field — there are none in this payload, and none may be inferred from it.

```bash
cd frontend && npx vitest run lib/roundState.test.ts && \
  git add lib/roundState.ts lib/roundState.test.ts && \
  git commit -m "feat: track live pool totals and the bettors count"
```

Expected: PASS, then one commit.

**Checkpoint 4: `round_locked` closes wagering**

- [ ] **Step 1: Write the failing test, then run it**

From an open round `rd1` with `pools: [300, 100]`, applying
`{type: "round_locked", data: {round_id: "rd1"}}` returns `phase: "locked"`
with `pools`, `total`, `multipliers`, `bettors`, `balance`, and `my_stake` all
unchanged.

Stale guard: `round_locked` for `round_id: "rd0"` returns the identical object
reference.

Run: `cd frontend && npx vitest run lib/roundState.test.ts`
Expected: FAIL — no `round_locked` case; `phase` stays `"open"`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `round_locked` sets `phase: "locked"` and nothing else.

```bash
cd frontend && npx vitest run lib/roundState.test.ts && \
  git add lib/roundState.ts lib/roundState.test.ts && \
  git commit -m "feat: close wagering when the round locks"
```

Expected: PASS, then one commit.

**Task boundary:**

```bash
cd frontend && npx vitest run && npm run lint && npx tsc --noEmit
```

Expected: PASS.

---

### Task 3: The reducer's money transitions

**Executed inline** — this is the balance arithmetic and the anonymity
boundary; it is the evidence behind the claim that the client never invents a
number.

**Files:**
- Modify: `frontend/lib/roundState.ts`
- Test: `frontend/lib/roundState.test.ts`

**Interfaces:**
- Consumes: everything Task 2 produced.
- Produces: no new exported names — three more `reduceRound` cases.

**The balance model, stated once.** Every number the client shows is anchored
to a server value; the client never re-derives a payout.
- At `round_opened`, `balance_at_open` snapshots the pre-round balance.
- During the round, `balance` comes **only** from `wager_accepted.balance`, the
  server's authoritative post-wager figure (Task 1 CP2).
- At `round_resolved`, `balance = balance_at_open + (my row's net, or 0 if I
  have no row)`. `ResultRow.net` is `returned - staked` as computed by
  `domain.Settle`.
- At `round_refunded`, `balance = balance_at_open` — a refund returns every
  stake, so the round is a no-op on the wallet. This is why the snapshot exists
  rather than per-player refund rows, which `RefundedEvent` does not carry.

**Checkpoint 1: `wager_accepted` applies the server's balance**

- [ ] **Step 1: Write the failing test, then run it**

From an open round `rd1` with `balance: 1000`, `balance_at_open: 1000`,
`my_stake: 0`, applying
`{type: "wager_accepted", data: {round_id: "rd1", outcome: 0, amount: 100, balance: 900}}`
returns `balance: 900` and `my_stake: 100`, with `balance_at_open` still
`1000` and `phase` still `"open"`.

A second wager on the same round —
`{round_id: "rd1", outcome: 1, amount: 50, balance: 850}` applied to that
result — returns `balance: 850` and `my_stake: 150` (stakes accumulate; the
balance is taken from the server, never decremented locally).

Stale guard: `wager_accepted` for `round_id: "rd0"` returns the identical
object reference.

Run: `cd frontend && npx vitest run lib/roundState.test.ts`
Expected: FAIL — no `wager_accepted` case; `balance` stays `1000`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: after the stale guard, return
`{...state, balance: data.balance, my_stake: state.my_stake + data.amount}`.
`balance` is assigned from the payload, never computed as
`state.balance - data.amount` — the server figure is authoritative and absorbs
idempotent replays.

```bash
cd frontend && npx vitest run lib/roundState.test.ts && \
  git add lib/roundState.ts lib/roundState.test.ts && \
  git commit -m "feat: apply the server's balance when a wager is accepted"
```

Expected: PASS, then one commit.

**Checkpoint 2: `round_resolved` reveals results and settles the balance**

- [ ] **Step 1: Write the failing test, then run it**

Three cases, all from a locked round `rd1` with `self_id: "u1"`,
`balance_at_open: 1000`, `balance: 900`, `my_stake: 100`.

*Winner.* Applying `round_resolved` with `winning_outcome: 0`, `dust: 3`,
`refunded: false`, and
`results: [{user_id: "u1", display_name: "Ann", staked: 100, returned: 250, net: 150}, {user_id: "u2", display_name: "Bob", staked: 100, returned: 0, net: -100}]`
returns `phase: "revealed"`, `results` equal to that array, `dust: 3`,
`refunded: false`, and `balance: 1150` (`balance_at_open + 150`).

*Non-participant.* The same event applied to a state whose `self_id` is `"u9"`
(no matching row) returns `balance: 1000` — `balance_at_open` plus zero.

*Nobody backed the winner.* `refunded: true` with
`results: [{user_id: "u1", ..., staked: 100, returned: 100, net: 0}]` returns
`refunded: true`, `phase: "revealed"`, and `balance: 1000`.

Stale guard: `round_resolved` for `round_id: "rd0"` returns the identical
object reference.

Run: `cd frontend && npx vitest run lib/roundState.test.ts`
Expected: FAIL — no `round_resolved` case; `phase` stays `"locked"` and
`results` stays `null`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: after the stale guard, find the row whose `user_id === state.self_id`;
let `net` be that row's `net`, or `0` when there is no such row. Return
`phase: "revealed"`, `results: data.results`, `dust`, `refunded`, and
`balance: (state.balance_at_open ?? state.balance) + net`. Do not recompute
`returned` or `net` from pools — display what `domain.Settle` produced.

```bash
cd frontend && npx vitest run lib/roundState.test.ts && \
  git add lib/roundState.ts lib/roundState.test.ts && \
  git commit -m "feat: reveal settlement results and settle the balance"
```

Expected: PASS, then one commit.

**Checkpoint 3: `round_refunded` restores the pre-round balance**

- [ ] **Step 1: Write the failing test, then run it**

From a locked round `rd1` with `balance_at_open: 1000`, `balance: 850`,
`my_stake: 150`, applying
`{type: "round_refunded", data: {round_id: "rd1", total: 400}}`
returns `phase: "revealed"`, `balance: 1000`, `refunded: true`,
`refund_total: 400`, and `results: null` — the host-disconnect fallback carries
no per-player rows, so none may be displayed.

Stale guard: `round_refunded` for `round_id: "rd0"` returns the identical
object reference.

Run: `cd frontend && npx vitest run lib/roundState.test.ts`
Expected: FAIL — no `round_refunded` case; `balance` stays `850`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: after the stale guard, return `phase: "revealed"`,
`balance: state.balance_at_open ?? state.balance`, `refunded: true`,
`refund_total: data.total`, `results: null`.

```bash
cd frontend && npx vitest run lib/roundState.test.ts && \
  git add lib/roundState.ts lib/roundState.test.ts && \
  git commit -m "feat: restore the pre-round balance on an auto-refund"
```

Expected: PASS, then one commit.

**Task boundary:**

```bash
cd frontend && npx vitest run --coverage && npm run lint && npx tsc --noEmit
```

Expected: PASS, with `lib/roundState.ts` at 100% statements — it is pure and
fully enumerable, and a gap here is a real gap, not wiring.

---

### Task 4: Lockout countdown

**Delegated.** Mechanical: pure arithmetic plus a timer hook, against a fully
stated contract.

**Files:**
- Create: `frontend/lib/countdown.ts`
- Test: `frontend/lib/countdown.test.ts`

**Interfaces:**
- Consumes: `RoundState.lock_at_ms`.
- Produces:
```ts
export function remainingMs(lockAtMs: number, nowMs: number): number;
export function useCountdown(lockAtMs: number | null): number; // remaining ms, 0 when null or elapsed
```

The countdown is **display-only**; `round_locked` from the server is what
actually closes wagering (Global Constraints). `lock_at_ms` is absolute server
wall-clock, so a skewed browser clock makes this cosmetic value wrong without
affecting correctness.

**Checkpoint 1: remaining time is clamped at zero**

- [ ] **Step 1: Write the failing test, then run it**

Table-driven: `remainingMs(1000, 400)` → `600`; `remainingMs(1000, 1000)` → `0`;
`remainingMs(1000, 1500)` → `0` (clamped, never negative);
`remainingMs(1000, 0)` → `1000`.

Run: `cd frontend && npx vitest run lib/countdown.test.ts`
Expected: FAIL — `lib/countdown.ts` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `remainingMs` returns `Math.max(0, lockAtMs - nowMs)`.

```bash
cd frontend && npx vitest run lib/countdown.test.ts && \
  git add lib/countdown.ts lib/countdown.test.ts && \
  git commit -m "feat: compute remaining lockout time, clamped at zero"
```

Expected: PASS, then one commit.

**Checkpoint 2: the hook ticks down and stops at zero**

- [ ] **Step 1: Write the failing test, then run it**

With `vi.useFakeTimers()` and `vi.setSystemTime(0)`, render a probe component
calling `useCountdown(1000)`. Initially it reports `1000`. After advancing
timers by 500ms it reports `500`. After advancing to 1200ms total it reports
`0` and does not go negative on further advancement.

A second case: `useCountdown(null)` reports `0` and registers no interval —
assert by advancing timers 5000ms and confirming the reported value stays `0`.

Wrap timer advancement in `act()` — Phase 6a's journal records that synchronous
state updates outside `act()` leave stale DOM between assertions.

Run: `cd frontend && npx vitest run lib/countdown.test.ts`
Expected: FAIL — `useCountdown` is not exported.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `useCountdown(lockAtMs)` holds `remainingMs(lockAtMs, Date.now())` in
state, and when `lockAtMs` is non-null starts a 200ms `setInterval` that
recomputes it, clearing the interval on unmount, when `lockAtMs` changes, and
once the value reaches `0`. Returns `0` when `lockAtMs` is `null`.

```bash
cd frontend && npx vitest run lib/countdown.test.ts && \
  git add lib/countdown.ts lib/countdown.test.ts && \
  git commit -m "feat: tick the lockout countdown down to zero"
```

Expected: PASS, then one commit.

**Task boundary:** `cd frontend && npx vitest run && npm run lint && npx tsc --noEmit` — Expected: PASS.

---

### Task 5: OddsBoard component

**Delegated.** Presentational, against a stated props contract.

**Files:**
- Create: `frontend/components/OddsBoard.tsx`
- Test: `frontend/components/OddsBoard.test.tsx`

**Interfaces:**
- Consumes: `RoundState` fields `outcomes`, `pools`, `total`, `multipliers`, `bettors`, `players`.
- Produces:
```ts
export type OddsBoardProps = {
  outcomes: string[];
  pools: number[];
  total: number;
  multipliers: number[];
  bettors: number;
  players: number;
  winningOutcome?: number | null;
};
export function OddsBoard(props: OddsBoardProps): JSX.Element;
```

**Checkpoint 1: each outcome shows its pool and multiplier**

- [ ] **Step 1: Write the failing test, then run it**

Render with `outcomes: ["Home", "Away"]`, `pools: [300, 100]`, `total: 400`,
`multipliers: [1.3333, 4]`, `bettors: 2`, `players: 5`. Assert the document
contains "Home", "Away", "300", "100", the total "400", and the multipliers
rendered to two decimals — `1.33×` and `4.00×`. Use regex matchers for any text
split across elements, per Phase 6a's RTL lesson.

Run: `cd frontend && npx vitest run components/OddsBoard.test.tsx`
Expected: FAIL — `components/OddsBoard.tsx` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: render a list, one row per entry of `outcomes`, each showing the
label, `pools[i]`, and `multipliers[i].toFixed(2)` followed by `×`. Render the
total. Use a semantic `<ul>`/`<li>` or a `<table>` with a header row — the
`accessibility` skill's rule that tabular data gets real table semantics.

```bash
cd frontend && npx vitest run components/OddsBoard.test.tsx && \
  git add components/OddsBoard.tsx components/OddsBoard.test.tsx && \
  git commit -m "feat: show pool totals and payout multipliers per outcome"
```

Expected: PASS, then one commit.

**Checkpoint 2: an outcome with an empty pool shows no multiplier**

- [ ] **Step 1: Write the failing test, then run it**

Render with `outcomes: ["Home", "Away"]`, `pools: [0, 100]`, `total: 100`,
`multipliers: [0, 1]`, `bettors: 1`, `players: 5`. Assert the "Home" row
renders `—` where its multiplier would go, and does **not** render `0.00×`.
The "Away" row still renders `1.00×`.

Run: `cd frontend && npx vitest run components/OddsBoard.test.tsx`
Expected: FAIL — the Home row renders `0.00×`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: when `pools[i] === 0`, render the em dash `—` in place of the
multiplier. A pool nobody has backed has no meaningful payout ratio, and
showing `0.00×` reads as "this pays nothing", which is the opposite of true.

```bash
cd frontend && npx vitest run components/OddsBoard.test.tsx && \
  git add components/OddsBoard.tsx components/OddsBoard.test.tsx && \
  git commit -m "fix: show a dash, not 0.00x, for an unbacked outcome"
```

Expected: PASS, then one commit.

**Checkpoint 3: the aggregate bettors counter**

- [ ] **Step 1: Write the failing test, then run it**

Render with `bettors: 2`, `players: 5`. Assert the text
`2/5 players have placed their bets` appears (regex matcher — it spans text
nodes).

A second case with `bettors: 0`, `players: 5` renders
`0/5 players have placed their bets`.

A third case guards the invariant: render with `bettors: 2`, `players: 5` and
assert the rendered output contains **no** `4` — proving the denominator is
`players` verbatim and the component is not subtracting a host from it.

Run: `cd frontend && npx vitest run components/OddsBoard.test.tsx`
Expected: FAIL — no counter text is rendered.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: render `{bettors}/{players} players have placed their bets`.
`players` is used exactly as received — `redisstore.PlayerCount` already
excludes the host (`internal/redisstore/room.go:187`). This counter is the only
in-round progress signal the spec permits; no per-user indicator of any kind
may be added to this component.

```bash
cd frontend && npx vitest run components/OddsBoard.test.tsx && \
  git add components/OddsBoard.tsx components/OddsBoard.test.tsx && \
  git commit -m "feat: show the aggregate count of players who have wagered"
```

Expected: PASS, then one commit.

**Task boundary:** `cd frontend && npx vitest run && npm run lint && npx tsc --noEmit` — Expected: PASS.

---

### Task 6: WagerPad component

**Delegated.** Presentational plus local validation, against a stated contract.

**Files:**
- Create: `frontend/components/WagerPad.tsx`
- Test: `frontend/components/WagerPad.test.tsx`

**Interfaces:**
- Produces:
```ts
export type WagerPadProps = {
  outcomes: string[];
  balance: number;
  disabled: boolean;
  onPlace: (outcome: number, amount: number) => void;
};
export function WagerPad(props: WagerPadProps): JSX.Element;
```

The pad never sees another player's data — it takes outcome labels and the
viewer's own balance, nothing else.

**Checkpoint 1: selecting an outcome and staking calls back**

- [ ] **Step 1: Write the failing test, then run it**

Render with `outcomes: ["Home", "Away"]`, `balance: 1000`, `disabled: false`,
and a `vi.fn()` `onPlace`. Click the "Away" button, type `150` into the amount
field, click "Place bet". Assert `onPlace` was called exactly once with
`(1, 150)`.

A second case: with no outcome selected, the "Place bet" button is disabled and
clicking it does not call `onPlace`.

Run: `cd frontend && npx vitest run components/WagerPad.test.tsx`
Expected: FAIL — `components/WagerPad.tsx` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: one button per outcome, each with `aria-pressed` reflecting
selection; a `type="number"` amount input labelled "Amount"; a "Place bet"
submit that is disabled until an outcome is selected and the amount is a
positive integer, and that calls `onPlace(selectedIndex, amount)`.

```bash
cd frontend && npx vitest run components/WagerPad.test.tsx && \
  git add components/WagerPad.tsx components/WagerPad.test.tsx && \
  git commit -m "feat: let a player pick an outcome and stake an amount"
```

Expected: PASS, then one commit.

**Checkpoint 2: a stake above the balance is refused locally**

- [ ] **Step 1: Write the failing test, then run it**

Render with `balance: 100`. Select "Home", enter `150`, click "Place bet".
Assert `onPlace` was **not** called and the text
`You only have 100 tokens` is shown.

Entering `100` (exactly the balance) then clicking calls `onPlace` with
`(0, 100)` — the boundary is inclusive.

Entering `0` shows `Enter an amount above zero` and does not call `onPlace`.

Run: `cd frontend && npx vitest run components/WagerPad.test.tsx`
Expected: FAIL — `onPlace` is called with `(0, 150)`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: before calling `onPlace`, reject `amount > balance` with
`You only have {balance} tokens`, and reject `amount <= 0` with
`Enter an amount above zero`. This is a convenience check only — the server
re-validates and answers `insufficient_funds`, which stays the authority.

```bash
cd frontend && npx vitest run components/WagerPad.test.tsx && \
  git add components/WagerPad.tsx components/WagerPad.test.tsx && \
  git commit -m "feat: refuse a stake larger than the player's balance"
```

Expected: PASS, then one commit.

**Checkpoint 3: a locked round disables the pad**

- [ ] **Step 1: Write the failing test, then run it**

Render with `disabled: true`, `outcomes: ["Home", "Away"]`, `balance: 1000`.
Assert every outcome button, the amount input, and the "Place bet" button all
report `disabled`, and that clicking "Home" then "Place bet" does not call
`onPlace`. Assert the text `Betting is closed` is shown.

Run: `cd frontend && npx vitest run components/WagerPad.test.tsx`
Expected: FAIL — controls are still enabled; `disabled` is not yet honored.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: when `disabled` is true, set `disabled` on every interactive element
and render `Betting is closed`. The room page passes
`disabled={state.phase !== "open"}`.

```bash
cd frontend && npx vitest run components/WagerPad.test.tsx && \
  git add components/WagerPad.tsx components/WagerPad.test.tsx && \
  git commit -m "feat: disable the wager pad once betting closes"
```

Expected: PASS, then one commit.

**Task boundary:** `cd frontend && npx vitest run && npm run lint && npx tsc --noEmit` — Expected: PASS.

---

### Task 7: HostConsole component

**Delegated.** A form against the validation bounds stated in the backend
contract.

**Files:**
- Create: `frontend/components/HostConsole.tsx`
- Test: `frontend/components/HostConsole.test.tsx`

**Interfaces:**
- Produces:
```ts
export type HostConsoleProps = {
  phase: Phase;
  outcomes: string[];          // the open round's outcomes, for the resolve picker
  onOpenRound: (question: string, outcomes: string[], lockInMs: number) => void;
  onResolve: (winningOutcome: number) => void;
};
export function HostConsole(props: HostConsoleProps): JSX.Element;
```

**Checkpoint 1: opening a round**

- [ ] **Step 1: Write the failing test, then run it**

Render with `phase: "idle"`, `outcomes: []`, and `vi.fn()` callbacks. The form
starts with a question field and exactly two outcome fields. Type
`Next goal?`, `Home`, `Away`, set the lock field to `30` seconds, submit.
Assert `onOpenRound` was called once with
`("Next goal?", ["Home", "Away"], 30000)` — note the seconds-to-milliseconds
conversion.

A second case: submitting with an empty question does not call `onOpenRound`
and shows `Enter a question`.

Run: `cd frontend && npx vitest run components/HostConsole.test.tsx`
Expected: FAIL — `components/HostConsole.tsx` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: a labelled question input, two outcome inputs, and a lock-seconds
number input defaulting to `30`. On submit, trim the question and every
outcome, block submission with `Enter a question` when the question is empty
and `Every outcome needs a label` when any outcome is empty, and otherwise call
`onOpenRound(question, outcomes, seconds * 1000)`.

```bash
cd frontend && npx vitest run components/HostConsole.test.tsx && \
  git add components/HostConsole.tsx components/HostConsole.test.tsx && \
  git commit -m "feat: let the host open a round with a question and outcomes"
```

Expected: PASS, then one commit.

**Checkpoint 2: two to four outcomes**

- [ ] **Step 1: Write the failing test, then run it**

From `phase: "idle"`: clicking "Add outcome" twice yields four outcome inputs,
and the "Add outcome" button is then disabled (the server's `MaxOutcomes` is 4).
Clicking "Remove" until two remain disables "Remove" (`MinOutcomes` is 2).
With four outcomes filled in as `A`, `B`, `C`, `D` and a question, submitting
calls `onOpenRound` with `["A", "B", "C", "D"]`.

The lock field is bounded too: entering `2` shows
`Lock must be between 3 and 120 seconds` and does not call `onOpenRound`;
entering `121` shows the same and does not call it; `3` and `120` are both
accepted.

Run: `cd frontend && npx vitest run components/HostConsole.test.tsx`
Expected: FAIL — there is no "Add outcome" control.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: "Add outcome" appends an empty outcome field while the count is below
4 and is disabled at 4; "Remove" drops the last while the count is above 2 and
is disabled at 2. Reject a lock value outside `3..120` inclusive with
`Lock must be between 3 and 120 seconds`. These mirror `MinOutcomes`/
`MaxOutcomes` and `MinLockIn`/`MaxLockIn` so the host sees a useful message
instead of a round-trip `invalid_spec`.

```bash
cd frontend && npx vitest run components/HostConsole.test.tsx && \
  git add components/HostConsole.tsx components/HostConsole.test.tsx && \
  git commit -m "feat: bound a round to 2-4 outcomes and a 3-120s lock"
```

Expected: PASS, then one commit.

**Checkpoint 3: resolving a locked round**

- [ ] **Step 1: Write the failing test, then run it**

Render with `phase: "locked"` and `outcomes: ["Home", "Away"]`. Assert the
open-round form is not shown, and one resolve button per outcome is. Click
"Away" and assert `onResolve` was called once with `1`.

With `phase: "open"`, assert no resolve buttons are shown — the host resolves
only after lockout.

Run: `cd frontend && npx vitest run components/HostConsole.test.tsx`
Expected: FAIL — resolve controls do not exist; the open-round form renders
regardless of phase.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: render the open-round form when `phase` is `"idle"` or `"revealed"`;
render a "Which outcome won?" picker with one button per outcome, calling
`onResolve(index)`, when `phase` is `"locked"`; render neither (just a waiting
message) when `phase` is `"open"`.

```bash
cd frontend && npx vitest run components/HostConsole.test.tsx && \
  git add components/HostConsole.tsx components/HostConsole.test.tsx && \
  git commit -m "feat: let the host resolve a locked round"
```

Expected: PASS, then one commit.

**Task boundary:** `cd frontend && npx vitest run && npm run lint && npx tsc --noEmit` — Expected: PASS.

---

### Task 8: SettlementReveal component

**Delegated.** Presentational, against a stated contract. This renders the one
moment per-user stakes become visible.

**Files:**
- Create: `frontend/components/SettlementReveal.tsx`
- Test: `frontend/components/SettlementReveal.test.tsx`

**Interfaces:**
- Produces:
```ts
export type SettlementRevealProps = {
  results: ResultRow[] | null;
  outcomes: string[];
  winningOutcome: number | null;
  dust: number;
  refunded: boolean;
  refundTotal: number | null;
  selfId: string | null;
};
export function SettlementReveal(props: SettlementRevealProps): JSX.Element | null;
```

**Checkpoint 1: the per-player reveal**

- [ ] **Step 1: Write the failing test, then run it**

Render with `outcomes: ["Home", "Away"]`, `winningOutcome: 0`, `dust: 3`,
`refunded: false`, `refundTotal: null`, `selfId: "u1"`, and
`results: [{user_id: "u1", display_name: "Ann", staked: 100, returned: 250, net: 150}, {user_id: "u2", display_name: "Bob", staked: 100, returned: 0, net: -100}]`.

Assert: the winning outcome label "Home" is shown; a row for Ann shows `100`,
`250`, and `+150`; a row for Bob shows `100`, `0`, and `-100`; Ann's row is
marked as the viewer's own (regex match on `/Ann.*\(you\)/`); and the dust `3`
is shown.

Run: `cd frontend && npx vitest run components/SettlementReveal.test.tsx`
Expected: FAIL — `components/SettlementReveal.tsx` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: return `null` when `results` is `null` and `refunded` is `false`.
Otherwise render the winning outcome's label and a table with a row per result:
display name (suffixed ` (you)` when `user_id === selfId`), staked, returned,
and net signed with an explicit `+` when positive. Render the dust figure.

```bash
cd frontend && npx vitest run components/SettlementReveal.test.tsx && \
  git add components/SettlementReveal.tsx components/SettlementReveal.test.tsx && \
  git commit -m "feat: reveal every player's stake and net at settlement"
```

Expected: PASS, then one commit.

**Checkpoint 2: the two refund paths read differently**

- [ ] **Step 1: Write the failing test, then run it**

*Nobody backed the winner.* Render with `refunded: true`, `winningOutcome: 1`,
`refundTotal: null`, and `results` containing one row with
`staked: 100, returned: 100, net: 0`. Assert the text
`Nobody backed the winning outcome — every stake was returned` is shown **and**
the per-player row is still rendered.

*Host-disconnect fallback.* Render with `results: null`, `refunded: true`,
`refundTotal: 400`, `winningOutcome: null`. Assert the text
`The round went unresolved — all 400 tokens were refunded` is shown and **no**
per-player rows are rendered (assert the table is absent).

Run: `cd frontend && npx vitest run components/SettlementReveal.test.tsx`
Expected: FAIL — both cases render the same generic reveal.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: distinguish the two paths by `results`. When `results` is non-null
and `refunded` is true, show the nobody-backed-the-winner message above the
normal table. When `results` is `null` and `refundTotal` is non-null, show the
unresolved-round message and render no table — `RefundedEvent` carries no
per-player data, so inventing rows would be fabrication.

```bash
cd frontend && npx vitest run components/SettlementReveal.test.tsx && \
  git add components/SettlementReveal.tsx components/SettlementReveal.test.tsx && \
  git commit -m "feat: distinguish an unbacked winner from an unresolved round"
```

Expected: PASS, then one commit.

**Task boundary:** `cd frontend && npx vitest run && npm run lint && npx tsc --noEmit` — Expected: PASS.

---

### Task 9: Web Audio cues

**Delegated.** Self-contained utility behind an injectable factory.

**Files:**
- Create: `frontend/lib/audio.ts`
- Test: `frontend/lib/audio.test.ts`

**Interfaces:**
- Produces:
```ts
export type Cue = "open" | "lock" | "resolve";
export type AudioContextFactory = () => AudioContext;
export function playCue(cue: Cue, factory?: AudioContextFactory): void;
```

The factory parameter exists so tests inject a fake — jsdom has no Web Audio
implementation. The default is `() => new AudioContext()`.

**Checkpoint 1: each cue plays a distinct tone**

- [ ] **Step 1: Write the failing test, then run it**

Build a fake context whose `createOscillator()` returns a recording stub
(`frequency: {value}`, `connect`, `start`, `stop` as `vi.fn()`) and whose
`createGain()` returns a stub with `gain: {value}` and `connect`. Pass it as
the factory.

Assert `playCue("open", fake)` calls `createOscillator` once, sets a frequency,
and calls `start` then `stop`. Assert the frequency set by `"open"`, `"lock"`,
and `"resolve"` are three **different** values — the test asserts distinctness,
not specific hertz, so tuning the tones later does not break it.

Run: `cd frontend && npx vitest run lib/audio.test.ts`
Expected: FAIL — `lib/audio.ts` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `playCue` creates an oscillator and a gain node from the factory's
context, connects oscillator → gain → destination, sets a per-cue frequency
(three distinct constants named `CUE_FREQUENCIES`), starts immediately, and
stops after a short fixed duration. Amounts of ceremony beyond this are not
wanted — spec §8 defers UI polish.

```bash
cd frontend && npx vitest run lib/audio.test.ts && \
  git add lib/audio.ts lib/audio.test.ts && \
  git commit -m "feat: play a distinct tone at each round phase change"
```

Expected: PASS, then one commit.

**Checkpoint 2: a missing or blocked AudioContext never breaks the page**

- [ ] **Step 1: Write the failing test, then run it**

Assert `playCue("open", () => { throw new Error("no audio"); })` does not
throw. Assert that after such a failure, a later `playCue("lock", workingFake)`
still creates an oscillator — one failure must not latch the module off.

Run: `cd frontend && npx vitest run lib/audio.test.ts`
Expected: FAIL — the thrown error propagates out of `playCue`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: wrap the whole body in `try`/`catch` and swallow, matching
`lib/session.ts`'s existing precedent that a browser-capability failure must
never crash a render. Browsers block audio before a user gesture, and some
environments have no `AudioContext` at all; neither is an error worth
surfacing. Do not cache the failure — each call retries.

```bash
cd frontend && npx vitest run lib/audio.test.ts && \
  git add lib/audio.ts lib/audio.test.ts && \
  git commit -m "fix: never let a blocked AudioContext break the room page"
```

Expected: PASS, then one commit.

**Task boundary:** `cd frontend && npx vitest run && npm run lint && npx tsc --noEmit` — Expected: PASS.

---

### Task 10: Wire the room page, prove a full round, and close the phase

**Executed inline** — acceptance evidence and the security review.

**Files:**
- Modify: `frontend/app/room/[code]/page.tsx`
- Test: `frontend/app/room/[code]/page.test.tsx`
- Create: `frontend/e2e/round.spec.ts`
- Modify: `docs/plans/2026-08-21-implementation-plan.md`, `CLAUDE.md`, `docs/project-history.md`

**Interfaces:**
- Consumes: everything Tasks 1–9 produced.
- Produces: no new exported names.

**Checkpoint 1: the room page plays a round**

- [ ] **Step 1: Write the failing test, then run it**

Extend `app/room/[code]/page.test.tsx`, reusing 6a's fake-WebSocket harness and
its `act()`-wrapped `fire()` helper.

*Player view.* Connect with `host: false`, then fire `round_opened`
(`["Home","Away"]`), then `odds_updated` with `bettors: 1, players: 3`. Assert
the question, both outcome labels, the `1/3 players have placed their bets`
counter, and the wager pad are all present, and that no host console is
rendered (assert "Which outcome won?" and the question form are absent).

*Host view.* Connect with `host: true`. Assert the open-round form is present
and the wager pad is **absent** — the host cannot wager, so the control must
not exist for them.

*Placing a wager.* As the player, select "Home", enter `100`, click "Place
bet"; assert the fake socket received a `place_wager` envelope whose data has
`outcome: 0`, `amount: 100`, and a non-empty `idempotency_key`. Then fire
`wager_accepted` with `balance: 900` and assert `900` is displayed.

*Settlement.* Fire `round_locked`, assert the pad is disabled; fire
`round_resolved` with the two-row results from Task 8 CP1 and assert Ann's and
Bob's rows appear.

Run: `cd frontend && npx vitest run "app/room/[code]/page.test.tsx"`
Expected: FAIL — the page renders only the 6a presence roster; no gameplay
components are mounted.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: replace the page's four `useState` calls for gameplay with
`useReducer(reduceRound, initialRoundState(summary?.session_balance ?? 0))`,
keeping the existing presence `useState`s as they are. Register socket handlers
for `connected`, `round_opened`, `odds_updated`, `round_locked`,
`wager_accepted`, `round_resolved`, and `round_refunded`, each dispatching the
matching action. Render `HostConsole` when `state.is_host`, `WagerPad` when not
(with `disabled={state.phase !== "open"}`), plus `OddsBoard`,
`SettlementReveal`, and the countdown from `useCountdown(state.lock_at_ms)`.
Call `playCue` on `round_opened`, `round_locked`, and `round_resolved`.
Generate the idempotency key with `crypto.randomUUID()` at send time, one per
placement.

```bash
cd frontend && npx vitest run && npm run lint && npx tsc --noEmit && \
  git add "app/room/[code]/page.tsx" "app/room/[code]/page.test.tsx" && \
  git commit -m "feat: play a full round from the room page"
```

Expected: PASS, then one commit.

**Checkpoint 2: two browsers play a round end to end**

- [ ] **Step 1: Write the failing test, then run it**

Create `e2e/round.spec.ts` following `e2e/join.spec.ts`'s two-context pattern.
Host registers, creates a room, and reads the code; guest joins by that code in
a second isolated context. Host opens a round (`Next goal?`, `Home`/`Away`,
lock `3` seconds). Assert the guest sees the question. Guest stakes `100` on
`Home`. Assert the guest's balance drops to `900` and the host sees
`1/1 players have placed their bets`. Wait for lockout, assert the guest's pad
is disabled. Host resolves `Home`. Assert both contexts show the guest's
result row with `+100` net (sole backer of the winner: staked 100, returned
200 from the 200-token pool... adjust the expected figures to whatever the
single-wager pari-mutuel actually yields — with only one wager the pool is
100, the multiplier is 1, and the net is 0, so assert `0` and a returned `100`).

Note the lock is set to the 3-second minimum so the test does not idle.

Run: `cd frontend && npx playwright test e2e/round.spec.ts`
Expected: FAIL — the spec does not exist yet; once written it fails on the
first gameplay assertion until the page is wired (it will pass after CP1, so
write and run this spec only after CP1 is committed).

- [ ] **Step 2: Make it pass, then verify-and-commit in one command**

Contract: no new product code should be needed. If this test exposes a genuine
integration defect — as 6a's E2E did with the newcomer roster — fix it in the
smallest way that preserves the existing protocol, and commit the fix as its
own separate commit *before* committing the test, keeping the correctness fix
and the acceptance test independently revertible.

Requires the full stack: `make up` and a running API with `JWT_SECRET` and
`CORS_ALLOWED_ORIGINS` set. Playwright's `webServer` handles the frontend;
quote the `PATH` export in any command it runs.

```bash
cd frontend && npx playwright test && \
  git add e2e/round.spec.ts && \
  git commit -m "test: prove two browsers play a full round end to end"
```

Expected: PASS, then one commit.

**Checkpoint 3: documentation and the parent plan**

- [ ] **Step 1: Write the failing check**

There is no automated test for prose. The check is a reading: `CLAUDE.md`'s
Repository Layout must list the new `lib/` and `components/` files, and its
anonymity invariant must record that 6b's reveal path is implemented and that
`wager_accepted` is a permitted private self-disclosure. The parent plan's §9
6b row must describe what shipped. `docs/project-history.md` must record
Task 1's three backend gaps as a 6b amendment, the way 6a's newcomer-roster fix
was recorded.

Expected: these statements are absent before the edit.

- [ ] **Step 2: Write them, then verify-and-commit**

Contract: update the three documents as described. Do **not** mark the 6b row
✅ — this project ties ✅ to "merged into `dev`", which is
`finishing-a-development-branch`'s decision, not this plan's. (6a marked it
early and had to revert; see `journal/2026-08-30_1554_ansh_phase-6a-execution.md`.)

```bash
git add CLAUDE.md docs/plans/2026-08-21-implementation-plan.md docs/project-history.md && \
  git commit -m "docs: record Phase 6b's gameplay surface and backend amendments"
```

Expected: one commit.

**Checkpoint 4: security review**

- [ ] **Step 1: Run the review**

Run the `security-reviewer` agent over `git diff dev...HEAD`. CLAUDE.md
requires this for any phase touching auth, money movement, or a network
surface; this phase touches all three. Direct it at five points:

1. **The new `host` JWT claim.** Confirm it is advisory-for-rendering only and
   that `round.Service` still re-checks `rm.HostID` against Redis for every
   host-gated action — a forged claim must buy nothing.
2. **The `wager_accepted` private reply.** Confirm it reaches only the sender's
   own connection via `c.Send`, is never broadcast, and discloses only that
   sender's own stake and balance.
3. **The anonymity invariant end to end.** Confirm no component, reducer
   branch, or handler surfaces per-user wager data before `round_resolved` or
   `round_refunded`, and that the bettors counter is the only in-round signal.
4. **Client-side validation as convenience, not control.** Confirm the wager
   pad's balance check and the host console's bounds are re-enforced
   server-side, so a crafted socket message gains nothing.
5. **`crypto.randomUUID()` idempotency keys.** Confirm one key per placement
   and no reuse across retries that could mask or duplicate a wager.

- [ ] **Step 2: Address findings, then verify-and-commit**

Contract: fix every CRITICAL and HIGH before the phase closes. Record all
findings and their dispositions — including any accepted as designed — in
`docs/project-history.md`, matching 6a's section.

```bash
cd frontend && npx vitest run --coverage && npm run lint && npx tsc --noEmit && \
  cd ../backend && export PATH="$PATH:$HOME/.local/go/bin:$HOME/go/bin" && \
  go vet ./... && gofmt -l . && go test ./... -race -cover -p 1 -count=1 && \
  cd .. && git add <exact paths the review changed> && \
  git commit -m "fix: address Phase 6b security review findings"
```

Expected: PASS. Name the changed paths explicitly in `git add` — never
`git add -u`, `-A`, or `.`; which files a review touches is not knowable in
advance, so the executor lists what it actually changed. If the review produced
no code changes, skip the commit and say so explicitly rather than creating an
empty one.

**Task boundary — the phase's final gate:**

```bash
cd backend && export PATH="$PATH:$HOME/.local/go/bin:$HOME/go/bin" && \
  go vet ./... && gofmt -l . && go build ./... && go test ./... -race -cover -p 1 -count=1 && \
  cd ../frontend && npx vitest run --coverage && npm run lint && npx tsc --noEmit && npx playwright test
```

Expected: everything PASS, frontend coverage over `lib/**` and `components/**`
at or above 80%, `gofmt -l .` silent.

The branch is now green and verified. **Stop here** — integration is
`finishing-a-development-branch`'s call, invoked by `executing-plans` Step 3,
and the merge decision belongs to the user.

---

## Self-Review

**1. Spec coverage.** Spec §4's requirements map to tasks as follows.
Round control over the WebSocket → Task 10 CP1 (the page sends `create_round`,
`place_wager`, `resolve_round`; no REST is added). Host types a question with
2–4 custom outcomes → Task 7 CP1–CP2. Host cannot wager → Task 10 CP1's host
view asserts the pad is absent, backed by the server's `host_cannot_bet`.
Server-side lockout at t=0 → Task 4 (display) with `round_locked` as the
authority. Host manually resolves; pari-mutuel payout → Task 7 CP3 and Task 8
CP1, with the math left entirely in `domain.Settle`. Host-disconnect
auto-refund → Task 3 CP3 and Task 8 CP2. Anonymity until terminal, with the
aggregate counter as the only in-round signal → the Global Constraints, Task 3
CP2, Task 5 CP3, and Task 10 CP4's review point 3. §3's partial buy-in is
already surfaced by the 6a page and is untouched. §2's Web Audio API → Task 9.
§7's latency targets are Phase 7's k6 work, not this phase's. §8 defers
N-outcome visual polish, which is why Task 5 and Task 7 stay functional.
No spec requirement is unassigned.

**2. Placeholder scan.** No "TBD", "add error handling", or "similar to Task N"
survives. Every checkpoint names exact inputs and exact expected outputs. One
place deliberately defers a number to execution — Task 10 CP2's expected E2E
payout — and it does so by stating the reasoning (a sole backer's pool equals
their stake, so the multiplier is 1 and the net is 0) rather than leaving a
blank.

**3. Type consistency.** `RoundState`, `RoundAction`, `Phase`,
`initialRoundState`, and `reduceRound` are defined once in Task 2 and used
under those names in Tasks 3, 4, 7, and 10. `ResultRow` is declared in
`lib/protocol.ts` (Task 2) and consumed by Tasks 3 and 8 — one declaration, not
two. `WagerAcceptedEvent`'s four fields match the Go struct Task 1 CP2 adds,
field for field. `Cue` and `playCue` match between Tasks 9 and 10. The
component prop types (`OddsBoardProps`, `WagerPadProps`, `HostConsoleProps`,
`SettlementRevealProps`) each name the exact `RoundState` fields Task 10 passes
into them.

**4. Delegation eligibility.** Tasks 4–9 are mechanical against contracts
stated here in full — a pure arithmetic helper plus a timer hook, and four
presentational components with fixed prop types. They are tagged for
delegation. Tasks 2 and 3 are the phase's flagship correctness work and stay
inline: Task 3 in particular is where a wrong `??` or a stale-guard omission
would silently corrupt a balance, and it is the reducer half that touches money.
Task 1 stays inline because it changes a JWT claim set and adds a disclosure
path, both of which the phase-closing security review examines. Task 10 stays
inline because it is the acceptance evidence.

**5. Phase sizing.** 10 tasks, 29 checkpoints — close to Phase 6a's 9 tasks and
26 checkpoints, which executed in a single session. Splitting was considered and
rejected: every deliverable here converges on one screen driven by one state
machine, so any cut line produces a half that cannot be demonstrated on its own,
which violates §9's "each phase ends in something runnable and verifiable." Six
delegated tasks against 6a's two should make this phase cheaper in tokens
despite being marginally larger.

## Known Risks

- **The E2E test needs the full stack plus a real lockout wait.** The lock is
  set to the 3-second minimum to keep the test quick, but Playwright's default
  timeouts still need to accommodate it. If the round-locked assertion proves
  flaky, raise that one assertion's timeout rather than lengthening the lock.
- **jsdom has no Web Audio.** Task 9's factory parameter exists for exactly
  this. If Task 10 CP1's page test trips over the real `AudioContext`, stub
  `lib/audio` at the module level with `vi.mock` rather than weakening
  `playCue`'s own contract.
- **`domain.Multipliers` behavior on an empty pool is not pinned here.** Task 5
  CP2 specifies the *client's* display rule against the client's own input
  (`pools[i] === 0` → `—`), so it holds regardless of whether the server sends
  `0`, `Infinity`, or omits the entry.
