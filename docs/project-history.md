# CallIt — Project History

Phase-by-phase outcomes, security-review findings, and coverage snapshots.

**This file is not loaded into context.** It exists so `CLAUDE.md` doesn't
have to carry it — that file is re-sent on every single turn, so anything
there costs tokens continuously whether or not it's relevant. Read this when
you need the archaeology: why a convention exists, what a past phase actually
cost, what a security review already checked.

The primary record is still `journal/` (one entry per session, newest first)
and each phase's own plan under `docs/plans/`. This file collects the parts
that used to live in `CLAUDE.md`.

---

## Commit-granularity convention: how it was validated

**Phase 0 landed as a single commit — that was the mistake the
branch-per-phase / commit-per-checkpoint convention exists to prevent.**

**Phase 1** validated the fix: 22 checkpoint commits, one per behavior, on
`phase-1-domain-core`. That run also surfaced a real defect — checkpoints
whose test passed the moment it was written, because an earlier checkpoint's
implementation already satisfied it. `writing-plans` now requires a checkpoint
to be a genuine RED→GREEN cycle, not just a labeled commit.

**Phase 2** was the first plan written under the spec-driven format (adopted
in `ab190b9`, *after* Phase 1's plan was written — which is why Phase 1 ran
under the old code-heavy format and hit 3,111 lines). It held up under a cold
executor: 1,591 lines for a phase with more moving parts, no precision lost.
The plan's own contracts caught a real plan defect during execution — its CP2
assertion `Σ wallets + Σ pools == 1500` was inconsistent with
`settle_round.lua`'s own KEYS list, which never touches the pools key; fixed
to the correct invariant, `Σ wallets + Dust`, while implementing. Landed as 28
commits against an estimated 31–32, because two checkpoints combined
(`lock_round.lua`'s `ALREADY_LOCKED` case turned out black-box
indistinguishable from its unconditional-OK predecessor at the Go API surface)
and the four concurrency verifications committed as two batches.

**Phase 3** landed ~49 commits against an estimate of ~44 — the largest phase
(12 tasks, 4 deliverables). The delta was real defects the plan didn't
anticipate, each fixed in its own commit:

- an early `go mod tidy` stripped the phase's new dependencies before anything
  imported them (the same failure Phase 2's journal already warned about, from
  `go get` immediately followed by `tidy`)
- a rate-limit-window test that passed even with eviction disabled, because the
  key's own `PEXPIRE` TTL masked the missing `ZREMRANGEBYSCORE`
- a `TestMain`-per-package `FLUSHDB` race across
  `redisstore`/`account`/`room`/`httpapi` once a second integration-test
  package existed — fixed with `-p 1`, which was also missing from CI
- Task 7 CP5's own test, whose "both succeed" narrative didn't arithmetically
  agree with its own contract; redesigned as a 20-trial statistical test after
  a single racing pair proved unfalsifiable in practice (~5/50 hits cold vs
  ~49/50 warm)

The observable-signal rule held for the cases the plan flagged in advance
(Task 6 CP3, Task 7 CP2 — both passed immediately as anticipated, verified as
genuine RED by disabling the guard). The CP5 case is a fourth pattern that rule
doesn't yet name: **an interaction test whose result depends on a resource's
warm-up state, not just its code path.** Worth folding into `writing-plans` if
it recurs.

**Phase 4** was split into 4a (transport) and 4b (round lifecycle) — the first
phase split before planning rather than after. Both ran under the two-step
checkpoint format. 4a: 25 checkpoints, 25 commits. 4b: 31 checkpoints, 32
commits, 6 real plan defects found during execution (including a genuine
`round`↔`ws` import cycle and a typed-nil-in-interface panic). Full accounting
in `journal/2026-08-26_0250_ansh_tuned-plan-experiment-verdict.md`.

---

## Security reviews

### Phase 3 — `internal/auth`, `internal/account`, `internal/httpapi`, three new Lua scripts

No CRITICAL or HIGH. Every item the plan called out to confirm held:
constant-time password comparison, HS256-only JWT verification,
no-enumeration login, generic 500s, rate limiter fail-closed,
`X-Forwarded-For` ignored. One LOW (a hardcoded `"3"` instead of
`domain.RefillQuota` in a response header) fixed on the spot.

### Phase 4b — `internal/round`, `internal/wager`, `internal/ws`

One HIGH, fixed on the spot: the socket router's error-code table had no case
for a throttled wager (`wager.ErrRateLimited`), so it fell through to the
generic `internal_error` code instead of `rate_limited`, giving a rate-limited
client no signal to back off. Now mapped, with the retry-after duration folded
into the message.

Every invariant the plan called out held: host-cannot-wager enforced in Lua and
mirrored in the router's error table; wager anonymity, with the `odds_updated`
payload's key set asserted directly rather than assumed; lockout via Redis
`TIME` not Go; UUIDv4 idempotency keys validated; settlement math computed once
in `internal/domain`; room ID always taken from the verified token claim rather
than the message payload; one shared rate limiter; no hardcoded secrets.

One MEDIUM raised (a session ending twice on a rapid disconnect/reconnect in
the same window) was the known limitation Task 8 CP2 already documented — not
a new gap.

### Open by design, deferred to Phase 7 hardening

1. **Login timing.** The unknown-email path skips argon2id and so responds
   faster than the wrong-password path, even though response bodies are
   byte-identical. Closing it needs a dummy-hash verify on the miss path.
2. **Room-code modulo bias.** Accepted — the code is a lookup handle, not a
   secret; authorization rests on the JWT.
3. **Reconnect ends a session.** `EndSession` fires on socket disconnect, so a
   dropped player restarts at the room buy-in. Resume needs a grace window.

---

## Coverage notes

**Don't keep a coverage table here or in `CLAUDE.md` — it goes stale every
phase.** Run `make test` for current figures. What's worth recording is the
two standing interpretations:

**`internal/account` and `internal/room` read low (~29%/50%) under a plain
per-package `go test ./pkg/ -cover`. That's a measurement artifact, not a
gap.** Their `Register`/`Login`/`Create`/`Join` methods are exercised entirely
by `internal/httpapi`'s black-box HTTP tests, and Go's coverage tool attributes
a line only to the test binary that directly executed it. Under
`go test ./... -coverpkg=./...` (which attributes to the *source* package):
`account.Register` 85.7%, `account.Login` 85.7%, `account.ClaimRefill` 82.4%,
`room.Create` 85.7%, `room.Join` 82.6%. **Don't add redundant direct tests to
move the per-package number** — that pads a metric rather than closing a gap.
Suspect a real gap? Check the `-coverpkg` profile first.

**`internal/redisstore` sits just under its ≥85% aspirational floor.** The
remainder is defensive `if err != nil` branches after a Redis call,
unreachable without fault-injecting the connection itself. Dead-but-safe —
don't chase them with a fake Redis to flip the percentage.

**`cmd/*` at 0% is expected**, not a gap: thin wiring with no branching logic.

---

## Tooling decisions

Skills are installed per-phase rather than up front, per the parent plan's
"Tooling to import" column (plan §9) — rule dirs are always-loaded into every
turn and shouldn't sit in context for a stack the project isn't touching yet.

- **Installed:** `golang-patterns`, `golang-testing`, `docker-patterns`
  (before Phase 0) · `redis-patterns` (before Phase 2) · `api-design` (before
  Phase 3 — drove `/api/v1` versioning, resource naming like
  `POST .../participants` rather than a `/rooms/join` verb, and the
  envelope/error-code conventions).
- **Not yet installed:** `postgres-patterns`, `database-migrations` (Phase 5).
- **Dormant:** `continuous-learning-v2` is present under `.claude/skills/`
  from the bulk ECC install but deliberately off (`observer.enabled: false`,
  no hooks wired). Don't enable it without re-reading
  `dev-workflow-guide.md` §9 — it was evaluated and declined for this project
  (redundant with memory + journal, statistically rather than
  judgment-curated).
- **Declined:** `subagent-driven-development`, same section. Note its stated
  revisit trigger — "a phase's implementation genuinely exhausts context
  before completing" — **has now fired** (Phase 3). See
  `journal/2026-08-26_0250_ansh_tuned-plan-experiment-verdict.md` for the
  measurements and why a thin project-local delegation skill is preferred over
  installing SDD itself.

---

## Environment verification log

Recorded once so it doesn't have to be re-derived; the actionable rules stay
in `CLAUDE.md`'s Known Environment Gotchas.

- **Go toolchain pins** (verified 2026-08-24/25): go-redis v9.19.0+ declares
  `go 1.24`; `golang.org/x/crypto` v0.34.0 declares `go 1.23.0` and `go get`
  will silently rewrite this module's `go` directive to accept it. Checked
  across go-redis v9.18.0 through v9.22.0. `gorilla/websocket` v1.5.3 declares
  `go 1.12` — safe (verified 2026-08-26).
- **Phase 5 dependency pins** (verified 2026-08-26, at Phase 5's planning
  pass): every *current* release of all three Phase 5 dependencies declares
  `go >= 1.23` and would rewrite this module's directive. A compatible set
  exists and was proven by building a probe module against
  `go 1.22.10` — `go build` clean with `pgxpool`, migrate's `postgres`
  driver, and `kafka.Writer` all imported, directive unchanged:

  | Module | Pin | First incompatible release |
  |---|---|---|
  | `github.com/jackc/pgx/v5` | **v5.7.4** | v5.7.5 (`go 1.23.0`) |
  | `github.com/segmentio/kafka-go` | **v0.4.48** | v0.4.49 (`go 1.23`) |
  | `github.com/golang-migrate/migrate/v4` | **v4.18.2** | v4.18.3 (`go 1.23.0`) |

  Rejected alternatives, both walled off at the same boundary:
  `twmb/franz-go` (v1.19.0+ needs `go 1.23.8`; last compatible v1.18.1) and
  `IBM/sarama` (v1.45.2 needs `go 1.23.0`). `kafka-go` was preferred anyway —
  its last four releases all declare `go 1.15`, so it has the most headroom
  before the next wall.

  **This is the phase where the 1.22.10 pin started costing something.** It
  was free through Phase 4; here it constrains three of three new
  dependencies. Go 1.22 is past upstream EOL, so each future phase's
  dependency search gets narrower. Raising the toolchain is not required for
  Phase 5 and was deliberately not bundled into it, but it is now a real
  candidate for Phase 7 hardening rather than a theoretical one.
- **Docker/WSL2** (verified 2026-08-23): with per-distro WSL integration on,
  `docker compose up -d` brings up Redis and PostgreSQL, both reporting
  `healthy` (`redis-cli ping` → `PONG`, `pg_isready` → accepting connections).
  The `full` profile was also verified — Kafka in KRaft mode starts, reports
  `healthy`, ~290MB RSS. The whole compose file is confirmed working, not just
  YAML-valid.
