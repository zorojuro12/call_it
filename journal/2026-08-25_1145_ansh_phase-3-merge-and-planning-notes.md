# 2026-08-25 — ansh — Phase 3 merge, and a planning-notes correction

**Status:** Phase 3 is merged into `dev` and pushed — `call_it` is caught up through auth + REST. Separately, a process lesson from Phase 3's retrospective went through a real placement correction this session: first added directly into the `writing-plans` skill (both the project-local and portable copies), then reverted at the user's correction and moved to a new standalone doc in the `claude-skills` library instead.
**Decided:** Skill bodies are always-loaded on every invocation; a one-time calibration lesson with a specific incident citation doesn't belong there even if it qualifies as a "rule" by the library's existing rule-vs-value test. The missing second axis is *frequency of relevance* — read-once-per-project guidance belongs in a separate reference doc, not a skill's runtime instructions.
**Spec:** No change.
**Next:** Phase 4 (WebSocket hub + round lifecycle) gets its own `writing-plans` pass whenever the user starts it — one deliverable, so it's a natural test of whether the phase-sizing lesson (see below) actually holds without needing to be applied deliberately.
**Blocked on:** Nothing.
**Touches:** `docs/plans/2026-08-21-implementation-plan.md` §9 (call_it, kept), `.claude/skills/writing-plans/SKILL.md` (call_it, change reverted), `~/projects/claude-skills/writing-plans/SKILL.md` (change reverted), `~/projects/claude-skills/PLANNING-NOTES.md` (new), `~/projects/claude-skills/README.md`

---

## What We Worked On

Two things, in sequence, both continuing from the Phase 3 execution session earlier today (see `journal/2026-08-25_0344_ansh_phase-3-auth-rest-execution.md`):

1. **Pushed and merged.** Pushed `dev` and the new `phase-3-auth-rest` branch to `origin` on request. Then, on a follow-up request, ran `finishing-a-development-branch` again — merged `phase-3-auth-rest` into `dev` with `--no-ff`, verified `make test && make lint && make build` green on the merged result before pushing, per the skill's own gate. Left `phase-3-auth-rest` in place on both local and `origin` rather than deleting it, matching the Phase 2 precedent of keeping merged branches around.
2. **A retrospective conversation about plan size and phase scoping**, prompted by the user noting Phase 3's plan was still ~2,900 lines and took over an hour to execute despite the spec-driven format adopted after Phase 1. Landed on: the format itself held up fine (~76 lines/checkpoint vs. Phase 2's ~64, not the near-doubling the raw totals suggest) — the real cause was Phase 3 bundling four separable deliverables (credentials, tokens, the rate limiter, the REST surface) into one phase, which the plan's own self-review had already flagged as splittable ("Tasks 1–10 are a coherent stopping point") without that being acted on.
3. **Documenting that lesson, twice.** First pass: added it to `docs/plans/2026-08-21-implementation-plan.md` §9 (kept — call_it-specific, read when scoping call_it's own future phases) and saved a `feedback` memory. The user then asked specifically about carrying this into *new* projects, which the parent plan can't do (it's this project's own doc). Second pass: added the lesson directly into `writing-plans`' "Scope Check" section, in both the project-local copy and the portable library copy at `~/projects/claude-skills/`, reasoning it passed the library's existing "did you invent a rule, or set a value?" test. The user corrected this — see What Didn't Work.

## Decisions Made

- **Keep `phase-3-auth-rest` after merging**, don't delete — same call as Phase 2, not re-litigated.
- **Phase-sizing lesson lives in `~/projects/claude-skills/PLANNING-NOTES.md`, not in the `writing-plans` skill body.** New file, linked from the library's `README.md` under a "Planning notes" section. Framed explicitly as read-once-per-new-project content, distinct from the skills table above it.
- **The library's rule-vs-value test needs a second axis: frequency of relevance, not just "is this a rule."** A skill body is read on every invocation; content that's only load-bearing at one rare decision point (starting a new project) doesn't belong there even if it's genuinely a portable rule. Recorded as its own memory (`feedback_skill_body_vs_reference_doc`) since it's a distinct, recurring judgment call, separate from the phase-sizing content itself.

## What Worked

- Merge went clean, no conflicts (59 files, all Phase 3 work landing on `dev` in one shot). Verified green from the merged tree before pushing, not just trusting the pre-merge branch state.
- `git revert --no-edit` cleanly undid both pushed skill-body commits (call_it and claude-skills) without touching history — appropriate since both had already reached `origin`, so a reset/force-push would have been the wrong tool.

## What Didn't Work

- **Adding the phase-sizing lesson directly into `writing-plans`' Scope Check section** (both the project-local and portable copies) — reverted at the user's correction. The library's existing "did you invent a rule, or set a value?" test said this belonged in the skill, and by that test alone it did — it's methodology, not a project-specific value. But that test only checks *portability*, not *invocation cost*: a skill body is re-read into context on every single use of the skill, and this lesson is only relevant once, when a new project's phase list is first being drafted. Baking a war-story citation into runtime instructions that get read on every plan (most of which have nothing to do with phase-list scoping at all) is the wrong tradeoff. Moved to a standalone doc instead, read situationally rather than injected automatically.

## Relevant Commits

`call_it` (`dev`):
- `aaa9872` — Merge branch 'phase-3-auth-rest' into dev
- `f4c2160` — docs: record phase-sizing recommendation from Phase 3's 12-task bundle (kept, in the parent plan)
- `1d67f54` — docs: strengthen writing-plans' scope check with the Phase 3 phase-sizing lesson (reverted)
- `acb740a` — Revert of the above

`claude-skills` (`main`):
- `1a2a41a` — docs: strengthen scope check to catch theme-bundled phases (reverted)
- `488721f` — Revert of the above
- `e743c62` — docs: add planning notes on phase sizing (`PLANNING-NOTES.md` + README pointer)

## Next Step

Nothing queued. Next real work is Phase 4's `writing-plans` pass, whenever the user starts it — no new tooling to install first (plan §9), and everything Phase 4 needs from Phase 3 (room/account services, the rate limiter, the token issuer) already exists and is tested.
