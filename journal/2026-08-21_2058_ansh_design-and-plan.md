# 2026-08-21 — ansh — CallIt design spec and implementation plan

**Status:** Design spec and implementation plan both written, committed, and approved. Repo has `main`/`dev` branch layout with `dev` as the active branch. No code written yet — next session starts Phase 0 (repo skeleton, docker-compose, CI).
**Decided:** Adopted a transactional outbox over Redis Streams for the wager write path — the original spec had the Lua script and the Kafka produce as two separate uncoordinated steps, which could silently diverge Redis and Postgres on a crash. See plan §1 and spec §5 for the full reasoning.
**Spec:** Updated — `docs/specs/2026-08-21-callit-design.md` §5 rewritten for the outbox amendment; §9 flipped from "open items" to a resolved-items index pointing into the plan.
**Next:** Start Phase 0 — monorepo skeleton (`backend/`, `frontend/`, `docker-compose.yml` with Redis/Postgres/Kafka-KRaft), Makefile, GitHub Actions CI, config loader with fail-fast validation, structured logging, `/healthz`.
**Blocked on:** Nothing.
**Touches:** `docs/specs/2026-08-21-callit-design.md`, `docs/plans/2026-08-21-implementation-plan.md`, `docs/dev-workflow-guide.md`

---

## What We Worked On

This was CallIt's kickoff session: took the project from "idea + a Gemini-drafted
tech-stack brief" to an approved design spec and implementation plan, ready to
start writing code next session.

Sequence:
1. Fixed git setup — repo was cloned from a blank GitHub remote with zero
   commits on a stray `init` branch. Set up `main` + `dev` with `dev` as the
   ongoing development branch, feature branches to merge into `dev`.
2. Reviewed the original project brief (Go/Redis/Kafka/Postgres/Next.js
   real-time micro-wagering platform) and confirmed the stack is genuinely
   resume-differentiated, not overbuilt for the sake of it — flagged solo-scope
   risk as the main thing to manage.
3. Ran a full architectural brainstorming pass (via the `brainstorming` skill)
   to resolve product-level ambiguity before any technical planning — see
   Decisions Made below.
4. Ran `/plan` against the approved spec to resolve all seven items the spec
   deliberately deferred (Redis schema, Lua contracts, Kafka topology, Postgres
   schema, WS hub internals, auth mechanism, repo layout), plus caught and
   fixed the outbox gap.

## Decisions Made

Full reasoning for all of these is written down in the spec and plan, not
restated here:

- Resume-signal-wins-by-default, and Kafka-over-Redpanda as a direct
  consequence of it — spec §1, §2.
- Hybrid identity model, host-configurable per-room buy-in, refill economy —
  spec §3.
- Host cannot bet in their own room; rounds support N custom outcomes — spec
  §4.
- Local Docker Compose demo first, live deployment deferred but not designed
  away — spec §8, plan §9 (phase 8).
- Monorepo layout, short-lived JWT for WS auth — plan §3, spec §6.
- Outbox amendment (caught during `/plan`, not brainstorming) — plan §1,
  spec §5.
- Library choices (`gorilla/websocket`, `segmentio/kafka-go`) — plan §2.

## Test Coverage

- **Covered:** Nothing yet — no code exists.
- **Not covered yet:** Everything. Plan §9/§11 specifies the concurrency suite
  (zero double-spend under contention) and the Redis↔Postgres reconciliation
  test as the two tests that most directly back the spec's correctness claims;
  these should not be treated as optional/deferred once Phase 2 and Phase 5
  start.

## Open Questions / Blockers

- None blocking. Two loose threads noted but not yet resolved:
  - `.claude/skills/journal/SKILL.md` showed as deleted (unstaged) in git
    status at the start of the session, alongside a large set of untracked
    `.claude/` directories (agents, commands, rules, many skills) and
    `docs/dev-workflow-guide.md`. These were left untouched/uncommitted since
    they're outside this session's scope — worth a deliberate look next
    session rather than assuming they're fine.
  - `dev` branch has not been pushed since the two most recent commits
    (spec amendment + plan). Only `main`/`dev` initial push happened earlier
    in the session.

## Relevant Commits

- `4903018` — chore: initial commit (README, journal placeholder, journal
  skill) — established `main`
- `0d9e801` — docs: add CallIt design spec
- `5fdab31` — docs: add implementation plan and adopt outbox amendment

## Spec Changes

`docs/specs/2026-08-21-callit-design.md` §5 (Write Path / Data Flow) rewritten
to describe the transactional outbox explicitly, including the reasoning for
why it's needed (crash between Redis write and Kafka produce would silently
diverge the two systems). §9 changed from "Open Items for the Implementation
Plan" to "Items Resolved in the Implementation Plan," now pointing at
`docs/plans/2026-08-21-implementation-plan.md` as the authoritative source for
each item instead of restating them.

## Next Step

Begin Phase 0 of the implementation plan: monorepo skeleton, `docker-compose.yml`
(Redis, Postgres, Kafka in KRaft mode), Makefile, GitHub Actions CI, a
fail-fast config loader, structured logging, and a `/healthz` endpoint. This
is the only phase with no dependencies and should be a self-contained, low-risk
first slice of actual code.
