# Off-WSL2 Load-Test Baseline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce one trustworthy load-test run of both k6 scenarios
(`rest_throughput.js`, `wager_latency.js`) on a Linux host that is not
WSL2, and document whether the throughput/p99 ceilings measured in
`docs/reports/2026-08-31-phase-7a-baseline.md` and
`docs/reports/2026-08-31-phase-7b-baseline.md` were a WSL2 artifact or a
real limit — the open question Phase 7b's Task 6 named but did not
resolve ("measuring the 5,000 req/s target fairly needs either a load
generator run natively... or a k6 configuration proven not to bind on its
own scheduler at this rate").

**Architecture:** This is a measurement exercise, not a code change —
provision a plain Linux host (cloud VM or spare bare-metal box), install
the exact pinned toolchain this repo already uses, re-run the two
existing k6 scenarios verbatim with no application-code changes, and
compare the server-side histogram numbers against the WSL2 baselines
already on record. If the single-host run reproduces the same ~3,174
req/s ceiling, a conditional second gate splits the load generator onto a
separate host from the server under test to isolate the other half of
7b's hypothesis.

**Tech Stack:** Go 1.26.7, Redis 7.2 (via `make up`), k6 v2.2.0 (pinned,
per `loadtest/README.md`), a non-WSL2 Ubuntu 22.04/24.04 (or equivalent)
Linux host.

**Spec:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md)
§7 (Performance & Scale Targets). Prior art this plan re-runs against:
[`docs/reports/2026-08-31-phase-7a-baseline.md`](../reports/2026-08-31-phase-7a-baseline.md),
[`docs/reports/2026-08-31-phase-7b-baseline.md`](../reports/2026-08-31-phase-7b-baseline.md),
and `loadtest/README.md`'s "Phase 7b throughput re-baseline and gap
attribution" section.

## Global Constraints

- Server-side `internal/metrics` histograms are authoritative; k6's own
  client-side `http_req_duration`/`wager_ack_ms` numbers are secondary,
  directionally-useful cross-checks only (established convention, both
  prior baseline reports).
- k6 is pinned to **v2.2.0** — install it exactly as `loadtest/README.md`
  documents, don't take whatever `latest` resolves to.
- Go toolchain floor is **1.26** (`CLAUDE.md`) — install `go1.26.7`
  specifically, matching the pin already in use.
- **No application code changes.** This plan touches only `docs/` and
  `loadtest/README.md`. If a run's result seems to call for a code fix,
  that is out of scope here — name it as a finding for a later phase
  instead of fixing it in this plan.
- **No per-user, per-room, or per-round data may appear in the resulting
  report.** Every number recorded must be a process-aggregate count or
  latency quantile from `internal/metrics`, or a k6 scenario-wide summary
  statistic — the same anonymity invariant the existing baseline reports
  already hold themselves to.
- Double-spend tolerance (spec §7's fourth target) is **not** re-measured
  by this plan — it's an environment-independent correctness proof
  (`internal/redisstore/concurrency_test.go`,
  `internal/ledger/reconcile_test.go`), already re-verified after real
  load in Phase 7b Task 7. State this explicitly in the report rather
  than silently omitting it.
- Git workflow per `CLAUDE.md`: branch off `dev`
  (`git checkout -b offwsl2-loadtest-baseline dev`), commit at each task
  boundary, commit format `type: description`. This plan ends at "branch
  is green and verified" — merge/PR is a separate decision handed to
  `finishing-a-development-branch`, not part of this plan.
- **Provisioning a cloud VM spends real money and creates an external
  account resource this session cannot see or manage on its own.** Every
  gate that creates a billed resource is marked below and requires the
  user's explicit go-ahead before it runs — never provision speculatively.
  Every such resource must be destroyed/stopped before this plan's final
  task closes.

---

## Task 1: Provision an off-WSL2 host and reproduce the existing baseline procedure on it

**Gate-only task — no repository files change here, so no commits in
this task.** The numbers this task records feed Task 2's write-up.

**Files:** None.

- [ ] **Gate 1: provision the host — requires the user's explicit go-ahead before creating any billed resource**

STOP and confirm with the user before running anything in this gate: which
provider/size to use, and that they accept the cost. Suggested spec,
chosen to isolate "not WSL2" as the only changed variable versus the 7a/7b
runs without introducing a new confound (CPU-steal noise from a shared
burstable instance):

- OS: Ubuntu 22.04 LTS or 24.04 LTS (any non-WSL2 Linux is acceptable;
  Ubuntu matches this repo's existing CI runner and install instructions).
- CPU: **≥4 dedicated vCPUs**, not a burstable/shared-core tier — e.g.
  Hetzner Cloud CPX21 (4 dedicated vCPU), a DigitalOcean CPU-Optimized
  droplet, AWS `c6i.xlarge`, or GCP `c2-standard-4`. All of these bill
  hourly, so the host can be destroyed immediately after Task 1 finishes.
- Reachable via SSH with a key the executor has access to.

Expected: a running host, its SSH connection string, and confirmed hourly
billing (not a long-term commitment).

- [ ] **Gate 2: install the pinned toolchain and verify it is genuinely not WSL2**

```bash
ssh <host> 'uname -r; nproc; lscpu | grep "Model name"'
```

Expected: `uname -r`'s output does **not** contain the substring
`microsoft` (the WSL2 kernel signature both prior baseline reports
recorded — e.g. `6.6.87.2-microsoft-standard-WSL2`); `nproc` reports `4`
or more.

Install dependencies:

```bash
ssh <host> 'sudo apt-get update && \
  sudo apt-get install -y docker.io docker-compose-plugin git openssl && \
  sudo usermod -aG docker $USER'
# Re-connect (or `newgrp docker`) so the docker group membership takes effect.
ssh <host> 'curl -fsSL https://go.dev/dl/go1.26.7.linux-amd64.tar.gz -o /tmp/go.tgz && \
  sudo tar -C /usr/local -xzf /tmp/go.tgz && \
  echo "export PATH=\$PATH:/usr/local/go/bin" >> ~/.bashrc'
ssh <host> 'mkdir -p ~/.local/bin && cd ~ && \
  curl -fsSLO https://github.com/grafana/k6/releases/download/v2.2.0/k6-v2.2.0-linux-amd64.tar.gz && \
  tar -xzf k6-v2.2.0-linux-amd64.tar.gz && \
  mv k6-v2.2.0-linux-amd64/k6 ~/.local/bin/k6 && \
  rm -rf k6-v2.2.0-linux-amd64 k6-v2.2.0-linux-amd64.tar.gz && \
  echo "export PATH=\$PATH:\$HOME/.local/bin" >> ~/.bashrc'
ssh <host> 'git clone https://github.com/zorojuro12/call_it call_it && cd call_it && git checkout dev'
```

(If the repository is private by the time this runs, the host needs its
own git credentials — an SSH deploy key or a PAT — before the `git clone`
step; substitute the appropriate authenticated URL.)

```bash
ssh <host> 'export PATH=$PATH:/usr/local/go/bin:$HOME/.local/bin && \
  go version && k6 version && docker --version'
```

Expected: `go version go1.26.7 linux/amd64`; `k6 version` reports
`v2.2.0`; `docker --version` succeeds without error.

- [ ] **Gate 3: run the REST throughput scenario on the optimized binary — verbatim procedure from `docs/reports/2026-08-31-phase-7b-baseline.md`'s Task 6**

```bash
ssh <host> 'cd call_it && make up'
ssh <host> 'export PATH=$PATH:/usr/local/go/bin && cd call_it && \
  JWT_SECRET=$(openssl rand -hex 32) nohup make loadtest-api > /tmp/api.log 2>&1 &'
sleep 3
ssh <host> 'export PATH=$PATH:$HOME/.local/bin && cd call_it && k6 run loadtest/rest_throughput.js'
ssh <host> 'curl -s http://127.0.0.1:9090/'
```

Record: k6's `http_reqs` rate, `http_req_failed`, `dropped_iterations`,
`http_req_duration` p95/p99; the full server-side metrics dump from the
`curl`. Compare directly against 7a's 3,174 req/s (0 failed, 6 dropped)
and 7b's two runs (3,174.30/s and 3,174.24/s, both 0.00% failed).

- [ ] **Gate 4: run the wager-latency scenario on a fresh process — verbatim procedure from `docs/reports/2026-08-31-phase-7b-baseline.md`'s Task 5**

```bash
ssh <host> 'pkill -f bin/api || true'
sleep 1
ssh <host> 'export PATH=$PATH:/usr/local/go/bin && cd call_it && \
  JWT_SECRET=$(openssl rand -hex 32) nohup make loadtest-api > /tmp/api2.log 2>&1 &'
sleep 3
ssh <host> 'export PATH=$PATH:$HOME/.local/bin && cd call_it && \
  WAGER_TEST_DURATION_S=240 WAGER_PLAYERS=25 k6 run loadtest/wager_latency.js'
ssh <host> 'curl -s http://127.0.0.1:9090/'
```

Record: `callit_wager_place_ok_count`, `callit_wager_place_ok_p50_ms`,
`callit_wager_place_ok_p99_ms`, `callit_wager_place_err_count`,
`callit_ws_sync_p50_ms`, `callit_ws_sync_p99_ms`, `callit_ws_send_dropped`.
Compare against 7b Task 5's post-tuning figures (count 5980, p50 5ms, p99
15ms, ws_sync p50 0ms/p99 1ms, err_count 8, `ws_send_dropped` 0).

- [ ] **Gate 5 (conditional — only if Gate 3's `http_reqs` rate lands within roughly 5% of the ~3,174 req/s figure every prior WSL2 run produced): split the load generator onto a second host**

7b's Task 6 named two untested hypotheses for the reproduced ceiling:
running k6 natively off WSL2 (Gates 2–3 above test this), or running it on
a host separate from the server under test (this gate tests that one). Run
this gate only if Gate 3 did **not** move the number — if it already
resolved the ceiling, skip straight to Gate 6.

Provision a second small host — same go-ahead requirement as Gate 1, but
smaller (2 vCPU is enough; 7b's Task 6 observed k6 itself using only
~1.2 cores), same region/network as the Gate 1 host to keep inter-host
network latency low and comparable. Install only k6 v2.2.0 and git on it
(skip Go and Docker — this host never runs `cmd/api`), using the same
commands as Gate 2.

```bash
ssh <hostB> 'git clone https://github.com/zorojuro12/call_it call_it'
ssh <hostB> 'export PATH=$PATH:$HOME/.local/bin && cd call_it && \
  API_BASE_URL=http://<hostA-private-ip>:8080 k6 run loadtest/rest_throughput.js'
```

Record the same fields as Gate 3. Compare against Gate 3's colocated
figure: a materially different rate here isolates same-host CPU
contention between k6 and `cmd/api` as the actual cause; an unchanged rate
rules that out too and narrows the ceiling to k6's own executor behavior
independent of both WSL2 and colocation.

- [ ] **Gate 6: tear down every billed resource created in this task**

Destroy/stop the Gate 1 host (and the Gate 5 host, if created) now that all
numbers above are recorded locally. This is a required step, not optional
cleanup — leaving either host running continues to bill after this plan
has what it needs from it.

Expected: no cloud resource from this task remains running. Proceed to
Task 2 with the recorded numbers from Gates 3–5 in hand.

---

## Task 2: Write the baseline report and close out

**Files:**
- Create: `docs/reports/<run-date>-offwsl2-loadtest-baseline.md` (use the
  actual date Task 1's runs happened, matching the existing reports'
  naming convention — do not backdate to this plan's date if execution
  happens on a later day)
- Modify: `loadtest/README.md`
- Modify: `docs/project-history.md`
- Modify: `docs/plans/2026-08-21-implementation-plan.md:718` (the risk
  table's "p99 measurement fidelity under WSL2" row)

**Interfaces:** None — this task produces documentation only, no code.

- [ ] **Gate 1: write the baseline report**

Follow the exact structure of
`docs/reports/2026-08-31-phase-7a-baseline.md` and the "Phase 7b
throughput re-baseline" section of `loadtest/README.md`:

- A **Results** table: one row per spec §7 target actually re-measured
  (bet placement p99, WS sync p99, throughput — not double-spend, per
  Global Constraints), with columns for the 7a/7b WSL2 figure, this run's
  off-WSL2 figure, and a verdict.
- An **Environment** section disclosing, for every host used: cloud
  provider and instance type (or "bare metal" plus hardware description),
  region, `uname -r` output, `nproc`, CPU model, Go version, k6 version,
  and whether `cmd/api` ran as `make loadtest-api`'s optimized binary
  (it must have, per Global Constraints).
- The exact commands run (copy from Task 1's gates).
- A **Fidelity finding** section stating plainly which of these applies,
  using the evidence gathered:
  - **RESOLVED, WSL2 was the constraint** — the off-WSL2 figures meet or
    come materially closer to the spec §7 targets. State the new numbers
    and by how much they moved.
  - **NOT RESOLVED, the ceiling persists off WSL2** — state the persisting
    ceiling, and, if Gate 5 ran, whether it isolated same-host CPU
    contention as the actual cause instead.
  - **PARTIALLY RESOLVED** — state each of the (at most two) re-measured
    targets' verdicts independently; don't blend them into one summary
    verdict if they disagree.
- Apply the same bucket-resolution honesty as the 7a report wherever a
  server-side histogram value is quoted (a rendered value is an upper
  bound on the true latency, not an exact reading).

```bash
git add docs/reports/<run-date>-offwsl2-loadtest-baseline.md && \
  git commit -m "docs: record an off-WSL2 load-test baseline"
```

Expected: PASS (file exists, follows the structure above), then one commit.

- [ ] **Gate 2: record the finding in `loadtest/README.md`**

Append a new section after the existing "Phase 7b throughput re-baseline
and gap attribution" section, titled `## Off-WSL2 baseline`, summarizing:
which of 7b's two named hypotheses (native-off-WSL2 vs. a separate load-
generator host) the evidence supports, the headline before/after numbers,
and a link to the new report from Gate 1.

```bash
git add loadtest/README.md && \
  git commit -m "docs: record the off-WSL2 finding in the load-test README"
```

Expected: PASS, then one commit.

- [ ] **Gate 3: close the loop in `docs/project-history.md` and the parent plan's risk table**

Add one bullet to `docs/project-history.md`'s "## Environment verification
log" section (matching that section's existing style — e.g. the "Docker/WSL2"
and "A Hyper-V dynamic port exclusion" bullets already there), stating what
was verified, the date, and linking to the new report.

Then append a short status clause to the "Mitigation" cell of
`docs/plans/2026-08-21-implementation-plan.md:718`'s
"p99 measurement fidelity under WSL2" row — e.g. `; verified off-WSL2
<date>, see docs/reports/<run-date>-offwsl2-loadtest-baseline.md` — without
rewriting the rest of that row.

```bash
git add docs/project-history.md docs/plans/2026-08-21-implementation-plan.md && \
  git commit -m "docs: close the WSL2 fidelity risk against a real off-WSL2 run"
```

Expected: PASS, then one commit.

- [ ] **Gate 4: verify the branch is clean**

```bash
git status --porcelain
```

Expected: empty output. No application code changed anywhere in this
plan, so `make test`/`make build` are unaffected by it — this plan does
not re-run them.

---

## Self-Review Notes

- **Spec coverage:** spec §7's three re-measurable targets (bet placement
  p99, WS sync p99, throughput) are each re-run in Task 1 Gates 3–4 and
  each get an independent verdict in Task 2 Gate 1. The fourth target
  (double-spend tolerance) is explicitly out of scope per Global
  Constraints, not silently dropped.
- **No placeholders:** every gate names its exact command, exact
  comparison figures (drawn from the two existing baseline reports), and
  exact expected output.
- **Delegation:** not applicable — Task 1 requires a human decision and
  billing go-ahead at Gates 1 and 5, which rules out a cold subagent for
  this plan regardless of mechanical/flagship status. Fully inline.
- **Where this plan stops:** ends at Task 2 Gate 4 (clean branch, verified
  commits). Merge/PR is `finishing-a-development-branch`'s decision, not
  written here.
