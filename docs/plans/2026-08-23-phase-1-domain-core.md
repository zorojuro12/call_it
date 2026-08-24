# Phase 1 — Domain Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `internal/domain` — the pari-mutuel payout engine, round state machine, and wallet/refill rules — as pure Go with no I/O, proving the money math correct before any infrastructure exists to hide bugs behind.

**Architecture:** One flat package, `backend/internal/domain`, split into small
single-responsibility files. Every exported function is a pure function of its
arguments: no clocks, no network, no globals, no `context.Context`. Redis and
PostgreSQL will later *call* these rules, but the rules never learn that Redis
or PostgreSQL exist. The package's only imports are `errors` and `fmt`.

**Tech Stack:** Go 1.22.10, standard library only. Table-driven tests via
`go test`, plus native Go fuzzing for the token-conservation property.

**Spec:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md)
(§3 identity and wallets, §4 gameplay and pari-mutuel, §7 targets)

**Parent plan:** [`docs/plans/2026-08-21-implementation-plan.md`](2026-08-21-implementation-plan.md)
(§4 Redis key schema, §5 Lua contracts, §8 economy constants, §9 phase table)

---

## Global Constraints

Every task's requirements implicitly include this section.

- **Go 1.22.10**, module `github.com/zorojuro12/call_it/backend`. No third-party
  dependencies — `go.mod` must still have no `require` block when this phase ends.
- **`internal/domain` performs no I/O.** No imports beyond `errors` and `fmt`.
  Not `os`, not `time`, not `context`, not any other `internal/` package. This
  is CLAUDE.md's first invariant and the entire reason Phase 1 precedes all
  infrastructure.
- **All amounts are `Tokens`** (a named `int64`). No `float64` touches a
  balance, pool, or stake. `Multiplier` and `Multipliers` in `odds.go` are the
  only functions in the package that may return a float, because odds are a
  presentation concern.
- **Payout flooring produces dust, and dust is never dropped.** Every settlement
  must satisfy `Σ payouts + dust == Σ stakes` exactly.
- **Wagers are anonymous until the round reaches a terminal state.** See
  "New invariant" below.
- **Coverage target for this package is 100%**, not the project's 80% floor.
  Parent plan §9 calls for "near-total unit coverage" here; there is no wiring
  code in this package to excuse a gap.
- **`gofmt` must be clean.** CI fails on any unformatted file.
- **Environment:** `go` is installed user-locally and is not on a
  non-interactive shell's `PATH`. Every task's commands assume this has been
  run once in the shell first:
  ```bash
  export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin
  ```

### Branch

All work happens on one branch off `dev`, per CLAUDE.md's branch-per-phase rule:

```bash
git checkout dev
git checkout -b phase-1-domain-core dev
```

Self-merge into `dev` at the end of Task 8. No PR.

---

## Amendments to the parent plan

Three decisions made while writing this plan depart from
`docs/plans/2026-08-21-implementation-plan.md`. They are recorded here rather
than silently applied. **Task 8 includes a step to fold them back into the
parent plan and the spec**, so the committed docs don't drift.

### A1 — Economy constants live in `internal/domain`, not `internal/config`

Parent plan §8 says "Centralized in `internal/config` as named constants." The
centralization is right; the package is not. `internal/config` is an
environment-variable loader (`Load(lookup LookupFunc)`), and nothing in the
economy table is environment-tunable — nobody sets the refill target per
deployment. Importing an env-loading package from the domain inverts the
dependency direction that Phase 1 exists to establish. The constants move to
`backend/internal/domain/economy.go`; `internal/config` keeps only deployment
configuration.

### A2 — The refill threshold is deleted; refills gate on the target

Parent plan §8 listed a separate "refill eligibility threshold: balance < 200",
derived from spec §3's suggestion of "20% of the platform refill target". That
created a dead zone: an account holding between 200 and 999 is too poor to enter
a 1,000-token room at full stake, but too rich to top up. The threshold is
removed and eligibility becomes `balance < RefillTarget`. Two names for one
number invite a future "cleanup" that makes them diverge again, so only
`RefillTarget` survives.

The quota — 3 claims per rolling 7-day window — is now the only limiter, which
it effectively already was. Claiming early is self-punishing (a claim at 950
gains 50 tokens and burns a third of the week's quota), so the incentive
mostly self-corrects. The remaining sharp edge belongs to the UI, which should
confirm the trade before spending a claim; it is not a domain rule.

### A3 — The room buy-in ceiling drops from 100,000 to 10,000

At 100,000 the ceiling sat a hundred times above the refill target, making
top-of-range rooms unreachable for anyone not already wealthy and pushing
everyone else permanently onto partial buy-in. 10,000 keeps a real ladder to
climb while staying on the same order as the refill target. A relational cap
(buy-in bounded by the host's balance) was considered and rejected: the host
cannot wager in their own room, so their balance measures nothing about the
room's stakes.

### New invariant — wagers are anonymous until the round closes

Not in the spec at all; added this session and binding from here on.

Nobody may learn who backed which outcome, or for how much, until the round
reaches a terminal state. At that point every participant's stake and net
result are revealed together.

**Why it is an invariant and not a UI preference:** the host resolves the
outcome. A host who could see positions before resolving could favour an
outcome to benefit a friend — reintroducing, through a side channel, the exact
conflict of interest that "the host cannot place wagers in their own room"
exists to remove. Anonymity-until-reveal closes it.

**What it costs:** almost nothing structurally. Live odds are computed from
*pool totals*, never per-user positions, so the per-wager broadcast (spec §5
step 3) is already aggregate by nature. The Redis wager key
`{userID}:{outcomeIdx}` (parent plan §4) is unaffected — it is server-side, and
this invariant governs what leaves the server.

**What it costs this phase:** `Settle` must return per-player *net* results, not
just gross credits. "You earned 150" (staked 100, returned 250) is a different
number from the 250 the ledger moves, and a losing player — who produces no
credit at all — still needs a row showing −100. Task 5 Checkpoint 2 builds this.

**Known limitation, accepted deliberately:** every broadcast is triggered by one
wager, so each pool delta *is* one player's exact stake — only the identity is
missing. In a room of three or four, whoever just acted is easy to guess; in a
room of thirty, wagers arrive faster than anyone can attribute and the crowd
does the hiding. So the guarantee is strong in large rooms and weak in small
ones. Closing it would mean batching or delaying pool updates, which fights the
<30 ms target in spec §7 head-on, or adding noise to displayed odds, which would
misstate a payout multiplier that has to be exact at settlement. The leak is
accepted rather than trading away live odds.

What the invariant does buy, and what it does not, stated plainly: the host never
gets a systematic, complete, reliable view. They cannot open a panel listing
every position and then decide how to resolve. Inferring one stake from broadcast
timing in a four-person room is a different thing from having the board.

**Required mitigation — aggregate progress only.** While a round is open, no
payload may carry anything identity-adjacent alongside the pool update: no
"Bob placed a bet" notification, no per-user committed indicator, no per-user
wager count. What may be shown is a single aggregate counter — "2/5 players have
placed their bets" — which costs no latency and reveals no identity. Two rules
for it:

- **The denominator excludes the host**, who cannot wager (spec §4). Counting
  them leaves the counter permanently unreachable and reading as broken.
- **It counts players, not wagers.** A player may hold stakes on several
  outcomes, so a second wager from the same player moves the pools but not the
  counter. This does mean a pool jump with no counter change reveals that a
  repeat bettor acted rather than a new one — far weaker than the per-user
  signal it replaces, and the unavoidable cost of the counter being meaningful.

**Binds later phases:** Phase 3 (REST) and Phase 4 (WebSocket) must not include
per-user wager data in any payload before the round is terminal; the aggregate
counter above is the only in-round progress signal. Phase 6 (frontend) must not
reconstruct one from client-side state either.

---

## Settled economy constants

| Constant | Value | Note |
|---|---|---|
| `StartingBalance` | 1,000 | Credited once at registration (consumed in Phase 3) |
| `RefillTarget` | 1,000 | Refills top *up to* this; also the eligibility ceiling (A2) |
| `RefillQuota` | 3 | Per rolling 7-day window; window counted in Redis (Phase 2) |
| `MinBuyIn` | 100 | |
| `MaxBuyIn` | 10,000 | Lowered from 100,000 (A3) |
| `StakeCapMultiple` | 3 | Account session wallet = `min(3 × buy-in, balance)` |

---

## Explicit non-goals for Phase 1

Listed so an executor doesn't wonder whether they were forgotten:

- **The host-cannot-bet guard.** Enforced in `place_wager.lua` (parent plan §5
  step 4), Phase 2. No Go counterpart is written until something calls it.
- **Lockout timing.** Evaluated with Redis `TIME` inside the Lua script, never
  in Go (CLAUDE.md invariant). The domain owns only the *status* rule — an open
  round accepts wagers — not the clock.
- **The 60-second host-disconnect timer.** Phase 4 owns the timer; the domain
  owns only the `locked → refunded` transition it triggers.
- **Idempotency keys.** Phase 2, in Lua and in the Postgres unique constraint.
- **`ErrPoolLocked`, `ErrHostCannotBet`, `ErrNotInRoom`.** These Lua return
  codes get Go counterparts in Phase 2, when something in Go actually returns
  them. Defining them now would be unused vocabulary.

---

## File Structure

All paths relative to `backend/internal/domain/`.

| File | Responsibility | Task |
|---|---|---|
| `round.go` | `RoundStatus`, the transition table, outcome-count and index validation | 1 |
| `errors.go` | The package's sentinel error vocabulary | 1 |
| `tokens.go` | `type Tokens int64` and the package doc comment | 2 |
| `economy.go` | The six economy constants | 2 |
| `wallet.go` | Buy-in bounds, session balances, partial buy-in, stake validation, session settlement | 2, 3 |
| `refill.go` | Refill eligibility and top-up amount | 4 |
| `payout.go` | `Stake`, `Payout`, `PlayerResult`, `Settlement`, `Settle` | 5, 6 |
| `odds.go` | Pari-mutuel multipliers — the only floats in the package | 7 |

Each has a matching `_test.go`. `payout_fuzz_test.go` is separate from
`payout_test.go` so the example-based tests and the property test stay legible
apart.

Files are split by responsibility rather than layer, per
`.claude/rules/ecc/common/coding-style.md`. None should exceed 150 lines.

---

## Task 1: Round state machine and outcome rules

**Files:**
- Create: `backend/internal/domain/round.go`
- Create: `backend/internal/domain/errors.go`
- Test: `backend/internal/domain/round_test.go`

**Interfaces:**
- Consumes: nothing — this is the first task.
- Produces:
  - `type RoundStatus string` with constants `RoundOpen`, `RoundLocked`, `RoundResolved`, `RoundRefunded` (values `"open"`, `"locked"`, `"resolved"`, `"refunded"`)
  - `func (s RoundStatus) Transition(next RoundStatus) (RoundStatus, error)`
  - `func (s RoundStatus) IsTerminal() bool`
  - `func (s RoundStatus) AcceptsWagers() bool`
  - `func ValidateOutcomeCount(n int) error`
  - `func ValidateOutcomeIndex(idx, count int) error`
  - `const MinOutcomes = 2`, `const MaxOutcomes = 4`
  - Sentinel errors `ErrInvalidTransition`, `ErrInvalidOutcomeCount`, `ErrInvalidOutcome`

The status *values* are the exact strings persisted in the `round:{roundID}`
Redis hash (parent plan §4), so Phase 2 needs no mapping layer between the
domain and the store.

### Checkpoint 1: Legal transitions succeed

- [ ] **Step 1: Write the failing test**

Create `backend/internal/domain/round_test.go`:

```go
package domain

import "testing"

func TestRoundStatusTransition_Legal(t *testing.T) {
	tests := []struct {
		name string
		from RoundStatus
		to   RoundStatus
	}{
		{
			name: "open round locks when the countdown expires",
			from: RoundOpen,
			to:   RoundLocked,
		},
		{
			name: "locked round resolves when the host calls the outcome",
			from: RoundLocked,
			to:   RoundResolved,
		},
		{
			name: "locked round refunds when it cannot be resolved",
			from: RoundLocked,
			to:   RoundRefunded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.from.Transition(tt.to)

			if err != nil {
				t.Fatalf("Transition(%s -> %s): unexpected error: %v", tt.from, tt.to, err)
			}
			if got != tt.to {
				t.Errorf("Transition(%s -> %s) = %s, want %s", tt.from, tt.to, got, tt.to)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run TestRoundStatusTransition_Legal -v`
Expected: FAIL — `undefined: RoundStatus` (the package does not exist yet).

- [ ] **Step 3: Write minimal implementation**

Create `backend/internal/domain/errors.go`:

```go
package domain

import "errors"

// The domain's failure vocabulary. Callers match with errors.Is and map
// to wire codes at the boundary. This list is deliberately limited to
// failures this package can actually produce — the remaining Lua return
// codes (POOL_LOCKED, HOST_CANNOT_BET, NOT_IN_ROOM) gain Go counterparts
// in Phase 2, when something here returns them.
var (
	ErrInvalidTransition = errors.New("domain: invalid round status transition")
)
```

Create `backend/internal/domain/round.go`:

```go
package domain

// RoundStatus is a round's lifecycle state. The values are the exact
// strings persisted in the round:{roundID} Redis hash (plan §4), so no
// mapping layer is needed between the domain and the store.
type RoundStatus string

const (
	RoundOpen     RoundStatus = "open"
	RoundLocked   RoundStatus = "locked"
	RoundResolved RoundStatus = "resolved"
	RoundRefunded RoundStatus = "refunded"
)

// validTransitions is the whole state machine. A status absent from this
// map has nowhere legal to go, which is what makes it terminal.
var validTransitions = map[RoundStatus][]RoundStatus{
	RoundOpen:   {RoundLocked},
	RoundLocked: {RoundResolved, RoundRefunded},
}

// Transition returns the status to move to, or ErrInvalidTransition if
// the move is illegal. It never mutates the receiver; on failure it
// returns the unchanged current status.
func (s RoundStatus) Transition(next RoundStatus) (RoundStatus, error) {
	for _, allowed := range validTransitions[s] {
		if allowed == next {
			return next, nil
		}
	}
	return s, ErrInvalidTransition
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -run TestRoundStatusTransition_Legal -v`
Expected: PASS — three subtests.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/round.go backend/internal/domain/errors.go backend/internal/domain/round_test.go
git commit -m "feat: add round status transition table"
```

### Checkpoint 2: Illegal transitions are rejected and terminal states go nowhere

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/domain/round_test.go`:

```go
func TestRoundStatusTransition_Illegal(t *testing.T) {
	tests := []struct {
		name string
		from RoundStatus
		to   RoundStatus
	}{
		{
			name: "open cannot skip lockout and resolve directly",
			from: RoundOpen,
			to:   RoundResolved,
		},
		{
			name: "open cannot refund before it locks",
			from: RoundOpen,
			to:   RoundRefunded,
		},
		{
			name: "locked cannot reopen for more wagers",
			from: RoundLocked,
			to:   RoundOpen,
		},
		{
			name: "resolved is terminal and cannot be refunded",
			from: RoundResolved,
			to:   RoundRefunded,
		},
		{
			name: "refunded is terminal and cannot be resolved",
			from: RoundRefunded,
			to:   RoundResolved,
		},
		{
			name: "a status cannot transition to itself",
			from: RoundOpen,
			to:   RoundOpen,
		},
		{
			name: "an unrecognized status has nowhere legal to go",
			from: RoundStatus("garbage"),
			to:   RoundOpen,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.from.Transition(tt.to)

			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("Transition(%s -> %s): got error %v, want ErrInvalidTransition", tt.from, tt.to, err)
			}
			if got != tt.from {
				t.Errorf("Transition(%s -> %s) returned status %s, want the unchanged %s", tt.from, tt.to, got, tt.from)
			}
		})
	}
}

func TestRoundStatusIsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		status RoundStatus
		want   bool
	}{
		{name: "open is not terminal", status: RoundOpen, want: false},
		{name: "locked is not terminal", status: RoundLocked, want: false},
		{name: "resolved is terminal", status: RoundResolved, want: true},
		{name: "refunded is terminal", status: RoundRefunded, want: true},
		{name: "an unrecognized status is terminal", status: RoundStatus("garbage"), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.want {
				t.Errorf("RoundStatus(%q).IsTerminal() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
```

Add `"errors"` to the test file's import block, which becomes:

```go
import (
	"errors"
	"testing"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run 'TestRoundStatusTransition_Illegal|TestRoundStatusIsTerminal' -v`
Expected: FAIL — `undefined: IsTerminal`. (`TestRoundStatusTransition_Illegal` may already pass, since Checkpoint 1's implementation returns the sentinel; that is fine — the compile failure is what makes this RED.)

- [ ] **Step 3: Write minimal implementation**

Enrich the error with context and add `IsTerminal` in `round.go`. Replace the
`Transition` method and add below it:

```go
// Transition returns the status to move to, or an error wrapping
// ErrInvalidTransition if the move is illegal. It never mutates the
// receiver; on failure it returns the unchanged current status.
func (s RoundStatus) Transition(next RoundStatus) (RoundStatus, error) {
	for _, allowed := range validTransitions[s] {
		if allowed == next {
			return next, nil
		}
	}
	return s, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, s, next)
}

// IsTerminal reports whether no further transition is legal from s. That
// covers resolved and refunded rounds, and equally any unrecognized
// status, which by definition has no legal move.
func (s RoundStatus) IsTerminal() bool {
	return len(validTransitions[s]) == 0
}
```

Add the import to `round.go`:

```go
import "fmt"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v`
Expected: PASS — all subtests across the three test functions.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/round.go backend/internal/domain/round_test.go
git commit -m "feat: reject illegal round transitions and identify terminal states"
```

### Checkpoint 3: Only an open round accepts wagers

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/domain/round_test.go`:

```go
func TestRoundStatusAcceptsWagers(t *testing.T) {
	tests := []struct {
		name   string
		status RoundStatus
		want   bool
	}{
		{name: "open accepts wagers", status: RoundOpen, want: true},
		{name: "locked rejects wagers", status: RoundLocked, want: false},
		{name: "resolved rejects wagers", status: RoundResolved, want: false},
		{name: "refunded rejects wagers", status: RoundRefunded, want: false},
		{name: "an unrecognized status rejects wagers", status: RoundStatus("garbage"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.AcceptsWagers(); got != tt.want {
				t.Errorf("RoundStatus(%q).AcceptsWagers() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run TestRoundStatusAcceptsWagers -v`
Expected: FAIL — `undefined: AcceptsWagers`.

- [ ] **Step 3: Write minimal implementation**

Append to `round.go`:

```go
// AcceptsWagers reports whether a round in this status may take new
// wagers. Only an open round does. This is the status half of the rule
// only — lockout is additionally enforced against the Redis clock inside
// place_wager.lua (plan §5), never against a Go timestamp.
func (s RoundStatus) AcceptsWagers() bool {
	return s == RoundOpen
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/round.go backend/internal/domain/round_test.go
git commit -m "feat: gate wager acceptance on open round status"
```

### Checkpoint 4: Outcome count and index validation

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/domain/round_test.go`:

```go
func TestValidateOutcomeCount(t *testing.T) {
	tests := []struct {
		name    string
		count   int
		wantErr bool
	}{
		{name: "two outcomes is the binary minimum", count: 2},
		{name: "three outcomes is allowed", count: 3},
		{name: "four outcomes is the maximum", count: 4},
		{name: "one outcome is not a prediction", count: 1, wantErr: true},
		{name: "zero outcomes is rejected", count: 0, wantErr: true},
		{name: "negative outcomes is rejected", count: -1, wantErr: true},
		{name: "five outcomes exceeds the maximum", count: 5, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOutcomeCount(tt.count)

			if tt.wantErr {
				if !errors.Is(err, ErrInvalidOutcomeCount) {
					t.Fatalf("ValidateOutcomeCount(%d) = %v, want ErrInvalidOutcomeCount", tt.count, err)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateOutcomeCount(%d): unexpected error: %v", tt.count, err)
			}
		})
	}
}

func TestValidateOutcomeIndex(t *testing.T) {
	tests := []struct {
		name    string
		idx     int
		count   int
		wantErr bool
	}{
		{name: "first outcome of two", idx: 0, count: 2},
		{name: "last outcome of four", idx: 3, count: 4},
		{name: "index equal to count is out of range", idx: 2, count: 2, wantErr: true},
		{name: "index beyond count is out of range", idx: 9, count: 4, wantErr: true},
		{name: "negative index is out of range", idx: -1, count: 4, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOutcomeIndex(tt.idx, tt.count)

			if tt.wantErr {
				if !errors.Is(err, ErrInvalidOutcome) {
					t.Fatalf("ValidateOutcomeIndex(%d, %d) = %v, want ErrInvalidOutcome", tt.idx, tt.count, err)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateOutcomeIndex(%d, %d): unexpected error: %v", tt.idx, tt.count, err)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run 'TestValidateOutcome' -v`
Expected: FAIL — `undefined: ValidateOutcomeCount`.

- [ ] **Step 3: Write minimal implementation**

Add the two sentinels to `errors.go`, so the var block reads:

```go
var (
	ErrInvalidTransition   = errors.New("domain: invalid round status transition")
	ErrInvalidOutcomeCount = errors.New("domain: round outcome count out of range")
	ErrInvalidOutcome      = errors.New("domain: outcome index out of range")
)
```

Append to `round.go`:

```go
// Outcome-count bounds. The host types 2-4 custom options per round
// (spec §4) — binary yes/no is the lower bound, not the shape.
const (
	MinOutcomes = 2
	MaxOutcomes = 4
)

// ValidateOutcomeCount rejects a round whose outcome list falls outside
// the permitted range.
func ValidateOutcomeCount(n int) error {
	if n < MinOutcomes || n > MaxOutcomes {
		return fmt.Errorf("%w: got %d, want %d-%d", ErrInvalidOutcomeCount, n, MinOutcomes, MaxOutcomes)
	}
	return nil
}

// ValidateOutcomeIndex rejects a reference to an outcome the round does
// not have. count is the round's outcome_count. This mirrors the
// INVALID_OUTCOME branch of place_wager.lua (plan §5) — the Lua script
// re-checks it because it cannot trust the caller, not because this
// check is redundant.
func ValidateOutcomeIndex(idx, count int) error {
	if idx < 0 || idx >= count {
		return fmt.Errorf("%w: index %d, round has %d outcomes", ErrInvalidOutcome, idx, count)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v -cover`
Expected: PASS, coverage 100.0%.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/round.go backend/internal/domain/errors.go backend/internal/domain/round_test.go
git commit -m "feat: validate round outcome count and index bounds"
```

---

## Task 2: Token type, economy constants, and room buy-in rules

**Files:**
- Create: `backend/internal/domain/tokens.go`
- Create: `backend/internal/domain/economy.go`
- Create: `backend/internal/domain/wallet.go`
- Modify: `backend/internal/domain/errors.go` (add `ErrInvalidBuyIn`)
- Test: `backend/internal/domain/wallet_test.go`

**Interfaces:**
- Consumes: `ErrInvalidBuyIn` pattern from Task 1's `errors.go`.
- Produces:
  - `type Tokens int64`
  - Constants `StartingBalance`, `RefillTarget`, `RefillQuota`, `MinBuyIn`, `MaxBuyIn`, `StakeCapMultiple`
  - `func ValidateBuyIn(buyIn Tokens) error`
  - `func GuestSessionBalance(roomBuyIn Tokens) Tokens`
  - `func AccountSessionBalance(accountBalance, roomBuyIn Tokens) Tokens`
  - `func IsPartialBuyIn(accountBalance, roomBuyIn Tokens) bool`

The package doc comment moves to `tokens.go` in this task — `round.go` should
have its `package domain` line left bare afterwards.

### Checkpoint 1: Room buy-in bounds

- [ ] **Step 1: Write the failing test**

Create `backend/internal/domain/wallet_test.go`:

```go
package domain

import (
	"errors"
	"testing"
)

func TestValidateBuyIn(t *testing.T) {
	tests := []struct {
		name    string
		buyIn   Tokens
		wantErr bool
	}{
		{name: "the minimum buy-in is allowed", buyIn: MinBuyIn},
		{name: "the maximum buy-in is allowed", buyIn: MaxBuyIn},
		{name: "a mid-range buy-in is allowed", buyIn: 2500},
		{name: "one token below the minimum is rejected", buyIn: MinBuyIn - 1, wantErr: true},
		{name: "one token above the maximum is rejected", buyIn: MaxBuyIn + 1, wantErr: true},
		{name: "a zero buy-in puts nothing at stake", buyIn: 0, wantErr: true},
		{name: "a negative buy-in is rejected", buyIn: -500, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBuyIn(tt.buyIn)

			if tt.wantErr {
				if !errors.Is(err, ErrInvalidBuyIn) {
					t.Fatalf("ValidateBuyIn(%d) = %v, want ErrInvalidBuyIn", tt.buyIn, err)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateBuyIn(%d): unexpected error: %v", tt.buyIn, err)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run TestValidateBuyIn -v`
Expected: FAIL — `undefined: Tokens`, `undefined: MinBuyIn`.

- [ ] **Step 3: Write minimal implementation**

Create `backend/internal/domain/tokens.go` — this file carries the package doc:

```go
// Package domain holds CallIt's money math and round rules. It performs
// no I/O by design: every rule here is unit-testable with nothing
// running, which is where correctness bugs in a wagering engine are
// cheapest to catch (plan §9). Its only imports are errors and fmt.
package domain

// Tokens is a quantity of virtual currency, always a whole number of
// units. The named type exists so that a float can never reach a
// balance, a pool, or a stake by accident — odds become floating point
// only at the presentation layer, and Multiplier is the one function
// whose signature says so.
type Tokens int64
```

Remove the now-duplicated package doc comment from `round.go`, leaving it
starting with a bare `package domain`.

Create `backend/internal/domain/economy.go`:

```go
package domain

// Economy constants. These are platform invariants rather than
// deployment configuration — none of them is tunable per environment —
// so they live here rather than in internal/config, which loads and
// validates environment variables. Plan §8 originally placed them in
// config; see docs/plans/2026-08-23-phase-1-domain-core.md §A1.
const (
	// StartingBalance is credited once, when an account registers. Its
	// consumer arrives in Phase 3; it is defined here so the whole
	// economy reads in one place.
	StartingBalance Tokens = 1000

	// RefillTarget is the balance a manual refill tops an account up to,
	// and equally the ceiling below which a refill may be claimed at
	// all. One number in two roles: an account under the target may
	// claim, and claiming brings it exactly to the target.
	RefillTarget Tokens = 1000

	// RefillQuota is how many refills an account may claim per rolling
	// seven-day window. The window is counted by the Redis
	// sliding-window limiter in Phase 2; this package owns the policy
	// only, not the counting.
	RefillQuota int = 3

	// MinBuyIn and MaxBuyIn bound the buy-in a host may set at room
	// creation. The ceiling is deliberately on the same order as
	// RefillTarget: far above it, top-stakes rooms become unreachable
	// for anyone not already wealthy.
	MinBuyIn Tokens = 100
	MaxBuyIn Tokens = 10_000

	// StakeCapMultiple is how many times the room's buy-in an account
	// holder may bring into a room, bounded by what they actually hold.
	StakeCapMultiple Tokens = 3
)
```

Add to `errors.go`'s var block:

```go
	ErrInvalidBuyIn = errors.New("domain: room buy-in out of range")
```

Create `backend/internal/domain/wallet.go`:

```go
package domain

import "fmt"

// ValidateBuyIn rejects a host-configured room buy-in outside the
// platform bounds.
func ValidateBuyIn(buyIn Tokens) error {
	if buyIn < MinBuyIn || buyIn > MaxBuyIn {
		return fmt.Errorf("%w: %d not in %d-%d", ErrInvalidBuyIn, buyIn, MinBuyIn, MaxBuyIn)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/tokens.go backend/internal/domain/economy.go backend/internal/domain/wallet.go backend/internal/domain/errors.go backend/internal/domain/round.go backend/internal/domain/wallet_test.go
git commit -m "feat: add Tokens type, economy constants, and buy-in bounds"
```

### Checkpoint 2: Session balances for guests and account holders

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/domain/wallet_test.go`:

```go
func TestGuestSessionBalance(t *testing.T) {
	tests := []struct {
		name  string
		buyIn Tokens
		want  Tokens
	}{
		{name: "a guest joins with exactly the buy-in", buyIn: 500, want: 500},
		{name: "the 3x account multiple never applies to guests", buyIn: MaxBuyIn, want: MaxBuyIn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GuestSessionBalance(tt.buyIn); got != tt.want {
				t.Errorf("GuestSessionBalance(%d) = %d, want %d", tt.buyIn, got, tt.want)
			}
		})
	}
}

func TestAccountSessionBalance(t *testing.T) {
	tests := []struct {
		name           string
		accountBalance Tokens
		roomBuyIn      Tokens
		want           Tokens
	}{
		{
			name:           "a wealthy account is capped at three times the buy-in",
			accountBalance: 10_000,
			roomBuyIn:      500,
			want:           1500,
		},
		{
			name:           "an account below the cap brings its whole balance",
			accountBalance: 800,
			roomBuyIn:      500,
			want:           800,
		},
		{
			name:           "an account short of the buy-in joins partial",
			accountBalance: 200,
			roomBuyIn:      2000,
			want:           200,
		},
		{
			name:           "an account exactly at the cap brings the cap",
			accountBalance: 1500,
			roomBuyIn:      500,
			want:           1500,
		},
		{
			name:           "an empty account brings nothing",
			accountBalance: 0,
			roomBuyIn:      500,
			want:           0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AccountSessionBalance(tt.accountBalance, tt.roomBuyIn)

			if got != tt.want {
				t.Errorf("AccountSessionBalance(%d, %d) = %d, want %d",
					tt.accountBalance, tt.roomBuyIn, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run 'SessionBalance' -v`
Expected: FAIL — `undefined: GuestSessionBalance`.

- [ ] **Step 3: Write minimal implementation**

Append to `wallet.go`:

```go
// GuestSessionBalance is what a guest joins a room with: exactly the
// room's buy-in, wiped when the session ends (spec §3). Guests hold no
// persistent account, so the account-holder multiple never applies to
// them.
func GuestSessionBalance(roomBuyIn Tokens) Tokens {
	return roomBuyIn
}

// AccountSessionBalance is what an account holder joins a room with:
// min(StakeCapMultiple x buy-in, account balance) (plan §8). Handing
// them the whole cap up front is what lets place_wager.lua check nothing
// but the session balance — the cap is embodied in the wallet rather
// than re-evaluated on every wager.
func AccountSessionBalance(accountBalance, roomBuyIn Tokens) Tokens {
	limit := roomBuyIn * StakeCapMultiple
	if accountBalance < limit {
		return accountBalance
	}
	return limit
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/wallet.go backend/internal/domain/wallet_test.go
git commit -m "feat: compute guest and account-holder session balances"
```

### Checkpoint 3: Partial buy-in detection

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/domain/wallet_test.go`:

```go
func TestIsPartialBuyIn(t *testing.T) {
	tests := []struct {
		name           string
		accountBalance Tokens
		roomBuyIn      Tokens
		want           bool
	}{
		{
			name:           "a balance below the buy-in is partial",
			accountBalance: 200,
			roomBuyIn:      2000,
			want:           true,
		},
		{
			name:           "a balance exactly at the buy-in is not partial",
			accountBalance: 2000,
			roomBuyIn:      2000,
			want:           false,
		},
		{
			name:           "a balance above the buy-in is not partial",
			accountBalance: 5000,
			roomBuyIn:      2000,
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPartialBuyIn(tt.accountBalance, tt.roomBuyIn)

			if got != tt.want {
				t.Errorf("IsPartialBuyIn(%d, %d) = %v, want %v",
					tt.accountBalance, tt.roomBuyIn, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run TestIsPartialBuyIn -v`
Expected: FAIL — `undefined: IsPartialBuyIn`.

- [ ] **Step 3: Write minimal implementation**

Append to `wallet.go`:

```go
// IsPartialBuyIn reports whether an account holder is joining with less
// than the room's full buy-in, which the UI surfaces transparently
// (spec §3, e.g. "joined with 200/2000"). It is a display rule, not a
// gate — a partial buy-in is always permitted.
func IsPartialBuyIn(accountBalance, roomBuyIn Tokens) bool {
	return accountBalance < roomBuyIn
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v -cover`
Expected: PASS, coverage 100.0%.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/wallet.go backend/internal/domain/wallet_test.go
git commit -m "feat: detect partial buy-in for transparent UI display"
```

---

## Task 3: Stake validation and session settlement

**Files:**
- Modify: `backend/internal/domain/wallet.go`
- Modify: `backend/internal/domain/errors.go` (add `ErrInvalidStake`, `ErrInsufficientFunds`)
- Modify: `backend/internal/domain/wallet_test.go`

**Interfaces:**
- Consumes: `Tokens` from Task 2.
- Produces:
  - `func ValidateStake(amount, sessionBalance Tokens) error`
  - `func ApplySessionResult(accountBalance, sessionStart, sessionEnd Tokens) Tokens`
  - `ErrInvalidStake`, `ErrInsufficientFunds`

### Checkpoint 1: Non-positive stakes are rejected

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/domain/wallet_test.go`:

```go
func TestValidateStake_NonPositive(t *testing.T) {
	tests := []struct {
		name   string
		amount Tokens
	}{
		{name: "a zero stake is not a wager", amount: 0},
		{name: "a negative stake would mint tokens", amount: -100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStake(tt.amount, 1000)

			if !errors.Is(err, ErrInvalidStake) {
				t.Fatalf("ValidateStake(%d, 1000) = %v, want ErrInvalidStake", tt.amount, err)
			}
		})
	}
}

func TestValidateStake_Valid(t *testing.T) {
	tests := []struct {
		name           string
		amount         Tokens
		sessionBalance Tokens
	}{
		{name: "a stake below the balance is accepted", amount: 100, sessionBalance: 1000},
		{name: "a stake equal to the balance is accepted", amount: 1000, sessionBalance: 1000},
		{name: "the smallest possible stake is accepted", amount: 1, sessionBalance: 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateStake(tt.amount, tt.sessionBalance); err != nil {
				t.Errorf("ValidateStake(%d, %d): unexpected error: %v", tt.amount, tt.sessionBalance, err)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run TestValidateStake -v`
Expected: FAIL — `undefined: ValidateStake`.

- [ ] **Step 3: Write minimal implementation**

Add to `errors.go`'s var block:

```go
	ErrInvalidStake = errors.New("domain: stake must be positive")
```

Append to `wallet.go`:

```go
// ValidateStake rejects a wager that is not a positive whole number of
// tokens. A zero stake is not a wager; a negative one would mint tokens
// out of the pool.
func ValidateStake(amount, sessionBalance Tokens) error {
	if amount <= 0 {
		return fmt.Errorf("%w: got %d", ErrInvalidStake, amount)
	}
	return nil
}
```

Note the unused `sessionBalance` parameter is deliberate at this checkpoint —
Checkpoint 2 gives it its rule. Go permits unused function parameters, so this
compiles.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/wallet.go backend/internal/domain/errors.go backend/internal/domain/wallet_test.go
git commit -m "feat: reject non-positive wager stakes"
```

### Checkpoint 2: Stakes exceeding the wallet are rejected

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/domain/wallet_test.go`:

```go
func TestValidateStake_InsufficientFunds(t *testing.T) {
	tests := []struct {
		name           string
		amount         Tokens
		sessionBalance Tokens
	}{
		{name: "one token more than the balance", amount: 1001, sessionBalance: 1000},
		{name: "far more than the balance", amount: 50_000, sessionBalance: 1000},
		{name: "any stake against an empty wallet", amount: 1, sessionBalance: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStake(tt.amount, tt.sessionBalance)

			if !errors.Is(err, ErrInsufficientFunds) {
				t.Fatalf("ValidateStake(%d, %d) = %v, want ErrInsufficientFunds",
					tt.amount, tt.sessionBalance, err)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run TestValidateStake_InsufficientFunds -v`
Expected: FAIL — `undefined: ErrInsufficientFunds`.

- [ ] **Step 3: Write minimal implementation**

Add to `errors.go`'s var block:

```go
	ErrInsufficientFunds = errors.New("domain: stake exceeds available balance")
```

Replace `ValidateStake` in `wallet.go`:

```go
// ValidateStake rejects a wager that is not a positive whole number of
// tokens, or that exceeds the wallet it would be drawn from. A zero
// stake is not a wager; a negative one would mint tokens out of the
// pool. The 3x account cap needs no check here — it is already embodied
// in sessionBalance by AccountSessionBalance.
func ValidateStake(amount, sessionBalance Tokens) error {
	if amount <= 0 {
		return fmt.Errorf("%w: got %d", ErrInvalidStake, amount)
	}
	if amount > sessionBalance {
		return fmt.Errorf("%w: stake %d, balance %d", ErrInsufficientFunds, amount, sessionBalance)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/wallet.go backend/internal/domain/errors.go backend/internal/domain/wallet_test.go
git commit -m "feat: reject stakes exceeding the session wallet"
```

### Checkpoint 3: Session profit and loss folds into the persistent balance

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/domain/wallet_test.go`:

```go
func TestApplySessionResult(t *testing.T) {
	tests := []struct {
		name           string
		accountBalance Tokens
		sessionStart   Tokens
		sessionEnd     Tokens
		want           Tokens
	}{
		{
			name:           "a winning session adds only the net gain",
			accountBalance: 1000,
			sessionStart:   1000,
			sessionEnd:     1600,
			want:           1600,
		},
		{
			name:           "a partial buy-in win adds the gain, not the session total",
			accountBalance: 300,
			sessionStart:   300,
			sessionEnd:     900,
			want:           900,
		},
		{
			name:           "a capped session adds the gain on top of the untouched balance",
			accountBalance: 10_000,
			sessionStart:   1500,
			sessionEnd:     2400,
			want:           10_900,
		},
		{
			name:           "a losing session subtracts only the net loss",
			accountBalance: 10_000,
			sessionStart:   1500,
			sessionEnd:     400,
			want:           8900,
		},
		{
			name:           "a total wipeout of a full-balance session floors at zero",
			accountBalance: 1000,
			sessionStart:   1000,
			sessionEnd:     0,
			want:           0,
		},
		{
			name:           "a break-even session changes nothing",
			accountBalance: 1000,
			sessionStart:   1000,
			sessionEnd:     1000,
			want:           1000,
		},
		{
			name:           "an inconsistent caller cannot drive the balance negative",
			accountBalance: 100,
			sessionStart:   5000,
			sessionEnd:     0,
			want:           0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplySessionResult(tt.accountBalance, tt.sessionStart, tt.sessionEnd)

			if got != tt.want {
				t.Errorf("ApplySessionResult(%d, %d, %d) = %d, want %d",
					tt.accountBalance, tt.sessionStart, tt.sessionEnd, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run TestApplySessionResult -v`
Expected: FAIL — `undefined: ApplySessionResult`.

- [ ] **Step 3: Write minimal implementation**

Append to `wallet.go`:

```go
// ApplySessionResult folds a finished session's net profit or loss into
// a persistent account balance (spec §3). It is the delta that carries
// across, not the session's final balance — an account holder who brings
// 1,500 of a 10,000 balance into a room and leaves with 2,400 gains 900,
// not 2,400.
//
// The floor at zero is defence in depth: a session balance never exceeds
// the account balance it came from, so the sum cannot legitimately go
// negative. It exists so that an inconsistent caller cannot mint a
// negative account rather than because the arithmetic needs it.
func ApplySessionResult(accountBalance, sessionStart, sessionEnd Tokens) Tokens {
	updated := accountBalance + (sessionEnd - sessionStart)
	if updated < 0 {
		return 0
	}
	return updated
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v -cover`
Expected: PASS, coverage 100.0%.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/wallet.go backend/internal/domain/wallet_test.go
git commit -m "feat: fold session profit and loss into persistent balance"
```

---

## Task 4: Refill eligibility and top-up amount

**Files:**
- Create: `backend/internal/domain/refill.go`
- Modify: `backend/internal/domain/errors.go` (add `ErrRefillNotEligible`, `ErrRefillQuotaExhausted`)
- Test: `backend/internal/domain/refill_test.go`

**Interfaces:**
- Consumes: `Tokens`, `RefillTarget`, `RefillQuota` from Task 2.
- Produces:
  - `func CanRefill(balance Tokens, claimsInWindow int) error`
  - `func RefillAmount(balance Tokens) Tokens`
  - `ErrRefillNotEligible`, `ErrRefillQuotaExhausted`

`claimsInWindow` is supplied by the caller. Phase 2's Redis sliding-window ZSET
does the counting; this package owns only the policy. That split is what keeps
the rule testable with nothing running.

### Checkpoint 1: Eligibility by balance

- [ ] **Step 1: Write the failing test**

Create `backend/internal/domain/refill_test.go`:

```go
package domain

import (
	"errors"
	"testing"
)

func TestCanRefill_Balance(t *testing.T) {
	tests := []struct {
		name    string
		balance Tokens
		wantErr bool
	}{
		{name: "an empty account may refill", balance: 0},
		{name: "an account well below target may refill", balance: 150},
		{name: "one token below target may refill", balance: RefillTarget - 1},
		{name: "an account exactly at target has nothing to claim", balance: RefillTarget, wantErr: true},
		{name: "an account above target has nothing to claim", balance: 5000, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CanRefill(tt.balance, 0)

			if tt.wantErr {
				if !errors.Is(err, ErrRefillNotEligible) {
					t.Fatalf("CanRefill(%d, 0) = %v, want ErrRefillNotEligible", tt.balance, err)
				}
				return
			}
			if err != nil {
				t.Errorf("CanRefill(%d, 0): unexpected error: %v", tt.balance, err)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run TestCanRefill_Balance -v`
Expected: FAIL — `undefined: CanRefill`.

- [ ] **Step 3: Write minimal implementation**

Add to `errors.go`'s var block:

```go
	ErrRefillNotEligible = errors.New("domain: balance is not below the refill target")
```

Create `backend/internal/domain/refill.go`:

```go
package domain

import "fmt"

// CanRefill reports whether an account may claim a manual refill right
// now. Eligibility is simply "below the target" — there is no separate
// threshold constant, because two names for one number invite a future
// cleanup that makes them diverge (see this phase's plan, §A2).
//
// claimsInWindow is supplied by the caller, counted by the Redis
// sliding-window limiter in Phase 2. This function owns the policy, not
// the counting, which is what keeps it testable with nothing running.
func CanRefill(balance Tokens, claimsInWindow int) error {
	if balance >= RefillTarget {
		return fmt.Errorf("%w: balance %d, target %d", ErrRefillNotEligible, balance, RefillTarget)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/refill.go backend/internal/domain/errors.go backend/internal/domain/refill_test.go
git commit -m "feat: gate refill eligibility on balance below target"
```

### Checkpoint 2: The rolling-window quota

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/domain/refill_test.go`:

```go
func TestCanRefill_Quota(t *testing.T) {
	tests := []struct {
		name           string
		claimsInWindow int
		wantErr        bool
	}{
		{name: "no claims used yet", claimsInWindow: 0},
		{name: "one claim used", claimsInWindow: 1},
		{name: "the last available claim", claimsInWindow: RefillQuota - 1},
		{name: "the quota is exhausted", claimsInWindow: RefillQuota, wantErr: true},
		{name: "somehow over quota is still exhausted", claimsInWindow: RefillQuota + 5, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CanRefill(0, tt.claimsInWindow)

			if tt.wantErr {
				if !errors.Is(err, ErrRefillQuotaExhausted) {
					t.Fatalf("CanRefill(0, %d) = %v, want ErrRefillQuotaExhausted", tt.claimsInWindow, err)
				}
				return
			}
			if err != nil {
				t.Errorf("CanRefill(0, %d): unexpected error: %v", tt.claimsInWindow, err)
			}
		})
	}
}

func TestCanRefill_BalanceCheckedBeforeQuota(t *testing.T) {
	// An ineligible balance should report why it is ineligible rather
	// than blaming the quota, so the UI can say the useful thing.
	err := CanRefill(RefillTarget, RefillQuota)

	if !errors.Is(err, ErrRefillNotEligible) {
		t.Fatalf("CanRefill(%d, %d) = %v, want ErrRefillNotEligible", RefillTarget, RefillQuota, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run TestCanRefill_Quota -v`
Expected: FAIL — `undefined: ErrRefillQuotaExhausted`.

- [ ] **Step 3: Write minimal implementation**

Add to `errors.go`'s var block:

```go
	ErrRefillQuotaExhausted = errors.New("domain: refill quota exhausted for the current window")
```

Append the quota branch inside `CanRefill` in `refill.go`, after the balance
check and before `return nil`:

```go
	if claimsInWindow >= RefillQuota {
		return fmt.Errorf("%w: %d of %d claims used", ErrRefillQuotaExhausted, claimsInWindow, RefillQuota)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/refill.go backend/internal/domain/errors.go backend/internal/domain/refill_test.go
git commit -m "feat: enforce rolling-window refill quota"
```

### Checkpoint 3: The top-up amount

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/domain/refill_test.go`:

```go
func TestRefillAmount(t *testing.T) {
	tests := []struct {
		name    string
		balance Tokens
		want    Tokens
	}{
		{name: "an empty account tops up by the full target", balance: 0, want: RefillTarget},
		{name: "a partial balance tops up by the difference", balance: 150, want: RefillTarget - 150},
		{name: "one token short tops up by one", balance: RefillTarget - 1, want: 1},
		{name: "an account at target tops up by nothing", balance: RefillTarget, want: 0},
		{name: "an account above target never returns a negative", balance: 5000, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RefillAmount(tt.balance); got != tt.want {
				t.Errorf("RefillAmount(%d) = %d, want %d", tt.balance, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run TestRefillAmount -v`
Expected: FAIL — `undefined: RefillAmount`.

- [ ] **Step 3: Write minimal implementation**

Append to `refill.go`:

```go
// RefillAmount is the top-up that brings balance exactly to the platform
// refill target. Callers should check CanRefill first; for a balance
// already at or above the target this returns 0 rather than a negative
// amount, so a careless caller cannot burn tokens.
func RefillAmount(balance Tokens) Tokens {
	if balance >= RefillTarget {
		return 0
	}
	return RefillTarget - balance
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v -cover`
Expected: PASS, coverage 100.0%.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/refill.go backend/internal/domain/refill_test.go
git commit -m "feat: compute refill top-up to platform target"
```

---

## Task 5: Pari-mutuel payout with dust distribution

The core of the phase. Every other task exists so this one can be written
against solid primitives.

**Files:**
- Create: `backend/internal/domain/payout.go`
- Test: `backend/internal/domain/payout_test.go`

**Interfaces:**
- Consumes: `Tokens` (Task 2), `ValidateOutcomeIndex`, `ErrInvalidOutcome` (Task 1), `ErrInvalidStake` (Task 3).
- Produces:
  - `type Stake struct { UserID string; Outcome int; Amount Tokens }`
  - `type Payout struct { UserID string; Amount Tokens }`
  - `type PlayerResult struct { UserID string; Staked, Returned, Net Tokens }`
  - `type Settlement struct { Payouts []Payout; Results []PlayerResult; Dust Tokens; Refunded bool }`
  - `func Settle(stakes []Stake, winningOutcome, outcomeCount int) (Settlement, error)`

`Stake` mirrors one field of the `round:{roundID}:wagers` Redis hash, whose key
is `{userID}:{outcomeIdx}` (parent plan §4). So `(UserID, Outcome)` is unique
within a round, and a player's repeat wagers on one outcome arrive already
summed by `HINCRBY`. A player may hold stakes on several *different* outcomes.

`Payouts` is one credit per settled stake, in input order — deterministic, so
settling the same round twice yields identical output.

### Checkpoint 1: The pari-mutuel payout formula

Covers the formula and everything that follows from it directly: proportional
split, flooring into dust, and losers receiving nothing. These are one
RED→GREEN cycle, not four — a correct `floor(stake * total / winningPool)`
satisfies all of them at once, so splitting them would produce checkpoints
whose tests pass the moment they are written.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/domain/payout_test.go`. Note the import block carries
only what this checkpoint uses — Go rejects an unused import, so `errors`
arrives in Checkpoint 3 when the first `errors.Is` does:

```go
package domain

import (
	"reflect"
	"testing"
)

func TestSettle_SoleWinnerTakesTotal(t *testing.T) {
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 100},
		{UserID: "bob", Outcome: 1, Amount: 300},
		{UserID: "carol", Outcome: 1, Amount: 600},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	want := []Payout{{UserID: "alice", Amount: 1000}}
	if !reflect.DeepEqual(got.Payouts, want) {
		t.Errorf("Settle payouts = %+v, want %+v", got.Payouts, want)
	}
	if got.Dust != 0 {
		t.Errorf("Settle dust = %d, want 0", got.Dust)
	}
	if got.Refunded {
		t.Error("Settle marked the round refunded, want resolved")
	}
}

func TestSettle_ProportionalSplit(t *testing.T) {
	// Winning pool 400 (alice 100, bob 300), losing pool 600.
	// Total 1000, so the multiplier is 2.5x.
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 100},
		{UserID: "bob", Outcome: 0, Amount: 300},
		{UserID: "carol", Outcome: 1, Amount: 600},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	want := []Payout{
		{UserID: "alice", Amount: 250},
		{UserID: "bob", Amount: 750},
	}
	if !reflect.DeepEqual(got.Payouts, want) {
		t.Errorf("Settle payouts = %+v, want %+v", got.Payouts, want)
	}
	if got.Dust != 0 {
		t.Errorf("Settle dust = %d, want 0 for an evenly divisible pool", got.Dust)
	}
}

func TestSettle_ThreeOutcomes(t *testing.T) {
	// Winning pool 200 of a 1000 total: a 5x multiplier.
	stakes := []Stake{
		{UserID: "alice", Outcome: 2, Amount: 200},
		{UserID: "bob", Outcome: 0, Amount: 500},
		{UserID: "carol", Outcome: 1, Amount: 300},
	}

	got, err := Settle(stakes, 2, 3)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	want := []Payout{{UserID: "alice", Amount: 1000}}
	if !reflect.DeepEqual(got.Payouts, want) {
		t.Errorf("Settle payouts = %+v, want %+v", got.Payouts, want)
	}
}

func TestSettle_FlooringProducesDust(t *testing.T) {
	// Winning pool 3 (alice 1, bob 2), total 10.
	// alice: floor(1 * 10 / 3) = 3.  bob: floor(2 * 10 / 3) = 6.
	// Paid 9 of 10 — one token of dust.
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 1},
		{UserID: "bob", Outcome: 0, Amount: 2},
		{UserID: "carol", Outcome: 1, Amount: 7},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	want := []Payout{
		{UserID: "alice", Amount: 3},
		{UserID: "bob", Amount: 6},
	}
	if !reflect.DeepEqual(got.Payouts, want) {
		t.Errorf("Settle payouts = %+v, want %+v", got.Payouts, want)
	}
	if got.Dust != 1 {
		t.Errorf("Settle dust = %d, want 1", got.Dust)
	}
}

func TestSettle_DustNeverExceedsWinnerCount(t *testing.T) {
	// Flooring can lose at most one token per winning stake, so dust is
	// strictly bounded by the number of winners. A larger remainder
	// means tokens are being lost somewhere other than rounding.
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 7},
		{UserID: "bob", Outcome: 0, Amount: 11},
		{UserID: "carol", Outcome: 0, Amount: 13},
		{UserID: "dave", Outcome: 1, Amount: 101},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	if got.Dust >= 3 {
		t.Errorf("Settle dust = %d, want less than the winner count 3", got.Dust)
	}

	var paid Tokens
	for _, p := range got.Payouts {
		paid += p.Amount
	}
	if paid+got.Dust != 132 {
		t.Errorf("payouts %d + dust %d = %d, want the 132-token total", paid, got.Dust, paid+got.Dust)
	}
}

func TestSettle_LosersReceiveNothing(t *testing.T) {
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 500},
		{UserID: "bob", Outcome: 1, Amount: 200},
		{UserID: "carol", Outcome: 2, Amount: 300},
		{UserID: "dave", Outcome: 3, Amount: 400},
	}

	got, err := Settle(stakes, 0, 4)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	if len(got.Payouts) != 1 {
		t.Fatalf("Settle produced %d payouts, want 1 — losers must produce no credit", len(got.Payouts))
	}
	if got.Payouts[0].UserID != "alice" {
		t.Errorf("Settle credited %q, want alice", got.Payouts[0].UserID)
	}
}

func TestSettle_PlayerBackingBothSidesIsPaidOnlyOnTheWinner(t *testing.T) {
	// A player may hold stakes on several outcomes; only the winning one
	// pays. Total 1000, winning pool 400, so a 2.5x multiplier.
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 400},
		{UserID: "alice", Outcome: 1, Amount: 100},
		{UserID: "bob", Outcome: 1, Amount: 500},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	want := []Payout{{UserID: "alice", Amount: 1000}}
	if !reflect.DeepEqual(got.Payouts, want) {
		t.Errorf("Settle payouts = %+v, want %+v", got.Payouts, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/domain/ -run TestSettle -v`
Expected: FAIL — `undefined: Stake`, `undefined: Settle`.

- [ ] **Step 3: Write minimal implementation**

Create `backend/internal/domain/payout.go`:

```go
package domain

// Stake is one participant's committed tokens on one outcome of a round.
// It mirrors a single field of the round:{roundID}:wagers hash, whose key
// is "{userID}:{outcomeIdx}" (plan §4) — so (UserID, Outcome) is unique
// within a round and a player's repeat wagers on one outcome arrive
// already summed. A player may hold stakes on several different outcomes.
type Stake struct {
	UserID  string
	Outcome int
	Amount  Tokens
}

// Payout is one credit produced by settling a round: exactly one balance
// movement to apply.
type Payout struct {
	UserID string
	Amount Tokens
}

// Settlement is the complete result of settling a round.
type Settlement struct {
	// Payouts is one credit per settled stake, in the order the stakes
	// were supplied. Settling the same round twice yields identical
	// output.
	Payouts []Payout

	// Dust is the remainder that flooring each payout leaves behind. It
	// is credited to the system_dust ledger account so debits and
	// credits still balance exactly (plan §5) — it must never be
	// silently dropped.
	Dust Tokens

	// Refunded is true when nobody backed the winning outcome. Every
	// stake goes back in full and the round ends RoundRefunded rather
	// than RoundResolved.
	Refunded bool
}

// Settle computes the pari-mutuel result of a round. Each backer of the
// winning outcome receives floor(stake * total / winningPool); whatever
// flooring leaves over becomes Dust.
//
// outcomeCount is the round's declared number of outcomes, used to
// reject a winning index the round never had.
func Settle(stakes []Stake, winningOutcome, outcomeCount int) (Settlement, error) {
	var total, winningPool Tokens
	for _, s := range stakes {
		total += s.Amount
		if s.Outcome == winningOutcome {
			winningPool += s.Amount
		}
	}

	payouts := make([]Payout, 0, len(stakes))
	var paid Tokens
	for _, s := range stakes {
		if s.Outcome != winningOutcome {
			continue
		}
		// Safe in int64 by a wide margin: the largest single stake is
		// StakeCapMultiple * MaxBuyIn = 30,000, so stake * total stays
		// many orders of magnitude below overflow.
		amount := s.Amount * total / winningPool
		payouts = append(payouts, Payout{UserID: s.UserID, Amount: amount})
		paid += amount
	}

	return Settlement{Payouts: payouts, Dust: total - paid}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/domain/ -run TestSettle -v`
Expected: PASS — all seven test functions.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/payout.go backend/internal/domain/payout_test.go
git commit -m "feat: add pari-mutuel payout formula with dust remainder"
```


### Checkpoint 2: Per-player net results for the post-round reveal

This is what the anonymity invariant needs: the reveal shows what each player
staked and what they *earned*, which is net, not the gross credit the ledger
moves.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/domain/payout_test.go`:

```go
func TestSettle_PlayerResults(t *testing.T) {
	// Total 1000, winning pool 400, so a 2.5x multiplier.
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 100},
		{UserID: "bob", Outcome: 0, Amount: 300},
		{UserID: "carol", Outcome: 1, Amount: 600},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	want := []PlayerResult{
		{UserID: "alice", Staked: 100, Returned: 250, Net: 150},
		{UserID: "bob", Staked: 300, Returned: 750, Net: 450},
		{UserID: "carol", Staked: 600, Returned: 0, Net: -600},
	}
	if !reflect.DeepEqual(got.Results, want) {
		t.Errorf("Settle results = %+v, want %+v", got.Results, want)
	}
}

func TestSettle_PlayerResultsAggregateAcrossOutcomes(t *testing.T) {
	// alice hedges across both outcomes and must appear once, with her
	// stakes summed. Total 1000, winning pool 400: a 2.5x multiplier.
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 400},
		{UserID: "alice", Outcome: 1, Amount: 100},
		{UserID: "bob", Outcome: 1, Amount: 500},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	want := []PlayerResult{
		{UserID: "alice", Staked: 500, Returned: 1000, Net: 500},
		{UserID: "bob", Staked: 500, Returned: 0, Net: -500},
	}
	if !reflect.DeepEqual(got.Results, want) {
		t.Errorf("Settle results = %+v, want %+v", got.Results, want)
	}
}

func TestSettle_PlayerResultsNetSumsToNegativeDust(t *testing.T) {
	// Every token a winner gains is a token a loser lost, except what
	// flooring strands as dust. So the players' nets must sum to exactly
	// minus the dust.
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 1},
		{UserID: "bob", Outcome: 0, Amount: 2},
		{UserID: "carol", Outcome: 1, Amount: 7},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	var netSum Tokens
	for _, r := range got.Results {
		netSum += r.Net
	}
	if netSum != -got.Dust {
		t.Errorf("player nets sum to %d, want %d (minus the dust)", netSum, -got.Dust)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run TestSettle_PlayerResults -v`
Expected: FAIL — `undefined: PlayerResult`, and `got.Results` undefined.

- [ ] **Step 3: Write minimal implementation**

Add the `PlayerResult` type to `payout.go`, below `Payout`:

```go
// PlayerResult is one row of the post-round reveal: what a player
// committed, what came back, and what they actually earned. Net is the
// number a player cares about — stake 100 on the winner at 2.5x and you
// earned 150, not the 250 the ledger moves. Losers appear here with a
// negative Net even though they produce no Payout at all.
//
// Wagers are private until the round reaches a terminal state (see this
// phase's plan), so this slice is the first moment anyone learns who
// backed what.
type PlayerResult struct {
	UserID   string
	Staked   Tokens
	Returned Tokens
	Net      Tokens
}
```

Add the `Results` field to `Settlement`, between `Payouts` and `Dust`:

```go
	// Results is the one-row-per-player summary revealed once the round
	// closes. Players appear in the order they first staked.
	Results []PlayerResult
```

Add the aggregation helper at the bottom of `payout.go`:

```go
// playerResults folds per-stake credits into the one-row-per-player
// summary the reveal shows. Players appear in the order they first
// staked rather than in map order, so the same round always produces the
// same slice.
func playerResults(stakes []Stake, payouts []Payout) []PlayerResult {
	index := make(map[string]int, len(stakes))
	results := make([]PlayerResult, 0, len(stakes))

	for _, s := range stakes {
		i, seen := index[s.UserID]
		if !seen {
			i = len(results)
			index[s.UserID] = i
			results = append(results, PlayerResult{UserID: s.UserID})
		}
		results[i].Staked += s.Amount
	}

	// Every payout derives from a stake, so its user is already indexed.
	for _, p := range payouts {
		results[index[p.UserID]].Returned += p.Amount
	}

	for i := range results {
		results[i].Net = results[i].Returned - results[i].Staked
	}

	return results
}
```

Populate it in `Settle`'s return statement:

```go
	return Settlement{
		Payouts: payouts,
		Results: playerResults(stakes, payouts),
		Dust:    total - paid,
	}, nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/payout.go backend/internal/domain/payout_test.go
git commit -m "feat: compute per-player net results for post-round reveal"
```

### Checkpoint 3: Invalid input is rejected

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/domain/payout_test.go`, and add `"errors"` to its
import block, which becomes:

```go
import (
	"errors"
	"reflect"
	"testing"
)
```

```go
func TestSettle_RejectsInvalidWinningOutcome(t *testing.T) {
	tests := []struct {
		name           string
		winningOutcome int
		outcomeCount   int
	}{
		{name: "winning index equal to the outcome count", winningOutcome: 2, outcomeCount: 2},
		{name: "winning index beyond the outcome count", winningOutcome: 7, outcomeCount: 3},
		{name: "negative winning index", winningOutcome: -1, outcomeCount: 2},
	}

	stakes := []Stake{{UserID: "alice", Outcome: 0, Amount: 100}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Settle(stakes, tt.winningOutcome, tt.outcomeCount)

			if !errors.Is(err, ErrInvalidOutcome) {
				t.Fatalf("Settle(_, %d, %d) = %v, want ErrInvalidOutcome",
					tt.winningOutcome, tt.outcomeCount, err)
			}
		})
	}
}

func TestSettle_RejectsStakeOnOutcomeTheRoundLacks(t *testing.T) {
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 100},
		{UserID: "bob", Outcome: 5, Amount: 100},
	}

	_, err := Settle(stakes, 0, 2)

	if !errors.Is(err, ErrInvalidOutcome) {
		t.Fatalf("Settle = %v, want ErrInvalidOutcome", err)
	}
}

func TestSettle_RejectsNonPositiveStakes(t *testing.T) {
	tests := []struct {
		name   string
		amount Tokens
	}{
		{name: "a zero stake", amount: 0},
		{name: "a negative stake would mint tokens", amount: -100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stakes := []Stake{
				{UserID: "alice", Outcome: 0, Amount: 100},
				{UserID: "bob", Outcome: 1, Amount: tt.amount},
			}

			_, err := Settle(stakes, 0, 2)

			if !errors.Is(err, ErrInvalidStake) {
				t.Fatalf("Settle with a %d stake = %v, want ErrInvalidStake", tt.amount, err)
			}
		})
	}
}

func TestSettle_EmptyRound(t *testing.T) {
	// A round nobody wagered in has an empty winning pool, so it takes
	// the refund path — with nothing to refund.
	got, err := Settle(nil, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	if len(got.Payouts) != 0 {
		t.Errorf("Settle produced %d payouts for an empty round, want 0", len(got.Payouts))
	}
	if len(got.Results) != 0 {
		t.Errorf("Settle produced %d results for an empty round, want 0", len(got.Results))
	}
	if got.Dust != 0 {
		t.Errorf("Settle dust = %d, want 0", got.Dust)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run 'TestSettle_Rejects|TestSettle_EmptyRound' -v`
Expected: FAIL — no validation exists yet, so the rejection tests get a nil
error, and `TestSettle_EmptyRound` panics on a division by zero winning pool.

- [ ] **Step 3: Write minimal implementation**

Replace `Settle`'s body in `payout.go` with the validating version. The
zero-winning-pool guard is a placeholder here — Task 6 gives it the real refund
behaviour:

```go
func Settle(stakes []Stake, winningOutcome, outcomeCount int) (Settlement, error) {
	if err := ValidateOutcomeIndex(winningOutcome, outcomeCount); err != nil {
		return Settlement{}, err
	}

	var total, winningPool Tokens
	for _, s := range stakes {
		if s.Amount <= 0 {
			return Settlement{}, fmt.Errorf("%w: user %q staked %d", ErrInvalidStake, s.UserID, s.Amount)
		}
		if err := ValidateOutcomeIndex(s.Outcome, outcomeCount); err != nil {
			return Settlement{}, err
		}
		total += s.Amount
		if s.Outcome == winningOutcome {
			winningPool += s.Amount
		}
	}

	payouts := make([]Payout, 0, len(stakes))
	var paid Tokens
	if winningPool > 0 {
		for _, s := range stakes {
			if s.Outcome != winningOutcome {
				continue
			}
			// Safe in int64 by a wide margin: the largest single stake is
			// StakeCapMultiple * MaxBuyIn = 30,000, so stake * total stays
			// many orders of magnitude below overflow.
			amount := s.Amount * total / winningPool
			payouts = append(payouts, Payout{UserID: s.UserID, Amount: amount})
			paid += amount
		}
	}

	return Settlement{
		Payouts: payouts,
		Results: playerResults(stakes, payouts),
		Dust:    total - paid,
	}, nil
}
```

Add the import to `payout.go`:

```go
import "fmt"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/payout.go backend/internal/domain/payout_test.go
git commit -m "feat: validate settlement inputs and guard the empty winning pool"
```

---

## Task 6: The empty-pool refund path

Parent plan §5 decided this edge case explicitly: nobody picked the winning
outcome, so there is nothing to redistribute and every participant is refunded
in full. Task 5 Checkpoint 3 left a placeholder guard that pays nobody and
reports the whole total as dust; that is wrong, and this task fixes it.

**Files:**
- Modify: `backend/internal/domain/payout.go`
- Modify: `backend/internal/domain/payout_test.go`

**Interfaces:**
- Consumes: everything from Task 5.
- Produces: no new signatures — `Settlement.Refunded` becomes meaningful.

### Checkpoint 1: An empty winning pool refunds every stake in full

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/domain/payout_test.go`:

```go
func TestSettle_EmptyWinningPoolRefundsEveryone(t *testing.T) {
	// Outcome 2 wins; nobody backed it.
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 100},
		{UserID: "bob", Outcome: 1, Amount: 300},
		{UserID: "carol", Outcome: 1, Amount: 600},
	}

	got, err := Settle(stakes, 2, 3)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	if !got.Refunded {
		t.Error("Settle marked the round resolved, want refunded")
	}
	want := []Payout{
		{UserID: "alice", Amount: 100},
		{UserID: "bob", Amount: 300},
		{UserID: "carol", Amount: 600},
	}
	if !reflect.DeepEqual(got.Payouts, want) {
		t.Errorf("Settle payouts = %+v, want every stake returned in full: %+v", got.Payouts, want)
	}
	if got.Dust != 0 {
		t.Errorf("Settle dust = %d, want 0 — refunds are exact and strand nothing", got.Dust)
	}
}

func TestSettle_RefundedPlayersBreakEven(t *testing.T) {
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 100},
		{UserID: "bob", Outcome: 1, Amount: 300},
	}

	got, err := Settle(stakes, 2, 3)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	want := []PlayerResult{
		{UserID: "alice", Staked: 100, Returned: 100, Net: 0},
		{UserID: "bob", Staked: 300, Returned: 300, Net: 0},
	}
	if !reflect.DeepEqual(got.Results, want) {
		t.Errorf("Settle results = %+v, want everyone at zero net: %+v", got.Results, want)
	}
}

func TestSettle_ResolvedRoundIsNotMarkedRefunded(t *testing.T) {
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 100},
		{UserID: "bob", Outcome: 1, Amount: 300},
	}

	got, err := Settle(stakes, 0, 2)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	if got.Refunded {
		t.Error("Settle marked a round with a backed winner as refunded")
	}
}

func TestSettle_RefundCoversEveryStakeOfAHedgingPlayer(t *testing.T) {
	// alice backed two of the three losing outcomes; outcome 2 wins and
	// nobody backed it. Both her stakes must come back. This guards
	// against "optimising" the refund loop into one entry per player.
	stakes := []Stake{
		{UserID: "alice", Outcome: 0, Amount: 100},
		{UserID: "alice", Outcome: 1, Amount: 250},
		{UserID: "bob", Outcome: 1, Amount: 400},
	}

	got, err := Settle(stakes, 2, 3)

	if err != nil {
		t.Fatalf("Settle: unexpected error: %v", err)
	}
	wantPayouts := []Payout{
		{UserID: "alice", Amount: 100},
		{UserID: "alice", Amount: 250},
		{UserID: "bob", Amount: 400},
	}
	if !reflect.DeepEqual(got.Payouts, wantPayouts) {
		t.Errorf("Settle payouts = %+v, want %+v", got.Payouts, wantPayouts)
	}
	wantResults := []PlayerResult{
		{UserID: "alice", Staked: 350, Returned: 350, Net: 0},
		{UserID: "bob", Staked: 400, Returned: 400, Net: 0},
	}
	if !reflect.DeepEqual(got.Results, wantResults) {
		t.Errorf("Settle results = %+v, want %+v", got.Results, wantResults)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run 'RefundsEveryone|BreakEven' -v`
Expected: FAIL — the placeholder guard produces zero payouts and reports the
whole 1000 as dust, and `Refunded` is never set.

- [ ] **Step 3: Write minimal implementation**

Replace the payout-building block in `Settle` (everything between the
validation loop and the `return`) with:

```go
	payouts := make([]Payout, 0, len(stakes))
	var paid Tokens
	refunded := winningPool == 0

	if refunded {
		// Nobody backed the winner, so there is nothing to redistribute.
		// Every stake goes back to whoever placed it, exactly — which is
		// why a refunded round strands no dust (plan §5).
		for _, s := range stakes {
			payouts = append(payouts, Payout{UserID: s.UserID, Amount: s.Amount})
			paid += s.Amount
		}
	} else {
		for _, s := range stakes {
			if s.Outcome != winningOutcome {
				continue
			}
			// Safe in int64 by a wide margin: the largest single stake is
			// StakeCapMultiple * MaxBuyIn = 30,000, so stake * total stays
			// many orders of magnitude below overflow.
			amount := s.Amount * total / winningPool
			payouts = append(payouts, Payout{UserID: s.UserID, Amount: amount})
			paid += amount
		}
	}

	return Settlement{
		Payouts:  payouts,
		Results:  playerResults(stakes, payouts),
		Dust:     total - paid,
		Refunded: refunded,
	}, nil
```

Also update the `Settle` doc comment to describe the refund branch:

```go
// Settle computes the pari-mutuel result of a round. Each backer of the
// winning outcome receives floor(stake * total / winningPool); whatever
// flooring leaves over becomes Dust. If nobody backed the winning
// outcome there is nothing to redistribute, so every stake is refunded
// in full and Refunded is set (plan §5).
//
// outcomeCount is the round's declared number of outcomes, used to
// reject a winning index the round never had.
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v`
Expected: PASS — including `TestSettle_EmptyRound` from Task 5, which now takes
the refund branch with nothing to refund.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/payout.go backend/internal/domain/payout_test.go
git commit -m "feat: refund every stake when nobody backed the winning outcome"
```

## Task 7: Pari-mutuel odds

The only place in the package where a float appears. Odds are a presentation
concern — nothing computed here is ever stored.

**Files:**
- Create: `backend/internal/domain/odds.go`
- Test: `backend/internal/domain/odds_test.go`

**Interfaces:**
- Consumes: `Tokens` from Task 2.
- Produces:
  - `func Multiplier(total, pool Tokens) float64`
  - `func Multipliers(total Tokens, pools []Tokens) []float64`

`Multipliers` takes the shape the WebSocket hub broadcasts after every accepted
wager (spec §5 step 3). It is aggregate by construction — pool totals only, no
per-user positions — which is what keeps the live-odds broadcast compatible
with the anonymity invariant.

### Checkpoint 1: The multiplier for one outcome

- [ ] **Step 1: Write the failing test**

Create `backend/internal/domain/odds_test.go`. As with `payout_test.go`, the
import block carries only what this checkpoint uses — `reflect` arrives in
Checkpoint 2:

```go
package domain

import "testing"

func TestMultiplier(t *testing.T) {
	tests := []struct {
		name  string
		total Tokens
		pool  Tokens
		want  float64
	}{
		{name: "a quarter of the total pays four to one", total: 1000, pool: 250, want: 4},
		{name: "an even split pays two to one", total: 1000, pool: 500, want: 2},
		{name: "the only backed outcome pays even money", total: 1000, pool: 1000, want: 1},
		{name: "a tenth of the total pays ten to one", total: 1000, pool: 100, want: 10},
		{name: "an unbacked outcome has no defined multiplier", total: 1000, pool: 0, want: 0},
		{name: "an empty round has no defined multiplier", total: 0, pool: 0, want: 0},
		{name: "a negative pool cannot happen and yields no multiplier", total: 1000, pool: -5, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Multiplier(tt.total, tt.pool); got != tt.want {
				t.Errorf("Multiplier(%d, %d) = %v, want %v", tt.total, tt.pool, got, tt.want)
			}
		})
	}
}

func TestMultiplier_FractionalResult(t *testing.T) {
	// Odds are the one place a float is correct — the payout itself is
	// still floored to whole tokens by Settle.
	got := Multiplier(1000, 300)

	const want = 10.0 / 3.0
	if got != want {
		t.Errorf("Multiplier(1000, 300) = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run TestMultiplier -v`
Expected: FAIL — `undefined: Multiplier`.

- [ ] **Step 3: Write minimal implementation**

Create `backend/internal/domain/odds.go`:

```go
package domain

// Multiplier is the pari-mutuel payout multiplier for one outcome: the
// total pool divided by that outcome's pool (spec §4). This is the one
// place in the domain where a float is correct — everything stored stays
// a whole token count, and Settle floors the actual payout.
//
// An outcome nobody has backed has no defined multiplier and returns 0.
// The sentinel is unambiguous because a real multiplier is never below
// 1: an outcome's pool is always part of the total.
func Multiplier(total, pool Tokens) float64 {
	if pool <= 0 {
		return 0
	}
	return float64(total) / float64(pool)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/odds.go backend/internal/domain/odds_test.go
git commit -m "feat: compute pari-mutuel multiplier for a single outcome"
```

### Checkpoint 2: The full odds board

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/domain/odds_test.go`, and widen its import block to:

```go
import (
	"reflect"
	"testing"
)
```

```go
func TestMultipliers(t *testing.T) {
	tests := []struct {
		name  string
		total Tokens
		pools []Tokens
		want  []float64
	}{
		{
			name:  "a two-outcome board",
			total: 1000,
			pools: []Tokens{250, 750},
			want:  []float64{4, 1000.0 / 750.0},
		},
		{
			name:  "an unbacked outcome sits at zero among backed ones",
			total: 1000,
			pools: []Tokens{500, 500, 0},
			want:  []float64{2, 2, 0},
		},
		{
			name:  "a round with no wagers yet",
			total: 0,
			pools: []Tokens{0, 0},
			want:  []float64{0, 0},
		},
		{
			name:  "no outcomes yields an empty board, not nil",
			total: 0,
			pools: []Tokens{},
			want:  []float64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Multipliers(tt.total, tt.pools)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Multipliers(%d, %v) = %v, want %v", tt.total, tt.pools, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run TestMultipliers -v`
Expected: FAIL — `undefined: Multipliers`.

- [ ] **Step 3: Write minimal implementation**

Append to `odds.go`:

```go
// Multipliers computes the multiplier for every outcome in index order,
// which is the shape broadcast to every client in the room whenever the
// odds move (spec §5 step 3). It reads pool totals only and never sees a
// per-user position, which is what keeps the live-odds broadcast
// compatible with wager anonymity.
func Multipliers(total Tokens, pools []Tokens) []float64 {
	board := make([]float64, len(pools))
	for i, pool := range pools {
		board[i] = Multiplier(total, pool)
	}
	return board
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v -cover`
Expected: PASS, coverage 100.0%.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/odds.go backend/internal/domain/odds_test.go
git commit -m "feat: compute the full odds board for broadcast"
```

---

## Task 8: Conservation property test and phase verification

The example-based tests prove `Settle` right on the cases someone thought of.
This task proves the invariant that matters — tokens are neither created nor
destroyed — across inputs nobody thought of.

**Files:**
- Create: `backend/internal/domain/payout_fuzz_test.go`
- Modify: `docs/plans/2026-08-21-implementation-plan.md` (fold in A1–A3)
- Modify: `docs/specs/2026-08-21-callit-design.md` (record the anonymity invariant)
- Modify: `CLAUDE.md` (record the anonymity invariant)

**Interfaces:**
- Consumes: everything from Tasks 1–7.
- Produces: no new production signatures.

### Checkpoint 1: Token conservation under arbitrary input

**Note on the TDD cycle here.** This checkpoint deviates from RED-GREEN
deliberately, and that is not an oversight. A property test over
already-implemented code should pass on first run; if it fails, the fuzzer has
found a real bug in Task 5 or 6 and the phase does not proceed until it is
fixed. The "expect FAIL" step is replaced by "expect PASS, and treat a failure
as a blocking defect."

- [ ] **Step 1: Write the property test**

Create `backend/internal/domain/payout_fuzz_test.go`:

```go
package domain

import (
	"fmt"
	"testing"
)

// FuzzSettleConservesTokens asserts the invariant the whole ledger rests
// on: settling a round neither creates nor destroys tokens. Whatever is
// not paid out must be accounted for as dust, and every player's net
// must add up to exactly minus that dust.
//
// The corpus byte string is decoded as: one byte selecting the winning
// outcome, then pairs of bytes, each a (outcome, amount) stake.
func FuzzSettleConservesTokens(f *testing.F) {
	f.Add([]byte{0})                            // a round with no wagers
	f.Add([]byte{0, 0, 100, 1, 200})            // one winner, one loser
	f.Add([]byte{2, 0, 100, 1, 200})            // nobody backs the winner
	f.Add([]byte{0, 0, 1, 0, 2, 1, 7})          // flooring strands dust
	f.Add([]byte{1, 0, 255, 1, 255, 2, 255, 3, 255})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			t.Skip()
		}
		const outcomeCount = 4
		winningOutcome := int(data[0]) % outcomeCount

		var stakes []Stake
		var total Tokens
		for i := 1; i+1 < len(data); i += 2 {
			amount := Tokens(data[i+1])
			if amount == 0 {
				// Settle rejects non-positive stakes by contract, which
				// Task 5 covers directly. Skip them so this test stays
				// focused on conservation.
				continue
			}
			stakes = append(stakes, Stake{
				UserID:  fmt.Sprintf("u%d", i),
				Outcome: int(data[i]) % outcomeCount,
				Amount:  amount,
			})
			total += amount
		}

		got, err := Settle(stakes, winningOutcome, outcomeCount)
		if err != nil {
			t.Fatalf("Settle returned an error for structurally valid input: %v", err)
		}

		var distributed Tokens
		for _, p := range got.Payouts {
			if p.Amount < 0 {
				t.Fatalf("Settle produced a negative payout %d for %s", p.Amount, p.UserID)
			}
			distributed += p.Amount
		}

		if distributed+got.Dust != total {
			t.Fatalf("tokens not conserved: payouts %d + dust %d = %d, want the %d staked",
				distributed, got.Dust, distributed+got.Dust, total)
		}
		if got.Dust < 0 {
			t.Fatalf("Settle produced negative dust %d, which would mint tokens", got.Dust)
		}
		if got.Refunded && got.Dust != 0 {
			t.Fatalf("a refunded round stranded %d dust, want 0 — refunds are exact", got.Dust)
		}

		var netSum, stakedSum Tokens
		for _, r := range got.Results {
			netSum += r.Net
			stakedSum += r.Staked
		}
		if stakedSum != total {
			t.Fatalf("player results account for %d staked, want %d", stakedSum, total)
		}
		if netSum != -got.Dust {
			t.Fatalf("player nets sum to %d, want %d (minus the dust)", netSum, -got.Dust)
		}
	})
}
```

- [ ] **Step 2: Run the seed corpus**

Run: `cd backend && go test ./internal/domain/ -run FuzzSettleConservesTokens -v`
Expected: PASS. A failure here is a real defect in `Settle` — fix it before
continuing rather than adjusting the test.

- [ ] **Step 3: Run the fuzzer**

Run: `cd backend && go test ./internal/domain/ -run FuzzSettleConservesTokens -fuzz FuzzSettleConservesTokens -fuzztime 60s`
Expected: `elapsed: 60.0s, execs: ... (n/sec), new interesting: ...` and no
failures. If the fuzzer writes a crasher into `testdata/fuzz/`, commit it — it
becomes a permanent regression case — and fix the defect it found before
continuing.

- [ ] **Step 4: Verify the whole package**

Run: `cd backend && go test ./internal/domain/ -race -cover -v`
Expected: PASS, coverage 100.0% of statements.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/payout_fuzz_test.go
git commit -m "test: fuzz token conservation across arbitrary settlements"
```

### Checkpoint 2: Fold the amendments back into the committed docs

The amendments in this plan's header are decisions, not notes. Leaving them
only here means the parent plan and spec quietly disagree with the code.

- [ ] **Step 1: Amend the parent plan**

In `docs/plans/2026-08-21-implementation-plan.md` §8, replace the economy table
with the settled values and add a note beneath it:

```markdown
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
```

- [ ] **Step 2: Record the anonymity invariant in the spec**

In `docs/specs/2026-08-21-callit-design.md` §4, append after the host-disconnect
bullet:

```markdown
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
```

- [ ] **Step 3: Record the invariant in CLAUDE.md**

In `CLAUDE.md` under "Critical Invariants", add:

```markdown
- **Wagers stay anonymous until the round is terminal.** No payload from
  any phase may carry per-user wager data before the round resolves or
  refunds — `internal/domain`'s `Settlement.Results` is the reveal, and
  nothing earlier. Live odds broadcast pool totals only. The host
  resolves outcomes, so early visibility would hand them the conflict of
  interest that the host-cannot-wager rule exists to remove. The only
  permitted in-round progress signal is an aggregate count of players
  who have wagered ("2/5 players have placed their bets") — denominator
  excludes the host, and it counts players, not wagers. Binds Phase 3
  (REST payloads), Phase 4 (WebSocket broadcasts), and Phase 6 (the
  frontend must not reconstruct per-user state client-side).
```

Also correct the economy figures if CLAUDE.md restates them, and update the
Testing section's coverage line to include `internal/domain`.

- [ ] **Step 4: Verify nothing broke**

Run from the repo root:
```bash
make lint && make build && make test
```
Expected: `go vet` clean, `gofmt -l` prints nothing, build succeeds, all
packages pass. Confirm `internal/domain` reports 100.0% coverage and that
`backend/go.mod` still has no `require` block.

- [ ] **Step 5: Commit**

```bash
git add docs/plans/2026-08-21-implementation-plan.md docs/specs/2026-08-21-callit-design.md CLAUDE.md
git commit -m "docs: settle economy constants and record wager anonymity invariant"
```

### Checkpoint 3: Close out the phase

This plan does **not** merge. `executing-plans` Step 3 hands off to
`finishing-a-development-branch`, which owns integration and keeps that decision
with the user.

- [ ] **Step 1: Confirm the branch is green**

```bash
cd backend && go vet ./... && gofmt -l . && go test ./... -race -cover
```
Expected: no vet findings, no unformatted files listed, all tests pass.

- [ ] **Step 2: Record the checkpoint-granularity result**

CLAUDE.md flags the per-checkpoint commit convention as "new and unverified in
practice — Phase 1 is the first real test." Count the commits this branch
produced and note in the phase's journal entry whether the granularity helped or
produced noise:

```bash
git log --oneline dev ^178f6d6 | wc -l
```
Expected: 21 or 22 — this plan has 22 checkpoints, of which Task 8 Checkpoint 3
produces no commit of its own.

The specific thing to watch: whether any commit turned out to be a test-only
addition pinning behaviour an earlier checkpoint already implemented. Four such
checkpoints existed in this plan's first draft and were merged away before
execution under the rule now in `.claude/skills/writing-plans/SKILL.md`
("a checkpoint is one RED→GREEN cycle"). If more slipped through, that rule
needs tightening before Phase 2.

- [ ] **Step 3: Hand off to `finishing-a-development-branch`**

Per `executing-plans` Step 3. Base branch is `dev`. CLAUDE.md's "self-merge into
`dev` once the phase's tests pass, no PR" makes **Option 1 (merge locally)** the
expected choice — but the menu is the user's to answer, so present it rather
than assuming. Use `--no-ff`, so the phase stays a visible merge commit instead
of fast-forwarding away the boundary.

---

## Verification Summary

At the end of this phase all of the following must hold:

- [ ] `backend/internal/domain` contains exactly: `tokens.go`, `economy.go`, `errors.go`, `round.go`, `wallet.go`, `refill.go`, `payout.go`, `odds.go`, plus their tests
- [ ] The package imports only `errors` and `fmt`; `backend/go.mod` still has no `require` block
- [ ] `go test ./internal/domain/ -race -cover` reports 100.0% coverage
- [ ] `FuzzSettleConservesTokens` survives 60 seconds of fuzzing with no crashers
- [ ] `make lint`, `make build`, `make test` are all clean from the repo root
- [ ] Parent plan §8, spec §4, and CLAUDE.md reflect A1–A3 and the anonymity invariant
- [ ] `phase-1-domain-core` is green and handed to `finishing-a-development-branch` — that skill, not this plan, decides how it integrates
