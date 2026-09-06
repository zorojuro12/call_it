# 2026-08-26 — ansh — Phase 5a token measurement: 5.77M/CP, and the 5x subagent result replicates

**Status:** Phase 5a measured at **5.77M tok/CP**. `phase_compare.py` — referenced by three prior journals but never actually committed — now exists at `scripts/phase_compare.py`. Branch `phase-5a-outbox-kafka` still unmerged, 28 commits, suite green.
**Decided:** 5a is a **control, not a test** — delegation was deferred, so the pre-registered `< 4.6M` bar never applied to it. It becomes the inline baseline any future delegated phase is scored against.
**Spec:** No change.
**Next:** Decide whether 5b runs delegated. The measurement now supports that decision with a same-codebase baseline instead of a cross-phase guess.
**Blocked on:** Nothing.
**Touches:** `scripts/phase_compare.py`, `journal/2026-08-26_1700_ansh_phase-5a-execution.md`, `docs/project-history.md`

---

## The number

Bounded from the `/executing-plans` invocation (`2026-08-26T23:13:05Z`) to the
close-out commit `a127858` (`00:08:12Z`), subagent log included:

| | turns | tokens | avg ctx |
|---|---|---|---|
| main session | 561 | 140,749,292 | 250,388 |
| subagent (`security-reviewer`) | 69 | 3,449,234 | 49,988 |
| **total** | **630** | **144,198,526** | |

**5.77M tok/CP** over 25 checkpoints. `turns/CP` 22.4. `cache_read` was **99.5%**
of main volume — matching the cost model's stated prediction exactly, which is
the strongest available check that the methodology is measuring the right thing.

Where it sits: Phase 4a 2.49M · Phase 2 **4.61M** · **Phase 5a 5.77M** ·
Phase 4b 9.08M · Phase 3 10.35M.

## Decisions Made

- **5a is a control, not a failed experiment.** The `< 4.6M` bar was
  pre-registered for a *delegated* 5a. Delegation was deferred and 5a ran
  inline, so scoring it against that bar would be measuring a run the bar was
  never written for. Reporting it as "FAIL" would be a category error, and a
  convenient one to fall into since the arithmetic works either way.
- **The right reading is the +25% over Phase 2, and why.** `turns/CP` was
  22.4 against Phase 2's 20.6 — only 9% more turns. The cost gap comes almost
  entirely from **per-turn context**, which is now larger: bigger codebase,
  longer plan, more rules. Execution discipline was not the driver. This is the
  cost model doing what it claims: cost tracks `turns x context`, and on a
  growing codebase the second term rises even when the first holds steady.

## What Worked

- **The subagent counterfactual replicated, independently.** The
  `security-reviewer` dispatch ran 69 turns at 49,988 avg context for 3.45M.
  The same 69 turns inline at this session's 250,388 avg would have cost
  **17.3M — a 5.0x difference.** Phase 4b's natural experiment measured ~6x on
  a different phase, a different reviewer run, and a different codebase size.
  Two independent replications now support the premise delegation rests on.
  That premise is no longer the uncertain part.
- **Bounding both ends mattered, measurably.** The unbounded session reads
  6.24M/CP; bounding removes 11.8M, 8% of the session. Smaller than Phase 4a's
  distortion (which was large enough to produce a wrong published figure) but
  the same failure mode, and it would have inflated this number by 8% silently.

## What Didn't Work

- **`phase_compare.py` did not exist.** Three journal entries
  (`0250_tuned-plan-experiment-verdict`, `1404_subagent-delegation-proposal`,
  `1700_phase-5a-execution`) all referenced it as an existing tool — one even
  planned around its behavior ("already bounds both ends and already sums
  `<session>/subagents/*.jsonl`"). `find` across the repo and home directory
  returned nothing. The methodology was living entirely in prose and being
  re-derived from it each time, which is exactly how Phase 4a's measurement
  went wrong in the first place. Now committed, with both rules encoded rather
  than described: bound both ends, include the subagent logs.

## Open Questions / Blockers

- **Does 5b run delegated?** 5b is the double-entry ledger and the
  reconciliation test — the flagship correctness work, and previously argued as
  the *wrong* place to run a process experiment. That argument still holds. But
  the counter is now stronger than it was: the 5x premise has two independent
  replications, and 5b's per-turn context will be larger still, since it
  inherits everything 5a added.
- **A delegated phase would be measured against 5.77M, not 4.61M.** Phase 2 is
  the wrong control for anything from here on — it ran against a codebase less
  than half the current size. Same-codebase baseline is what 5a now provides,
  and it is the only honest comparator for 5b.
- **One caveat on the subagent counterfactual, stated so it isn't over-trusted:**
  it assumes those 69 turns would have taken the same count inline. A security
  review is read-only and self-contained; execution work has write-verify-fix
  loops. Turn inflation under delegation remains unmeasured — still the main way
  the 2-4x estimate could be wrong.

## Relevant Commits

- `9d6a6af` — reclassify kafka-go as a direct dependency (landed on the branch during this session's verification)
