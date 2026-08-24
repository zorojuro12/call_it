# 2026-08-23 — ansh — CLAUDE.md review and Docker WSL2 verification

**Status:** `CLAUDE.md` reviewed, rated, and committed along with both pending journal entries (`178f6d6`). Docker Desktop's WSL2 integration got enabled mid-session and the full `docker-compose.yml` is now genuinely verified — Redis, PostgreSQL, and the `full`-profile Kafka all confirmed healthy, not just YAML-valid. Working tree clean, `dev` branch.
**Decided:** No new architectural decisions — this session was verification and documentation upkeep on top of already-decided Phase 0 work.
**Spec:** No change.
**Next:** Phase 1 (domain core) — branch off `dev`, `writing-plans` → `executing-plans`, per `docs/dev-workflow-guide.md`'s suggested order. User said they're ending this session and starting a new one, so Phase 1 starts cold.
**Blocked on:** Nothing. The Docker gap that blocked real Phase 2+ verification is closed.
**Touches:** `CLAUDE.md`, `docker-compose.yml`, `journal/2026-08-23_1544_ansh_workflow-tooling-and-git-granularity.md`, `journal/2026-08-23_1549_ansh_phase-0-implementation.md`

---

## What We Worked On

Two things, both triggered by the user having written a `CLAUDE.md` in a
different chat and asking for a rating plus a check on whether anything from
Phase 0 was still outstanding:

1. Fact-checked every checkable claim in `CLAUDE.md` against live repo state
   (Makefile targets, CI step order, directory layout, coverage numbers,
   installed skills, `continuous-learning-v2` dormancy) — all accurate.
2. The user then asked specifically about the Docker gotcha in `CLAUDE.md`
   ("don't I need to set up Docker or something?"), which led to actually
   diagnosing and fixing the WSL2 integration gap rather than just
   describing it.

## What Worked

- Diagnosed the Docker gap precisely: `wsl.exe -l -v` showed both `Ubuntu`
  and `docker-desktop` distros running, but no `docker` binary or
  `/var/run/docker.sock` in `Ubuntu` — meaning Docker Desktop was installed
  and running on Windows, just not integrated with this specific distro.
  Gave the user the exact fix (Settings → Resources → WSL Integration →
  enable per-distro, not just "default distro" → Apply & Restart → new
  shell).
- User did this; verified `docker --version` (28.4.0), `docker compose
  version` (v2.39.2-desktop.1), and `/var/run/docker.sock` all present.
- `docker compose up -d` (core services): both containers reported
  `healthy`. Confirmed for real, not just via container status —
  `redis-cli ping` → `PONG`, `pg_isready -U callit` → accepting
  connections.
- `docker compose --profile full up -d kafka`: started, reported `healthy`,
  ~290MB RSS at idle (checked via `docker stats`) — not the resource
  problem the plan's risk table worried about, at least at idle/startup.
  Stopped and removed the Kafka container afterward since Phase 1 doesn't
  need it, leaving Redis + Postgres running to match `make up`'s intended
  default state.
- Updated `CLAUDE.md`'s Known Environment Gotchas entry in place — it
  previously said Docker "has never actually been brought up," which was
  now false; replaced with the actual fix steps and the verification
  evidence above.

## Test Coverage

- **Covered:** `docker-compose.yml` is now behaviorally verified (both the
  default and `full` profiles), not just syntactically. This closes the gap
  the Phase 0 entry flagged as blocking Phase 2+.
- **Not covered yet:** Kafka's resource behavior *under actual load* (the
  plan's risk table entry is about sustained traffic, not idle startup —
  today's ~290MB measurement doesn't resolve that risk, just the "does it
  even start" question). Left the plan's risk table untouched rather than
  overstating what was verified.

## Relevant Commits

- `178f6d6` — docs: add project CLAUDE.md, journal two prior sessions

## Next Step

User is ending this session here. Next session starts cold on Phase 1
(domain core: odds math, payout/dust distribution, round state machine,
wallet rules) — pure Go, no I/O, so none of today's Docker verification
work is a prerequisite for it. Follow the now-settled workflow: branch off
`dev`, `writing-plans` to break Phase 1 into checkpointed tasks, then
`executing-plans`.
