---
name: writing-plans-tuned
description: Experimental token-tuned variant of writing-plans — use when writing a phase plan whose execution must fit a constrained token budget. Delta skill; read writing-plans first.
---

# Writing Plans (Tuned)

## What this is

**An experiment, dated 2026-08-25.** It is a *delta* over
`.claude/skills/writing-plans/SKILL.md`, not a replacement — that file is
still the base and everything in it applies unless this file overrides it.
The delta is deliberately small so the A/B against the base skill is
readable as a diff rather than a rewrite.

**Read `.claude/skills/writing-plans/SKILL.md` first, then apply the
overrides below.**

**Why it exists.** Phase 3's plan-plus-implementation exhausted a full
5-hour token window (at boosted limits). Cumulative token burn is roughly
`round trips × context size per round trip`, and the base skill's plan
format controls both: it sets how many tool calls each checkpoint costs,
how much test output lands in context, and how long the plan document
itself is. This variant tunes those three without touching the TDD
discipline underneath.

**Announce at start:** "I'm using the writing-plans-tuned skill to create
the implementation plan."

---

## Override A: the checkpoint template is two steps, not five

The base skill's five-step checkpoint (write test / run / implement / run /
commit) spends one tool call per step. The commit step never needs to be
its own call — chained behind the verification command it becomes free, and
becomes *safer*, since a red test now makes the commit impossible rather
than merely inadvisable.

Replace the base skill's checkpoint template with:

````markdown
**Checkpoint N: [specific behavior or case this checkpoint covers]**

- [ ] **Step 1: Write the failing test, then run it**

Spec: [exact input(s) → exact expected output or error, stated precisely
enough that two different implementers would write the same test. Same bar
as the base skill — this override changes step *count*, never spec
precision.]

Run: [exact package-scoped command — see Override B]
Expected: FAIL with [exact expected failure reason]

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: [the behavior in 1-2 lines, using the exact signature from
Interfaces above.]

```bash
[exact package-scoped test command] && \
  git add [exact paths] && \
  git commit -m "[exact type: description]"
```

Expected: PASS, then one commit.
````

**The `&&` chain is load-bearing, not cosmetic.** It is what makes
collapsing "verify" and "commit" into one call safe: the commit is
unreachable unless the test passed. Never write this as `;` or as separate
lines in one block — that reintroduces the exact failure the base skill's
separate commit step was protecting against.

**`git add` still names exact paths.** Never `git add -A` or `git add .` —
a chained commit gets less human scrutiny than a standalone one, so the
staging set must be pinned by the plan, not discovered at runtime.

---

## Override B: every checkpoint's command is package-scoped

The base skill says "exact command" without constraining scope. In this
project a full `make test` is a Docker round trip, a Redis healthcheck
wait, and eight packages serialized by `-p 1` — and its output lands in
context on *every* checkpoint that runs it.

- **Inside a checkpoint:** scope to the package under test, and to the test
  when the package is slow.
  `cd backend && go test ./internal/ws/ -race -count=1 -run TestClientEvictsOnSlowRead`
- **At a task boundary:** the full suite once, chained into one call.
  `make test && make lint && make build`
- **Never** put `make test` inside a checkpoint.

`-count=1` is required in checkpoint commands — a cached PASS from the
previous checkpoint would mask a genuine RED.

---

## Override C: state the plan's own length target

Add to the plan document header, under Global Constraints:

```markdown
**Plan budget:** ~N checkpoints, target ≤60 lines/checkpoint.
```

Phase 2 held ~64 lines/checkpoint and executed cleanly; Phase 3 drifted to
~76 and overran. Naming the target in the document makes drift visible
while writing rather than at the retrospective.

---

## What this override does NOT change

Listed explicitly because each is a plausible misreading of "tuned," and
each would trade correctness for budget:

- **Commit granularity.** One checkpoint still equals one commit. Batching
  affects tool calls, never git history. A phase with 37 checkpoints still
  produces ~37 commits.
- **Checkpoint realness.** A checkpoint is still one genuine RED→GREEN
  cycle. Merging checkpoints to save turns is *not* part of this
  experiment — that trades safety for budget, where the overrides above
  trade nothing.
- **The observable-signal rule.** Still applies in full. Batching makes an
  unfalsifiable checkpoint *easier to miss*, since the write and the run
  now sit in one step: if Step 1's test passes on first run, stop and treat
  it exactly as the base skill requires.
- **Spec precision, No Placeholders, Self-Review, Where a Plan Stops,
  Execution Handoff.** Unchanged, all of them.

---

## Measurement protocol

This is an experiment; it only pays off if the comparison actually gets
made. At phase close-out, record in the journal entry:

| Metric | Base skill (Phase 3) | This phase |
|---|---|---|
| Plan lines | 2,904 | |
| Checkpoints | 38 | |
| Lines/checkpoint | ~76 | |
| Commits landed vs. planned | ~49 vs ~44 | |
| Windows exhausted | 1 (full 5h, boosted) | |
| Checkpoints that had to be un-batched | n/a | |

The last row is the one that decides the experiment's fate: if checkpoints
routinely need splitting back into five steps mid-execution, the batching
is wrong and this skill should be deleted rather than merged.

**Outcome:** if the tuned loop holds, fold Overrides A–C into
`.claude/skills/writing-plans/SKILL.md` and its portable copy at
`~/projects/claude-skills/writing-plans/`, then delete this skill — a
permanent delta skill is a maintenance liability, since divergence in the
base is invisible from here. If it doesn't hold, delete it and record why
in `docs/dev-workflow-guide.md` §9 alongside the other declined tooling.
