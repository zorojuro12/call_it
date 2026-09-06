# 2026-08-26 — ansh — The writing-plans-tuned experiment: verdict

**Status:** Phase 4b is executed and green on `phase-4b-round-lifecycle` (34 commits, `make test`/`lint`/`build` pass, `round` 80.5% / `wager` 84.6% / `ws` 92.6% / `httpapi` 92.1%), **not yet merged into `dev`**. The `writing-plans-tuned` experiment now has both data points and a verdict: **its process claims held, its token claim is falsified.**
**Decided:** The token-reduction hypothesis the tuned skill was built for **does not survive a hard phase**. Phase 4b cost 9.08M tokens/checkpoint against the Phase 2 control's 4.61M — roughly 2× *worse*, on a plan that was 27% *shorter* than Phase 2's. Fold Overrides A and B into `writing-plans` as defaults (they cost nothing and demonstrably preserve discipline), delete the variant, and drop the token-savings claim entirely.
**Spec:** No change.
**Next:** Merge `phase-4b-round-lifecycle` into `dev` (`--no-ff`), then consolidate the two plan skills into one. Note the consolidation is a **merge, not a rename** — see Open Questions.
**Blocked on:** Nothing.
**Touches:** `.claude/skills/writing-plans/SKILL.md`, `.claude/skills/writing-plans-tuned/SKILL.md`, `docs/plans/2026-08-26-phase-4b-round-lifecycle.md` (Measured section)

---

## What We Worked On

Measured Phase 4b's execution against the pre-registered prediction made before 4a ran, closing out the two-phase experiment.

## Decisions Made

- **The tuned format's token claim is falsified; its process claims are confirmed.** Both halves matter and they point different ways — see What Worked / What Didn't.
- **Keep Overrides A and B, drop the marketing.** The 2-step checkpoint (`go test && git commit` chained) and package-scoped test commands cost nothing, and un-batching held at 0 across two structurally different phases. They belong in `writing-plans` as defaults. Override C (the lines/checkpoint budget) is harmless but did not predict cost, so it carries no weight.

## What Worked

- **Un-batching held at zero across both phases.** 4a (transport, self-contained) and 4b (money movement, four-package wiring) both ran every checkpoint's combined implement-verify-commit step as written. Different enough problem shapes that this isn't one lucky case. The `&&` chain also means no commit can land on a red test by construction.
- **Commit granularity improved and stayed improved:** commits/CP of 1.00 (4a) and 1.03 (4b), against 1.12 (Phase 2) and 1.29 (Phase 3). The tuned template preserves the one-checkpoint-one-commit convention better than the untuned one did.
- **Phase 4b's own execution was high quality despite the cost** — 6 real defects/plan gaps found and fixed, including a genuine `round`↔`ws` import cycle, a typed-nil-in-interface panic in `registerWSRoutes`, and rate-limit test cross-contamination that only failed under the full package run. A `security-reviewer` pass found one HIGH (unmapped `wager.ErrRateLimited` falling through to `internal_error`), fixed on the spot.

## What Didn't Work

- **The token savings did not materialize on the harder phase — the primary hypothesis failed.**

  | phase | plan lines | CPs | turns/CP | tools/CP | tok/CP | commits/CP |
  |---|---|---|---|---|---|---|
  | Phase 2 (control) | 1,599 | 25 | 20.6 | 11.4 | **4.61M** | 1.12 |
  | Phase 3 | 2,904 | 38 | 29.4 | 15.7 | **10.35M** | 1.29 |
  | Phase 4a | 899 | 25 | 15.9 | 9.3 | **2.49M** | 1.00 |
  | Phase 4b | 1,161 | 31 | 29.5 | 17.1 | **9.08M** | 1.03 |

  Pre-registered predictions were `turns/CP < 20` and `tok/CP < 4.0M`. 4b came in at 29.5 and 9.08M — **failed both, by a wide margin.**

- **4a's headline result was confounded, exactly as pre-registered.** Its Tasks 1–6 needed no Redis: no Docker round trips, no integration flakiness, one self-contained package. That confounder was worth approximately the entire apparent 2× win. 4b — Redis throughout, four packages wired together — was named in advance as the fairer test, and it is the one that counts.

- **Plan format is not what drives token cost; problem difficulty is.** 4b's plan was *shorter* than Phase 2's and still cost 2× per checkpoint. The tell is `tools/CP` (17.1 vs 11.4): 50% more tool calls per checkpoint is diagnostic work, not template overhead. 4b hit 6 real defects; 4a hit 1. Against its nearest neighbour in difficulty — Phase 3, 38 CPs, also integration-heavy — 4b is 9.08M vs 10.35M, a 12% improvement. Real, but marginal, and nothing like the claim.

- **My measurement methodology was wrong the first time and produced a number I reported.** The original 4a figure (2.53M/CP) bounded the window with a *start* marker only. That session then continued past the phase into a security review, so re-running the same script later returned 74.5M instead of 63.2M for "the same" phase. Fixed by bounding both ends — `/executing-plans` invocation → that phase's own journal commit — and by including `<session>/subagents/*.jsonl`, which carry real cost (4.6M each for Phase 3's and 4b's `security-reviewer` runs) and were being dropped entirely. Every figure in the table above is the corrected measurement; the earlier ones in conversation are superseded. **A session is not a phase — always bound both ends.**

## Test Coverage

- **Covered:** Phase 4b's own suites, all green — `internal/round` 80.5% (a close-out checkpoint added `TestTimerCancelledAfterLock` to clear the 80% floor from 78.1%), `internal/wager` 84.6%, `internal/ws` 92.6%, `internal/httpapi` 92.1%. Verified independently this session on the merged working tree, not taken on report.
- **Not covered yet:** nothing new. `cmd/api` and `cmd/callit-cli` sit at 0% by design (thin wiring, no branching logic).

## Open Questions / Blockers

- **The consolidation is a merge, not a rename.** `writing-plans-tuned` is a *delta* skill — 158 lines that explicitly say "read `.claude/skills/writing-plans/SKILL.md` first, then apply the overrides below." Deleting the base and renaming the delta would leave a skill referencing a file that no longer exists, and would silently drop everything the base carries: Task Right-Sizing, Bite-Sized Task Granularity, the observable-signal rule, No Placeholders, Self-Review, Where a Plan Stops, Execution Handoff. The correct move is to apply Overrides A–C into the 297-line base, producing one standalone file, then delete the variant.
- **The tuned skill is smaller than the base (158 vs 297 lines)**, so the "is it bigger" premise for a dead-comment sweep does not hold for it. A staleness pass over the *base* is still worth doing while it is being edited.
- **Phase 4b is unmerged.** Merging it satisfies parent plan §12's first acceptance box — "Phases 0–4 complete, producing an end-to-end playable round" — which the close-out already marked, with Task 10 CP1's end-to-end test as the evidence.

## Relevant Commits

- `e1c65ad` — Merge branch 'phase-4a-ws-transport' into dev
- `bc6e510` — docs: verify Phase 4b's plan against 4a's landed interfaces, no revision needed
- `2a93899` — revert: remove baseline docs mistakenly committed into the skill directory
- `1259d37` — docs: close out Phase 4b and the MVP acceptance criteria
- `1e89da2` — docs: journal Phase 4b round-lifecycle execution session

## Next Step

Merge 4b into `dev` with `--no-ff`, verifying green on the merged result before anything else. Then consolidate the plan skills per Open Questions — apply the overrides into the base, delete the variant, and remove the token-savings framing from the surviving text, since it is now measured to be false.
