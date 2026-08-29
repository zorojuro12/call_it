---
name: delegating-plan-tasks
description: Use when executing a written implementation plan and you want a plan's tasks carried out by cold subagents instead of inline, to cut token cost. Sits inside executing-plans, not beside it.
---

# Delegating Plan Tasks

## Overview

`executing-plans` runs a plan's tasks inline, in the session that holds the
whole conversation. This skill changes **where a task's turns execute**, and
nothing else. The plan format, the checkpoint discipline, the commit
granularity, and the branch model are all unchanged.

**Announce at start:** "I'm using the delegating-plan-tasks skill to execute
this task in a subagent."

### Why this exists

The cost model, validated against Phase 4b (~915 turns × ~303k avg context ≈
277M, measured 281M):

```
cost ≈ Σ over turns of (context size at that turn)
```

There are exactly two levers: **turn count** and **context size per turn**.
`writing-plans-tuned` attacked turn count and failed — `tok/CP` was unmoved
because cost tracks problem difficulty and defect density, not plan format
(`journal/2026-08-26_0250_ansh_tuned-plan-experiment-verdict.md`).

This skill attacks the other term. A subagent's turns execute at 50–80k
instead of 300k. The natural experiment from Phase 4b's own
`security-reviewer` dispatch measured ~6× — but that was read-only,
self-contained work returning a report. Execution has write→verify→fix loops
and needs continuity across checkpoints. **Treat 6× as a ceiling, not a
forecast; 2–4× is the realistic range.**

Cold-start preamble is **19k** (rules + `CLAUDE.md` + skills) — ~6% of one
late-session main turn. That number is what sets the granularity rule below.

---

## The Four Rules

Everything else is already in `writing-plans` and `executing-plans`. These
four are the entire delta. Do not add a fifth.

### 1. Task granularity, never checkpoint granularity

Dispatch **one subagent per plan task**. A task is 3–5 checkpoints and
already ends at "independently testable deliverable" — the boundary the plan
format was built around.

A checkpoint is minutes of work. At 19k cold start per dispatch,
per-checkpoint delegation spends the entire saving on preamble and can cost
more than running inline.

**The brief is the plan's own `Interfaces: Consumes / Produces` block.** That
block was written for a cold executor and has never yet been used as one.
Do not paraphrase the task into a new brief — point the subagent at the plan
file and the task number, and let it read the real thing.

### 2. The subagent commits; the parent verifies cheaply

Commit-per-checkpoint discipline must survive delegation, and only the
subagent knows the moment a checkpoint went green. So the subagent runs the
plan's own `test && git add && git commit` chains exactly as written.

The parent does **not** re-read the work. At the task boundary it runs two
things:

```bash
cd backend && go vet ./... && gofmt -l . && go test ./... -race -cover -p 1 -count=1
git log --oneline dev..HEAD
```

A few hundred tokens, instead of re-absorbing everything the subagent read.
If the suite is green and the commit log matches the task's checkpoints, the
task is done. That is the whole verification.

### 3. Bounded, structured return contract

**This is where delegation quietly fails.** A 5k-token narrative per task
re-accumulates in the parent and you have paid the cold start for nothing.

Require exactly four sections, and cap the whole return at ~300 words:

```
COMMITS:    one line per commit — `<sha> <subject> — CPn` (or `CPn–CPm` if
            more than one checkpoint landed in that commit). Every
            checkpoint in the task must be named against exactly one line.
INTERFACES: what was actually produced — exact signatures, only where they
            differ from the plan's Produces block. "As planned" is a
            complete answer.
DEVIATIONS: anything done differently from the plan, and why. State
            explicitly whether every checkpoint landed in its own commit.
            If not, name which checkpoints shared a commit and why — this
            must agree with the CPn ranges in COMMITS above; the parent
            checks both against `git log --oneline` and treats a mismatch
            as a defect in the report, not just in the commits.
DEFECTS:    bugs found in existing code, plan contradictions hit, or
            "none".
```

`INTERFACES` is load-bearing: the next task's brief is only accurate if the
previous task reported what it really built. `DEVIATIONS` is what keeps the
journal honest at close-out — the `CPn` tag on every `COMMITS` line exists so
a fold shows up as a fact in that section (a `CP2–CP5` range against one
sha), not just as an honesty test on the summary sentence below it.

No narrative. No "what I learned". No restating the plan back.

### 4. Escalate, don't improvise

**A subagent that hits a plan contradiction must stop and report it, not
design around it.**

Phase 4b's expensive moments were *plan* defects, not coding defects — the
`round`↔`ws` import cycle and the typed-nil-in-interface panic. A subagent
holding one task's context cannot see the consequences of redesigning around
one. An invented workaround is damage the parent cannot detect without
reading everything, which destroys the token saving *and* the correctness.

Put this verbatim in every dispatch:

> If the plan contradicts the code, contradicts itself, or asks for something
> that cannot work, STOP. Do not redesign around it. Return immediately with
> `DEFECTS:` describing the contradiction and what you did up to that point.
> A halted task the parent can fix is cheap; a silently reinterpreted task is
> not.

---

## How to Dispatch

```
Agent(
  subagent_type: "general-purpose",
  model:         "sonnet",
  description:   "Execute Task N",
  prompt:        <the brief below>
)
```

**`subagent_type` must be `general-purpose`, never `fork`.** A fork inherits
the parent's full conversation context and runs on the parent's model —
which is exactly the 300k-per-turn cost this skill exists to avoid. A fork
dispatch produces the ceremony of delegation with none of the saving.

**Never `isolation: "worktree"`.** The subagent's commits must land on the
phase branch the parent is on. A worktree puts them somewhere the parent
cannot see.

**Dispatch tasks strictly sequentially — one at a time, never in parallel.**
`.claude/rules/ecc/common/agents.md` says to always parallelize independent
operations; plan tasks are **not** independent. Task N+1 consumes Task N's
`Produces` interfaces and commits to the same branch. Parallel dispatch
races the branch and briefs later tasks against interfaces that do not exist
yet.

You own collection: wait for the result, verify per Rule 2, then dispatch the
next. Never end a turn with tasks still running.

### The brief

```
Execute Task N of docs/plans/<plan-file>.md on the current branch.

Read the plan file first: its header, its Global Constraints section, and
Task N in full. Task N's `Interfaces: Consumes / Produces` block is your
contract — the Consumes entries already exist in the codebase, and later
tasks depend on the Produces entries being exactly as written.

Follow every checkpoint's two steps exactly as the plan states them,
including the RED step. Run the plan's own commands verbatim — the
test-and-commit chains are written with `&&` so a red test makes the commit
unreachable. Do not batch checkpoints into one commit.

Do NOT: merge, push, run finishing-a-development-branch, or touch any task
other than N.

<Rule 4's escalation paragraph, verbatim>

Return exactly four sections and nothing else — COMMITS, INTERFACES,
DEVIATIONS, DEFECTS — in under 300 words total.
```

---

## What to Delegate, and What Not To

**Per-task selectivity, not per-phase.** The decision is made task by task
when the plan is written, not once for the whole phase.

**Delegate** tasks that are mechanical against a clear contract: a
repository over a known schema, a config surface, a decode/validate layer, a
binary that wires existing pieces together.

**Keep inline** the phase's flagship correctness work — the test that is the
*evidence* for a claim the project makes. For Phase 5b that is Task 6, the
Redis↔PostgreSQL reconciliation suite, which parent plan §6 calls "the
evidence behind the 0.00% double-spend claim". Cross-task continuity
genuinely pays there, and it is the wrong place to absorb the risk of a
process experiment.

The general form: **run the experiment where a mistake is cheap; don't bet
the correctness proof on it.**

---

## Measuring It

Delegation is a hypothesis, not a conclusion. Register the prediction
**before** dispatching anything — that is the lesson from
`writing-plans-tuned`, which was evaluated after the fact and produced an
argument instead of a result.

Record in the phase's journal entry, before Task 1 is dispatched:

- **`tok/CP` target**, compared against the most recent *inline* control on a
  codebase of similar size. The current control is **5.77M/CP** (Phase 5a,
  inline, 25 checkpoints — `journal/2026-08-26_1915_ansh_phase-5a-token-measurement.md`).
  Phase 2's 4.61M is **not** the right comparator: it ran against a codebase
  less than half the current size.
- **Guardrails proving discipline survived:** `un-batched = 0` and
  `commits/CP ≤ 1.10`. A token win bought by collapsing checkpoints is not a
  win.
- **Turn inflation**, the main way the estimate could be wrong: a cold
  subagent may need materially more turns to rediscover cross-task context.
  If inflation exceeds ~40%, the 2–4× range compresses toward break-even.
  There is no data on this yet — this skill's first run is what produces it.

Measure with `scripts/phase_compare.py`. It already bounds both ends of the
window and already sums `<session>/subagents/*.jsonl`, so delegated tokens
land in the same column as inline ones — **no methodology change**, which is
what keeps the number comparable to Phases 2/3/4a/4b/5a.

If it lands above the control, delegation is the wrong lever too, and the
honest conclusion is that these phases are simply expensive. Write that
down rather than retrying with a tweak.

---

## Relationship to Other Skills

- **`executing-plans` owns the process.** This skill is invoked *from* its
  Step 2, per task. Step 1 (branch, critical review) and Step 3 (hand off to
  `finishing-a-development-branch`) stay with the parent and are never
  delegated — a subagent must not merge, push, or decide integration.
- **`subagent-driven-development` is still declined** (`docs/dev-workflow-guide.md`
  §9). The objection to it was always its ceremony — 7 files, a 32KB
  `SKILL.md`, minimum 2 dispatches per task rising to ~12 through a fix loop,
  a git-ignored progress ledger, and an explicitly autonomous "rulings, not
  stalls" posture. The objection was never to delegation itself. This skill
  is the delegation without the ceremony: one dispatch per task, no reviewer
  agent, no ledger, and escalation instead of rulings.
- **`dispatching-parallel-agents` does not apply here.** Plan tasks are
  sequentially dependent. See the dispatch rules above.

## Portability

This is the CallIt copy. The four rules, the cost model, the return
contract, and the escalation posture are **portable** — they are rules. The
`tok/CP` figures, the Go test commands, the `dev` branch, and the Phase 5b
example are **values** and stay here.

If a rule changes, carry it to `~/projects/claude-skills/`; if a value
changes, it stays. See `docs/dev-workflow-guide.md` §9 for the full split.
