# 2026-08-27 — ansh — Phase 5b plan written, delegation skill built and wired

**Status:** All three items outstanding from this morning's entry are **done**. `docs/plans/2026-08-27-phase-5b-ledger.md` (6 tasks, 28 checkpoints, 1,694 lines) committed as `c241496`; the `delegating-plan-tasks` skill plus its three wiring edits committed as `9cd740a`. No code written — this was a planning and tooling session. `dev` is 3 commits ahead of `origin/dev` at time of writing.
**Decided:** Phase 5b runs **Tasks 1–5 delegated, Task 6 inline** — the delegation experiment finally runs, but not on the reconciliation suite that is the evidence behind the 0.00% double-spend claim. Recorded in the 5b plan's header, not just here.
**Spec:** No change yet. The 5b plan pre-registers three amendments (F1 wire-format tags, F2 migration 0002 indexes, F3 the reconciliation identity) for Task 6 to write into the parent plan at close-out.
**Next:** Register the `tok/CP` prediction and guardrails in a journal entry, **then** open a fresh window and run `executing-plans` on the 5b plan. The prediction must be written before Task 1 is dispatched.
**Blocked on:** Nothing.
**Touches:** `docs/plans/2026-08-27-phase-5b-ledger.md`, `.claude/skills/delegating-plan-tasks/`, `.claude/skills/executing-plans/SKILL.md`, `CLAUDE.md`

---

## What We Worked On

Closing the gap this morning's entry recorded: the 5b plan and the delegation
skill both existed only as intentions. Both now exist. Confirmed at the top of
the session that **CI is green on the Actions tab** — that settles Phase 5a's
one open risk, the Kafka/PostgreSQL `services:` block never having run against
a real runner.

Order was plan → skill → wiring, as the previous entry prescribed. That
ordering earned its keep: the skill's central claim is that a plan's per-task
`Interfaces: Consumes / Produces` blocks *are* the delegation brief, and
writing it against 5b's real blocks is what let Rule 1 say "point the subagent
at the plan file, don't paraphrase the task" with confidence.

## Decisions Made

The 5b plan fixes seven design decisions up front (its "Design Decisions Fixed
Before Execution" section, D1–D7) so no task re-derives them. Three are
non-obvious enough to restate here, since each has a plausible-looking wrong
answer a future session might drift into:

- **D1 — sign convention is "credit = tokens in, debit = tokens out."**
  Deliberately *not* classical accounting, where a debit increases an asset.
  Under the classical rule every reader must first decide whether a user
  wallet is an asset or a liability to know which way a wager moves it. One
  rule removes the question, and the deferred trigger enforces the same
  equality either way.
- **D2 — the ledger records outbox movements only, so a `user_wallet` balance
  is a net session delta.** `Store.JoinRoom` is a Go pipeline
  (`redisstore/room.go:114`), not a Lua script, so the opening stake never
  reaches the outbox. The reconciliation identity is therefore
  `redis_wallet − opening_stake == ledger_balance`, **not** parent plan §6's
  literal "each user's Redis balance equals the balance derived by summing
  their ledger_entries." Rejected the alternative — emit a `system_mint` grant
  on join — because making it atomic means rewriting a Phase 4 write path into
  Lua to improve a 5b assertion. Logged as a Phase 7 candidate.
- **D4 — cross-topic ordering does not affect ledger correctness.**
  `wagers-placed` and `rounds-settled` are separate topics with no mutual
  ordering guarantee, so a settlement can be consumed before the wagers it
  settles. Safe because every transaction is internally balanced: the
  settlement drives `round_pool` transiently negative and the wagers bring it
  back to zero. Parent plan §7 asserts per-room ordering matters "concretely"
  because a settlement must never be processed before its wagers — that is
  true for the *pool's transient sign* and not for any final balance. Worth
  knowing before someone tries to build cross-topic ordering that isn't needed.

Two structural calls on the plan itself:

- **Task 6 is declared a verification task, not RED→GREEN.** Everything its
  tests exercise is built in Tasks 1–5, so they will PASS on first run. Rather
  than manufacture a fake RED, the plan says so in a blockquote and tells the
  executor not to treat the PASS as an `executing-plans` stop-on-mismatch
  event. Tasks 3 and 4 similarly name in advance the checkpoints likely to
  pass on write, with instructions to fold and record — the Phase 5a collapse
  pattern, pre-empted instead of rediscovered.
- **`writing-plans`' header template was left alone.** Only the 5b plan's own
  header carries the delegation block. Making every future plan carry one is a
  decision that should follow the experiment's data, not precede it.

## What Worked

- **Reading the real code before writing a line of plan.** Every signature,
  line reference, and Lua field name in the plan came from the tree. This
  surfaced three things a from-memory plan would have missed: the Kafka wire
  format currently has **no JSON tags** (so it is Go field *spelling* — D7,
  now Task 1); the balance trigger does a per-row
  `WHERE transaction_id = ...` lookup, making an index on that column a correctness-at-scale requirement
  rather than a nicety (Task 3 CP1); and `accounts` has no unique constraint
  on its natural keys at all, so account provisioning had no idempotent path.
- **5b needs zero new dependencies.** `pgxpool` ships inside the pinned
  `jackc/pgx/v5`, `kafka.Reader` inside the pinned `segmentio/kafka-go`. Given
  how Phase 5a went, that is a real de-risk — the plan says to stop and re-read
  `CLAUDE.md` if any `go get` seems necessary.
- **Three dispatch guardrails the original four-rule design didn't have.**
  Writing the skill against the actual `Agent` tool surfaced them: never
  `subagent_type: "fork"` (a fork inherits full parent context and the parent's
  model — the ceremony of delegation with none of the saving), never
  `isolation: "worktree"` (commits must land on the phase branch), and
  **dispatch strictly sequentially**. The last one resolves a genuine conflict:
  `.claude/rules/ecc/common/agents.md` says "ALWAYS use parallel Task execution
  for independent operations" and is always loaded, but plan tasks are not
  independent — Task N+1 consumes Task N's `Produces` and commits to the same
  branch. Unstated, the always-loaded rule wins.

## What Didn't Work

- **Writing a 1,694-line plan through a single `bash` heredoc.** It produced
  two defects needing a second pass: nested backticks inside a markdown inline
  span (a Go `[]byte(`...`)` literal quoted inline, which breaks rendering),
  and a stray `git commit --allow-empty` line followed by a "Note: drop the
  trailing clause" correction that should never have been written rather than
  patched. Both fixed. For a document this size the lesson is to verify
  structure after writing — a `grep -c '^```'` fence-balance check plus a
  checkpoint/step count caught both quickly (88 fences even, 28 checkpoints ×
  2 = 56 steps). Don't assume a long heredoc landed clean.
- **Nothing else was attempted and abandoned.** No approach was tried and
  discarded this session.

## Test Coverage

- **Covered:** Nothing new — **zero code was written this session.** All four
  changed files are documentation or skill definitions. The suite was last
  verified green on merged `dev` this morning and has not been touched since.
- **Not covered yet:** The `delegating-plan-tasks` skill is **entirely
  unexercised**. It has never dispatched a subagent. Every number in it — the
  2–4× range, the 19k cold start's implications for granularity, the claim
  that a bounded return contract prevents re-accumulation — is inherited from
  Phase 4b's natural experiment and prior analysis, not from running this
  skill. Its first run is the measurement, and it may well be wrong.

## Open Questions / Blockers

- **The `tok/CP` prediction is not yet registered.** It must be, before Task 1
  is dispatched — that is the whole lesson of `writing-plans-tuned`, which was
  evaluated after the fact and produced an argument rather than a result. The
  bar: **< 5.77M/CP** (Phase 5a inline, current codebase size), with
  `un-batched = 0` and `commits/CP ≤ 1.10` as guardrails proving discipline
  survived. Phase 2's 4.61M is the wrong comparator — half the codebase.
- **Turn inflation is the main way the estimate could be wrong**, and there is
  still no data on it. A cold subagent may need materially more turns to
  rediscover cross-task context; above roughly 40% inflation the 2–4× range
  compresses toward break-even.
- **Delegating 5 of 6 tasks means the phase's measured number is a blend**, not
  a clean delegated figure. Task 6 is inline by design and is the largest
  single task in the plan. Worth accounting for when reading the result — the
  honest comparison may need Task 6's turns excluded.
- **Task 6's declared-PASS framing is itself untested against a cold
  executor.** The plan states the exception explicitly so `executing-plans`'
  stop-on-mismatch rule doesn't fire, but whether that survives contact with an
  executor that has been told 23 times to expect FAIL is unknown.

## Relevant Commits

- `c241496` — the Phase 5b plan: 6 tasks, 28 checkpoints, 7 pre-fixed design decisions
- `9cd740a` — the `delegating-plan-tasks` skill plus three wiring edits

## Next Step

In order, and the first must precede the second:

1. Register the prediction and guardrails above in the journal.
2. Open a **fresh window** and run `executing-plans` on
   `docs/plans/2026-08-27-phase-5b-ledger.md`. Its Step 1 creates
   `phase-5b-ledger` off `dev`; Tasks 1–5 dispatch per the plan header, Task 6
   runs inline.

Both wiring edits that could have resolved *against* delegation are now
closed: `executing-plans`' Note block used to say inline execution was the
project's default with no alternative installed, and `CLAUDE.md`'s Installed
list didn't mention the skill. A cold executor now finds a consistent story in
all three places.
