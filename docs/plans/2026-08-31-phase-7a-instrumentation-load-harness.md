# Phase 7a — Instrumentation + Load Harness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give spec §7's performance SLAs real measured numbers — a raised
toolchain, server-side latency histograms on the wager and broadcast paths, a
k6 harness that drives genuine load, and a recorded baseline stating which
targets are met and which are not.

**Architecture:** A new pure package `internal/metrics` owns a fixed-bucket
latency histogram and a named registry; it has no dependencies and no I/O
beyond one `http.Handler` that renders the registry as text.
`internal/wager` and `internal/ws` each take a consumer-side `Recorder`
interface and observe their own latency — neither imports a registry.
`cmd/api` owns the registry and serves it on a **separate, default-disabled,
loopback-bound listener**, never on the public CORS'd mux. k6 scripts under
`loadtest/` drive REST and WebSocket load against a live stack; k6's own
`thresholds` encode the SLAs so a run exits non-zero when a target is missed.

**Tech Stack:** Go 1.26.7 (raised from EOL 1.22.10 in Task 1) · no new Go
dependencies · k6 v2.2.0 (external binary, user-local install) · existing
Redis/PostgreSQL/Kafka Compose stack.

**Spec:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md) §7 (Performance & Scale Targets)
**Parent plan:** [`docs/plans/2026-08-21-implementation-plan.md`](2026-08-21-implementation-plan.md) §9 (row 7a), §10 (Risks)

---

## Global Constraints

Every task's requirements implicitly include this section.

- **SLA targets, verbatim from spec §7:** p99 bet placement latency **< 15 ms**;
  global WebSocket sync latency **< 30 ms**; target throughput **5,000+
  requests/sec**; double-spend tolerance **exactly 0.00%**.
- **Go toolchain floor is 1.26 from Task 1 onward** (`go.mod` directive
  `1.26.7`, CI `go-version: "1.26"`). Before Task 1 completes, the repo is
  still on 1.22.10.
- **Never run `go get -u`, and pin every `go get` target explicitly,
  subpackages included** (`CLAUDE.md`). This phase adds **no new Go
  dependency** — if a task appears to need one, stop and report rather than
  adding it.
- **`go` may report "not found" in a non-interactive shell.** Prefix every Go
  command with
  `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin` (see `CLAUDE.md`,
  Known Environment Gotchas). No sudo is available; install user-locally.
- **`-p 1` is load-bearing.** Never drop it from a `go test ./...` invocation.
  Integration suites share Redis **DB 15**.
- **`internal/domain` stays free of I/O.** `internal/metrics` is a new
  package and is *not* part of `internal/domain`; nothing in this phase adds
  an import to `internal/domain`.
- **All amounts are integer token units.** Latency values are
  `time.Duration`, never floats, until rendering.
- **Wagers stay anonymous until the round is terminal.** No metric may carry
  a per-user label, a room ID, a round ID, or any per-wager amount. Every
  metric in this phase is an aggregate over the whole process. A metrics
  surface that let a scraper count one user's wagers would breach the same
  invariant `Settlement.Results` exists to gate.
- **The browser origin allowlist has exactly one definition.** The metrics
  listener is a separate `http.Server` and never passes through
  `httpapi.CORS`; it adds no second allowlist and no route to `NewMux`.
- **`cmd/*` at 0% coverage is expected, not a gap.** Wiring in `cmd/api` is
  verified at a task boundary, not checkpointed.
- **Judge coverage from `go test ./... -coverpkg=./...`**, never the
  per-package figure.

### A note on checkpoint honesty in Tasks 6 and 7

Tasks 1–5 are ordinary RED→GREEN checkpoints. **Tasks 6 and 7 are
deliberately written as numbered *verification gates*, not checkpoints,
because their deliverables are a k6 script and a measurement report — neither
has a Go test that can fail before the artifact exists.** Writing them as
checkpoints would produce a Step 1 that says "expect FAIL" for something that
cannot fail, which is exactly the contradiction that halts a cold executor
(`writing-plans`, Bite-Sized Task Granularity). Each gate still ends in one
commit, chained behind a real command whose exit code gates it.

---

## File Structure

**Created:**

| Path | Responsibility |
|---|---|
| `backend/internal/toolchain/toolchain.go` | Pure parsers for `go.mod`'s `go` directive and CI's `go-version` pin, plus the `MinGo` floor constant. |
| `backend/internal/toolchain/toolchain_test.go` | Parser table tests + the drift/floor guard that reads the real `go.mod` and `ci.yml`. |
| `backend/internal/metrics/histogram.go` | Fixed-bucket latency histogram: `Observe`, `Quantile`, `Count`, `Sum`. Lock-free via `sync/atomic`. |
| `backend/internal/metrics/registry.go` | Named histogram registry + `Handler` rendering it as line-oriented text. |
| `backend/internal/metrics/histogram_test.go` | Quantile exactness, overflow, concurrency. |
| `backend/internal/metrics/registry_test.go` | Registry get-or-create, render format. |
| `loadtest/README.md` | How to install k6 user-locally and run each scenario. |
| `loadtest/lib/setup.js` | Shared helpers: register a user, create a room, join by code, return tokens. |
| `loadtest/rest_throughput.js` | REST ramp measuring requests/sec against the 5,000 rps target. |
| `loadtest/wager_latency.js` | WebSocket wager storm measuring end-to-end wager acknowledgement latency. |
| `docs/reports/2026-08-31-phase-7a-baseline.md` | The measured baseline vs. each spec §7 target, with the WSL2 fidelity caveat. |

**Modified:**

| Path | Change |
|---|---|
| `backend/go.mod` | `go 1.22.10` → `go 1.26.7`. |
| `.github/workflows/ci.yml:43,118` | `go-version: "1.22"` → `"1.26"` (both jobs). |
| `backend/internal/config/config.go` | Add `MetricsAddr string` with validation. |
| `backend/internal/wager/service.go` | Observe placement latency; `NewService` takes recorders. |
| `backend/internal/ws/client.go` | `send chan []byte` → `chan outbound`; `WritePump` observes enqueue→write. |
| `backend/internal/ws/room.go` | `broadcastCmd` carries an enqueue timestamp; evictions count a drop. |
| `backend/internal/ws/hub.go` | Thread the recorder through `NewHub`. |
| `backend/cmd/api/main.go` | Build the registry, inject recorders, start the metrics listener. |
| `Makefile` | Real `loadtest` target. |
| `CLAUDE.md` | Toolchain pin note rewritten; `METRICS_ADDR` and real `make loadtest` documented; one new invariant. |
| `README.md` | `METRICS_ADDR` section. |
| `docs/plans/2026-08-21-implementation-plan.md` | §12 acceptance pointer. |

---

## Task 1: Raise the Go toolchain off EOL 1.22.10

Go 1.22 is past upstream EOL, so the runtime and standard library no longer
receive security patches — retiring it is hardening, which is what Phase 7
is for. This is Task 1 rather than a later task because **a p99 baseline
measured on 1.22.10 and re-taken on 1.26.7 was never a baseline**: the
toolchain must stop being a variable before anything is measured.

**Scope decision — the toolchain moves, the five pinned dependencies do
not.** `CLAUDE.md` pins `go-redis/v9`, `golang.org/x/crypto`, `jackc/pgx/v5`,
`segmentio/kafka-go`, and `golang-migrate/migrate/v4` *only* because newer
versions declare a `go` directive above 1.22.10. Raising to 1.26.7 lifts that
constraint for all five at once, but lifting a constraint is not a reason to
act on it. Upgrading five dependencies inside the phase whose job is stable
measurement would add a second variable to every number this phase produces.
Task 1 therefore rewrites the pin note to record that the constraint is
lifted and the versions are now a deliberate hold; the upgrades themselves
belong to 7b or Phase 8.

**Files:**
- Create: `backend/internal/toolchain/toolchain.go`
- Test: `backend/internal/toolchain/toolchain_test.go`
- Modify: `backend/go.mod:3`, `.github/workflows/ci.yml:43`, `.github/workflows/ci.yml:118`, `CLAUDE.md` (Stack section's pin paragraph)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `const MinGo = "1.26"` — the major.minor floor.
  - `func ParseGoDirective(gomod string) (string, error)` — returns the full
    version string from the `go` line, e.g. `"1.26.7"`.
  - `func ParseCIPin(workflow string) ([]string, error)` — returns every
    `go-version:` value in the workflow, in file order, quotes stripped.
  - `func MajorMinor(version string) string` — `"1.26.7"` → `"1.26"`;
    `"1.26"` → `"1.26"`.

**Checkpoint 1: the two parsers and the major.minor reducer**

- [ ] **Step 1: Write the failing test, then run it**

Table-driven tests for three pure functions. Exact cases:

`ParseGoDirective`:
- input `"module example.com/x\n\ngo 1.22.10\n\nrequire (\n)\n"` → `"1.22.10"`, nil error
- input `"module example.com/x\n\ngo 1.26\n"` → `"1.26"`, nil error
- input `"module example.com/x\n"` (no `go` line) → `""`, error whose message
  contains `no go directive`
- input `"module x\n// go 1.99\n"` (commented-out directive only) → `""`,
  error containing `no go directive` — a comment is not a directive
- input `"module x\ngo 1.26.7 // toolchain floor\n"` → `"1.26.7"` (a trailing
  comment on a real directive is stripped, not an error)

`ParseCIPin`:
- input `"jobs:\n  backend:\n    steps:\n      - uses: actions/setup-go@v5\n        with:\n          go-version: \"1.22\"\n"` → `["1.22"]`, nil error
- input containing two `go-version: "1.26"` lines → `["1.26", "1.26"]`
- input with `go-version: 1.26` (unquoted) → `["1.26"]` — YAML permits it
- input with no `go-version` line → empty slice, error containing
  `no go-version pin`
- input with `node-version: "24"` and no `go-version` → error containing
  `no go-version pin`; a `node-version` line must never be matched

`MajorMinor`:
- `"1.26.7"` → `"1.26"`; `"1.26"` → `"1.26"`; `"1.22.10"` → `"1.22"`;
  `""` → `""`

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/toolchain/... -race -count=1
```
Expected: FAIL — `no required module provides package .../internal/toolchain`
(the package does not exist yet).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `ParseGoDirective` scans lines, ignores anything after `//`, and
returns the single token following a line whose first field is exactly `go`.
`ParseCIPin` collects the value after `go-version:` on any line whose trimmed
prefix is `go-version:`, stripping surrounding quotes and any trailing
comment. `MajorMinor` returns the first two dot-separated components, or the
input unchanged when it has fewer than two. All three are pure — no file I/O
inside these functions; the caller supplies the text.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/toolchain/... -race -count=1 && \
  cd .. && git add backend/internal/toolchain/toolchain.go backend/internal/toolchain/toolchain_test.go && \
  git commit -m "test: parse the go directive and CI's go-version pin"
```
Expected: PASS, then one commit.

**Checkpoint 2: the floor-and-drift guard, and the raise that satisfies it**

This is the checkpoint that actually moves the toolchain. It goes RED against
today's repository because `go.mod` says `1.22.10`, which is below the `1.26`
floor.

- [ ] **Step 1: Write the failing test, then run it**

Add `TestToolchainPinsMeetFloorAndAgree` to the same test file. It reads two
real repository files relative to the package directory:
- `../../go.mod` (i.e. `backend/go.mod`)
- `../../../.github/workflows/ci.yml`

and asserts three things:
1. `MajorMinor(ParseGoDirective(gomod))` is **not less than** `MinGo`,
   compared component-wise as integers (so `1.9` < `1.26`, which a string
   compare would get wrong). Failure message must name both the found version
   and `MinGo`.
2. `ParseCIPin(workflow)` returns **at least two** pins — the workflow has a
   `backend` job and a `frontend-e2e` job, and a raise that moves only one is
   the drift this guard exists to catch.
3. **Every** returned CI pin equals `MajorMinor(goDirective)` exactly.
   Failure message must name the mismatching pin and the `go.mod` value.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/toolchain/... -race -count=1 -run TestToolchainPinsMeetFloorAndAgree
```
Expected: FAIL on assertion 1 — go.mod's directive is `1.22.10`, major.minor
`1.22`, which is below the `1.26` floor.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

There is no production code to write here — the "implementation" is the raise
itself. Perform it in this order:

1. **Install Go 1.26.7 user-locally** (no sudo — `CLAUDE.md`):
   ```bash
   cd "$HOME" && \
     curl -fsSLO https://go.dev/dl/go1.26.7.linux-amd64.tar.gz && \
     rm -rf "$HOME/.local/go" && \
     mkdir -p "$HOME/.local" && \
     tar -C "$HOME/.local" -xzf go1.26.7.linux-amd64.tar.gz && \
     rm go1.26.7.linux-amd64.tar.gz && \
     "$HOME/.local/go/bin/go" version
   ```
   Expected output: `go version go1.26.7 linux/amd64`. **`rm -rf $HOME/.local/go`
   replaces the existing 1.22.10 tree — that path holds only the unpacked
   toolchain, no project files, and `$HOME/go` (GOPATH) is a different
   directory and is not touched.** If the download fails, stop and report;
   do not fall back to `sudo`, which cannot read a password here.
2. **`backend/go.mod` line 3:** `go 1.22.10` → `go 1.26.7`.
3. **`.github/workflows/ci.yml` lines 43 and 118:** `go-version: "1.22"` →
   `go-version: "1.26"`. Both. Leave `node-version: "24"` untouched.
4. **`CLAUDE.md`, the Stack section's dependency-pin paragraph:** rewrite it
   so it no longer claims the five versions are forced by the toolchain.
   State that Phase 7a raised the module to `go 1.26.7` (CI pin `1.26`),
   which lifted the `go`-directive ceiling on all five; that the five
   versions are now a deliberate hold rather than a constraint, unchanged so
   that Phase 7a's baseline has exactly one variable; and that
   `backend/internal/toolchain`'s guard test now fails CI if `go.mod` and
   either CI `go-version` pin ever drift apart or fall below the floor —
   which is the automated form of the manual rule that paragraph used to
   state. Keep the standing warnings: **never `go get -u`**, and **always
   pin every `go get` target explicitly, subpackages included**.

Then run the **full** suite — a toolchain raise is exactly the change that
can break a package far from the edit:

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  go version && make test && \
  git add backend/go.mod .github/workflows/ci.yml CLAUDE.md backend/internal/toolchain/toolchain_test.go && \
  git commit -m "chore: raise the Go toolchain to 1.26.7 and guard the CI pin"
```
Expected: `go version go1.26.7 linux/amd64`, then the full backend suite
PASSes (Redis, PostgreSQL and Kafka are brought up by `make test`), then one
commit. **If any package fails on the new toolchain, stop and report — do not
silence a failure to get the commit through.** `go.sum` is unchanged because
no dependency version moved; if `go.sum` does change, stop, because that
means something upgraded a dependency and the scope decision above was
violated.

---

## Task 2: `internal/metrics` — the latency histogram and registry

Pure computation, no I/O, no dependencies. This is the measurement primitive
every later task feeds.

**Design.** Fixed bucket boundaries with **no interpolation**: `Quantile(q)`
returns the upper bound of the lowest bucket whose cumulative count reaches
`ceil(q × N)`. This reports "p99 is at most X", which is exactly the shape an
SLA check needs — `p99 ≤ 15ms` is directly readable — and it is exactly
testable, unlike an interpolated estimate. Boundaries sit **on** both SLA
values so neither target needs a bucket straddled:

`0.5, 1, 2, 5, 10, 15, 20, 30, 50, 100, 250, 500, 1000` milliseconds, plus a
final overflow bucket.

Counters are `atomic.Uint64` rather than mutex-guarded: at a 15 ms target the
recorder must not meaningfully distort what it measures, and an atomic add is
nanoseconds. The consequence is that a snapshot read taken *during* concurrent
writes may see cumulative bucket counts that do not sum to `Count()`; that is
accepted and must be stated in the doc comment. Quantile tests therefore run
against a quiesced histogram.

**Files:**
- Create: `backend/internal/metrics/histogram.go`, `backend/internal/metrics/registry.go`
- Test: `backend/internal/metrics/histogram_test.go`, `backend/internal/metrics/registry_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `var Bounds = []time.Duration{...}` — the 13 boundaries above, ascending.
  - `type Histogram struct{ ... }`; `func NewHistogram() *Histogram`
  - `func (h *Histogram) Observe(d time.Duration)`
  - `func (h *Histogram) Quantile(q float64) (time.Duration, bool)` — `ok` is
    false when no samples have been observed.
  - `func (h *Histogram) Count() uint64`
  - `func (h *Histogram) Sum() time.Duration`
  - `var OverTopBucket = time.Duration(-1)` — returned by `Quantile` when the
    quantile falls in the overflow bucket.
  - `type Counter struct{ ... }`; `func (c *Counter) Inc()`; `func (c *Counter) Value() uint64`
  - `type Registry struct{ ... }`; `func NewRegistry() *Registry`
  - `func (r *Registry) Histogram(name string) *Histogram` — get-or-create,
    same pointer for the same name.
  - `func (r *Registry) Counter(name string) *Counter` — get-or-create.
  - `func (r *Registry) Render() string`
  - `func Handler(r *Registry) http.Handler`

**Checkpoint 1: Observe and Quantile over a quiesced histogram**

- [ ] **Step 1: Write the failing test, then run it**

Spec, all against a fresh `NewHistogram()`:
- No observations → `Quantile(0.99)` returns `(0, false)`.
- Observe exactly one sample of `3*time.Millisecond` → `Quantile(0.99)`
  returns `(5*time.Millisecond, true)` — 3 ms falls in the bucket whose upper
  bound is 5 ms.
- Observe 100 samples of `1*time.Millisecond` → `Quantile(0.99)` returns
  `(1*time.Millisecond, true)`; `Count()` returns `100`;
  `Sum()` returns `100*time.Millisecond`.
- Observe 99 samples of `1*time.Millisecond` and 1 sample of
  `40*time.Millisecond` → `Quantile(0.99)` returns `(1*time.Millisecond, true)`
  (ceil(0.99×100) = 99, reached inside the 1 ms bucket) and
  `Quantile(1.0)` returns `(50*time.Millisecond, true)`.
- Observe a single sample of exactly `15*time.Millisecond` →
  `Quantile(0.5)` returns `(15*time.Millisecond, true)`. **A sample equal to a
  boundary belongs to that boundary's bucket, not the next one** — otherwise a
  run that lands exactly on the 15 ms SLA would report as a miss.
- Observe a single sample of `0` → `Quantile(0.5)` returns
  `(500*time.Microsecond, true)`.
- Observe a single negative sample of `-1*time.Millisecond` →
  `Count()` returns `0` and `Quantile(0.5)` returns `(0, false)`. A negative
  duration means the caller's clock went backwards; it must be dropped, not
  folded into the first bucket where it would silently improve the p99.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/metrics/... -race -count=1
```
Expected: FAIL — `no required module provides package .../internal/metrics`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Observe(d)` ignores `d < 0`; otherwise it increments the counter
for the lowest bucket whose bound is `>= d`, increments the total count, and
adds `d` to the sum. `Quantile(q)` computes `target := ceil(q * float64(N))`,
walks buckets ascending accumulating counts, and returns the first bound whose
cumulative count `>= target`. Returns `(0, false)` when `N == 0`. Document
the accepted snapshot-skew under concurrent writes in the type's doc comment.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/metrics/... -race -count=1 && \
  cd .. && git add backend/internal/metrics/histogram.go backend/internal/metrics/histogram_test.go && \
  git commit -m "feat: a fixed-bucket latency histogram with exact quantiles"
```
Expected: PASS, then one commit.

**Checkpoint 2: samples above the top bucket are counted, never dropped**

A p99 that silently discards its slowest samples reports a target as met when
it was missed. This is the single most dangerous failure mode of the whole
phase, so it gets its own checkpoint.

- [ ] **Step 1: Write the failing test, then run it**

Spec:
- Observe 1 sample of `5*time.Second` (far above the 1000 ms top bound) →
  `Count()` returns `1`, `Sum()` returns `5*time.Second`, and
  `Quantile(0.99)` returns `(OverTopBucket, true)`.
- Observe 99 samples of `1*time.Millisecond` plus 1 sample of `5*time.Second`
  → `Quantile(0.99)` returns `(1*time.Millisecond, true)` and `Quantile(1.0)`
  returns `(OverTopBucket, true)`.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/metrics/... -race -count=1 -run TestHistogramOverTopBucket
```
Expected: FAIL — with only the 13 bounds implemented, a 5 s sample matches no
bucket and is dropped, so `Count()` is `0`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: the bucket array has `len(Bounds)+1` entries; index `len(Bounds)` is
the overflow bucket, incremented when `d` exceeds the last bound. `Quantile`
returns `(OverTopBucket, true)` when the target count is only reached in the
overflow bucket. `Count()` and `Sum()` include overflow samples.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/metrics/... -race -count=1 && \
  cd .. && git add backend/internal/metrics/histogram.go backend/internal/metrics/histogram_test.go && \
  git commit -m "fix: count samples above the top bucket instead of dropping them"
```
Expected: PASS, then one commit.

**Checkpoint 3: concurrent Observe loses no samples**

- [ ] **Step 1: Write the failing test, then run it**

Spec: 50 goroutines each call `Observe(2*time.Millisecond)` 1,000 times
against one shared `*Histogram`; after all join, `Count()` returns exactly
`50000` and `Sum()` returns exactly `50000 * 2 * time.Millisecond`. The test
must run under `-race` with no race reported.

Write the *implementation* of `Histogram`'s fields as plain `uint64` first if
they are not already atomic — the point of this checkpoint is that the test
fails against a non-atomic implementation.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/metrics/... -race -count=1 -run TestHistogramConcurrentObserve
```
Expected: FAIL — the race detector reports a data race on the bucket, count,
or sum field, and/or `Count()` returns fewer than 50000.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: bucket counters, total count, and sum are all `atomic.Uint64`
(store the sum as nanoseconds). `Observe` performs one `Add` per field. No
mutex.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/metrics/... -race -count=1 && \
  cd .. && git add backend/internal/metrics/histogram.go backend/internal/metrics/histogram_test.go && \
  git commit -m "fix: make histogram observation atomic under concurrent writers"
```
Expected: PASS, then one commit.

**Checkpoint 4: the registry and its text rendering**

- [ ] **Step 1: Write the failing test, then run it**

Spec:
- `r := NewRegistry()`; `r.Histogram("wager_place_ok")` called twice returns
  the **same pointer** both times.
- `r.Counter("ws_send_dropped")` called twice returns the same pointer.
- Counters and histograms live in separate maps: requesting
  `r.Counter("wager_place_ok")` after `r.Histogram("wager_place_ok")` returns
  a distinct counter rather than colliding. (Stated explicitly so an
  implementer does not merge them into one map keyed by name.)
- `Render()` on an empty registry returns the empty string.
- After `r.Histogram("wager_place_ok").Observe(3*time.Millisecond)` and
  `r.Counter("ws_send_dropped").Inc()`, `Render()` returns exactly these
  lines, sorted by emitted metric name ascending, each terminated by `\n`:
  ```
  callit_wager_place_ok_count 1
  callit_wager_place_ok_p50_ms 5
  callit_wager_place_ok_p99_ms 5
  callit_wager_place_ok_sum_ms 3
  callit_ws_send_dropped 1
  ```
  Milliseconds render as integers. A quantile in the overflow bucket renders
  as `-1`. Deterministic ordering matters: a test asserting an exact string
  against Go's randomized map iteration is otherwise flaky.
- `Handler(r)` responds to `GET /` with status 200, `Content-Type:
  text/plain; charset=utf-8`, and a body equal to `r.Render()`.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/metrics/... -race -count=1 -run 'TestRegistry|TestHandler'
```
Expected: FAIL — `undefined: NewRegistry`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Registry` holds a `map[string]*Histogram` and a
`map[string]*Counter` guarded by a `sync.Mutex` (registration is rare; only
`Observe` is hot, and that path touches no map). `Render` collects every
metric's lines, sorts them by name, and joins with `\n`. `Handler` writes
`Render()`'s output.

Then run the full backend suite once — this is the task boundary:

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./... -race -cover -p 1 -count=1 && \
  cd .. && git add backend/internal/metrics/registry.go backend/internal/metrics/registry_test.go && \
  git commit -m "feat: a named metric registry with deterministic text rendering"
```
Expected: PASS across every package, then one commit.

---

## Task 3: Instrument the wager placement path

Spec §7's "p99 bet placement latency" is the cost of
`wager.Service.Place` — rate-limit check, idempotency parse, round lookup,
balance read, the atomic Lua write, and the odds broadcast. Timing it at the
service boundary measures what the SLA names.

**Success and failure are recorded separately, deliberately.** A rate-limited
rejection returns in microseconds without touching the Lua script; folding
those into one histogram would drag the p99 down and let a server that is
rejecting most traffic report an excellent placement latency. The SLA reads
off `wager_place_ok`; `wager_place_err` keeps failures visible.

**Files:**
- Modify: `backend/internal/wager/service.go`
- Test: `backend/internal/wager/service_test.go`

**Interfaces:**
- Consumes: `metrics.Histogram` structurally (via the interface below —
  `internal/wager` does **not** import `internal/metrics`).
- Produces:
  - `type Recorder interface{ Observe(d time.Duration) }` — declared in
    `internal/wager`, consumer-side.
  - `func NewService(store *redisstore.Store, b round.Broadcaster, ok, failed Recorder) *Service`
    — **note the signature change**; existing callers (`cmd/api/main.go`,
    `internal/wager/service_test.go`, and any `internal/httpapi` test
    fixture that constructs one) must pass recorders. Passing `nil` for
    either is legal and disables that recording.

**Checkpoint 1: a successful Place records exactly one observation**

- [ ] **Step 1: Write the failing test, then run it**

Spec: construct a `Service` with a stub recorder that counts calls and
retains the last observed duration. Place one valid wager that succeeds
(reuse the existing successful-placement fixture in `service_test.go`).
Assert: the success recorder saw exactly **1** observation, its observed
duration is `> 0`, and the failure recorder saw **0** observations.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/wager/... -race -count=1 -run TestPlaceRecordsSuccessLatency
```
Expected: FAIL to compile — `NewService` takes 2 arguments, not 4, and
`wager.Recorder` is undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add `okLatency, errLatency Recorder` fields to `Service`; extend
`NewService` to accept both. At the top of `Place`, capture
`start := time.Now()`. On the success return, call
`s.okLatency.Observe(time.Since(start))` when `s.okLatency != nil`. Update
every existing `NewService` call site to pass `nil, nil` except where a test
needs a real recorder.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/wager/... -race -count=1 && \
  cd .. && git add backend/internal/wager/service.go backend/internal/wager/service_test.go && \
  git commit -m "feat: record wager placement latency on the success path"
```
Expected: PASS, then one commit.

**Checkpoint 2: a rejected Place records on the failure histogram only**

- [ ] **Step 1: Write the failing test, then run it**

Spec: with the same stub recorders, call `Place` with an `IdempotencyKey` of
`"not-a-uuid"`. Assert the call returns `wager.ErrBadIdempotency`, the
**failure** recorder saw exactly 1 observation, and the **success** recorder
saw 0. Then, in a second sub-test, exhaust the rate limiter (place
`wager.Limit` valid wagers, then one more) and assert the final call returns
a `*wager.RateLimitError`, the failure recorder's count increased by exactly
1, and the success recorder's count did not change.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/wager/... -race -count=1 -run TestPlaceRecordsFailureLatency
```
Expected: FAIL — the failure recorder's count is 0, because Checkpoint 1
instrumented only the success return.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: every error return path in `Place` records on `errLatency` when it
is non-nil. Prefer a single `defer` over a call at each `return` — a deferred
helper that inspects the named error return records exactly once per call
regardless of which branch returns, which is what stops a future branch from
being silently unmeasured.

Task boundary — run the full suite:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./... -race -cover -p 1 -count=1 && \
  cd .. && git add backend/internal/wager/service.go backend/internal/wager/service_test.go backend/cmd/api/main.go && \
  git commit -m "feat: record wager placement latency on every failure path"
```
Expected: PASS across every package, then one commit.

---

## Task 4: Instrument the WebSocket broadcast → write path

Spec §7's "global WebSocket sync latency < 30 ms" is the time from a
broadcast being issued to that payload being written to a client's socket.
The parent plan's risk table names the failure mode this must expose: *"Slow
WebSocket client stalls room broadcast — breaks the <30 ms target."*
Measuring only the fan-out loop would miss it entirely; the interesting time
is spent waiting in a client's `send` buffer.

**This requires `send` to carry a timestamp**, so `chan []byte` becomes
`chan outbound`. The change is contained to `internal/ws` — four production
sites (`Client.Send`, `Client.WritePump`, `Room.run`'s `broadcastCmd` case,
and the channel's construction) plus the `close(c.send)` calls, which are
unaffected by the element type.

**Files:**
- Modify: `backend/internal/ws/client.go`, `backend/internal/ws/room.go`, `backend/internal/ws/hub.go`
- Test: `backend/internal/ws/client_test.go`, `backend/internal/ws/room_test.go`

**Interfaces:**
- Consumes: the same structural shape as `wager.Recorder`, declared
  independently here — `internal/ws` does not import `internal/wager`.
- Produces:
  - `type Recorder interface{ Observe(d time.Duration) }`
  - `type DropCounter interface{ Inc() }`
  - `type outbound struct { payload []byte; enqueued time.Time }` — unexported.
  - `func NewHub(sync Recorder, drops DropCounter) *Hub` — **signature
    change**; `nil` for either disables that metric.
  - `func NewRoom(id string, onEmpty func(string), sync Recorder, drops DropCounter) *Room`
    — **signature change**, threaded from the hub.

**Checkpoint 1: a delivered broadcast records enqueue→write latency**

- [ ] **Step 1: Write the failing test, then run it**

Spec: build a `Client` over the existing stub `Conn` from `client_test.go`,
with a stub recorder. Enqueue one payload via `Client.Send`, sleep
`5*time.Millisecond`, then run `WritePump` in a goroutine until the payload is
written. Assert: the stub `Conn` received exactly one `WriteMessage` with the
payload bytes, the recorder saw exactly 1 observation, and the observed
duration is `>= 5*time.Millisecond`. The `>=` bound is what proves the metric
measures *queue wait*, not just the write call — a naive implementation that
timestamps inside `WritePump` would report near-zero and fail this.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/ws/... -race -count=1 -run TestWritePumpRecordsSyncLatency
```
Expected: FAIL to compile — `ws.Recorder` is undefined and `Client` has no
recorder field.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `send` becomes `chan outbound`. `Client.Send(payload []byte)` wraps
it as `outbound{payload: payload, enqueued: time.Now()}`. `Room.Broadcast`
stamps `enqueued` **once, at the call**, into `broadcastCmd`, and `Room.run`
copies that same timestamp into every client's `outbound` — so the metric
includes time spent waiting on the room's unbuffered command channel, not
only time in the client buffer. `WritePump`, immediately after a successful
`conn.WriteMessage`, calls `c.sync.Observe(time.Since(o.enqueued))` when
`c.sync != nil`. Update every `NewHub`/`NewRoom` call site.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/ws/... -race -count=1 && \
  cd .. && git add backend/internal/ws/client.go backend/internal/ws/room.go backend/internal/ws/hub.go backend/internal/ws/client_test.go && \
  git commit -m "feat: measure websocket sync latency from enqueue to socket write"
```
Expected: PASS, then one commit.

**Checkpoint 2: an evicted slow client counts a drop and records no latency**

Without this, a stalled client makes the p99 look *better*: its slow payloads
are discarded and never observed. The drop counter is what keeps that
visible.

- [ ] **Step 1: Write the failing test, then run it**

Spec: construct a `Room` with a stub recorder and a stub drop counter, and
join one client whose `send` buffer is full and whose `WritePump` is **not**
running (reuse the eviction fixture already in `room_test.go`). Broadcast one
payload. Assert: the client is evicted (room member count drops to 0), the
drop counter saw exactly **1** `Inc()`, and the latency recorder saw **0**
observations.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/ws/... -race -count=1 -run TestBroadcastCountsDroppedClient
```
Expected: FAIL to compile — `ws.DropCounter` is undefined and `NewRoom` takes
2 arguments.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: in `Room.run`'s `broadcastCmd` case, the `default:` branch of the
non-blocking send — the branch that appends to `evicted` — also calls
`r.drops.Inc()` when `r.drops != nil`. One `Inc` per dropped payload per
client. No latency is observed for a dropped payload, by construction: the
observation lives in `WritePump`, which never sees it.

Task boundary — run the full suite:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./... -race -cover -p 1 -count=1 && \
  cd .. && git add backend/internal/ws backend/internal/httpapi backend/cmd/api/main.go && \
  git commit -m "feat: count dropped payloads when a slow client is evicted"
```
Expected: PASS across every package, then one commit.

---

## Task 5: Serve the metrics on a separate, default-disabled, loopback listener

An unauthenticated metrics endpoint on the public API port is a finding a
security review would raise, and Phase 7 is the hardening phase. Putting it on
its **own** listener means it is off by default, cannot be reached through the
public port at all, adds no route to `NewMux`, and — per Global Constraints —
adds no second origin allowlist.

**Files:**
- Modify: `backend/internal/config/config.go`, `backend/cmd/api/main.go`, `README.md`, `CLAUDE.md`
- Test: `backend/internal/config/config_test.go`

**Interfaces:**
- Consumes: `metrics.Handler`, `metrics.NewRegistry` (Task 2).
- Produces: `Config.MetricsAddr string` — empty means disabled.

**Checkpoint 1: `METRICS_ADDR` defaults to disabled and validates as host:port**

- [ ] **Step 1: Write the failing test, then run it**

Spec, using the existing `LookupFunc` stub pattern in `config_test.go` (every
case also supplies a valid `JWT_SECRET`, since `Load` requires it):
- `METRICS_ADDR` unset → `cfg.MetricsAddr == ""`, nil error.
- `METRICS_ADDR="127.0.0.1:9090"` → `cfg.MetricsAddr == "127.0.0.1:9090"`, nil error.
- `METRICS_ADDR="localhost:9090"` → accepted, nil error.
- `METRICS_ADDR="9090"` (no host, no colon) → error containing `METRICS_ADDR`
  and `host:port`.
- `METRICS_ADDR="127.0.0.1:notaport"` → error containing `METRICS_ADDR`.
- `METRICS_ADDR="127.0.0.1:99999"` → error containing `METRICS_ADDR`
  (out of the 1–65535 range, matching how `PORT` is already validated).
- `METRICS_ADDR=""` explicitly set → treated as unset: `cfg.MetricsAddr == ""`,
  nil error.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/config/... -race -count=1 -run TestLoadMetricsAddr
```
Expected: FAIL — `cfg.MetricsAddr` undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add `MetricsAddr string` to `Config`, defaulting to `""`. When
`METRICS_ADDR` is present and non-empty, split it with `net.SplitHostPort`,
returning a wrapped error naming `METRICS_ADDR` and the expected `host:port`
shape on failure; then validate the port as an integer in 1–65535, reusing
the same range check `PORT` already applies.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/config/... -race -count=1 && \
  cd .. && git add backend/internal/config/config.go backend/internal/config/config_test.go && \
  git commit -m "feat: accept an optional METRICS_ADDR, disabled by default"
```
Expected: PASS, then one commit.

**Checkpoint 2: in production, `METRICS_ADDR` must be loopback**

`CORS_ALLOWED_ORIGINS` already tightens under `ENV=production` rather than
relaxing; the metrics listener follows the same rule so the strictness of the
two surfaces cannot diverge.

- [ ] **Step 1: Write the failing test, then run it**

Spec, all with a valid `JWT_SECRET` and a valid `CORS_ALLOWED_ORIGINS`:
- `ENV=production`, `METRICS_ADDR="127.0.0.1:9090"` → accepted, nil error.
- `ENV=production`, `METRICS_ADDR="localhost:9090"` → accepted, nil error.
- `ENV=production`, `METRICS_ADDR="[::1]:9090"` → accepted, nil error.
- `ENV=production`, `METRICS_ADDR="0.0.0.0:9090"` → error containing
  `METRICS_ADDR` and `loopback`.
- `ENV=production`, `METRICS_ADDR="10.0.0.5:9090"` → error containing
  `METRICS_ADDR` and `loopback`.
- `ENV=production`, `METRICS_ADDR=":9090"` (empty host = all interfaces) →
  error containing `METRICS_ADDR` and `loopback`.
- `ENV=development`, `METRICS_ADDR="0.0.0.0:9090"` → **accepted**, nil error.
  Binding broadly in dev is how a WSL2 host reaches it, matching the
  `api-lan` Makefile target's existing assumption.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/config/... -race -count=1 -run TestLoadMetricsAddrProductionLoopback
```
Expected: FAIL — `0.0.0.0:9090` is accepted under `ENV=production`, because
Checkpoint 1 validated shape only.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: when `cfg.Env == "production"` and `MetricsAddr` is non-empty,
require the host to be a loopback address. Accept `localhost` literally;
otherwise parse with `net.ParseIP` and require `IsLoopback()`. An empty host,
an unparseable host, or a non-loopback IP is an error naming `METRICS_ADDR`
and the word `loopback`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/config/... -race -count=1 && \
  cd .. && git add backend/internal/config/config.go backend/internal/config/config_test.go && \
  git commit -m "feat: require a loopback METRICS_ADDR in production"
```
Expected: PASS, then one commit.

**Checkpoint 3: wire the registry into `cmd/api` and document it**

`cmd/api` is not unit-tested by convention (0% coverage on `cmd/*` is
expected), so this checkpoint's RED lives where the wiring is observable:
the metric names the process must register.

- [ ] **Step 1: Write the failing test, then run it**

Spec: add `TestMetricNamesAreStable` to
`backend/internal/metrics/registry_test.go`. It builds a registry, requests
each of the four names this phase defines, and asserts `Render()` contains
lines beginning `callit_wager_place_ok_`, `callit_wager_place_err_`,
`callit_ws_sync_`, and `callit_ws_send_dropped`. Declare the four names as
exported constants in `internal/metrics` so `cmd/api` and the test reference
one definition rather than two string literals that can drift:
`NameWagerPlaceOK`, `NameWagerPlaceErr`, `NameWSSync`, `NameWSSendDropped`.

Run:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./internal/metrics/... -race -count=1 -run TestMetricNamesAreStable
```
Expected: FAIL — `undefined: metrics.NameWagerPlaceOK`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract, in four parts.

*In `internal/metrics`:* declare the four name constants
(`"wager_place_ok"`, `"wager_place_err"`, `"ws_sync"`, `"ws_send_dropped"`).

*In `cmd/api/main.go`:* build `reg := metrics.NewRegistry()` before the
services. Pass `reg.Histogram(metrics.NameWagerPlaceOK)` and
`reg.Histogram(metrics.NameWagerPlaceErr)` to `wager.NewService`, and
`reg.Histogram(metrics.NameWSSync)` / `reg.Counter(metrics.NameWSSendDropped)`
to `ws.NewHub`. When `cfg.MetricsAddr != ""`, start a second
`&http.Server{Addr: cfg.MetricsAddr, Handler: metrics.Handler(reg)}` in its
own goroutine, log `"metrics listener starting"` with the address, and add it
to the graceful-shutdown path alongside the main server — shut it down with
the same `shutdownCtx`, and surface its error the same way. It must **not** be
wrapped in `httpapi.CORS` and must **not** be registered on `NewMux`.

*In `README.md`:* add a `### METRICS_ADDR` section under Backend, mirroring
the existing `CORS_ALLOWED_ORIGINS` section: disabled when unset; `host:port`
when set; must be loopback under `ENV=production`; serves plain text at any
path; carries no per-user, per-room, or per-round data.

*In `CLAUDE.md`:* add `METRICS_ADDR` to the Build & Test environment list
next to `JWT_SECRET`/`JWT_TTL`, and add one Critical Invariant: **"Metrics
are process-aggregate only and never labelled by user, room, or round"** —
with the one-line reason that a per-user label would let a scraper
reconstruct wager activity the anonymity invariant exists to withhold, and
that the metrics listener is a separate server so it adds no second origin
allowlist.

Task boundary — full suite plus a clean build:

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && go test ./... -race -cover -p 1 -count=1 && go build ./... && \
  cd .. && git add backend/internal/metrics backend/cmd/api/main.go README.md CLAUDE.md && \
  git commit -m "feat: serve metrics on a separate loopback listener"
```
Expected: PASS across every package, a clean build, then one commit.

Then verify the listener by hand (not committed, just confirmed) with Redis
already up via `make up`:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  cd backend && JWT_SECRET=$(openssl rand -hex 32) METRICS_ADDR=127.0.0.1:9090 \
    go run ./cmd/api & \
  sleep 3 && curl -s http://127.0.0.1:9090/ && \
  curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8080/metrics ; \
  kill %1
```
Expected: the metrics port returns the rendered text (empty, or a few
zero-count lines, is correct on a process that has served no traffic); the
**API** port returns `404` for `/metrics`, proving the endpoint is not on the
public mux.

---

## Task 6: The k6 load harness

**Verification gates, not RED→GREEN checkpoints** — see the note in Global
Constraints. Each gate ends in one commit chained behind a command whose exit
code gates it.

k6 is an external binary, not a Go dependency, so it adds nothing to `go.mod`.
No sudo is available, so it installs user-locally.

**Files:**
- Create: `loadtest/README.md`, `loadtest/lib/setup.js`, `loadtest/rest_throughput.js`, `loadtest/wager_latency.js`
- Modify: `Makefile` (the `loadtest` target, currently an echo stub at line 55)

**Gate 1: k6 installed and the Makefile target is real**

- [ ] Install k6 v2.2.0 user-locally:
```bash
mkdir -p "$HOME/.local/bin" && cd "$HOME" && \
  curl -fsSLO https://github.com/grafana/k6/releases/download/v2.2.0/k6-v2.2.0-linux-amd64.tar.gz && \
  tar -xzf k6-v2.2.0-linux-amd64.tar.gz && \
  mv k6-v2.2.0-linux-amd64/k6 "$HOME/.local/bin/k6" && \
  rm -rf k6-v2.2.0-linux-amd64 k6-v2.2.0-linux-amd64.tar.gz && \
  "$HOME/.local/bin/k6" version
```
Expected: `k6 v2.2.0 ...`. If the download 404s, list the release assets via
`https://api.github.com/repos/grafana/k6/releases/latest` and use the actual
`linux-amd64` asset name rather than guessing.

- [ ] Replace the `loadtest` target. It must fail loudly when k6 is absent
rather than silently doing nothing — the current stub's `@echo` exits 0, which
is precisely how a load test gets skipped without anyone noticing:
```make
SCENARIO ?= rest_throughput

loadtest:
	@command -v k6 >/dev/null 2>&1 || { \
	  echo "k6 not found. Install it user-locally — see loadtest/README.md"; exit 1; }
	k6 run loadtest/$(SCENARIO).js
```
`loadtest` is already on the `.PHONY` line; leave it there.

- [ ] Write `loadtest/README.md`: the install command above, the `PATH` note
(`$HOME/.local/bin` must be on `PATH`), what each scenario measures, which
services must be running (`make up` plus a running `cmd/api` with
`METRICS_ADDR` set), and the explicit warning that k6's client-side figures
are **secondary** to the server-side histograms — the parent plan's risk
table calls out p99 measurement fidelity under WSL2 as a known limitation.

- [ ] Commit:
```bash
command -v k6 && git add Makefile loadtest/README.md && \
  git commit -m "chore: install k6 and make the loadtest target real"
```
Expected: k6's path prints, then one commit.

**Gate 2: the REST throughput scenario**

- [ ] Write `loadtest/lib/setup.js` exporting:
  - `const BASE = __ENV.API_BASE_URL || 'http://localhost:8080'`
  - `registerUser()` → `{token, userId}` — `POST` the register route with a
    unique email per virtual user
    (`vu-${__VU}-${__ITER}-${Date.now()}@loadtest.local`) and a valid
    password; unwrap the response envelope; return the JWT.
  - `createRoom(token)` → `{roomId, code, roomToken}`
  - `joinRoom(code, token)` → `{roomToken}`

  Read the exact route paths, request field names, and envelope shape from
  `backend/internal/httpapi/auth_handlers.go`, `room_handlers.go`, and
  `respond.go` — **do not guess them**. If anything in those files disagrees
  with this plan, the Go source wins and this plan is stale.

- [ ] Write `loadtest/rest_throughput.js`: a ramping-arrival-rate scenario
climbing to **5,000 iterations/sec** (spec §7's target), each iteration doing
one authenticated request against a cheap read route, with `thresholds`:
```js
thresholds: {
  http_req_failed: ['rate==0'],
  http_req_duration: ['p(99)<15'],
}
```
Pinning `http_req_failed` at exactly 0 is deliberate: spec §7's double-spend
tolerance is `0.00%`, and a throughput run that quietly sheds requests is not
a throughput run.

- [ ] Run it against a live stack and record the result. **A non-zero exit is
an expected, informative outcome here**, not a failure of the task — this gate
is about the harness producing a trustworthy number, not about the number
being good. Task 7 records whatever it is; 7b does the tuning:
```bash
make up && \
  (export PATH=$PATH:$HOME/.local/go/bin && cd backend && \
   JWT_SECRET=$(openssl rand -hex 32) METRICS_ADDR=127.0.0.1:9090 go run ./cmd/api &) && \
  sleep 5 && \
  PATH=$PATH:$HOME/.local/bin k6 run loadtest/rest_throughput.js ; \
  echo "k6 exit: $?"
```
Expected: k6 prints a full summary including the `http_reqs` rate and
`http_req_duration p(99)`. Capture that summary verbatim — Task 7 needs it.

- [ ] Commit:
```bash
git add loadtest/lib/setup.js loadtest/rest_throughput.js && \
  git commit -m "test: add a k6 REST throughput scenario with SLA thresholds"
```

**Gate 3: the WebSocket wager-latency scenario**

- [ ] Write `loadtest/wager_latency.js` using k6's built-in
`k6/experimental/websockets` module (no external dependency). Each virtual
user: registers, joins a room by a code created in `setup()`, opens
`GET /api/v1/socket` with its room token, waits for the host to open a round,
then sends `place_wager` messages with a **fresh UUIDv4 `idempotency_key` per
message** — the backend dedupes on that key, so a scenario reusing one
measures the dedupe path instead of the placement path.

Record a custom `Trend` metric `wager_ack_ms`: elapsed time from sending
`place_wager` to receiving the matching `wager_accepted` reply. Threshold:
```js
thresholds: { wager_ack_ms: ['p(99)<15'] }
```

Two constraints the script must respect, both from `CLAUDE.md`'s invariants:
- **The host cannot place wagers in their own room** — the VU that creates
  the room opens and resolves rounds and never wagers. Wagering VUs join as
  separate players.
- **The wager rate limiter allows 20 placements per 10 s per user**
  (`wager.Limit`, `wager.Window`). A scenario exceeding it measures
  `rate_limited` rejections, not placement latency. Either pace each VU below
  that rate or scale VU count rather than per-VU rate — state in the script's
  header comment which was chosen and why.

- [ ] Run it, capture the summary, and additionally capture the **server-side**
figures, which are the authoritative ones:
```bash
PATH=$PATH:$HOME/.local/bin k6 run loadtest/wager_latency.js ; echo "k6 exit: $?" ; \
  curl -s http://127.0.0.1:9090/
```
Expected: k6's `wager_ack_ms` trend **and** a metrics dump showing a non-zero
`callit_wager_place_ok_count`, a `callit_wager_place_ok_p99_ms` value, a
`callit_ws_sync_p99_ms` value, and `callit_ws_send_dropped`. **If
`callit_wager_place_ok_count` is 0 while k6 reports successful wagers, the
instrumentation is not wired — stop and fix Task 3/5 before continuing.**

- [ ] Commit:
```bash
git add loadtest/wager_latency.js && \
  git commit -m "test: add a k6 websocket wager-latency scenario"
```

---

## Task 7: Record the baseline

**Verification gates, not checkpoints.** The deliverable is a measurement
report; there is no test that can fail before it exists.

**Files:**
- Create: `docs/reports/2026-08-31-phase-7a-baseline.md`
- Modify: `docs/plans/2026-08-21-implementation-plan.md` (§12), `CLAUDE.md` (Build & Test)
- Create: `journal/<timestamp>_ansh_phase-7a-execution.md`

**Gate 1: the baseline report**

- [ ] Run both scenarios once more against a freshly restarted stack, so every
number comes from one coherent session rather than Task 6's incremental runs.
Restart the API process between the two scenarios so each starts with empty
histograms. Capture, for each:
  - k6's own summary block, verbatim.
  - The `curl http://127.0.0.1:9090/` metrics dump, verbatim, taken
    immediately after the run and before the process is stopped.

- [ ] Write `docs/reports/2026-08-31-phase-7a-baseline.md` containing:
  - **A results table** with one row per spec §7 target: the target, the
    server-side measured value, the k6 client-side value, and MET / MISSED.
    All four rows: p99 bet placement < 15 ms, WS sync < 30 ms, throughput
    5,000+ rps, double-spend 0.00%.
  - **How each number was produced** — exact command, scenario file, VU count,
    duration, and environment (WSL2, `uname -r`, `nproc`, Go version).
  - **The fidelity caveat**, quoting the parent plan's §10 risk row: p99
    measurement fidelity under WSL2 is a known Medium risk; the mitigation is
    to treat server-side histograms as primary and k6 client figures as
    secondary, and this report follows that rule.
  - **Bucket-resolution honesty:** the histogram reports an upper bound, so a
    p99 rendered as `15` means "at or below 15 ms", not "exactly 15 ms". Say
    so explicitly — an SLA table implying more precision than the instrument
    has is worse than one that admits the bound.
  - **A "what 7b must act on" list**: every MISSED row, plus
    `callit_ws_send_dropped` if it is non-zero, each with the specific
    observation motivating it. This list is 7b's input.
  - **No per-user, per-room, or per-round data anywhere** — the anonymity
    constraint binding the metrics binds a document derived from them.

- [ ] Commit:
```bash
git add docs/reports/2026-08-31-phase-7a-baseline.md && \
  git commit -m "docs: record the Phase 7a performance baseline"
```

**Gate 2: close the phase's paper trail**

- [ ] In `docs/plans/2026-08-21-implementation-plan.md` §12, the box
`Redis↔PostgreSQL reconciliation test passes after a load run` belongs to
**7b**, not 7a. Leave it unticked and add a one-line pointer to the baseline
report beside it. Do not tick an acceptance box this phase did not earn.

- [ ] In `CLAUDE.md`'s Build & Test section, replace the line stating
`make loadtest` is a stub with its real usage: `make loadtest` runs
`SCENARIO ?= rest_throughput`; `make loadtest SCENARIO=wager_latency` runs the
socket scenario; both need k6 on `PATH` and a live stack; k6 is an external
binary and deliberately not a Go dependency.

- [ ] Write the journal entry with the project-local `journal` skill (not
`journal-global`). Record: the measured-vs-target table in brief; the
toolchain raise and the decision to hold the five dependency pins; the
decision to record success and failure latency separately; the decision to
put metrics on a separate loopback listener; and the
`send chan []byte` → `chan outbound` change with its reason (queue wait is
what the 30 ms target actually loses to).

- [ ] Final verification — everything green on the branch:
```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  make test && make lint && \
  git add docs/plans/2026-08-21-implementation-plan.md CLAUDE.md journal/ && \
  git commit -m "docs: journal the Phase 7a instrumentation and baseline"
```
Expected: full suite PASS, `go vet` clean, `gofmt -l` prints nothing, then one
commit.

- [ ] **Run the `security-reviewer` agent** before the phase closes.
`CLAUDE.md` requires it for any phase touching auth, money movement, or a
network surface — this phase adds a new **network listener** and instruments
the **money path**, so it qualifies twice. Record its findings in
`docs/project-history.md` under a `### Phase 7a` heading, in the same format
as the Phase 5b and 6b entries, including anything accepted by design.

**The branch stops here, green and verified.** Merging is
`finishing-a-development-branch`'s decision, not this plan's.

---

## Self-Review

**1. Spec coverage.** Spec §7 has four targets. p99 bet placement — Task 3
instruments it, Task 6 Gate 3 drives it, Task 7 records it. WS sync — Task 4,
Gate 3, Task 7. Throughput — Gate 2, Task 7. Double-spend 0.00% — already
proven by `internal/redisstore`'s concurrency suite and
`internal/ledger/reconcile_test.go`; 7a restates it in the baseline table but
adds no new proof, and the §9 row assigns the post-load reconciliation re-run
to **7b**, which Task 7 Gate 2 explicitly declines to tick early. Parent plan
§9's 7a row lists four items: toolchain raise (Task 1), histograms (Tasks
2–5), k6 scripts (Task 6), recorded baseline (Task 7). Covered.

**2. Placeholder scan.** No "TBD", no "add error handling", no "similar to
Task N". Two places defer to source over this document, deliberately, each
naming the file to consult and the resolution rule: Task 6 Gate 2 (the Go
handlers are authoritative for route and field names) and Task 6 Gate 1 (use
the real k6 release asset name if the pinned URL 404s). Both are resolution
procedures, not placeholders.

**3. Type consistency.** `Recorder` is declared independently in
`internal/wager` and `internal/ws` with the identical method set
`Observe(time.Duration)`; `*metrics.Histogram` satisfies both structurally,
and neither package imports `internal/metrics`. `DropCounter` (`Inc()`) is
satisfied by `*metrics.Counter`. `NewService`, `NewHub` and `NewRoom` all
change signature, and each task's Step 2 names the call sites to update.
`OverTopBucket` is defined in Task 2 CP2 and referenced in CP4's render rule
(`-1`) and Task 7's honesty note. The four metric-name constants are defined
once in Task 5 CP3 and used by both `cmd/api` and the test.

**4. Delegation eligibility.** **Not used — this plan is fully inline**, and
the header carries no `**Delegation:**` line. Tasks 2 and 5 would qualify as
mechanical work against a known contract, but Tasks 3, 4, 6 and 7 are the
phase's flagship work: they are the *evidence* for the performance claims the
project makes, and the histogram's correctness gates every number in them.
Spreading cold subagents across a measurement chain, where a silent
instrumentation gap yields a plausible-but-wrong p99, is the wrong place to
absorb a process experiment.

**One gap found and closed during review.** The first draft measured WS sync
latency inside `Room.run`'s fan-out loop, which cannot see time a payload
spends waiting in a client's `send` buffer — the exact failure the parent
plan's risk table names as what breaks the 30 ms target. Task 4 CP1's
assertion (`observed >= 5ms` after a deliberate 5 ms delay between `Send` and
`WritePump`) exists specifically to make that implementation fail.
