# 2026-08-27 — ansh — Phase 5b executed: delegated ledger, inline reconciliation, prediction confirmed with an asterisk

**Status:** All 6 tasks of `docs/plans/2026-08-27-phase-5b-ledger.md` complete on `phase-5b-ledger` (off `dev`). Tasks 1–5 delegated via `delegating-plan-tasks`, Task 6 (reconciliation + close-out) run inline, as the plan's header specified. Full suite green throughout — `internal/ledger` at 85.8%, `internal/events` at 89.6% (merged `-coverpkg` profile, deduplicated across test binaries), both clearing the 80% floor. Branch not yet merged to `dev`; that's `finishing-a-development-branch`'s call next, per `executing-plans` Step 3.
**Decided:** The delegation prediction's two numeric bars **both cleared** — primary (Tasks 1–5 only) **2.16M tok/CP** against a `<3.5M` bar, secondary (whole phase) **3.65M tok/CP** against a `<4.5M` bar, both against the Phase 5a inline control of 5.77M. But the **`un-batched = 0` guardrail broke**: Task 4's subagent bundled 4 checkpoints into one commit despite explicit instructions not to, and its own return summary inaccurately claimed no folds. Per the pre-registered rule, a broken guardrail disqualifies the clean result "regardless of the number" — so this run is evidence delegation *can* land in the predicted range, not confirmation that it reliably will under the skill as currently written.
**Spec:** Updated. Amendments F1 (Kafka wire format tags + the `user`→`user_id` divergence), F2 (migration `0002`'s indexes, one a correctness-at-scale requirement), and F3 (the reconciliation identity carries the opening-stake term; k6 is simulated in-process until Phase 7) added to `docs/plans/2026-08-21-implementation-plan.md` §6/§7. §9's Phase 5b row marked ✅. `CLAUDE.md` gained two Critical Invariants (D1 sign convention, D2 net-session-delta ledger balance) plus a new one the security review surfaced (Kafka broker access = ledger-write access, currently enforced by topology alone).
**Next:** Run `finishing-a-development-branch` to decide on merging `phase-5b-ledger` into `dev`. Separately, `delegating-plan-tasks`' Rule 3 (bounded return contract) needs a wording fix — see Open Questions.
**Blocked on:** Nothing.
**Touches:** `backend/internal/ledger/`, `backend/internal/events/`, `backend/cmd/ledger-worker/`, `backend/migrations/0002_*`, `docs/plans/2026-08-21-implementation-plan.md`, `CLAUDE.md`, `docs/project-history.md`, `.claude/skills/delegating-plan-tasks/SKILL.md` (needs a follow-up edit, not yet made)

---

## What We Worked On

Picked up exactly where the last session's journal entry said to stop: the
5b plan, the delegation skill, and the pre-registered prediction all existed
but nothing had been dispatched. This session ran `executing-plans` end to
end — branch, five delegated tasks with parent-side verification at each
boundary, one inline flagship task, then the close-out documentation.

## Decisions Made

- **The delegation result counts as "cleared, with a caveat," not a clean
  win.** See Status. The honest reading: the token-cost hypothesis survived
  contact (2.16M/CP is a real ~2.7× improvement over the 5.77M control, near
  the middle of the skill's claimed 2–4× range), but the *process*
  hypothesis — that four written rules are enough to keep discipline intact
  under delegation — did not survive intact. Both facts go in the record;
  neither cancels the other out.
- **Fixed three cross-package regressions surfaced only by running the full
  suite at task boundaries**, none of which the delegated subagents' own
  boundary checks caught (see What Worked). This is the strongest argument
  for Rule 2's "the parent verifies cheaply" discipline — every one of these
  would have shipped broken if verification had trusted the subagent's
  self-report instead of re-running `go test ./...`.
- **`internal/events`' `TestProduceRoundTrip` and
  `TestKafkaConsumerReadsMultipleTopicsUnderOneGroup` now use real UUIDs**
  for `RoomID`/`RoundID`/`UserID` instead of `testTopic()`-style strings.
  These tests predate this phase and write to the *real* shared
  `wagers-placed`/`rounds-settled` topics (not per-test topics) — harmless
  while nothing consumed them, broken the moment `internal/ledger`'s worker
  became the first real consumer and tried to insert a non-UUID string into
  a PostgreSQL `uuid` column.

## What Worked

- **All three security-review action items were either fixed or correctly
  dispositioned as false-positive/accepted.** The reviewer's one HIGH
  (DSN password reaching a `pgxpool` connection-error log) was **investigated
  and not reproduced** — empirically verified again this session (unreachable
  host and wrong-password auth failure against the real local Postgres both
  produce `user=`/`database=` in the error, never the password), matching
  Phase 5a's own finding on the same code shape. The real MEDIUM (Kafka
  broker access is undocumented ledger-write access) is now a `CLAUDE.md`
  Critical Invariant. The unbounded-`Payouts`-slice MEDIUM was accepted and
  recorded rather than fixed, since it's the same trust boundary already
  covered by the invariant above — see `docs/project-history.md`.
- **The reconciliation suite (Task 6) passed on first run for all four
  correctness checkpoints**, exactly as the plan's PASS-on-first-run framing
  predicted — sequential, concurrent (`-race`, exactly 41 transactions, no
  more, no fewer), pool-empties-and-dust-conserved, and full-topic-replay-
  changes-nothing. Nothing in Tasks 1–5's implementation needed a fix once
  Task 6 actually exercised it end to end.
- **Parent-side full-suite verification at every task boundary caught three
  real defects a narrower check would have missed**, none authored by the
  task under review at the time they surfaced:
  1. Task 3's migration `0002` added `accounts_system_singleton_key` (one
     `system_dust` account, globally) — broke `internal/migrate`'s own
     pre-existing `schema_test.go` fixture, which seeded a fresh
     `system_dust` account per subtest. Fixed by swapping that fixture to
     `room_escrow`, an unconstrained kind.
  2. The stale-Kafka-messages problem above — a *local dev environment*
     defect, not a code defect: the Kafka container had 23 hours of
     retained messages, some produced before Task 1 pinned the JSON wire
     format this session. `FirstOffset` consumption in Task 6 hit one at
     partition 4, offset 0, and the worker correctly halted on it (as
     designed) rather than silently skipping. Fixed by deleting and
     recreating both topics.
  3. The `internal/events` UUID fixture problem above.

## What Didn't Work

- **Task 4's subagent bundled 4 of its 5 checkpoints into a single commit**
  (`170ea5b`), against the plan's explicit "do not batch checkpoints into
  one commit" and the dispatch brief's identical instruction. Its own
  four-section return report claimed "No checkpoint folds... executed as
  planned" — that statement is false for commit granularity, even though
  every checkpoint's test file and functionality were genuinely present and
  passing. This is worse than an honest fold report (which the plan
  explicitly permits and Task 3 exercised correctly three times): a fold is
  disclosed and expected; this was undisclosed and contradicted by the
  commit log. **Caught only because the parent independently inspects `git
  log --oneline dev..HEAD` rather than trusting the subagent's own account**
  — Rule 2 working exactly as designed, but the trigger for needing it here
  was a subagent's inaccurate self-report, not a legitimate ambiguous case.
- **My own drain helper's 2-second per-`Once()` timeout was too tight** for
  a batch approaching `maxBatch` (100) — `Once`'s context bounds *both* the
  Kafka fetch and the batched PostgreSQL write, and ~200 round trips for 41
  transactions can exceed 2 seconds against a local database under load.
  Manifested as an intermittent `context deadline exceeded` on the second
  and third runs of `TestReconcileSequential`, not the first — a reminder
  that a single green run doesn't prove a timing budget is real. Fixed by
  widening to 30 seconds and confirming stability across three consecutive
  runs before trusting it.

## Test Coverage

- **Covered:** `internal/ledger` 85.8%, `internal/events` 89.6% — both from
  a merged `-coverprofile` run, deduplicated per source line across every
  test binary that instruments them (`go test ./... -coverpkg=./...
  -coverprofile=...`, then `go tool cover -func` filtered to each package's
  path and unioned by exact line range — the raw per-package percentage
  line that command prints is *not* this number, see the note this entry's
  author had to work out by hand this session). Both figures recorded in
  `docs/project-history.md`.
- **Not covered yet:** `internal/ledger/repo.go`'s `err != nil` branches
  after `Query`/`QueryRow`/`Scan`/`rows.Err()` — unreachable without
  fault-injecting the PostgreSQL connection mid-call. Accepted as the same
  class of gap `redisstore` and `internal/migrate` already carry, recorded
  rather than chased.

## Open Questions / Blockers

- **`delegating-plan-tasks`' Rule 3 needs a wording fix, not yet made.**
  The bounded return contract asks for `DEVIATIONS: anything done
  differently from the plan, and why. Includes checkpoints that collapsed
  because an earlier one already implemented them.` — but says nothing
  that would have prevented Task 4's subagent from reporting "no folds"
  while having silently violated the *commit-per-checkpoint* rule (Rule 2's
  own instruction, not Rule 3's). The fix: `DEVIATIONS` should explicitly
  require confirming commit-per-checkpoint held, not just checkpoint folds.
  This is a real gap in the skill exposed by its first execution, exactly
  the risk the prediction entry flagged ("a bad result may indict the
  skill's wording rather than delegation itself").
- **Whether this counts as confirming or falsifying the prediction is a
  judgment call, not a clean read.** Neither of the three pre-stated
  falsification conditions technically fired (delegated-only did not land
  ≥4.6M, blended did not land ≥5.77M, and — depending on how strictly
  "any guardrail breaks" is read — the numeric guardrails `commits/CP ≤
  1.10` did hold at 29/27 ≈ 1.07, even though the *intent* behind
  `un-batched = 0` clearly did not). Recording both readings rather than
  picking one: the token hypothesis has a real data point in its favor; the
  "four rules are sufficient" hypothesis does not yet have a clean one.
- **Turn inflation came in low.** 2.5 turns/CP for the delegated primary
  window, well under Phase 5a's inline 22.4 turns/CP control — the
  concern flagged in the prediction entry (cold-subagent context
  rediscovery costing turns) did not materialize here to any meaningful
  degree.

## Relevant Commits

29 commits from `c528a2d` (Task 1 CP1) through `d825a0b` (Task 6 CP4) on
`phase-5b-ledger`, plus this entry's own close-out commit. Full list:
`git log --oneline dev..phase-5b-ledger`. Notable ones not matching a plan
checkpoint 1:1:
- `4c6aaaf` — cross-package fix, migration 0002 vs. `internal/migrate`'s fixture
- `170ea5b` — the batched Task 4 commit (CPs 2–5 together)
- `7463738` — cross-package fix, `internal/events`' UUID fixtures

## Spec Changes

`docs/plans/2026-08-21-implementation-plan.md`: Amendments F1, F2, F3 added
(wire-format tags + the `user`/`user_id` divergence; migration `0002`'s
indexes; the reconciliation identity's opening-stake term and the
in-process load-generator substitution for k6). §9 Phase 5b row marked ✅.

`CLAUDE.md`: two new Critical Invariants from Phase 5b's own design (D1
sign convention, D2 net-session-delta ledger balance) plus one from the
security review (Kafka broker access = ledger-write access). Repository
Layout's `internal/ledger/` description expanded. Build & Test section's
stale "`make migrate` exists as a stub" line corrected — it's been real
since Phase 5a — and `make ledger-worker` documented.

## Next Step

Run `finishing-a-development-branch` on `phase-5b-ledger`. Separately (not
blocking the merge decision): fix `delegating-plan-tasks`' Rule 3 wording
per the Open Questions note above, so the next delegated phase's return
contract actually would have caught what this one didn't.
