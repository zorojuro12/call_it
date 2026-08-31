# CallIt load test scripts

k6 scripts that drive real load against a live backend, added in Phase 7a
to give spec §7's performance targets real measured numbers. k6 is an
external binary, not a Go dependency — it installs user-locally and adds
nothing to `go.mod`.

## Install

```bash
mkdir -p "$HOME/.local/bin" && cd "$HOME" && \
  curl -fsSLO https://github.com/grafana/k6/releases/download/v2.2.0/k6-v2.2.0-linux-amd64.tar.gz && \
  tar -xzf k6-v2.2.0-linux-amd64.tar.gz && \
  mv k6-v2.2.0-linux-amd64/k6 "$HOME/.local/bin/k6" && \
  rm -rf k6-v2.2.0-linux-amd64 k6-v2.2.0-linux-amd64.tar.gz && \
  k6 version
```

`$HOME/.local/bin` must be on `PATH` (the same non-interactive-shell caveat
CLAUDE.md documents for Go — add it explicitly if a tool-execution shell
reports `k6: not found`). No sudo is available or needed; this is a
user-local install like the Go toolchain.

## Running a scenario

Both scenarios need the backend and Redis (`make up`) running, and
`cmd/api` started with `METRICS_ADDR` set so the server-side histograms are
reachable:

```bash
make up
JWT_SECRET=$(openssl rand -hex 32) METRICS_ADDR=127.0.0.1:9090 go run ./backend/cmd/api &
make loadtest                          # runs rest_throughput.js
make loadtest SCENARIO=wager_latency   # runs wager_latency.js
```

## What each scenario measures

- **`rest_throughput.js`** — a ramping-arrival-rate scenario climbing
  toward spec §7's 5,000 requests/sec target, each iteration an
  authenticated REST call. Threshold: `http_req_failed` pinned at exactly
  `rate==0` (spec §7's double-spend tolerance is 0.00%, so a run that
  quietly sheds requests isn't measuring throughput) and
  `http_req_duration p(99)<15`.
- **`wager_latency.js`** — a WebSocket scenario: one VU opens rounds as
  host, separate VUs join as players and place wagers, each with a fresh
  UUIDv4 idempotency key. Measures `wager_ack_ms`, the elapsed time from
  sending `place_wager` to receiving the matching `wager_accepted` reply.
  Threshold: `wager_ack_ms p(99)<15`.

## Server-side figures are primary, k6's are secondary

k6's own client-side timing is measured from wherever k6 runs, which under
WSL2 crosses the VM's virtualized network stack before it ever reaches
`cmd/api` — the parent implementation plan's risk table names this as a
known Medium risk to p99 measurement fidelity. **Treat the
`callit_wager_place_ok_p99_ms` / `callit_ws_sync_p99_ms` values from the
`METRICS_ADDR` endpoint as authoritative**, and k6's own
`http_req_duration` / `wager_ack_ms` summaries as a secondary,
directionally-useful cross-check — not the number reported against spec
§7's targets.
