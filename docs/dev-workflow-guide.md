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

| Situation | Use |
|---|---|
| Spec is approved (✅ done — `docs/specs/2026-08-21-callit-design.md`), need a concrete build plan for one of the §9 open items (Redis schema, WS hub internals, Kafka partitioning, Postgres schema, repo layout) | `/impl-plan` command — restates requirements, assesses risk, produces step-by-step plan, **waits for CONFIRM before touching code** (renamed from `/plan` to avoid colliding with Claude Code's built-in Plan Mode) |
| Feature/refactor plan needs deeper multi-file architectural reasoning first | `planner` agent, or `code-architect` agent if there's existing code to pattern-match against (N/A yet — repo is pre-code) |
| Deciding Go package/folder layout specifically | `planner` agent, cross-checked against the Go microservice reference layout at `/home/chikara/projects/ECC/examples/go-microservice-CLAUDE.md` |

## 3. Setting project-level conventions

| Situation | Use |
|---|---|
| Writing CallIt's `CLAUDE.md` | Do this **after Phase 0** exists (real code to pattern-match, not the spec/plan alone). Run `/project-init` first for a verified, command-checked scaffold (build/test/lint/dev commands, detected stack) — never invent commands. Then layer in the "why" content manually: non-obvious invariants, rejected alternatives, gotchas from `docs/specs/2026-08-21-callit-design.md`, `docs/plans/2026-08-21-implementation-plan.md`, and `journal/` entries. The built-in `init` skill or `codebase-onboarding` are generic fallbacks if `/project-init`'s ECC-checkout dependency (`/home/chikara/projects/ECC`) isn't available, but produce less project-relevant output since they don't know this project's plan/spec history |
| Installing stack-specific rule packs (Go idioms, Postgres, etc.) | Copy from `/home/chikara/projects/ECC/rules/golang`, `.../postgres` etc. into `call_it/.claude/rules/ecc/` (project-local, per the ecc-survey's rules section). **Stagger these per-phase** (see the "Tooling to import" column in the plan's §9 phase table) — rule dirs are always-loaded full text into every turn once installed, unlike skills, so importing a phase's rules only right before that phase starts avoids irrelevant standing context |
| Pulling in stack-specific skills | Bucket 3 picks per `ecc-survey.md`: `redis-patterns`, `postgres-patterns`, `golang-*` (patterns/testing/tdd/verification), `react-patterns`/`nextjs-turbopack`, `database-migrations` — install project-locally into `call_it/.claude/skills/`, not globally. Unlike rule dirs, skills are cheap (one line in the availability listing until invoked), so it's fine to pull in the full Bucket 2/3 list for the whole project now rather than staggering |
| Generating an onboarding/install plan for this project's ECC surface | `/project-init` — also copied project-locally at `call_it/.claude/commands/project-init.md`, adjusted to call ECC's scripts by absolute path since `call_it`'s working directory isn't inside the ECC checkout |

## 4. Implementation (TDD loop)

| Situation | Use |
|---|---|
| Starting any new feature/bug fix — write-tests-first enforcement | `tdd-guide` agent (RED → GREEN → IMPROVE, 80%+ coverage per `.claude/rules/ecc/common/testing.md`) |
| End-to-end gated feature build (research → plan → TDD → review → commit, one wrapped flow) | `orch-add-feature` skill |
| Changing existing working behavior to a new spec | `orch-change-feature` skill |
| Fixing a bug — reproduce as failing regression test first | `orch-fix-defect` skill |
| Behavior-preserving refactor (tests stay green throughout) | `orch-refine-code` skill |
| Bootstrapping the whole MVP skeleton from the design spec in one orchestrated pass | `orch-build-mvp` skill |
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

## Suggested order for CallIt specifically, right now

1. `/impl-plan` — resolve the §9 open items (start with Redis Lua wager-placement schema, since it's the hot-path critical piece; each subsystem gets its own `/impl-plan` pass).
2. Write `CLAUDE.md` once repo layout + schemas are decided.
3. Install Bucket 3 stack-specific skills/rules (`golang-*`, `redis-patterns`, `postgres-patterns`) project-locally.
4. `tdd-guide` / `orch-build-mvp` for the first implementation slice (Go WS handler + Redis Lua script, since it's the riskiest path).
5. `security-review` on the wager-placement and auth code before first commit (mandatory, per code-review rules — this is exactly the kind of code the trigger list names).
6. `journal` entry to close out the session.
