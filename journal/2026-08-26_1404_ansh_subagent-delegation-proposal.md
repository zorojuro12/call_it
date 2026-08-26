# 2026-08-26 — ansh — Subagent delegation as the token-cost lever (proposal, not yet adopted)

**Status:** Phases 0–4 complete and merged to `dev` (`ee60cb0`), pushed. Subagent delegation analyzed and designed but **nothing built** — parked deliberately.
**Decided:** Nothing binding. Working recommendation: adopt task-granularity delegation for Phase 5a via a thin project-local skill, keep 5b inline, measure with the same `tok/CP` methodology. Awaiting go/no-go.
**Spec:** No change.
**Next:** Decide (a) whether Phase 5 splits 5a/5b like Phase 4 did, and (b) whether the delegation skill gets written before or after the 5a plan. Preference on record: split → skill → plan.
**Blocked on:** Nothing technical. Awaiting the user's call.
**Touches:** `docs/dev-workflow-guide.md`, `.claude/skills/writing-plans/`, `.claude/skills/executing-plans/`, `docs/plans/2026-08-21-implementation-plan.md` §9, `journal/2026-08-26_0250_ansh_tuned-plan-experiment-verdict.md`

---

## What We Worked On

Follow-on to the `writing-plans-tuned` experiment. That experiment falsified its
token claim, which left the original problem open: **one phase's plan +
execution exhausts a 5-hour quota window**, even at boosted limits. This session
worked out *why*, and what actually attacks it.

## The cost model (this is the load-bearing part)

```
cost ≈ Σ over turns of (context size at that turn)
```

Every API turn resends the whole conversation. Cache reads are heavily
discounted but still count against quota, and they are ~99.5% of raw volume.

Validated against Phase 4b: ~915 turns × ~303k avg context ≈ 277M, measured
281M. The model holds.

**There are exactly two levers: turn count, and context size per turn.**
`writing-plans-tuned` attacked turn count and failed. Delegation attacks the
other term — a subagent's turns execute at 50–80k instead of 300k.

## The evidence

Natural experiment from Phase 4b's own `security-reviewer` dispatch:

| | turns | avg context | cost |
|---|---|---|---|
| `security-reviewer` subagent | 90 | 51k (19k cold start → 80k final) | **4.64M** |
| same 90 turns inline | 90 | ~303k | **~27.3M** |

~6×. And that understates it — inline, those turns would also have permanently
raised the floor for the ~800 turns that followed.

**Caveat, stated so a future session doesn't over-trust the number:** a security
review is read-only, self-contained, and returns a report. Execution has
write→verify→fix loops and needs continuity across checkpoints. 6× is a
**ceiling, not a forecast**. Realistic range for a delegated 5a: **2–4×**.

Cold-start preamble measured at **19k** (rules + CLAUDE.md + skills) — ~6% of
one main-session turn at 4b's context. Small enough that per-task dispatch pays
for itself; per-*checkpoint* dispatch would not.

## Design of the thin skill (four decisions, not more)

Most machinery already exists in `writing-plans` / `executing-plans`. The delta:

1. **Task granularity, not checkpoint.** A checkpoint is minutes of work; a 19k
   cold start per checkpoint eats the win. A task is 3–5 checkpoints and already
   ends at "independently testable deliverable." The plan format's per-task
   `Interfaces: Consumes / Produces` blocks **are the brief** — designed for cold
   executors, never yet used as such.
2. **The subagent commits; the parent verifies cheaply.** Commit-per-checkpoint
   discipline must survive, and only the subagent knows when a checkpoint went
   green. Parent runs the full suite once at the task boundary and reads
   `git log --oneline` — a few hundred tokens, instead of re-absorbing the work.
3. **Bounded, structured return contract.** This is where delegation quietly
   fails: a 5k narrative per task re-accumulates in the parent and you've paid
   the cold start for nothing. Return only: commits made, interfaces actually
   produced (so the next brief is accurate), deviations from plan, defects found.
4. **Escalate, don't improvise.** Phase 4b's expensive moments were *plan*
   defects (the `round`↔`ws` import cycle, the typed-nil-in-interface panic). A
   subagent holding one task's context must **stop and report** a plan
   contradiction rather than redesign around it. Getting this wrong is the real
   risk — an invented workaround is damage the parent can't see without reading
   everything, which destroys the savings *and* the correctness.

Explicitly **not** installing `subagent-driven-development`. The objection to it
was always its ceremony, not delegation itself.

## Scope recommendation

- **5a (outbox relay, Kafka producers, migrations)** — delegate. Genuinely
  task-separable; right test bed.
- **5b (double-entry ledger, reconciliation test)** — keep inline. Flagship
  correctness work, cross-task continuity actually pays there, and it's the
  wrong place to run a process experiment.

## Pre-registered prediction (register before anything runs, per the tuned-plan lesson)

- `tok/CP` **< 6.0M** (Phase 4b baseline: 9.08M)
- `un-batched = 0` and `commits/CP ≤ 1.10` as guardrails proving discipline survived

`phase_compare.py` already bounds both ends and already sums
`<session>/subagents/*.jsonl`, so delegated tokens land in the same column — **no
methodology change**, which is what makes it comparable to Phases 2/3/4a/4b.

If it lands above 6.0M, delegation is the wrong lever too, and the honest
conclusion is that Phase 5 is simply expensive.

## What Didn't Work

- **`writing-plans-tuned`'s token hypothesis** — attacked turn count via a
  leaner plan format. Process claims held (0 un-batching, better commit
  granularity) but `tok/CP` was unmoved; 4b came in at 9.08M vs Phase 2's 4.61M.
  Cost tracks **problem difficulty and defect density** (4b: 6 real defects, 17.1
  tools/CP), not plan format. Don't retry a format tweak expecting token savings.
- **My earlier recommendation *against* subagents** — argued they'd worsen quota
  because each dispatch re-reads CLAUDE.md cold. Wrong: measured cold start is
  19k, ~6% of a single late-session main turn. The objection applied to SDD's
  ceremony, not to delegation. Corrected on the evidence.

## Open Questions / Blockers

- Does Phase 5 split 5a/5b? (Drives the task boundaries the brief format is
  shaped around, which is why the split should come first.)
- Skill before or after the 5a plan? Preference on record: **split → skill →
  plan**, so the brief format is designed against real task boundaries rather
  than imagined ones.
- Unmeasured: whether a cold subagent needs materially *more* turns to
  rediscover cross-task context. If turn inflation exceeds ~40%, the 2–4× range
  compresses toward break-even. No data on this yet — it's the main way the
  estimate could be wrong.

## Relevant Commits

- `ee60cb0` — merge `phase-4b-round-lifecycle` into `dev`; Phases 0–4 complete
- `37f6991` — trim CLAUDE.md to binding rules, move history to `docs/project-history.md`
- `1ddd48d` — merge `writing-plans-tuned` into `writing-plans`, delete the variant
