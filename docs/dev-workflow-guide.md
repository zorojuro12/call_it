# CallIt — Dev Workflow Guide: What to Reach For, When

**Date:** 2026-08-21
**Purpose:** Map each stage of CallIt's development to the specific skill,
agent, or command that fits it, using their real invocable names. Companion
to [`docs/specs/2026-08-21-callit-design.md`](specs/2026-08-21-callit-design.md)
(the living design spec) and `/home/chikara/projects/ecc-survey.md` (the
broader ECC bucket-tier survey this guide draws its Bucket 1/2/3 picks from).

This is a lookup table by situation, not a rulebook to follow start to
finish in order — jump to the section matching what you're about to do.

---

## 1. Design & spec work

| Situation | Use |
|---|---|
| Starting a new feature/subsystem design from scratch (e.g. the WebSocket hub, the Redis Lua schema) | `brainstorming` skill — one invocation per independent design unit, runs explore → clarify → propose → spec → self-review → hand off to `writing-plans` |
| Multiple independent subsystems bundled in one request | `brainstorming` decomposes first, then runs the flow once per sub-project — don't force one pass to cover all of them |
| Recording *why* a specific architectural choice was made (e.g. "why Kafka over Redpanda") | `architecture-decision-records` skill — durable ADR under `docs/decisions/NNNN-*.md`, separate from the spec (spec = current state, ADR = historical rationale) |
| Need a second opinion / trade-off analysis on a design decision | `architect` agent |

## 2. Turning an approved spec into an implementation plan

**Two layers, don't confuse them.** `/impl-plan` is the *architectural* layer:
run once per project (or per major subsystem) to resolve open design questions
and produce the phase table. `writing-plans` is the *execution* layer: run once
per phase (or per standalone feature), right before that phase's branch starts,
to break one phase into numbered tasks with exact files, interfaces, and
test→implement→verify→commit steps. `/impl-plan` was already run for CallIt —
its output is `docs/plans/2026-08-21-implementation-plan.md`. Don't re-run it
per phase.

| Situation | Use |
|---|---|
| Spec is approved (✅ done — `docs/specs/2026-08-21-callit-design.md`), need a concrete build plan for one of the §9 open items (Redis schema, WS hub internals, Kafka partitioning, Postgres schema, repo layout) | `/impl-plan` command — restates requirements, assesses risk, produces step-by-step plan, **waits for CONFIRM before touching code** (renamed from `/plan` to avoid colliding with Claude Code's built-in Plan Mode) |
| About to start a phase from the plan's §9 table, need it broken into committable tasks | `writing-plans` skill → saves to `docs/plans/YYYY-MM-DD-<name>.md` → hands off to `executing-plans` |
| Building a standalone feature that was never one of the 9 phases (post-MVP additions) | `writing-plans` skill directly — no need to route through `/impl-plan` unless it raises genuinely new architectural questions |
| Feature/refactor plan needs deeper multi-file architectural reasoning first | `planner` agent, or `code-architect` agent if there's existing code to pattern-match against (N/A yet — repo is pre-code) |
| Deciding Go package/folder layout specifically | `planner` agent, cross-checked against the Go microservice reference layout at `/home/chikara/projects/ECC/examples/go-microservice-CLAUDE.md` |

### 2a. The two-model loop: plan in Opus, execute in Sonnet

**Adopted 2026-08-23, starting with Phase 1.** Planning and execution run in
*separate Claude Code windows* on different models:

| Window | Model | Does |
|---|---|---|
| Planning | Opus | `writing-plans` — resolves open questions, argues amendments against the spec, produces the phase plan |
| Executing | Sonnet | `executing-plans` — works the plan task by task, commits per checkpoint |

**Why the split.** `.claude/rules/ecc/common/performance.md` puts Sonnet on main
development work and reserves Opus for architecture and deep reasoning. A plan
that has done its job leaves no design to derive — so the expensive model earns
its keep during planning, and the cheaper one does the typing.

**The executing window needs no conversation history.** This is the property
that makes the split work, and it's a constraint on the *plan*, not a hope about
the executor: `CLAUDE.md` auto-loads in any session in this repo, and everything
else the executor needs must be written into the plan itself — Global
Constraints, environment gotchas, and any amendment the plan makes to the parent
plan or spec. If a plan can only be executed by someone who watched it being
written, it isn't finished.

**Mechanics:**

1. **Commit the plan to `dev` before handing off.** The executing window's first
   act is `git checkout -b <slug> dev`, and an untracked plan file follows onto
   the feature branch — landing the plan's own creation in the phase's history.
   Sweep up any other stray files (a previous session's journal entry) at the
   same time.
2. Point the Sonnet window at the plan path. **Don't create the branch by hand** —
   `executing-plans` Step 1 does it.
3. Expect it to raise questions before starting; its Step 1 requires a critical
   review pass. That's the skill working, not a defect in the plan.
4. **Only one window edits `.claude/skills/` at a time.** Both sessions can reach
   the same files, and `ListAgents` won't necessarily see the other one. On
   2026-08-23 an executing window rewrote `writing-plans` (`ab190b9`) while a
   planning window was mid-review of the same file.

**Watch item:** a long plan costs real context on Sonnet. Tasks are independent
in execution order, so a degraded session can be resumed in a fresh window —
checkbox state in the plan plus `git log` shows exactly where it stopped.

## 3. Setting project-level conventions

| Situation | Use |
|---|---|
| Writing CallIt's `CLAUDE.md` | Do this **after Phase 0** exists (real code to pattern-match, not the spec/plan alone). Run `/project-init` first for a verified, command-checked scaffold (build/test/lint/dev commands, detected stack) — never invent commands. Then layer in the "why" content manually: non-obvious invariants, rejected alternatives, gotchas from `docs/specs/2026-08-21-callit-design.md`, `docs/plans/2026-08-21-implementation-plan.md`, and `journal/` entries. The built-in `init` skill or `codebase-onboarding` are generic fallbacks if `/project-init`'s ECC-checkout dependency (`/home/chikara/projects/ECC`) isn't available, but produce less project-relevant output since they don't know this project's plan/spec history |
| Installing stack-specific rule packs (Go idioms, Postgres, etc.) | Copy from `/home/chikara/projects/ECC/rules/golang`, `.../postgres` etc. into `call_it/.claude/rules/ecc/` (project-local, per the ecc-survey's rules section). **Stagger these per-phase** (see the "Tooling to import" column in the plan's §9 phase table) — rule dirs are always-loaded full text into every turn once installed, unlike skills, so importing a phase's rules only right before that phase starts avoids irrelevant standing context |
| Pulling in stack-specific skills | Bucket 3 picks per `ecc-survey.md`: `redis-patterns`, `postgres-patterns`, `golang-*` (patterns/testing/tdd/verification), `react-patterns`/`nextjs-turbopack`, `database-migrations` — install project-locally into `call_it/.claude/skills/`, not globally. Unlike rule dirs, skills are cheap (one line in the availability listing until invoked), so it's fine to pull in the full Bucket 2/3 list for the whole project now rather than staggering |
| Generating an onboarding/install plan for this project's ECC surface | `/project-init` — also copied project-locally at `call_it/.claude/commands/project-init.md`, adjusted to call ECC's scripts by absolute path since `call_it`'s working directory isn't inside the ECC checkout |

## 4. Implementation (TDD loop)

**Default path: `writing-plans` → `executing-plans`.** For anything substantial
— a phase, a real feature — write the task/step plan first, then execute it.
You get a reviewable artifact before code starts, and commit boundaries are
fixed in the plan rather than decided ad hoc mid-session (which is what
produced Phase 0's single mega-commit). The `orch-*` commands cover the same
ground as one wrapped flow without leaving a standalone plan document; they're
the better fit only for the narrow, well-shaped cases noted below.

| Situation | Use |
|---|---|
| **Building a phase or a substantial feature (the default)** | `writing-plans` → `executing-plans` |
| Starting any new feature/bug fix — write-tests-first enforcement | `tdd-guide` agent (RED → GREEN → IMPROVE, 80%+ coverage per `.claude/rules/ecc/common/testing.md`) |
| Fixing a bug — reproduce as failing regression test first | `orch-fix-defect` skill — genuinely well-shaped for this; a full task/step plan is overkill for "reproduce, fix, verify" |
| Behavior-preserving refactor (tests stay green throughout) | `orch-refine-code` skill |
| End-to-end gated feature build, one wrapped flow, no separate plan artifact wanted | `orch-add-feature` skill — the alternative to `writing-plans` when you don't need a reviewable plan first |
| Changing existing working behavior to a new spec | `orch-change-feature` skill, or `writing-plans` if the change is large enough to want a plan document |
| Bootstrapping the whole MVP skeleton from the design spec in one orchestrated pass | `orch-build-mvp` skill — superseded here; CallIt is building phase-by-phase instead |
| Build/compile errors block progress | `build-fix` skill, or `go-build-resolver` agent once installed (Bucket 3, Go-specific) |
| Removing dead code / unused exports as the codebase grows | `refactor-clean` skill / `refactor-cleaner` agent |

## 5. Review (before every commit to `dev` or `main`)

| Situation | Use |
|---|---|
| General code quality/pattern review after writing code | `code-review` skill (local diff) or `code-reviewer` agent |
| Security-sensitive code — auth (JWT issuance), wager placement, wallet deduction, payment-like flows | `security-review` skill and/or `security-reviewer` agent — **mandatory per `.claude/rules/ecc/common/code-review.md`** for auth/user-input/DB/external-API/crypto code, all of which CallIt's write path touches |
| Go-specific idiom/error-handling review | `go-reviewer` agent (Bucket 3 — install once Go code exists) |
| Checking for swallowed errors / bad fallbacks (critical given the "0.00% double-spend tolerance" target) | `silent-failure-hunter` agent |
| Reviewing comment accuracy/rot | `comment-analyzer` agent |
| Auditing type/struct design for invariant enforcement (e.g. wager state machine, pool math) | `type-design-analyzer` agent |
| Full multi-agent PR review before merge | `review-pr` skill |
| Heavier, paid multi-agent cloud review of a whole branch or PR | "ultrareview" — `/code-review ultra` (or `/code-review ultra <PR#>`); user-triggered only, not something to launch autonomously |

## 6. Testing & coverage

| Situation | Use |
|---|---|
| Checking coverage gaps against the 80% minimum bar | `test-coverage` skill |
| E2E flow validation (host creates room → guests join → wager → lockout → resolve → payout) | `e2e-runner` agent once a browser-testable frontend exists |
| Load/throughput validation against the spec's perf targets (p99 <15ms, 5k+ req/s) | Not covered by an ECC skill — spec already names k6 directly; script this manually per §7 of the design spec |
| Actually running the app to confirm a change works, not just tests | `run` skill |

## 7. Docs & knowledge capture

| Situation | Use |
|---|---|
| End-of-session log entry (state, decisions, next steps) | `journal` skill — **this project's replacement for `/save-session`/`/resume-session`**, per the ecc-survey's overlap flag |
| Resuming work from a prior session | `journal` skill (resume action) |
| Updating README/codemaps as structure solidifies | `doc-updater` agent |
| Extracting a reusable pattern from this session into a durable skill/instinct | `learn` or `learn-eval` skill |

## 8. Git & shipping

**Branch-per-phase, no PR ceremony (decided 2026-08-23).** Before starting a
plan phase's implementation, branch off `dev` (e.g. `phase-1-domain-core`).
Commit incrementally as each logical unit reaches GREEN in the TDD cycle —
not one squashed commit at the end of the phase (Phase 0 did this and it
didn't reflect the actual work; don't repeat it). Merge into `dev` directly
once the phase's tests/acceptance criteria pass — no `/pr`, no waiting for
review; this is a solo project and PR ceremony doesn't add value with one
developer. Sub-task-level branching (a branch per file/component within a
phase) was considered and explicitly rejected as too much overhead for solo
work.

| Situation | Use |
|---|---|
| Starting a phase's implementation | `git checkout -b <phase-slug> dev` first, before writing code |
| Committing | Follow `.claude/rules/ecc/common/git-workflow.md` format (`type: description`) directly, incrementally per logical unit — no dedicated skill needed for the commit itself |
| Finishing a phase | Verify tests pass, then merge the phase branch into `dev` directly (`git merge`), delete the branch. `finishing-a-development-branch` skill covers the decision-making if the situation is more ambiguous than "tests pass, merge it" |
| Opening a PR | Not used in this project's current workflow (solo, self-merge) — `pr` skill stays available if that changes (e.g. a collaborator joins) |
| Scanning for leaked secrets/config issues before pushing | `security-scan` skill |

---

## 9. Decisions & tradeoffs (things deliberately NOT adopted)

Each entry records what was considered, why it was declined, and the concrete
condition that should flip the answer. **Verdicts are project-specific;
the reasoning is portable.** Re-run the reasoning against a new project's
conditions rather than inheriting the verdict — a team project, a
multi-repo setup, or a verbose stack changes several of these.

### Subagent-driven development — declined 2026-08-23

**What it is:** `superpowers:subagent-driven-development` — dispatches a fresh
implementer subagent per plan task, a task reviewer after each, a 5-round fix
loop, and a final whole-branch review, coordinating through a git-ignored
ledger at `.superpowers/sdd/<plan>/progress.md`.

**Declined because:** the problem it solves — main-context pollution from
implementation output — wasn't demonstrated here. Phase 0, an entire phase,
was 464 lines across 10 files; Go plus terse `go test` output is cheap in
context. The one compaction that did occur in this project came from
*workflow discussion* (reading multiple SKILL.md files, the survey, the spec,
the plan) not from building. Reading SDD's own 32KB `SKILL.md` costs about as
much context as a whole phase of implementation.

The cheaper substitute already exists: **branch per phase → journal entry →
fresh session for the next phase** resets context to zero between phases for
free. SDD addresses *within*-phase accumulation, which these phase sizes don't
hit. If context feels tight, `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE` in
`~/.claude/settings.json` (currently `50`, i.e. compacting at half the window)
is a one-line lever with more leverage than seven files of orchestration.

**Costs if adopted:** 7 files (SKILL.md, 3 prompt templates, 3 scripts) plus
the sibling `requesting-code-review` skill. Minimum 2 subagent dispatches per
task (implementer + reviewer), up to ~12 for a task that runs the full fix
loop, plus a final whole-branch review on the most capable model. It also
carries an explicitly autonomous posture — *"Do not pause to check in with
your human partner between tasks"* and *"Rulings, not stalls"* — stopping only
for irreversible/destructive operations, security-sensitive actions, side
effects outside the worktree (merge/push/publish), or a plan too broken to act
on. Every ruling it makes is surfaced at the end, but not before.

**Genuine upsides forgone:** the compaction-surviving ledger (it names
re-dispatching already-completed tasks as the most expensive failure mode
observed), and the per-task review gate.

**Revisit when:** a phase's implementation genuinely exhausts context before
completing — Phase 5 (Kafka + ledger + migrations + reconciliation) or Phase 6
(Next.js frontend; JS/TS is far more verbose than Go) are the plausible
candidates. **Both have since been split** — 5 into 5a/5b, 6 into 6a/6b — which
is the cheaper answer to the same pressure and was reached first in each case;
weigh that before reading a large phase as evidence for SDD. Adopt with evidence about *which* phase forced it, not
preemptively.

**Portable?** The reasoning generalizes; the verdict does not. A verbose
stack, larger tasks, or work you intend to run unattended flips this quickly.

**Update 2026-08-27 — the revisit trigger fired, and SDD was still not
adopted.** Phase 5b was the first phase sized to make delegation worth
trying. Rather than installing SDD itself, the response was the thin
project-local `.claude/skills/delegating-plan-tasks/SKILL.md`: one dispatch
per task (not implementer+reviewer), no git-ignored ledger, escalation
instead of autonomous "rulings," and a return contract capped at ~300 words
across four fixed sections. The objection above was always SDD's ceremony,
never delegation itself — this is delegation without the ceremony, wired
into `executing-plans` Step 2, invoked only for tasks a plan's header opts
in (inline stays the default).

Run once, on Phase 5b's Tasks 1–5: **3.0× token saving, 9× fewer turns**
per checkpoint against the Phase 5a inline control, both pre-registered
bars cleared (`journal/2026-08-27_1548_ansh_phase-5b-delegation-prediction.md`,
`journal/2026-08-27_1725_ansh_phase-5b-ledger-execution.md`). The one
process gap the run surfaced — a subagent bundled four checkpoints into one
commit and its own return report claimed no folds had happened, caught only
because the parent checked `git log` directly rather than trusting the
report — was patched the same day: the return contract's `COMMITS` field
now tags every commit with the checkpoint(s) it covers, so a fold has to
appear as a fact in a structured field rather than rely on an honest summary
sentence.

### Continuous learning v2 / instincts — declined 2026-08-23

**What it is:** ECC's `continuous-learning-v2` — hooks capture every tool call
to a JSONL log, a background Haiku agent periodically mines it for patterns
(corrections, repeated workflows, error resolutions) and writes atomic
"instincts" with confidence scores (0.3–0.9), scoped per-project or global.
`/evolve` clusters instincts into skills; `/promote` graduates project
instincts to global once seen in 2+ projects at ≥0.8 confidence. Commands:
`/instinct-status`, `/instinct-export`, `/instinct-import`, `/evolve`,
`/promote`, `/projects`, `/prune`, plus `/skill-create --instincts` (mines git
history directly, no daemon required).

**Current state:** present as a skill but **dormant** — `config.json` has
`observer.enabled: false`, and `~/.claude/settings.json` has no `hooks` block
registering `observe.sh`, nor is it installed as a plugin. Nothing is being
captured.

**Declined because:** it's a third store overlapping two that already work
here — Claude's own memory (which captures the same "user corrected me" signal
as `feedback` memories, with explicit reasoning, written on judgment) and the
`journal` skill (session decisions and rationale). Adding a third creates the
same "which store does this go in?" ambiguity the survey already flagged for
`/save-session` vs. `journal`.

It's also curated by *statistics* rather than judgment: "the user didn't
correct this" is weak evidence for "this is right" — it may mean they didn't
notice. Once an instinct crosses 0.7 confidence it auto-injects into every
session with no human ever having approved it. False-positive memories are
worse than missing ones, because they're trusted by default.

**Cost shape (three separate line items):** the capture hook is cheap (shell
script, no LLM). The background Haiku analysis pass is a genuine recurring
spend that runs whether or not anything useful comes out. The one that costs
the *working* model is SessionStart injection — up to
`ECC_MAX_INJECTED_INSTINCTS` (default 6) instincts, capped by
`ECC_SESSION_START_MAX_CHARS` (default 8000), pushed into every session
regardless of relevance to that session's task.

**Also:** the observer needs ≥20 queued observations before its first analysis
runs, and much of what it would "discover" for a Go project is already written
down in `golang-patterns`/`golang-testing` — rediscovering existing skill
content, statistically, at recurring cost.

**Revisit when:** *volume* and *novelty* both exist — many sessions, producing
patterns not already captured in an installed skill or rule, ideally across
several concurrent projects (which is what makes `/promote` meaningful). The
trigger is juggling multiple repos, not reaching a particular phase here.
`/skill-create --instincts` is the cheap on-ramp to try first: one-shot, mines
git history, no daemon, no standing hook.

**Portable?** Reasoning generalizes. For a team, or someone rotating across
many client repos, the calculus genuinely flips — cross-project detection and
export/import are solving real problems there that don't exist for one solo
project.

### Carrying adapted skills to other projects — decided 2026-08-25

Of the 22 skills in `.claude/skills/`, **18 are byte-identical to their
sources** (14 from the ECC marketplace, 4 from the `obra/superpowers` clone at
`~/projects/superpowers`). Only four diverge, and only one of those is worth
carrying anywhere:

| Skill | Divergence | Portable? |
|---|---|---|
| `writing-plans` | +130 lines | **Yes** — real methodology (see below) |
| `executing-plans` | +18 lines | No — pure config: `dev` branch, no-PR, `--no-ff`, skill path prefixes |
| `brainstorming` | +2 lines | No — spec directory path only |
| `journal` | written from scratch | Already generalized to `~/.claude/skills/journal-global/` |

The test applied: **did we invent a rule, or set a value?** Rules travel; values
don't. `executing-plans`' additions all encode *this* repo's branch names and
merge policy — a new project needs different values, not these ones.

`writing-plans`' additions are rules: multi-checkpoint task granularity, the
RED→GREEN reality test for a checkpoint (Phase 1), the observable-signal rule
(Phase 2's `lock_round.lua` `ALREADY_LOCKED` case), "a plan stops at green,
never merges", the two-implementers specificity bar, and committing the plan
before handoff. None of them mention wagering, Redis, or Go.

**Where the portable copy lives:** `~/projects/claude-skills/` — a git repo you
install *from*, not a live-loaded directory. It is deliberately **not** in
`~/.claude/skills/`, for two reasons: a live-loaded global copy would need an
awkward `-global` suffix to stay distinguishable from the project copy in a flat
skill listing, and an auto-loaded adapted skill can carry this project's
assumptions into an unrelated repo with no explicit step where you'd notice.
Copy-and-adapt is the same model ECC and superpowers already use — one mental
model, not two.

**One thing that stays project-specific and must be re-decided per project:**
the spec-driven-vs-code-driven plan format. It's contingent on execution mode
(inline here; subagent-driven wants pre-written code), so the library version
states it as a fork to choose, not a default to inherit.

**Don't edit `~/projects/superpowers/`** — it's a clean clone of upstream, not a
fork. Edits get clobbered by `git pull` and can't be pushed.

### Other tradeoffs decided here

| Decision | Verdict | Revisit when |
|---|---|---|
| Branch granularity (§8) | Per phase, not per sub-task | A phase grows big enough that its branch stops being reviewable as one unit |
| PR vs self-merge (§8) | Self-merge to `dev`, no PR | A collaborator joins — then `/pr` and `review-pr` earn their keep immediately |
| `writing-plans` vs `orch-*` (§4) | `writing-plans` default; `orch-fix-defect`/`orch-refine-code` for their narrow shapes | Never really — they coexist; just don't use both for the same job |
| Skills vs rules import timing (§3) | Skills eagerly (cheap, listing-only until invoked); rule dirs staggered per-phase (always-loaded full text) | If a rule pack turns out small enough that staggering costs more attention than it saves |
| `journal` local vs `journal-global` | Project-local copy wins here (ADRs at `docs/decisions/`, not the global `docs/adr/`) | Starting a new project — use `journal-global` unless it needs project-specific tailoring |
| Portable skills: global live-load vs install-from library | Library at `~/projects/claude-skills/`, copied in per project | A skill turns out to need zero per-project adaptation — then live-loading it globally costs nothing |
| `CLAUDE.md` timing (§3) | After Phase 0, not from the spec | Never — writing it before real code exists means documenting guesses |

---

## Suggested order for CallIt specifically, right now

1. ~~`/impl-plan`~~ ✅ done — `docs/plans/2026-08-21-implementation-plan.md`.
2. ~~Import Phase 0 tooling~~ ✅ done — `golang-*` rules/skills, `docker-patterns`.
3. ~~Phase 0~~ ✅ done — commit `d60bd8d`.
4. **Write `CLAUDE.md`** — Phase 0 exists now, so the gate is met. Run `/project-init` for a command-verified scaffold, then layer in the "why" content (invariants, rejected alternatives, gotchas) from the spec, plan, and journal. This is also where the commit-granularity convention belongs, since `CLAUDE.md` is always loaded.
5. **Phase 1** — `git checkout -b phase-1-domain-core dev` → `writing-plans` (break the phase into committable tasks) → `executing-plans` (execute inline, commit per task) → merge to `dev`.
6. `journal` entry, then a fresh session for Phase 2 (which is also the context reset that makes SDD unnecessary — see §9).
