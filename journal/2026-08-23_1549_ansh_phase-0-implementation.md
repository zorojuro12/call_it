# 2026-08-23 — ansh — Phase 0 implementation (foundations)

**Status:** Phase 0 built, tested, and committed (`d60bd8d`) — config loader, `/healthz`, docker-compose, Makefile, CI. Landed as a single commit directly on `dev`, before the branch-per-phase convention existed (see the separate `2026-08-23_1544_ansh_workflow-tooling-and-git-granularity.md` entry for that decision, made outside this session). Mid-session also caught up on those three external commits — no new content to add here beyond what that entry already covers.
**Decided:** Real Kafka (KRaft mode) goes in `docker-compose.yml` but sits behind a `full` Compose profile, so default `make up` only starts Redis + PostgreSQL — implements the plan's own risk mitigation (plan §10, "Kafka resource-heavy under WSL2") rather than a new call.
**Spec:** No change — this session executed the existing plan, didn't revise it.
**Next:** Write `CLAUDE.md` (gate was "Phase 0 exists," now met), then start Phase 1 (domain core) via `writing-plans` → `executing-plans`, branched off `dev` per the now-settled convention.
**Blocked on:** Docker isn't available in this WSL distro (Desktop's WSL2 integration isn't enabled here) — `docker-compose.yml` is YAML-valid but never actually brought up. Needs that enabled on the Windows side before `make up` can be verified for real, and before any phase that needs Redis/Postgres running (Phase 2 onward).
**Touches:** `backend/`, `docker-compose.yml`, `Makefile`, `.github/workflows/ci.yml`, `.gitignore`

---

## What We Worked On

Executed Phase 0 of `docs/plans/2026-08-21-implementation-plan.md`: the
foundations phase — monorepo skeleton, config loader, `/healthz`, Docker
Compose, Makefile, CI — with TDD on everything that has real logic.

## What Worked

- `internal/config.Load` — fail-fast env validation (port range, log-level
  enum, env enum), written test-first, RED confirmed (undefined symbols)
  before implementing. 100% coverage.
- `internal/httpapi` — `HealthHandler` + `NewMux` using Go 1.22's
  method-pattern routing (`"GET /healthz"`), also RED-confirmed first.
  85.7% coverage.
- `cmd/api/main.go` — wired both together with `log/slog` JSON logging and
  graceful shutdown on SIGINT/SIGTERM. Ran the actual binary and curled
  `/healthz`: `200 {"status":"ok"}`, confirmed live, not just via test.
- `go build`, `go vet`, `gofmt -l` (clean), `go test -race -cover` all pass.
- Caught and fixed a swallowed error in `health.go` (an ignored
  `json.Encode` return) after the golang rule pack's error-handling
  guidance surfaced mid-session — logged via `slog` instead, since headers
  are already sent by that point so the response itself can't change.
- Added `-race` to both the Makefile `test` target and CI after the
  project's Go testing rule (`always run with -race`) became visible.

## What Didn't Work

- `sudo tar -C /usr/local -xzf go.tar.gz` to install Go the normal way —
  failed with "a terminal is required to read the password" (no
  interactive sudo available in this environment). Worked around by
  installing to `~/.local/go` instead (no root needed) and appending it to
  `~/.bashrc`. A future session hitting the same wall should skip straight
  to the user-local install rather than retrying sudo.

## Test Coverage

- **Covered:** `internal/config` 100%, `internal/httpapi` 85.7% (both via
  table-driven tests per `golang-testing` conventions).
- **Not covered yet:** `cmd/api` shows 0% — expected, it's thin wiring with
  no branching logic of its own. The `docker-compose.yml` stack itself is
  entirely unverified (see Blocked on) — Redis/Postgres/Kafka have never
  actually been started against this compose file.

## Relevant Commits

- `d60bd8d` — feat: Phase 0 foundations — config loader, /healthz, compose, CI

## Next Step

`CLAUDE.md` first (Phase 0 gives it real code to pattern-match against
instead of guesses), including the branch-per-phase + checkpoint-commit
convention explicitly since `CLAUDE.md` is the one doc that's always
loaded. Then Phase 1 (domain core: odds math, payout/dust, round FSM,
wallet rules) — pure Go, no I/O, so it's unaffected by the Docker gap.
