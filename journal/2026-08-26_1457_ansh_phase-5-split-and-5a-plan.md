# 2026-08-26 — ansh — Phase 5 split, B1 resolved, and the 5a plan

**Status:** Phase 5 split into 5a/5b in the parent plan; `docs/plans/2026-08-26-phase-5a-outbox-kafka.md` written (6 tasks, 25 checkpoints) and committed to `dev`. Phase 5's two skills imported. **No code written** — 5a is planned, not started. Five commits on `dev`, unpushed.
**Decided:** Split at the durability boundary (5a = events reach Kafka + schema exists; 5b = events become ledger rows that reconcile). Amendment B1 resolved — persistent accounts **stay in Redis permanently**. Three new amendments (E1–E3) resolved in the 5a plan document rather than left to execution. Delegation experiment **deferred** — 5a executes inline.
**Spec:** No change. Parent plan (`docs/plans/2026-08-21-implementation-plan.md`) §4, §9 amended.
**Next:** Execute 5a with `executing-plans` in a Sonnet window — inline, not delegated. Point it at the plan path; don't create the branch by hand.
**Blocked on:** Nothing.
**Touches:** `docs/plans/2026-08-21-implementation-plan.md`, `docs/plans/2026-08-26-phase-5a-outbox-kafka.md`, `docs/project-history.md`, `CLAUDE.md`, `.claude/skills/{postgres-patterns,database-migrations}/`

---

## What We Worked On

Phase 5's planning pass. Phases 0–4 were merged and the MVP acceptance box
ticked, so this was the first phase with no pre-existing detailed plan and two
open questions parked from earlier phases.

## Decisions Made

- **Phase 5 → 5a/5b, split before the task breakdown.** Reasoning in the parent
  plan §9's "Phase 5 split into 5a/5b" note. Worth recording separately: this is
  the first time the phase-sizing recommendation (added at Phase 3 close-out)
  was applied *as intended* rather than noted after the fact. That note
  explicitly named Phase 5 as the one to check.
- **Amendment B1 — accounts stay in Redis.** Reasoning in parent plan §9's
  Phase 5 note. Revisit at Phase 7, where the FK-integrity argument actually
  starts to pay.
- **Amendments E1–E3 resolved in-document.** All three in
  `docs/plans/2026-08-26-phase-5a-outbox-kafka.md` § "Amendments to the parent
  plan". The reason they were resolved at plan time rather than execution time
  is specific to the split: 5a defines contracts 5b consumes, so a wrong guess
  in 5a is damage that only surfaces a phase later.
- **Delegation deferred; 5a executes inline.** See below.

## What Worked

- **The dependency wall turned out to be passable.** Every current release of
  all three Phase 5 dependencies declares `go >= 1.23`, which per `CLAUDE.md`
  would silently rewrite the module directive. Rather than assume, built a
  throwaway probe module pinned at `go 1.22.10` importing `pgxpool`, migrate's
  `postgres` driver, and `kafka.Writer` — `go build ./...` clean, directive
  unchanged. Pins: `pgx@v5.7.4`, `kafka-go@v0.4.48`, `migrate@v4.18.2`. Full
  table with the exact walls and rejected alternatives is in
  `docs/project-history.md`.
- **Every checkpoint's commit is gated behind `grep -q '^go 1.22.10$' go.mod`**,
  chained before `git add`. A dependency that rewrites the directive makes the
  commit unreachable rather than merely detectable at CI.

## What Didn't Work

- **Writing the delegation skill first.** This was the preference on record from
  the previous session ("split → skill → plan"), and it was reversed on
  examination. Three of the four rules (commit/verify split, bounded return
  contract, escalate-don't-improvise) are entirely task-agnostic, and the
  fourth (task granularity) was already settled by the measured 19k cold start.
  So there was very little in the skill that actually depended on 5a's task
  shape — writing it first meant speculating about a structure not yet drawn.
  The real dependency runs the other way: the skill's premise is that a plan's
  per-task `Interfaces: Consumes/Produces` blocks *are* the brief, and whether
  they're good enough to brief a cold executor can only be judged against real
  ones. Correct order is **plan → skill → execute**; the skill must exist
  before *execution*, not before *planning*, and the plan is on the critical
  path either way so this costs nothing.
- **Assuming a skill in `.claude/skills/` would be picked up on its own.** It
  would not, and the failure is worse than silence. A fresh session sees only a
  skill's name and one-line description; the body loads on invocation. Nothing
  in "execute this plan" triggers a delegation skill — that phrasing matches
  `executing-plans`'s own description almost verbatim. And `executing-plans`
  lines 14–17 currently state that subagent execution is "deliberately not"
  used and that it "is the execution path for `call_it`", so a fresh Sonnet
  would be *actively told not to delegate*. Silence resolves the question
  against the experiment rather than leaving it open. The three wiring points
  are recorded in the 5a plan's Execution notes.

## Open Questions / Blockers

- **The delegation experiment is designed, wired-up-on-paper, and unrun.** The
  cost problem it attacks is unchanged: 4b measured 9.08M tok/CP and one phase
  still consumes a full quota window. 5a will now be a *control* rather than a
  test bed — an inline plumbing phase, directly comparable to Phase 2.
- **The pre-registered bar was tightened from 6.0M to `< 4.6M`.** The previous
  session registered `tok/CP < 6.0M` against Phase 4b's 9.08M. That bar is
  confounded: 5a is plumbing (a stream reader, a Kafka writer, SQL files) and
  4b was four packages of money movement wired together, so the threshold could
  be cleared by 5a simply being easier — the exact confound that made 4a's
  headline result worthless. **Phase 2 (4.61M) is the honest control**: also a
  new infrastructure layer, also integration-heavy, also the phase where a new
  dependency first had to be made to work. A result between 4.6M and 6.0M is
  ambiguous, not a win. Since 5a now runs inline, this figure becomes the
  baseline any future delegated phase is measured against.
- **~~Task 2 CP2 may not go RED.~~ Resolved — the checkpoint was removed.**
  Checked against the code rather than left as a risk: `mapSettleStatus`
  (`internal/redisstore/errors.go:53-63`) is shared by both scripts and already
  maps `NOT_LOCKED` → `ErrNotLocked` for refunds, so a Go-side guard returning
  the same sentinel against the same untouched Redis state has **no** observable
  delta — unfalsifiable by construction, not merely at risk. Folded into the
  refund-payload checkpoint and enforced structurally instead: `ReadStakes` is
  called inside the `locked` branch of the status switch, with a comment
  carrying the reason. No test can catch a refactor that hoists it out, so the
  comment is the guard, and the plan says so plainly rather than implying test
  coverage that doesn't exist.
- **CI's Kafka step is the least-verified part of the plan.** Task 6 CP3 brings
  Kafka up via `docker compose --profile full up -d kafka` in a workflow step
  rather than a `services:` block, because KRaft's listener configuration is
  awkward as a GitHub Actions service container. That reuses the compose file
  and keeps CI and local dev on one definition, but it has not been run against
  a real runner.
- **Go 1.22.10 started costing something this phase.** Free through Phase 4; here
  it constrains three of three new dependencies, each one release from a wall,
  and 1.22 is past upstream EOL. Not required for Phase 5 and deliberately not
  bundled into it — but now a real Phase 7 candidate rather than a theoretical
  one.

## Test Coverage

- **Covered:** Nothing new — no code was written this session. The existing
  suite is untouched and was green at `ee60cb0`.
- **Not covered yet:** All of Phase 5a. The plan sets the 80% floor per new
  package (`internal/events`, `internal/relay`, `internal/migrate`) and makes
  Task 6 CP4 verify it via `-coverpkg=./...` before close-out, explicitly
  forbidding padding with tests that re-exercise covered lines.

## Auditing the plan against the standing rules found three gaps

Asked whether the plan was actually enough for a cold Sonnet window —
`dev-workflow-guide` §2a's bar is *"if a plan can only be executed by someone
who watched it being written, it isn't finished."* Auditing it against
`CLAUDE.md`'s binding rules (a separate pass from checking it against the spec,
and it found things the spec pass did not) surfaced three:

1. **`make test` would have failed from Task 1 onward.** It starts only Redis
   and waits on that one container; the new `internal/migrate` and
   `internal/events` suites need PostgreSQL and Kafka and **fail rather than
   skip** by design. `CLAUDE.md`'s Build & Test section documents the
   Redis-only behavior, and its Known Environment Gotchas still frame Kafka as
   "gated behind `make up-full` ... rather than running by default through
   Phases 0-4" — now historical. Task 6 CP3 updates all three files.
2. **No security review.** `CLAUDE.md` makes the `security-reviewer` agent
   mandatory before closing any phase touching auth, money movement, or a
   network surface. 5a touches two: every outbox event is a money movement, and
   Kafka is a new outbound surface. Added as Task 6 CP4, scoped to four
   specific surfaces so it doesn't re-review Phases 0–4.
3. **The unfalsifiable guard checkpoint** — see Open Questions.

Net checkpoint count is unchanged at 25 (one merged out, one added). The
earlier "22 checkpoints" figure in commit `a7b9517`'s message was simply an
arithmetic slip; the plan and this entry now both read 25.

## Relevant Commits

- `5c4b831` — split Phase 5 into 5a/5b, resolve Amendment B1, log the dependency pins
- `a7b9517` — add the Phase 5a outbox-to-Kafka and ledger schema plan
- `c787cdb` — record that 5a executes inline unless the delegation skill exists
- `62e5645` — journal this session
- *(this entry amended in the follow-up commit below, after the rules audit)*

## Next Step

Hand `docs/plans/2026-08-26-phase-5a-outbox-kafka.md` to a Sonnet window for
inline execution. Expect it to raise questions before starting — `executing-plans`
Step 1 requires a critical review pass, and Task 2 CP2's RED risk is the most
likely thing it flags. If the delegation skill is written first after all, the
three wiring points in the plan's Execution notes must all land *before* that
window opens, per §2a's one-window-at-a-time rule.
