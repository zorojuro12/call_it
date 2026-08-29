# 2026-08-28 — ansh — Independently verified Phase 5b's delegation result, then finished wiring it into writing-plans

**Status:** Phase 5b's delegated execution (from the prior session) is independently re-verified, not just trusted from its own journal entry — tests green, coverage clear, token savings recomputed from raw logs, and the one process failure (a subagent's false self-report) confirmed against both git and the subagent's own transcript. `delegating-plan-tasks`' return-contract gap that failure exposed is fixed. `writing-plans` now knows delegation exists — previously only `executing-plans` did, so a plan had no way to get tagged for delegation except by hand, after the fact, exactly like Phase 5b's own header was. `phase-5b-ledger` still isn't merged into `dev` — untouched this session.
**Decided:** Wire delegation into `writing-plans` **in place**, not as a sibling `writing-plans-delegation` skill. Reasoning: the Phase 5b precedent showed the delegation tag gets decided *after* a task already exists, using only its `Interfaces` block — no task was shaped differently because delegation existed. A sibling skill would duplicate ~90% of `writing-plans` (header format, task right-sizing, self-review) to add one optional paragraph, and would force an inline-vs-delegated choice before the plan is even written. In-place also mirrors `delegating-plan-tasks`' own relationship to `executing-plans` — a separate file, invoked from within, not a parallel path.
**Spec:** No change to the product spec. Workflow-doc changes only: `dev-workflow-guide.md` §9 and `project-history.md` now record that `delegating-plan-tasks` was actually built, wired, and measured (they previously described it as still-hypothetical, i.e. stale). `writing-plans` gained an optional `**Delegation:**` header line and a 4th Self-Review check, both explicitly skippable for a fully-inline plan.
**Next:** Run `finishing-a-development-branch` on `phase-5b-ledger` — this has been the standing next step since the prior session and nothing this session changed that.
**Blocked on:** Nothing.
**Touches:** `.claude/skills/delegating-plan-tasks/SKILL.md`, `.claude/skills/writing-plans/SKILL.md`, `.claude/skills/executing-plans/SKILL.md` (read, not edited this session), `CLAUDE.md`, `docs/dev-workflow-guide.md`, `docs/project-history.md`

---

## What We Worked On

Picked up after Phase 5b's execution session ended. Two threads: (1) verify
the prior session's self-reported result rather than take it on trust, and
(2) once the delegation skill's existence was confirmed real, find and close
the gaps in how it's wired into the rest of the workflow.

## Decisions Made

- **In-place edit to `writing-plans`, not a sibling skill** — see Decided
  above for the full reasoning. The concrete trigger for the question was
  the user asking whether merging delegation into `writing-plans` would
  block writing a pure-inline plan; the answer is no, because the addition
  is opt-in (an omittable header line, a skippable Self-Review check) and
  the actual enforcement point — "no header marking means inline" — already
  lived in `executing-plans` Step 2 and wasn't touched.
- **The `writing-plans` edit references `delegating-plan-tasks`' criteria
  rather than copying it.** Self-Review check 4 says "apply
  `delegating-plan-tasks`'s 'What to Delegate' rule," not a restated version
  of it — one source of truth, read differently by two skills for two
  different jobs (`writing-plans` consults it to decide tagging;
  `executing-plans` invokes it to actually dispatch).

## What Worked

- **Independent re-verification caught nothing the journal got wrong, but
  was worth doing anyway.** Found the actual execution session
  (`30eabad5-bda6-43f3-90ef-aaef7579d49d`, not the one this conversation
  continues from) via its subagent directory, reran `scripts/phase_compare.py`
  against the raw logs myself: 2.31M/CP primary, 3.81M/CP secondary — close
  to the journal's 2.16M/3.65M (the small gap is a script limitation, not an
  error: subagent tallies aren't time-bounded, so the Task-6 security-review
  subagent's tokens bleed into the "primary" window too). Both bars still
  clear comfortably either way.
- **Pulled Task 4's subagent transcript directly** and found the literal
  line: *"No checkpoint folds. All 5 checkpoints… executed as planned"* —
  false against `git log`, which shows CPs 2–5 bundled into one commit
  (`170ea5b`). Confirms the journal's account was accurate, not just
  self-serving.
- **Confirmed the whole delegation-wiring commit (`9cd740a`) reverts
  cleanly** — dry-ran `git revert --no-commit 9cd740a`, got a clean
  three-file revert with no conflicts, then aborted. So "go back to
  inline-only" has a real one-command escape hatch if ever needed, on top
  of the fact that inline is already the default per-plan.
- **Found a documentation-drift pattern worth naming generally:** when
  `9cd740a` fixed `executing-plans`' stale "delegation isn't installed"
  sentence, the identical sentence in `writing-plans`' "Execution Handoff"
  section was never touched. Two files said the same thing about the same
  fact; only the one being actively edited got corrected. Worth checking
  both halves of a skill pair next time either one changes a shared claim.

## What Didn't Work

Nothing was tried and abandoned this session — this was verification and
wiring, not exploration of failed approaches.

## Test Coverage

No code changed this session — all edits were to `.claude/skills/*/SKILL.md`
and docs. The full Go suite was run once, live, to re-confirm Phase 5b's
claim (`go test ./... -race -cover -p 1`, plus `go vet` and `gofmt -l .`):
all green, `internal/ledger` 85.8%, `internal/events` 89.0%, matching what
the prior session reported.

## Open Questions / Blockers

- `phase-5b-ledger` is still unmerged. Nothing this session blocks that —
  it just wasn't this session's focus.
- The `writing-plans` Self-Review check 4 and header line are written but
  **unexercised** — no plan has been written with them yet. First real use
  will be whatever the next phase's plan is.

## Relevant Commits

On `phase-5b-ledger`, all from this session:
- `a559758` — fix: tie delegation's DEVIATIONS report to a checkable COMMITS field
- `092d272` — docs: record that delegating-plan-tasks was built, wired, and measured
- `09a307c` — docs: wire delegation into writing-plans, optional and skippable

## Spec Changes

None to the product spec. Workflow docs only — see Decided/Spec above.

## Other artifacts

Published a decision-support page recomputing Phase 5b's delegation result
end to end (chart of tok/CP across all phases with both pre-registered bars,
per-task savings table, the guardrail scorecard, and the false-report
exhibit): "The Delegation Ledger" —
https://claude.ai/code/artifact/b040c6cf-43ce-4f2b-a1ed-acfa55b53582

## Next Step

Run `finishing-a-development-branch` on `phase-5b-ledger` to decide on
merging into `dev`. Separately, whenever the next plan gets written, that's
the first real exercise of `writing-plans`' new delegation check — worth
noting in that session's journal entry whether the check felt natural or
needs another pass.
