# 2026-08-31 — ansh — Phase 7a: instrumentation + load harness, executed and baselined

**Status:** Phase 7a's plan (`docs/plans/2026-08-31-phase-7a-instrumentation-load-harness.md`)
executed task-by-task on `phase-7a-instrumentation-load-harness`, all 7
tasks/gates green: toolchain raised to Go 1.26.7, `internal/metrics`
built, wager and WebSocket paths instrumented, a separate loopback
metrics listener wired into `cmd/api`, k6 installed and both load
scenarios run, the baseline recorded, `security-reviewer` run and its
one HIGH finding fixed, and the branch merged `--no-ff` into `dev`.
**Decided:** Measured-vs-target table (server-side, authoritative):
p99 bet placement **50 ms** vs 15 ms target — MISSED; WS sync **≤0.5 ms**
vs 30 ms — MET; throughput **3,174 req/s** vs 5,000 — MISSED;
double-spend 0.00% — MET (pre-existing proof, not re-measured this
phase). Full detail: `docs/reports/2026-08-31-phase-7a-baseline.md`.
Also: `security-reviewer` found a genuine HIGH — a data race on
`Client.sync` between `Room.run`'s join handling and `WritePump`,
introduced by this phase's own wiring (`internal/ws/room.go`) — fixed by
making `Room.Join` block on an acknowledgement channel until `run()`
finishes processing the join, closing the happens-before gap. See
`docs/project-history.md`'s `### Phase 7a` entry for the full finding.
**Spec:** No change — this phase measures against spec §7, it doesn't
change it.
**Next:** Phase 7b — hardening and tuning driven by this baseline's two
MISSED targets (bet placement p99, throughput), the three security items
still open by design (login timing, room-code bias, reconnect grace
window), and the post-load Redis↔PostgreSQL reconciliation re-run this
phase deliberately deferred.
**Blocked on:** Nothing.
**Touches:** `backend/internal/toolchain/`, `backend/internal/metrics/`,
`backend/internal/wager/service.go`, `backend/internal/ws/{client,room,hub}.go`,
`backend/internal/config/config.go`, `backend/cmd/api/main.go`,
`loadtest/`, `docs/reports/2026-08-31-phase-7a-baseline.md`.

---

## What We Worked On

Picked up from the prior session's journal (WSL2 browser-access) plus a
newer commit found in git log (`44c3ce1`, from a peer session) that had
already split Phase 7 into 7a/7b in the implementation plan and written
7a's full detailed plan. User asked to start executing 7a once Docker was
confirmed reachable in this WSL2 shell (it initially wasn't — the known
per-distro WSL-integration gotcha — user started Docker Desktop mid-session
and it came up fine).

Used the `executing-plans` skill throughout: branch `phase-7a-instrumentation-load-harness`
off `dev`, then all 7 tasks in order, each a real RED→GREEN→commit cycle.

## Decisions Made

- **Two checkpoints were front-run during implementation** (the
  production-loopback METRICS_ADDR check in Task 5 CP1, mirroring an
  earlier slip in Task 1 CP1 that wasn't caught until CP2) — caught by
  re-reading the plan's own checkpoint separation before moving on, in
  Task 5's case by temporarily stripping the just-written logic, proving
  the test failed against the stripped version, then restoring it. No
  functional cost, just a discipline note for next time: write the
  narrower CP1 contract literally as specified, don't anticipate CP2's
  requirement even when the code is trivial to add together.
- **`rest_throughput.js` targets `GET /healthz`, not an authenticated
  route**, despite the plan's text describing "an authenticated request
  against a cheap read route." Every authenticated route in this codebase
  is rate-limited to 60 req/min/user (`apiThrottle`); sustaining 5,000
  req/s against one would mean minting a fresh account every ~200µs,
  making `argon2id` hashing (deliberately slow) the thing under test
  instead of REST throughput. No such cheap authenticated GET route
  exists in the codebase to begin with. Documented in the script's header
  comment and the baseline report — this is the "Go source wins, plan is
  stale" resolution the plan itself authorizes for exactly this kind of
  mismatch.
- **`wager_latency.js`'s players connect and join *before* the host opens
  the round**, not after (host has a `startTime: '5s'` delay, players
  start at `0s`). `round_opened` is broadcast once, at creation, with no
  catch-up message for a client that connects afterward (unlike
  `player_joined`'s newcomer backfill from Phase 6a) — a player joining
  after the broadcast would simply never see it and never wager. Found
  this via a smoke test that produced zero `place_wager` sends before the
  reorder.
- **Baseline numbers are taken at face value, not chased down**, per
  Task 7's own framing: 7a's job is a trustworthy measurement, not a good
  number. `go run` (not `go build`), WSL2 networking, and 4 cores shared
  with this Claude Code session are all named as uncontrolled variables in
  the report rather than something this session tried to eliminate.

## What Worked

- **Server-side histograms confirmed correctly wired** by cross-checking
  `curl http://127.0.0.1:9090/` against k6's own summary after every load
  run — `callit_wager_place_ok_count` matched k6's completed-wager count
  each time, `callit_ws_send_dropped` stayed `0` throughout.
- **k6 v2.2.0's `k6/websockets` module** (the plan named
  `k6/experimental/websockets`, which now warns as deprecated in favor of
  the graduated `k6/websockets` — confirmed via a smoke test rather than
  guessed) and a `Promise`-returning exec function both work as expected
  for a long-lived, event-driven per-VU socket session.
- **`go.sum` unchanged after the toolchain raise** — confirmed the Task 1
  scope decision (raise the toolchain, hold the five dependency pins)
  held exactly as intended.

## What Didn't Work

- **`k6/experimental/timers`'s `setTimeout`** — errors at runtime in
  k6 v2.2.0 ("has been graduated... remove the import"); the global
  `setTimeout` (no import) is what actually works now.
- **A `rest_throughput.js`-style single-shared-user setup for
  `wager_latency.js`'s players connecting after the host opens the
  round** — zero wagers ever sent, silently (no error, just an idle
  scenario) — see the reorder decision above.
- **`__ITER`/`__VU` referenced inside code paths called from k6's
  `setup()`** (used for email uniqueness in `registerUser`) — `ReferenceError:
  __ITER is not defined`, since neither is defined in the `setup()`
  execution context. Replaced with `Date.now()` + `Math.random()`, which
  works in both `setup()` and per-VU exec functions.
- **Killing the API process between baseline runs with `pkill -f
  "cmd/api"`** — missed the actual compiled binary (`go run` spawns a
  child process whose name is just the binary, not the `cmd/api` source
  path), leaving a stale listener that then failed a fresh `go run` with
  "address already in use." Had to find and kill the real PID via `ss
  -tlnp` / `lsof` by port instead.
- **Threading `Recorder`/`DropCounter` into `Room.Join` (Task 4 CP2)
  without also making `Join` wait for `run()` to finish processing** —
  every task-boundary test passed, including a dedicated `-race` test for
  the `WritePump`/broadcast path itself, because none of them modeled
  `Handler`'s real call sequence (`hub.Join()` then immediately `go
  c.WritePump()`) under real goroutine scheduling. Only
  `security-reviewer`'s targeted repeated-Join/WritePump probe caught it.
  Lesson for next time a value gets assigned onto a struct from inside an
  actor goroutine after a caller's blocking call returns: the channel
  rendezvous only orders the *value transfer*, never the actor's
  subsequent statements — anything written after the receive needs its
  own acknowledgement if a caller depends on seeing it.

## Test Coverage

- **Covered:** `internal/toolchain` 96.4%, `internal/metrics` 96.6%,
  `internal/wager` 87.5%, `internal/ws` 93.8%/94.7% across the phase's
  checkpoints, `internal/config` 91.4% — all via `go test
  ./... -coverpkg=./...` runs at every task boundary, full suite green
  throughout (Redis/PostgreSQL/Kafka all live).
- **Not covered yet:** the k6 scripts themselves have no automated
  assertion beyond their own thresholds (by design — Task 6 is a
  verification gate, not a Go test); 7b's post-load reconciliation
  re-run is explicitly deferred, not skipped by oversight.

## Relevant Commits

- `7c44182` — test: parse the go directive and CI's go-version pin
- `cfdb262` — chore: raise the Go toolchain to 1.26.7 and guard the CI pin
- `7c694f1`, `94e4924`, `632c25f`, `dfb52fd` — the histogram + registry (Task 2)
- `c364936`, `3e46a96` — wager placement latency (Task 3)
- `5854ee6`, `6a678ed` — WebSocket sync latency + drop counting (Task 4)
- `60620ca`, `7730e97`, `5dd7778` — METRICS_ADDR config + listener wiring (Task 5)
- `d493bde`, `7b5d9ed`, `79a2ebd` — k6 install + both scenarios (Task 6)
- `0f96965` — docs: record the Phase 7a performance baseline (Task 7 Gate 1)
- `c9b635a` — fix: close a data race between Room.Join and WritePump on Client.sync (post-review)
- `2372ef9` — docs: record Phase 7a security review findings

## Next Step

Phase 7b: tune the wager placement path toward the 15 ms target (profile
`wager.Service.Place`'s span rather than assuming the Lua round trip
dominates — this session's numbers came from an unoptimized `go run`
build under host contention, not a controlled measurement), investigate
the throughput ceiling seen even against `GET /healthz` (the cheapest
route in the system), land the three security items open by design
(login timing, room-code modulo bias, reconnect grace window), and
re-run the Redis↔PostgreSQL reconciliation test after a real k6 load run
(`docs/plans/2026-08-21-implementation-plan.md` §12).
