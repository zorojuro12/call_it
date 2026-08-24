# 2026-08-23 — ansh — Workflow tooling: planning skills, git granularity, tooling tradeoffs

**Status:** Phase 0 is done and merged to `dev`. This session was pure workflow/tooling work, no product code — the plan-execution pipeline (`writing-plans` → `executing-plans`) and the git commit granularity for phases are now settled and committed. Next real step is `CLAUDE.md`, then Phase 1.
**Decided:** Adopted `writing-plans` + `executing-plans` (from superpowers) as the default execution path for phases/features, with `subagent-driven-development` and `continuous-learning-v2` explicitly declined for now — see `docs/dev-workflow-guide.md` §9 for the full reasoning and revisit conditions on both.
**Spec:** No change to the design spec. `docs/dev-workflow-guide.md` changed substantially (see below).
**Next:** Write `CLAUDE.md` — run `/project-init` for a verified command scaffold, then layer in the "why" content (invariants, rejected alternatives, gotchas) from the spec, plan, and journal. Include the branch-per-phase + checkpoint-commit convention explicitly, since `CLAUDE.md` is the only doc that's always loaded.
**Blocked on:** Nothing.
**Touches:** `.claude/skills/writing-plans/`, `.claude/skills/executing-plans/`, `.claude/commands/impl-plan.md`, `.claude/commands/project-init.md`, `docs/dev-workflow-guide.md`, `docs/plans/2026-08-21-implementation-plan.md`

---

## What We Worked On

Last session ended right after Phase 0 landed as a single commit directly on
`dev` — which prompted a full pass on the plan-execution and git-workflow
layers before touching Phase 1. Also renamed `/plan` to `/impl-plan` (collision
with Claude Code's built-in Plan Mode) and copied `/project-init` into the
project, both landed before the deeper workflow discussion below.

## Decisions Made

- **Branch per phase, self-merge, no PR** — see `docs/dev-workflow-guide.md`
  §8. Confirmed via `AskUserQuestion`.
- **`writing-plans` + `executing-plans` (superpowers) as the default execution
  path**, adapted: plans save to `docs/plans/`, `subagent-driven-development`
  references dropped, `using-git-worktrees` demoted from required to optional,
  `dev` added alongside `main` as a protected branch. `orch-*` commands kept
  for their narrower shapes (`orch-fix-defect` for bugs, `orch-refine-code`
  for refactors) — see `dev-workflow-guide.md` §4.
- **`subagent-driven-development` declined** — the context-pollution problem
  it solves wasn't demonstrated (Phase 0 was 464 lines/10 files; the one
  compaction this project has hit came from workflow discussion, not
  implementation). Branch-per-phase + journal + fresh session already resets
  context between phases for free. Full reasoning and revisit trigger (Phase 5
  or 6 genuinely exhausting context) in `dev-workflow-guide.md` §9.
- **`continuous-learning-v2`/instincts declined** — currently dormant anyway
  (`observer.enabled: false`, no hooks wired). Redundant with memory + journal,
  curated by statistics rather than judgment, and SessionStart injection is a
  standing tax on every session regardless of relevance. Revisit if juggling
  multiple concurrent projects — full reasoning in `dev-workflow-guide.md` §9.
- **Commit granularity fix: checkpoints within a task.** `writing-plans`
  originally forced exactly one commit per task regardless of how many
  distinct behaviors it covered — would've meant one commit per feature per
  phase, not a real dev's rhythm. Added a "Checkpoint" layer inside the Task
  Structure template: a task can contain multiple checkpoints, each its own
  test→implement→verify→commit cycle. Branch scope unchanged (still one
  branch per phase) — this only changes commit granularity within it.

## What Didn't Work

- Restoring the deleted local `journal` `SKILL.md` via `git checkout HEAD --`
  lost real content — a fuller, uncommitted 181-line version (listing action,
  staleness check, `Touches` verification, "What Didn't Work" exception) had
  existed on disk before an unstaged deletion, but was never committed. The
  restore pulled back an older 147-line committed version instead. Recovered
  by manually re-merging the missing sections back in from the parallel
  `~/.claude/skills/journal-global/` copy, which still had them. Lesson: check
  `git diff`/file dates before assuming `git checkout HEAD --` is a safe
  restore for an unstaged deletion — it silently reverts to the last commit,
  not the pre-deletion state, if the two differ.

## Open Questions / Blockers

- None blocking. The multi-checkpoint template change (`ad1027a`) is untested
  in practice — Phase 1 is the first real exercise of `writing-plans` →
  `executing-plans` end to end, including whether checkpoint boundaries land
  where expected or need further adjustment.

## Relevant Commits

- `67a72f0` — docs: codify branch-per-phase git workflow, no PR ceremony
- `5d9d955` — chore: add writing-plans/executing-plans, document declined tooling
- `ad1027a` — feat: allow multi-checkpoint tasks in writing-plans for solo commit rhythm

## Next Step

`CLAUDE.md`: run `/project-init` first for the command-verified scaffold
(build/test/lint/dev commands actually run this session, not invented), then
hand-write the invariants/rationale layer on top — `internal/domain` stays
I/O-free, the outbox amendment and why, library choices vs. their
alternatives, the WSL2/Kafka resource caveat, and the branch-per-phase +
checkpoint-commit convention stated as a rule, not a description. Self-test
against whether it would have prevented Phase 0's single-commit landing.
