# 2026-09-02 — ansh — Catching up post-7c, then planning an off-WSL2 load-test baseline

**Status:** No code touched this session. Confirmed Phase 7c is merged into
`dev` (`d1b529f`, working tree clean) — it had merged after its own execution
journal entry was written, so that entry's "keep as-is" note is stale; the
merge did happen. Wrote and committed a new plan,
`docs/plans/2026-09-02-offwsl2-loadtest-baseline.md` (commit `b029af0`, on
`dev`), for a follow-up question the user raised about WSL2 as a testing
environment. Plan is not yet executed.
**Decided:** Answering "are there things we might need to do for testing
outside WSL?" surfaced that CI already runs outside WSL2 (`ubuntu-latest`) and
that's fine, but load-test *fidelity* is a real, still-open gap: Phase 7b's
Task 6 (`loadtest/README.md`) named but never resolved whether the ~3,174
req/s ceiling both 7a and 7b measured was a WSL2 artifact (virtualized network
stack / k6 scheduler interaction) or a real limit. The user asked for a plan
to resolve that, scoped deliberately small per their own instruction — one
clean off-WSL2 run and a report, not a new implementation phase with
manufactured TDD checkpoints for infra work that has none.
**Spec:** No change. The plan amends the parent implementation plan's §10 risk
table (one line on the "p99 measurement fidelity under WSL2" row) as its own
Task 2 Gate 3 — not done yet, that's plan content, not an edit made this
session.
**Next:** Execute `docs/plans/2026-09-02-offwsl2-loadtest-baseline.md` via
`executing-plans` whenever the user is ready. Task 1 Gate 1 (and conditionally
Gate 5) provision billed cloud VMs and require the user's explicit go-ahead on
provider/size before anything is created — this is a hard stop, not a formality.
**Blocked on:** A reachable off-WSL2 host. This session has no cloud account
access or spare bare-metal box of its own; either the user provisions one and
hands over SSH access, or runs the plan's commands themselves.
**Touches:** `docs/plans/2026-09-02-offwsl2-loadtest-baseline.md` (new),
and — as planned, not yet touched — `docs/reports/`, `loadtest/README.md`,
`docs/project-history.md`, `docs/plans/2026-08-21-implementation-plan.md:718`.

---

## What We Worked On

Session opened with a `/clear` and a request to get up to speed: ran `git
status`, `git log`, and read the two most recent journal entries (7c planning
and 7c execution) plus `docs/project-history.md`'s tail to confirm current
state and next steps. Reported back that Phase 7c had closed the three
deferred security items (login timing, reconnect grace window, Kafka message
bounding) plus the README rewrite, and that only the parked, unplanned Phase 8
remained.

The user then asked a scoped question — "are there things we might need to do
for testing outside wsl?" — which I answered by grepping every WSL2 reference
across `docs/` and `CLAUDE.md` and reading the WSL2-browser-access journal
entry, rather than guessing from memory. Landed on: CI is already
WSL2-independent (proven, no action needed); a few local-dev conveniences
(`Makefile`'s `api-lan`/`fe-dev-lan`, `next.config.ts`'s hardcoded WSL2 `eth0`
IP) are WSL2-specific workarounds that just wouldn't apply elsewhere, not
things to fix; and load-test *numbers* are the one place environment change
would produce a materially different, more trustworthy answer, per 7a/7b's own
documented fidelity caveat.

The user then said "make a plan for this," which I took as the load-test
fidelity gap specifically (the concrete open item from my prior answer, not
the WSL2-conveniences list). Read both prior baseline reports in full
(`docs/reports/2026-08-31-phase-7a-baseline.md`,
`docs/reports/2026-08-31-phase-7b-baseline.md` — the latter's content
actually lives in `loadtest/README.md`'s "Phase 7b" sections) plus the parent
plan's risk-table row and the k6 scripts themselves (`loadtest/wager_latency.js`,
`loadtest/rest_throughput.js`, `loadtest/lib/setup.js`) before writing
anything, via the `writing-plans` skill.

## Decisions Made

- **Adopted this project's existing "Gate" pattern instead of RED→GREEN
  checkpoints**, since the plan's own arguments explicitly called for that —
  precedent found in `docs/plans/2026-08-31-phase-7b-tuning-reconciliation.md`
  Task 6 (its own throughput re-baseline gate), which is measurement-only work
  with no test to write first. Reused its exact structure: numbered Gates, no
  commit until a gate actually changes a repo file.
- **Scoped the host spec to ≥4 dedicated (non-burstable) vCPUs** specifically
  to avoid introducing a new confound (CPU-steal noise) on top of the one
  variable this plan means to isolate (WSL2 vs. not). Named concrete example
  instance types (Hetzner CPX21, DO CPU-Optimized, AWS `c6i.xlarge`, GCP
  `c2-standard-4`) without mandating one.
- **Made the second-host split (Gate 5) conditional, not automatic** — only
  runs if the single-host move off WSL2 reproduces the same ~3,174 req/s
  ceiling. Keeps the common-case cost to one cheap VM and one hour, per the
  user's explicit "small, scoped task" instruction, while still being able to
  resolve 7b's second named hypothesis (same-host CPU contention between k6
  and `cmd/api`) if the first test is inconclusive.
- **Flagged cloud provisioning as requiring the user's explicit go-ahead**,
  twice in the plan (Gate 1 and the conditional Gate 5) — this spends real
  money and creates an account resource outside this session's visibility,
  which the session-wide risk-confirmation norm treats as needing a stop, not
  a judgment call to make alone.
- **Verified before reusing:** confirmed `cmd/api`'s config only requires
  `REDIS_ADDR` (`POSTGRES_DSN`/`KAFKA_BROKERS` belong to `migrate`/`relay`
  config, not `cmd/api`'s), so the plan uses `make up` rather than
  `make up-full` — matches what the load scenarios actually need. Also
  confirmed both k6 scripts already parameterize the target host via
  `API_BASE_URL` (`loadtest/lib/setup.js:9`), so the conditional two-host
  split (Gate 5) needs no script changes, just an env var.

## What Worked

- Grepping `docs/` for `-i wsl` before answering the environment question
  surfaced every relevant fact (the WSL2-browser-access journal, the Hyper-V
  port-exclusion note, the 7a/7b fidelity caveats) without needing to guess or
  re-derive anything from memory.
- Reading the actual `internal/config` code before assuming what env vars
  `cmd/api` needs — avoided writing a plan that told the executor to bring up
  Kafka/Postgres unnecessarily for a REST/WS-only load test.

## What Didn't Work

Nothing was attempted and abandoned this session — no code was written, so
there's nothing to warn a future session away from retrying.

## Test Coverage

Not applicable — no code changed. The plan itself specifies no `make
test`/`make build` re-run, since it touches no application code (stated
explicitly in the plan's Global Constraints and Task 2 Gate 4).

## Open Questions / Blockers

- No off-WSL2 host is provisioned yet. Task 1 Gate 1 is a hard stop pending
  the user's provider/size choice and cost go-ahead.
- Whether the ceiling turns out to be WSL2-bound, host-colocation-bound, or
  neither is genuinely unknown until Gate 3 (and possibly Gate 5) actually
  run — the plan is deliberately written to report whichever the evidence
  shows, not to assume WSL2 is the answer just because that's the
  hypothesis being tested.

## Relevant Commits

- `b029af0` — docs: plan an off-WSL2 load-test baseline run

## Next Step

Hand the plan to `executing-plans` when the user has a host ready (or is
ready to run the SSH commands themselves) — starting at Task 1 Gate 1's
go-ahead checkpoint.
