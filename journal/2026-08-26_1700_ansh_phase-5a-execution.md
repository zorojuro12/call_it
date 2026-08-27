# 2026-08-26 — ansh — Phase 5a execution: outbox → Kafka + ledger schema

**Status:** Phase 5a complete. All 6 tasks, 25 planned checkpoints plus one
unplanned coverage checkpoint, executed inline on branch `phase-5a-outbox-kafka`
off `dev`. Full suite green (`go test ./... -race -cover -p 1`), `go vet`/`gofmt`
clean, security review clean (no CRITICAL/HIGH). Branch not yet merged — this
entry closes the plan's Task 6, next step is `finishing-a-development-branch`.
**Decided:** Executed inline per the plan's own instruction — the delegation
skill described in `journal/2026-08-26_1404_ansh_subagent-delegation-proposal.md`
was never written (confirmed absent from `.claude/skills/` before starting),
so per the plan's Execution notes that's not an oversight to route around, it
means the experiment stays deferred. No delegation attempted this session.
**Spec:** Updated — parent plan §4 (added the `wager-outbox` consumer group),
§5 (Amendments E1/E2 rewrite the `settle_round.lua`/`refund_round.lua`
sections), §7 (Amendment E3 next to the Kafka topology table). `CLAUDE.md`
Stack section gained three pinned dependencies and a new `go get` gotcha.
**Next:** Hand off to `finishing-a-development-branch` to merge into `dev`
(`--no-ff`, no PR, per `CLAUDE.md`). Then Phase 5b (double-entry ledger writer
+ reconciliation test) — the flagship correctness work the plan says to keep
inline regardless of how delegation experiments land elsewhere.
**Blocked on:** Nothing.
**Touches:** `backend/internal/{migrate,events,relay,redisstore,config}/`,
`backend/migrations/`, `backend/cmd/{migrate,relay}/`, `backend/scripts/lua/`,
`.github/workflows/ci.yml`, `Makefile`, `CLAUDE.md`,
`docs/plans/2026-08-21-implementation-plan.md`, `docs/project-history.md`.

---

## What We Worked On

Executed `docs/plans/2026-08-26-phase-5a-outbox-kafka.md` task-by-task via
`executing-plans`: PostgreSQL ledger schema + migration runner (Task 1),
outbox payload enrichment on the existing Lua scripts (Task 2), a pure typed
event schema (Task 3), the Redis-Stream-to-Kafka relay loop (Task 4), the
Kafka producer (Task 5), and CI/security/close-out wiring (Task 6).

## Decisions Made

- **Inline execution, not delegated.** See `Decided` above — the plan's own
  Execution notes section anticipated this exact situation and said what to
  do about it, so this wasn't really a decision so much as following the
  plan's pre-registered fallback.
- **`blockInterval` set to 200ms, not the plan's 1s ceiling.** The plan said
  "1 second or less"; 1s turned out to make `Run`'s shutdown too slow to
  reliably clear a 1s test deadline after cancellation (see What Didn't
  Work). 200ms is still comfortably inside the stated ceiling.
- **Task 1 Checkpoint 6's Makefile comment was corrected mid-task.** First
  draft claimed a default DSN; `LoadMigrate` actually has no default (fails
  fast on unset `POSTGRES_DSN`, matching `Load`'s style for `JWT_SECRET`).
  Fixed before committing rather than leaving a doc/code mismatch.

## What Worked

- **The plan's line-number references held exactly.** `settle.go:69-76`'s
  status switch and `errors.go:53-63`'s `mapSettleStatus` matched the plan's
  citations verbatim before any code was touched — the plan was written
  against real code, not guessed.
- **The dependency pins held.** `pgx@v5.7.4`, `kafka-go@v0.4.48`,
  `migrate@v4.18.2` all resolved and built clean against `go 1.22.10` on the
  first correctly-pinned attempt, confirming the probe module from the
  planning session.
- **The security review came back clean.** No CRITICAL/HIGH on any of the
  four scoped surfaces (event-payload exfiltration, payouts JSON decoding,
  the Postgres DSN, the Kafka connection). One MEDIUM (DSN could theoretically
  leak in an error string) and one LOW accepted with reasoning rather than
  fixed — recorded in `docs/project-history.md`.
- **Coverage cleared 80% on all three new packages** without padding:
  `internal/events` 83.8%, `internal/relay` 89.1%, `internal/migrate` 85.7%
  (after one extra checkpoint — see below).

## What Didn't Work

- **`go get`ting golang-migrate's subpackages without repeating the version
  pin.** `go get .../database/postgres .../source/iofs` (no `@v4.18.2`)
  silently pulled the parent module to v4.19.1, which declares `go >= 1.24`
  and rewrote `go.mod`'s directive to `1.24.0` — exactly the wall
  `CLAUDE.md` warns about, hit for the first time on a multi-package module
  rather than a single-package one. Fixed by re-running `go get` with every
  target pinned explicitly, including subpackages, then hand-correcting the
  directive line and letting `go build`/`go mod tidy` settle the transitive
  versions back down. Recorded as a new gotcha in `CLAUDE.md`'s Stack
  section so a future `go get` on a similar module doesn't repeat it.
- **`blockInterval = time.Second` (the plan's stated ceiling) for `Run`'s
  shutdown test.** go-redis's blocking `XREADGROUP` does not observe context
  cancellation mid-block — it only returns once the block elapses — so a
  context cancelled at 50ms still left an in-flight 1s block running,
  pushing `Run`'s return past the test's 1s deadline. Fixed by lowering
  `blockInterval` to 200ms; still within the plan's "1 second or less," but
  the plan's own phrasing undersold how tight that ceiling needed to be
  given go-redis's actual blocking behavior.
- **Two checkpoints collapsed because their implementation was already
  written by an earlier one.** `migrate.Down` (Task 1 CP2) was written
  alongside `Up` in CP1's commit, and `Topic()`/`PartitionKey()`/`Key()`
  (Task 3 CP5) had to exist from CP1 onward for `WagerPlaced` to satisfy the
  `Event` interface. Neither RED step fired genuinely; both checkpoints
  landed as verification-only commits instead. Noted at the time rather than
  faked — Go's structural interfaces make the second case somewhat
  unavoidable when a plan's task-1 checkpoint needs an interface a later
  checkpoint's test is nominally "for."
- **`docker compose down` (no `--profile full`) leaves Kafka running.**
  Discovered while doing Task 6 CP3's required RED check (only Redis up, to
  prove the new suites fail rather than skip) — a plain `down` from a
  previous session had stopped Redis and Postgres but left the Kafka
  container up from an earlier `up-full`. Fixed `make down` to always pass
  `--profile full`, and recorded the gotcha in `CLAUDE.md`.
- **`internal/migrate` sat at 71.4% coverage after Task 1**, under the 80%
  floor. Diagnosed rather than waved through: `newMigrator`'s error branch
  (a malformed DSN failing `NewWithSourceInstance`) was genuinely reachable
  without fault injection and untested; `Up`/`Down`'s own non-`ErrNoChange`
  error branches are not, without a live DB fault (same shape as
  `redisstore`'s already-accepted defensive-branch gap). Added one test for
  the reachable branch (85.7% after), left the rest alone rather than
  padding.

## Test Coverage

- **Covered:** `internal/migrate` 85.7%, `internal/events` 83.8%,
  `internal/relay` 89.1% (per `-coverpkg=./...`, deduplicated across test
  binaries — the raw per-binary figures `go test` prints understate this).
  `internal/config`'s two new surfaces (`LoadMigrate`, `LoadRelay`) at 85.0%
  package-wide. `internal/redisstore` unchanged at 82.4% despite the
  settlement/refund rewrite.
- **Not covered yet:** `internal/migrate`'s `Up`/`Down` non-`ErrNoChange`
  error paths (accepted, see above). `cmd/relay` and `cmd/migrate` at 0%,
  expected per the project's standing interpretation (thin wiring).

## Open Questions / Blockers

- **`tok/CP` for this phase is not measured in this entry.** The
  pre-registered bar (`< 4.6M`, Phase 2 as the honest control) needs
  `phase_compare.py` run against this session's logs, which this session
  doesn't have direct access to — a future session (or the user, from
  outside this conversation) should run it and amend this entry or the
  parent plan with the actual figure before treating 5a as a settled data
  point for the delegation question.
- **CI's Kafka step is still the least-verified part of the plan** in the
  sense that it's only been run via local `docker compose` commands
  mirroring the workflow, not an actual GitHub Actions run. The `services:`
  block for Postgres was added in the same commit as the Kafka step; both
  are unverified against a real runner.

## Relevant Commits

24 commits on `phase-5a-outbox-kafka`, `bdd5061` through `d1dac09` — one per
checkpoint (`git log --oneline dev..HEAD` for the full list). Highlights:
`4b31f6f`/`c8b603e` (Amendments E1/E2), `12aec0d`/`0763daf` (Kafka producer),
`fbffffc` (security review).

## Spec Changes

Parent plan (`docs/plans/2026-08-21-implementation-plan.md`): §4 gained the
`wager-outbox` consumer group note; §5's `settle_round.lua`/`refund_round.lua`
subsections rewritten with Amendments E1/E2 in place (not left as separate
addenda); §7 gained Amendment E3 on explicit topic creation. `CLAUDE.md`'s
Stack section gained the three new pinned dependencies and the
subpackage-`go get` gotcha; Build & Test section and Known Environment
Gotchas updated for the full-stack `make test`/`make down`.

## Next Step

`finishing-a-development-branch` — verify tests, merge `phase-5a-outbox-kafka`
into `dev` with `--no-ff`, delete the branch. Then decide Phase 5b's plan
(double-entry ledger writer, reconciliation test) via `writing-plans`, per the
parent plan's Phase 5 split.
