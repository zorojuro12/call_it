# 2026-08-27 — ansh — Pre-registered prediction for the delegated Phase 5b

**Status:** Prediction **registered**. Nothing has been dispatched — `phase-5b-ledger` does not exist yet, no `executing-plans` window has opened, and this entry is committed before either happens. That ordering is the entire point: a prediction written after the run is an argument, not a result.
**Decided:** Two bars, not one. **Primary — delegated-only `< 3.5M/CP`** over Tasks 1–5's 23 checkpoints, which is the actual experiment. **Secondary — blended `< 4.5M/CP`** over all 28, which is what the phase costs in practice. Plus three guardrails and three pre-stated falsification conditions, below.
**Spec:** No change.
**Next:** Fresh window, then `executing-plans` on `docs/plans/2026-08-27-phase-5b-ledger.md`. Nothing is owed before that.
**Blocked on:** Nothing.
**Touches:** `docs/plans/2026-08-27-phase-5b-ledger.md`, `.claude/skills/delegating-plan-tasks/`, `scripts/phase_compare.py`

---

## The Prediction

Phase 5b is 28 checkpoints across 6 tasks. **23 are delegated** (Tasks 1–5),
**5 are inline** (Task 6, the reconciliation suite and close-out). That 82/18
split is what makes two bars necessary rather than one.

| | bar | scope | denominator |
|---|---|---|---|
| **Primary** | `tok/CP < 3.5M` | Tasks 1–5 only — bounded from the Task 1 dispatch to Task 5's final commit | 23 |
| **Secondary** | `tok/CP < 4.5M` | whole phase — `executing-plans` invocation to close-out commit | 28 |

Guardrails, all three of which must hold for either bar to count:

- `un-batched = 0` — no checkpoint executed without its own commit.
- `commits/CP ≤ 1.10`.
- Collapsed checkpoints recorded explicitly, not silently absorbed. The plan
  pre-names the likely ones (Task 3 CPs 3–5, Task 4 CP 3); a collapse shrinks
  the denominator and inflates `tok/CP`, so the count must be honest in both
  directions.

Also recorded, not as a bar but because it is the main way the estimate could
be wrong: **turns/CP for the delegated tasks**, against Phase 5a's inline
**22.4 turns/CP**. Above ~40% inflation the 2–4× range compresses toward
break-even.

## Why two bars, and why not the obvious one

The obvious bar was **`< 5.77M/CP`** — beat the Phase 5a inline control. I
nearly registered it and it is too weak to be a test. Work it through: if
delegation delivers even a modest 1.5× on the delegated 82%, the blend lands
near 4.2M/CP, and at 2× it lands near 3.4M. A 5.77M bar is cleared by a
result that would tell us almost nothing. Registering a bar you cannot
plausibly fail is the same mistake `writing-plans-tuned` made in a different
costume.

So the primary bar isolates the experiment from the blend. `phase_compare.py`
bounds both ends of a window by timestamp, so Tasks 1–5 can be measured on
their own — Task 1's dispatch to Task 5's final commit — without Task 6's
inline turns contaminating the number. **3.5M/CP against a 5.77M control is a
~1.65× improvement**, which sits at the pessimistic end of the 2–4× range the
skill claims. If delegation cannot clear the pessimistic end of its own
estimate, the estimate was wrong.

The secondary bar is the honest answer to "what did the phase actually cost,"
which is the number that matters for deciding whether to do this again.

## What Falsifies It

Stated now so the conclusion is not negotiated after the fact:

1. **Delegated-only ≥ 4.6M/CP** (under ~20% improvement on the control) — the
   2–4× estimate was wrong, and cold-start plus turn inflation ate the
   saving. Delegation is not the lever.
2. **Blended ≥ 5.77M/CP** — the phase cost more delegated than Phase 5a cost
   inline. Conclude that these phases are simply expensive and stop looking
   for a process lever. **Do not retry with a tweak** — that is exactly what
   was done after `writing-plans-tuned` failed, and it produced this
   experiment rather than an admission.
3. **Any guardrail breaks** — a token win bought by collapsing checkpoints or
   skipping commits is disqualified regardless of the number. Discipline
   surviving is a precondition of the result, not a bonus.

## Methodology

`scripts/phase_compare.py`, unchanged. It already bounds both ends of the
window and already sums `<session>/subagents/*.jsonl`, so **delegated tokens
land in the same column as inline ones**. That is what keeps this number
comparable to Phases 2, 3, 4a, 4b, and 5a — no methodology change, which was
the whole reason the script got committed in the first place.

Comparator is **Phase 5a's 5.77M/CP**, not Phase 2's 4.61M. Phase 2 ran
against a codebase less than half the current size; using it would flatter
any result.

## Open Questions / Blockers

- **Task 6 being inline is a design choice, not a hedge, and it costs
  measurement clarity.** It is the largest single task in the plan and runs
  last, when context is deepest. The primary bar exists precisely because the
  blended figure cannot separate that from the experiment.
- **No data exists on turn inflation for a cold executing subagent.** Phase
  4b's 6× came from a read-only `security-reviewer` dispatch with no
  write→verify→fix loop. This is the first run that produces the real number.
- **`delegating-plan-tasks` has never dispatched anything.** Its first
  execution is simultaneously the experiment and the skill's own debut, so a
  bad result may indict the skill's wording rather than delegation itself.
  If it fails, read the `DEVIATIONS` and `DEFECTS` returns before concluding
  the lever is wrong.

## Next Step

Close this window. Open a fresh one and run `executing-plans` on
`docs/plans/2026-08-27-phase-5b-ledger.md`. Its Step 1 creates
`phase-5b-ledger` off `dev` and reviews the plan critically before any code;
Tasks 1–5 dispatch per the plan header, Task 6 runs inline.
