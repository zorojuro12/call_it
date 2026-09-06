# 2026-08-25 — ansh — Portable skills library + observable-signal checkpoint rule

**Status:** No phase code written — this was a workflow/tooling session. Phase 2 remains the last code landed; Phase 3 has not started. Three things shipped: the Phase 2 open question was resolved into a concrete `writing-plans` rule, the portable-vs-project-specific split across all 22 installed skills was settled and documented, and a new library repo (`~/projects/claude-skills` → `github.com/zorojuro12/claude-skills`) now holds the one skill worth carrying between projects. `call_it` `dev` pushed to `origin/dev` (3 commits, incl. the previously-unpushed `d5a2b2c`).
**Decided:** Adapted skills worth reusing live in a **git library repo you install *from***, not a live-loaded `~/.claude/skills/` directory — same copy-and-adapt model ECC and superpowers already use. See `docs/dev-workflow-guide.md` §9 "Carrying adapted skills to other projects" for the full reasoning.
**Spec:** No change — `docs/specs/2026-08-21-callit-design.md` untouched. Changes were to `.claude/skills/writing-plans/SKILL.md`, `docs/dev-workflow-guide.md` §9, and `CLAUDE.md`'s tooling status line.
**Next:** Phase 3 (Auth + REST — register/login, room creation, join-by-code, JWT issuance, rate-limit middleware) gets its `writing-plans` pass. `api-design` is installed; the new observable-signal rule gets its first real trial here.
**Blocked on:** Nothing. (`gh` is not installed on this machine — not blocking, but it means repo/PR/issue operations need `sudo apt install gh` from an interactive shell first.)
**Touches:** `.claude/skills/writing-plans/SKILL.md`, `docs/dev-workflow-guide.md` §9, `CLAUDE.md`, `~/projects/claude-skills/` (outside this repo)

---

## What We Worked On

Started as a journal catch-up, turned into resolving the open question Phase 2 left behind, then into a broader question: which of this project's skill adaptations are actually portable, and how to carry them forward without re-deriving them each time.

Three threads, in order:

1. **The Phase 2 open question, answered.** `lock_round.lua`'s `ALREADY_LOCKED` case couldn't RED because the Go wrapper's return type never exposed the distinction — a different failure mode from Phase 1's (where an earlier checkpoint's implementation already satisfied a later checkpoint's test). Phase 1's problem is fixable by reordering; this one isn't fixable at all, because the checkpoint is unfalsifiable by construction. Now written into `writing-plans` as **"Name the observable signal, at the interface the test actually calls,"** with two prescribed exits (merge into a neighbor with a real delta, or make "extend the interface to surface this case" its own earlier checkpoint) and a fourth Self-Review check.

2. **Auditing what's actually been adapted.** Diffed all 22 skills in `.claude/skills/` against their upstreams. Result was much narrower than expected — see Decisions Made.

3. **Building the library and documenting the split.** New repo, README explaining the model and what it deliberately excludes, provenance sections pointing both directions, and a `dev-workflow-guide.md` §9 subsection recording the decision.

## Decisions Made

- **Library repo, not global live-load** — `~/projects/claude-skills/`, installed *from* per project. Rejected `~/.claude/skills/` live-loading for two reasons: a live-loaded global copy needs an awkward `-global` suffix to stay distinguishable from the project copy in a flat skill listing (the reason `journal-global` is named that way), and an auto-loaded adapted skill can carry one project's assumptions into an unrelated repo with no explicit step where you'd notice. Full reasoning in `docs/dev-workflow-guide.md` §9.
- **The portability test: "did I invent a rule, or set a value?"** Rules travel; values don't. This is what made the audit decisive rather than a judgment call per skill.
- **Only 1 of 22 skills is worth carrying.** 18 are byte-identical to their sources (14 ECC, 4 superpowers) — reinstall, don't vendor. `executing-plans` (+18 lines) and `brainstorming` (+2) are pure config (`dev` branch, no-PR, `--no-ff`, skill path prefixes, spec directory path) — zero transferable methodology. `journal` was already generalized to `journal-global`. That leaves `writing-plans` (+130 lines) as the entire portable asset.
- **The spec-driven-vs-code-driven format is stated in the library as a fork, not a default.** It's contingent on execution mode — subagent-driven execution genuinely wants pre-written code, inline genuinely doesn't. Baking this project's answer into the portable copy would export a decision that isn't universal.
- **`journal-global` stays live-loaded in `~/.claude/skills/`.** It's genuinely project-agnostic — nothing in it encodes CallIt — so it's the one case where live-loading costs nothing. This is exactly the "revisit when" condition in §9's tradeoff table.
- **Local branch renamed `master` → `main`** in `claude-skills` before first push; `git init` defaulted to `master` here, GitHub expects `main`.

## What Worked

- Diffing every skill against its upstream source (`~/projects/superpowers/skills/`, `~/.claude/plugins/marketplaces/ecc/`) rather than reasoning about which *felt* adapted — turned a vague "port my workflow forward" problem into "it's one file." The audit is what made the rest of the session short.
- `journal-global` as an existing precedent — the project had already solved global-vs-project-copy once, which surfaced *why* the `-global` suffix exists and therefore how to avoid needing it.
- Both pushes: `call_it` `40e10b7..ed537c9` → `origin/dev`; `claude-skills` `main` → new `origin/main` with upstream tracking set.

## What Didn't Work

- **Proposed porting the new rules back into `~/projects/superpowers/` — wrong, and retracted mid-session.** That checkout is a *pristine clone of `obra/superpowers`* (clean, tracking `origin/main`), not a personal fork. Edits there get clobbered by the next `git pull` and can't be pushed (no write access to someone else's repo). It is a read-only reference. This is now stated in both the library README and `writing-plans`' provenance section so it doesn't get re-suggested.
- **`gh` is not installed** — checked `PATH`, `~/.local/bin`, `/usr/bin`, `/usr/local/bin`. Meant the GitHub repo had to be created by hand rather than via `gh repo create`. `sudo apt install gh` needs an interactive shell (same `sudo`-can't-read-a-password gotcha as the Go install — see CLAUDE.md Known Environment Gotchas).

## Test Coverage

- **Covered:** Nothing new — no application code changed this session. Coverage is unchanged from Phase 2: `internal/config` 100%, `internal/domain` 100%, `internal/httpapi` 85.7%, `internal/redisstore` 87.2%, `cmd/api` 0% (expected). No test run was needed and none was performed.
- **Not covered yet:** The library repo has no CI and no tests — it is prose, and its only correctness check is being used. The observable-signal rule is **unvalidated**: it was derived from one real incident but has never been applied while writing a plan. Phase 3 is its first trial; if it proves awkward in practice, fix it in `call_it` first and only promote the fix to the library once it's earned.

## Open Questions / Blockers

- Does Claude Code **shadow** same-named skills (project-local wins, global hidden) or list both ambiguously? Unverified — the skill listing is injected at session start, so testing it needs a throwaway skill and a fresh session. Not blocking under the library model (names never collide, since nothing adapted is live-loaded globally), but it's the deciding fact if a same-name global cascade is ever wanted instead.
- Phase 2's original open question is now **closed** — no longer carry it forward.

## Relevant Commits

- `d9edfde` — docs: mark api-design as installed in the tooling status (pre-existing uncommitted `CLAUDE.md` edit, committed separately to keep history clean)
- `ed537c9` — docs: add observable-signal checkpoint rule and record the portable-skills split
- `91f3172` — feat: seed skills library with adapted writing-plans (in `~/projects/claude-skills`, not this repo)

## Next Step

Phase 3's `writing-plans` pass. Two things to carry into it: the new observable-signal rule applies directly (Phase 3 wraps Phase 2's Redis writers behind `internal/room`/`internal/round`, which is exactly the layered-wrapper shape where unobservable checkpoints appear), and per CLAUDE.md the phase branches off `dev` as `phase-3-<slug>`.
