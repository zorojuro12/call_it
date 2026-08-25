---
name: writing-plans
description: Use when you have a spec or requirements for a multi-step task, before touching code
---

# Writing Plans

## Overview

Write comprehensive implementation plans assuming the engineer has zero context for our codebase and questionable taste. Document everything they need to know: which files to touch for each task, code, testing, docs they might need to check, how to test it. Give them the whole plan as bite-sized tasks. DRY. YAGNI. TDD. Frequent commits.

Assume they are a skilled developer, but know almost nothing about our toolset or problem domain. Assume they don't know good test design very well.

**Announce at start:** "I'm using the writing-plans skill to create the implementation plan."

**Context:** If working in an isolated worktree, it should have been created via the `superpowers:using-git-worktrees` skill at execution time.

**Save plans to:** `docs/plans/YYYY-MM-DD-<feature-name>.md`
- (This project keeps all plans in `docs/plans/`, alongside the top-level
  phased implementation plan. See `docs/dev-workflow-guide.md` for when to
  use this skill vs. `/impl-plan` vs. the `orch-*` commands.)

## Scope Check

If the spec covers multiple independent subsystems, it should have been broken into sub-project specs during brainstorming. If it wasn't, suggest breaking this into separate plans — one per subsystem. Each plan should produce working, testable software on its own.

## File Structure

Before defining tasks, map out which files will be created or modified and what each one is responsible for. This is where decomposition decisions get locked in.

- Design units with clear boundaries and well-defined interfaces. Each file should have one clear responsibility.
- You reason best about code you can hold in context at once, and your edits are more reliable when files are focused. Prefer smaller, focused files over large ones that do too much.
- Files that change together should live together. Split by responsibility, not by technical layer.
- In existing codebases, follow established patterns. If the codebase uses large files, don't unilaterally restructure - but if a file you're modifying has grown unwieldy, including a split in the plan is reasonable.

This structure informs the task decomposition. Each task should produce self-contained changes that make sense independently.

## Task Right-Sizing

A task is the smallest unit that carries its own test cycle and is worth a
fresh reviewer's gate. When drawing task boundaries: fold setup,
configuration, scaffolding, and documentation steps into the task whose
deliverable needs them; split only where a reviewer could meaningfully
reject one task while approving its neighbor. Each task ends with an
independently testable deliverable.

## Bite-Sized Task Granularity

**Each step is one action (2-5 minutes). A task may contain multiple
checkpoints — one per distinct behavior/case — each checkpoint running its
own cycle:**
- "Write the failing test" - step
- "Run it to make sure it fails" - step
- "Implement the minimal code to make the test pass" - step
- "Run the tests and make sure they pass" - step
- "Commit" - step

A task with one straightforward behavior has one checkpoint (one commit). A
task covering several cases gets one checkpoint per case (several commits) —
don't invent checkpoints that aren't real distinctions, and don't collapse
real ones into a single commit either.

**A checkpoint is one RED→GREEN cycle — that's the test for whether it's real.**
If you can't write a test that fails *before* the implementation exists, it
isn't a checkpoint: fold those test cases into the checkpoint that implements
the behavior they cover. A checkpoint whose test passes the moment it's written
is the signal that granularity has been pushed one notch past where the cycle
actually divides.

This matters beyond tidiness. Every checkpoint's Step 2 says "expect FAIL", so
a checkpoint that expects PASS contradicts its own template — and
`executing-plans` requires stopping on any mismatch between an instruction and
reality. A cold executor hits that, halts, and may "fix" a correct test until
it fails. (Observed in the Phase 1 plan, 2026-08-23: four such checkpoints —
regression pins for behavior an earlier checkpoint's implementation already
satisfied.)

**Name the observable signal, at the interface the test actually calls.** A
checkpoint can also fail to RED for a second, unrelated reason: the behavior it
specifies is real, but the tested interface can't *see* it. Before writing a
checkpoint, answer — what value, error, or side effect changes at the public
surface this checkpoint's test calls? Not "the script sets status X internally,"
but "the wrapper returns `ErrAlreadyLocked`." If no such signal can be named,
the checkpoint is unfalsifiable by construction, and no reordering fixes it.

Two ways out, both decided while writing the plan rather than discovered
mid-execution:

1. **The distinction doesn't matter to callers** → merge the checkpoint into a
   neighbor that does have an observable delta.
2. **The distinction does matter** → make "extend the interface to surface this
   case" its own earlier checkpoint, then checkpoint the behavior against it.

This is the failure mode of any layer whose lower level has more states than its
wrapper exposes — a wrapper over a script, a client over a protocol, an ORM over
a stored procedure. (Observed in Phase 2, 2026-08-24: `lock_round.lua`'s
`ALREADY_LOCKED` case was black-box indistinguishable from its unconditional-OK
predecessor at the Go wrapper's return type. Cost a full unwind — revert the
script, re-run the test to prove it still passed, then recombine three planned
checkpoints into one commit.)

## Plan Document Header

**Every plan MUST start with this header:**

```markdown
# [Feature Name] Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** [One sentence describing what this builds]

**Architecture:** [2-3 sentences about approach]

**Tech Stack:** [Key technologies/libraries]

**Spec:** [path to the spec/design doc this plan implements — the plan
argues from the spec, so the spec travels with it; executors read both]

## Global Constraints

[The spec's project-wide requirements — version floors, dependency limits,
naming and copy rules, platform requirements — one line each, with exact
values copied verbatim from the spec. Every task's requirements implicitly
include this section.]

---
```

## Task Structure

**Spec-driven, not code-driven (project adaptation, 2026-08-23).** Upstream's
template pre-writes full test and implementation code for every checkpoint.
That earns its keep under `subagent-driven-development`, where each task goes
to a fresh, cold-context subagent that never sees the rest of the plan —
pre-written code means a cheap model can transcribe and verify instead of a
capable model re-deriving the implementation. This project doesn't use that
execution mode (declined — `docs/dev-workflow-guide.md` §9); `executing-plans`
runs inline, in the same context that wrote the plan. Under that mode,
pre-writing full code buys nothing — the executor would derive essentially
the same code either way — and it made a single phase's plan exceed 3000
lines (Phase 1: 8 tasks, 35 checkpoints, 61 code blocks, ~88 lines/checkpoint
average — see `journal/` for the session that flagged this). So: **specify
the exact behavior precisely; let execution write the code.** If this project
ever adopts subagent-driven execution, revert to full pre-written code for
whichever tasks go that route — the two modes need different plan formats,
this isn't a universal improvement.

````markdown
### Task N: [Component Name]

**Files:**
- Create: `exact/path/to/file.py`
- Modify: `exact/path/to/existing.py:123-145`
- Test: `tests/exact/path/to/test.py`

**Interfaces:**
- Consumes: [what this task uses from earlier tasks — exact signatures]
- Produces: [what later tasks rely on — exact function names, parameter
  and return types. Kept exact regardless of execution mode: this is what
  keeps cross-task types consistent, e.g. not `clearLayers()` in Task 3 vs
  `clearFullLayers()` in Task 7.]

**Checkpoint 1: [specific behavior or case this checkpoint covers]**

- [ ] **Step 1: Write a failing test for this exact behavior**

Spec: [exact input(s) → exact expected output or error, stated precisely
enough that two different implementers would write the same test — e.g.
"input `[]`, pool empty → returns `ErrEmptyPool`", not "handle the empty
case." Show a code block only if a subtle assertion detail needs pinning
down (a specific float tolerance, an exact error message string) — not by
default.]

- [ ] **Step 2: Run test to verify it fails**

Run: [exact command]
Expected: FAIL with [exact expected failure reason]

- [ ] **Step 3: Implement to satisfy the test**

Contract: [the behavior in 1-2 lines, using the exact signature from
Interfaces above. Not a function body — the executor writes that against
this contract and the test from Step 1.]

- [ ] **Step 4: Run test to verify it passes**

Run: [exact command]
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add [exact paths]
git commit -m "[exact type: description]"
```

**Checkpoint 2: [next behavior or case, if this task has one]**

- [ ] Step 1: Write a failing test for: [exact spec, as above]
- [ ] Step 2: Run — expect FAIL
- [ ] Step 3: Implement to satisfy: [exact contract, as above]
- [ ] Step 4: Run — expect PASS
- [ ] Step 5: Commit

[Repeat Checkpoint N for each further distinct behavior. Omit Checkpoint 2+
entirely when the task genuinely has only one behavior — a single checkpoint
is a complete, valid task, not a truncated one.]
````

## No Placeholders

Every step must contain the actual content an engineer needs. These are **plan failures** — never write them:
- "TBD", "TODO", "implement later", "fill in details"
- "Add appropriate error handling" / "add validation" / "handle edge cases"
- "Write tests for the above" (without an exact input→output/error spec)
- "Similar to Task N" (repeat the full spec — the executor may work tasks out of order)
- "Handle the edge case" without saying which edge case and what the exact
  expected behavior is
- References to types, functions, or methods not defined in any task's
  Interfaces block

**In this project, a precise behavior spec satisfies this rule — full code is
not required** (see Task Structure above for why). The bar is: could two
different implementers, given only this step, write the same test/same
implementation? If yes, it's specific enough. "Reject values below the room
buy-in" fails that bar (which values? what happens instead?); "input stake
50, room buy-in 100 → returns `ErrBelowBuyIn`, wallet untouched" passes it.

## Self-Review

After writing the complete plan, look at the spec with fresh eyes and check the plan against it. This is a checklist you run yourself — not a subagent dispatch.

**1. Spec coverage:** Skim each section/requirement in the spec. Can you point to a task that implements it? List any gaps.

**2. Placeholder scan:** Search your plan for red flags — any of the patterns from the "No Placeholders" section above. Fix them.

**3. Type consistency:** Do the types, method signatures, and property names you used in later tasks match what you defined in earlier tasks? A function called `clearLayers()` in Task 3 but `clearFullLayers()` in Task 7 is a bug.

If you find issues, fix them inline. No need to re-review — just fix and move on. If you find a spec requirement with no task, add the task.

## Where a Plan Stops

**Don't write merge, push, or PR steps into a plan.** `executing-plans` Step 3
hands off to `finishing-a-development-branch`, which verifies tests and presents
the merge/PR/keep menu — it owns integration, and deliberately keeps that
decision with the user. A plan whose final task also merges gives the executor
two paths for one merge, and pre-empts a choice that isn't the plan's to make.

A plan's final task ends at **"branch is green and verified."** Phase-specific
wrap-up that neither skill covers — amending a parent plan, recording a
convention's outcome, writing a journal entry — belongs in that final task. The
merge does not.

## Execution Handoff

Before handing off, **commit the plan to the integration branch.** A plan
executed in a different session is untracked at handoff, and the executing
session's first act is `git checkout -b <slug> dev` — so an uncommitted plan
follows onto the feature branch and lands in that phase's history as though it
were phase work. Commit any other stray files (a previous session's journal
entry, say) at the same time, so the executor starts from a clean tree.

Then report the plan's path and confirm before executing:

**"Plan complete and saved to `docs/plans/<filename>.md`. Review it, then I'll
execute inline with the `executing-plans` skill — task by task, committing at
each task boundary, stopping if I hit a blocker. Ready?"**

- **REQUIRED SUB-SKILL:** Use the `executing-plans` skill
- Inline execution in this session, with checkpoints for review

**Executing in a separate session** — including a different model — is equally
valid and needs no conversation history: `CLAUDE.md` auto-loads, and the plan
carries its own Global Constraints and any amendments it makes to a parent plan
or spec. See `docs/dev-workflow-guide.md` for this project's two-model loop.

(Upstream also offers a subagent-driven mode — a fresh subagent per task with
two-stage review. That skill isn't installed in this project; inline execution
is the deliberate default. Install `subagent-driven-development` from the
superpowers checkout if that changes.)

## Relationship to the portable copy

This file is the CallIt-adapted copy. A generalized version lives in the
skills library at `~/projects/claude-skills/writing-plans/` — same rules, with
the project-specific parts (plan location, `docs/dev-workflow-guide.md` links,
`dev` branch, CallIt examples) stripped, and the spec-driven-vs-code-driven
choice stated as a fork rather than a decision already made.

**When a rule here changes, decide which copy it belongs to.** The test: did you
invent a rule, or set a value? Rules go to the library too; values stay here.
See `docs/dev-workflow-guide.md` §9 for the full split and why the library isn't
live-loaded from `~/.claude/skills/`.

Upstream is `obra/superpowers`, cloned read-only at `~/projects/superpowers` —
don't edit that checkout.
