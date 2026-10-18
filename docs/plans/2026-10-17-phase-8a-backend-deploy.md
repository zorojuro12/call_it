# Phase 8a — Backend Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The CallIt backend answers on a public `https://` / `wss://`
hostname from one free cloud VM, with secrets that never touch git, data
stores reachable by nothing but the processes that need them, restorable
backups, and a one-command deploy and rollback to any git tag.

**Architecture:** One container image carries all four static Go binaries
(`api`, `relay`, `ledger-worker`, `migrate`). A production Compose file
(`deploy/compose.prod.yml`) runs them next to Redis, PostgreSQL and Kafka,
and splits them across four Docker networks. The split means the API process
*cannot* open a connection to PostgreSQL or Kafka, which turns two
`CLAUDE.md` invariants (the WebSocket server never writes PostgreSQL; only
`cmd/relay` produces to Kafka) from conventions into topology. Caddy is the
only container with published ports. It terminates TLS with an automatic
Let's Encrypt certificate (HTTP-01) for a DuckDNS hostname, and the API
learns real client IPs from Caddy's `X-Forwarded-For` through a new,
explicit trusted-proxy setting. The hostname is one value in one file
(`deploy/.env`'s `CALLIT_HOSTNAME`): the Caddyfile and the API's CORS
origin both derive from it, so a bought domain later is a one-line change.

**Tech Stack:** Go 1.26.7 · Docker Compose v2 · Caddy 2 · distroless static
base image · Bash + `jq` for the deploy checks · Oracle Cloud Always Free
(Ampere A1, arm64, 2 OCPU / 12 GB, verified 2026-10-17 against Oracle's
docs) · DuckDNS. No new Go module dependencies.

**Spec:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md) §8 (live deployment, lifted by this phase)
**Parent plan:** [`docs/plans/2026-08-21-implementation-plan.md`](2026-08-21-implementation-plan.md) §9, rows 8a–8d and the "Phase 8 split" note

---

## Global Constraints

Every task's requirements implicitly include this section.

- **Go toolchain stays at `go 1.26.7` / CI pin `1.26`; the five held
  dependency versions stay held** (`CLAUDE.md` "Stack"). This phase adds no
  Go dependency. The builder image is `golang:1.26.7-alpine`, matching
  `go.mod` exactly.
- **Every container image is pinned to an explicit version tag, never
  `latest` or a bare major** — `redis:7.2-alpine`, `postgres:16-alpine`,
  `apache/kafka:3.7.0` (the same tags local dev runs), `caddy:2.8.4-alpine`,
  `gcr.io/distroless/static-debian12:nonroot`.
- **The WebSocket server never writes PostgreSQL directly; only
  `cmd/relay` produces to Kafka.** In this phase both are enforced by
  network membership as well (Decision 2). No task may attach `api` to the
  `ledger` or `events` network.
- **No secret is ever committed.** Real values live only in `deploy/.env` on
  the server, which is created by `deploy/gen-env.sh`, has mode `0600`, and
  is covered by the repo's existing `.env` ignore rule. Test fixtures use
  obviously fake values.
- **`ENV=production` on `api`, `relay` and `ledger-worker`.** This activates
  the fail-fast rules that already exist: `JWT_SECRET` required,
  `CORS_ALLOWED_ORIGINS` required, `METRICS_ADDR` loopback-only.
- **`METRICS_ADDR` stays unset in production.** A loopback listener inside a
  container is unreachable from the host anyway; exposing metrics is
  Phase 9's Prometheus work.
- **Test commands need the Go PATH export:** `export
  PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin` (`CLAUDE.md`, Known
  Environment Gotchas). Every Go command below carries it.
- **Docker must be reachable from the WSL distro before Task 3** (`CLAUDE.md`
  gotcha: Docker Desktop → Settings → Resources → WSL Integration, then a new
  shell). Tasks 1–2 are pure Go and do not need it. At planning time
  (2026-10-17) `docker` was **not** reachable from this distro.
- **`security-reviewer` runs before the phase closes.** This phase changes
  how the auth rate limiter identifies callers and opens the first public
  network surface, both `CLAUDE.md` triggers.

---

## Decisions This Plan Fixes

1. **Client IP behind the proxy is the rightmost untrusted
   `X-Forwarded-For` entry, and only when the TCP peer is a configured
   trusted proxy.** The peer address alone is what `ClientIP` reads today,
   and its own comment defers the proxy design to "when a real proxy sits in
   front". This phase is that time. Behind Caddy every request's peer is
   Caddy, so without this change all users share **one** `auth` bucket (10
   register/login attempts per minute for the whole site) and one
   `ws_connect` fallback bucket. Rightmost-untrusted rather than leftmost:
   Caddy's docs say it "sets or augments" the header, so a client-supplied
   prefix can survive into it. The entry Caddy itself appends is the
   rightmost one, and it's the only one this process can trust. A peer that
   is not a trusted proxy has its header ignored entirely, exactly as today.

2. **Four networks, one per trust relationship.** `edge` (caddy, api — not
   internal, Caddy needs egress for ACME), `state` (api, relay, redis),
   `events` (relay, ledger-worker, kafka), `ledger` (migrate, ledger-worker,
   postgres). `state`, `events` and `ledger` are `internal: true`.
   Consequences: `api` can reach Redis and nothing else behind it, Kafka is
   reachable only by its producer and its consumer, and no data store has a
   route to the internet.

3. **Kafka stays PLAINTEXT with no ACLs, and Redis stays without `AUTH`.**
   `CLAUDE.md` says Kafka broker access equals ledger-write access and must
   be revisited before a shared deployment. This is that revisit, and the
   answer is Decision 2: the broker is reachable only from `relay` and
   `ledger-worker` on an internal network. Redis gets the same treatment for
   the same reason. Adding `requirepass` would mean threading a password
   through `redisstore.New`'s signature and every test that calls it, to
   defend against an attacker who already has code execution inside one of
   our own containers. Revisit both if any data store ever leaves this
   single host. PostgreSQL keeps a password because its image requires one.

4. **Images are built on the VM, from a git tag; there is no registry.** The
   VM is arm64, so native `docker compose build` there needs no
   cross-compilation and no registry credentials, and it costs nothing. CI
   proves the image still builds for `linux/arm64` (Task 7) so a broken
   Dockerfile is caught before a deploy, not during one. The release unit is
   an annotated git tag (`v0.1.0` already exists as the pre-deploy
   baseline); `deploy/deploy.sh <tag>` deploys one and rollback is the same
   command with the previous tag.

5. **One hostname, path-routed.** Caddy serves `/api/*` and `/healthz` from
   `api` today and returns 404 for everything else. Phase 8b puts the web
   frontend at `/` on the same hostname, which is why the API's
   `CORS_ALLOWED_ORIGINS` is set to `https://${CALLIT_HOSTNAME}` now: the
   browser origin and the API share one origin from 8b onward.

6. **HTTP-01, not DNS-01.** Stock `caddy:2.8.4-alpine` obtains the
   certificate over port 80. The DuckDNS DNS module would need a custom Caddy
   build for no benefit on a host where port 80 is open anyway. The DuckDNS
   token is used only by the IP-updater cron (Task 6).

7. **Caddy delays closing WebSockets on a config reload.** Its docs say
   streams are force-closed on every reload by default; `stream_close_delay
   5m` on the API proxy avoids dropping every live room on a Caddy reload.
   (A container *restart* still drops them, and the 7c reconnect grace
   window is what absorbs that.)

8. **Local rehearsal uses `CALLIT_HOSTNAME=localhost`.** Caddy issues
   `localhost` a certificate from its own local CA. Go clients trust that CA
   through `SSL_CERT_FILE`, which Go's `crypto/x509` honors on Linux, so
   `cmd/callit-cli` needs no "skip TLS verification" flag, and must never
   get one.

---

## Review Focus

Inputs and failure modes the tasks' own tests don't naturally reach. Each
line names the test that now pins it and the task that owns it.

1. **A client spoofs `X-Forwarded-For` to get fresh rate-limit buckets.**
   Expected: no effect unless the request's TCP peer is a trusted proxy, and
   behind Caddy only the rightmost entry counts. Pinned by Task 1
   Checkpoint 2's spoofed-prefix and untrusted-peer cases, and by Task 7's
   `smoke.sh` check 2, which runs through real Caddy.
2. **`TRUSTED_PROXIES=0.0.0.0/0` (or `::/0`) is set to "make it work".**
   Expected: the API refuses to start, because trusting every peer is the
   same as trusting every header. Pinned by Task 1 Checkpoint 1.
3. **The VM is replaced or Oracle reclaims it.** Accounts and balances live
   permanently in Redis (parent plan §4, Amendment B1), so losing Redis loses
   every account, not just a cache. Expected: a restore from the latest
   backup brings back a user who can log in. Pinned by Task 5's restore
   drill.
4. **Someone runs `gen-env.sh` a second time on a live server.** Expected:
   it refuses to overwrite. A fresh `JWT_SECRET` would invalidate every
   session, and a fresh `POSTGRES_PASSWORD` would no longer match the
   existing database volume. Pinned by Task 6 Checkpoint 1.
5. **A mistaken edit puts the `api` service on the `ledger` network, or
   publishes Redis's port "to debug".** Expected: CI fails. Pinned by Task 4's
   `check-compose.sh`, which Task 7 runs in CI.

---

## File Structure

```
backend/
├── Dockerfile                     # NEW  multi-stage → distroless, all four binaries in /app
├── .dockerignore                  # NEW
├── cmd/callit-cli/main.go         # MOD  -secure flag (https/wss)
├── cmd/callit-cli/main_test.go    # NEW
├── cmd/api/main.go                # MOD  pass cfg.TrustedProxies into Deps
├── internal/config/config.go      # MOD  TRUSTED_PROXIES → Config.TrustedProxies
├── internal/config/config_test.go # MOD
├── internal/httpapi/middleware.go # MOD  ClientIP → NewClientIP(trusted)
├── internal/httpapi/health.go     # MOD  Deps.TrustedProxies
├── internal/httpapi/ws_handlers.go# MOD  wsThrottleKey takes the resolver
└── internal/httpapi/*_test.go     # MOD/NEW
deploy/
├── compose.prod.yml               # NEW  the production topology
├── Caddyfile                      # NEW
├── gen-env.sh                     # NEW  creates deploy/.env with generated secrets, never overwrites
├── deploy.sh                      # NEW  deploy.sh <git-tag>: checkout, build, migrate, up, smoke
├── backup.sh / restore.sh         # NEW
├── duckdns-update.sh              # NEW  cron: keeps the DuckDNS record on the VM's IP
├── smoke.sh                       # NEW  healthz + spoof check + a full CLI round
├── check-image.sh                 # NEW  image invariants (nonroot, four binaries run)
├── check-compose.sh               # NEW  topology invariants via `docker compose config --format json` + jq
├── tests/fixture.env              # NEW  fake values for check-compose.sh
├── tests/gen-env_test.sh          # NEW
├── tests/check-compose_test.sh    # NEW  proves check-compose.sh fails on a mutated topology
└── README.md                      # NEW  the runbook: VM, firewall, DuckDNS, first deploy, rollback, restore
.github/workflows/ci.yml           # MOD  `deploy` job
```

---

## Task 1: Trusted-proxy client IP

Executed inline: this is the phase's one change to request-handling code,
and it decides how the auth rate limiter tells callers apart.

**Files:**
- Modify: `backend/internal/config/config.go`, `backend/internal/config/config_test.go`
- Modify: `backend/internal/httpapi/middleware.go`, `health.go`, `ws_handlers.go`, `auth_handlers.go`
- Modify: `backend/cmd/api/main.go`
- Test: `backend/internal/httpapi/clientip_test.go` (new), `backend/internal/httpapi/ratelimit_test.go` (existing `ClientIP` callers move to `NewClientIP(nil)`)

**Interfaces:**
- Produces: `config.Config.TrustedProxies []netip.Prefix`, parsed from `TRUSTED_PROXIES`.
- Produces: `httpapi.NewClientIP(trusted []netip.Prefix) func(*http.Request) string`. It replaces the package-level `ClientIP`, which is removed, so nothing can keep calling the proxy-unaware version.
- Produces: `httpapi.Deps.TrustedProxies []netip.Prefix`. `authThrottle(store, clientIP)` and `wsThrottleKey(issuer, clientIP)` take the resolver built from it once, in `NewMux`.

**Checkpoint 1: `TRUSTED_PROXIES` parses into a validated prefix list**

- [ ] **Step 1: Write the failing test, then run it**

Spec, table-driven in `config_test.go`, each case on top of a valid base env (`JWT_SECRET` set):
- unset → `TrustedProxies == nil`, no error
- `"172.30.0.10/32"` → one prefix, `172.30.0.10/32`
- `"172.30.0.10/32, 10.0.0.0/8"` → two prefixes, whitespace trimmed, order kept
- `"172.30.0.10"` (no mask) → error containing `TRUSTED_PROXIES entry "172.30.0.10" is not a CIDR prefix`
- `"0.0.0.0/0"` → error containing `TRUSTED_PROXIES must not trust every address`; same for `"::/0"`
- `"172.30.0.10/32,"` (trailing comma) → one prefix; empty elements are skipped, matching `CORS_ALLOWED_ORIGINS`

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/config -run TestLoad_TrustedProxies -count=1`
Expected: FAIL, because `Config.TrustedProxies` is undefined (compile error).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add a `loadTrustedProxies(cfg *Config, lookup LookupFunc) error` helper beside `loadAllowedOrigins`, called from `Load`. Parse each entry with `netip.ParsePrefix`, store it `.Masked()`, and reject `Bits() == 0`. Same rule in every `ENV`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/config -count=1 && \
  git add internal/config/config.go internal/config/config_test.go && \
  git commit -m "feat: parse TRUSTED_PROXIES into a validated prefix list"
```

Expected: PASS, then one commit.

**Checkpoint 2: `NewClientIP` resolves the rightmost untrusted forwarded address, and only from a trusted peer**

- [ ] **Step 1: Write the failing test, then run it**

Spec, table-driven in `clientip_test.go`, with `trusted = [172.30.0.10/32]` unless stated otherwise:

| RemoteAddr | X-Forwarded-For | want |
|---|---|---|
| `198.51.100.7:4000` | *(none)* | `198.51.100.7` |
| `198.51.100.7:4000` | `203.0.113.9` | `198.51.100.7` (untrusted peer, header ignored) |
| `172.30.0.10:4000` | *(none)* | `172.30.0.10` |
| `172.30.0.10:4000` | `203.0.113.9` | `203.0.113.9` |
| `172.30.0.10:4000` | `1.1.1.1, 203.0.113.9` | `203.0.113.9` (client-supplied prefix ignored) |
| `172.30.0.10:4000` | two header lines `1.1.1.1` then `203.0.113.9` | `203.0.113.9` (lines joined in order) |
| `172.30.0.10:4000` | `203.0.113.9, 172.30.0.10` | `203.0.113.9` (trusted entries skipped from the right) |
| `172.30.0.10:4000` | `172.30.0.10` | `172.30.0.10` (all trusted → peer) |
| `172.30.0.10:4000` | `203.0.113.9, garbage` | `172.30.0.10` (unparseable entry → peer, fail closed) |
| `[::ffff:172.30.0.10]:4000` | `203.0.113.9` | `203.0.113.9` (IPv4-mapped peer is unmapped before matching) |
| `[2001:db8::1]:4000`, trusted `[2001:db8::/32]` | `2001:db8:ffff::5, 2001:db8::1` | `2001:db8::1` (both entries fall inside the /32, so all are trusted → peer; guards a prefix-contains bug) |
| `[2001:db8::1]:4000`, trusted `[2001:db8::/32]` | `2001:db9::5, 2001:db8::1` | `2001:db9::5` (IPv6 rightmost untrusted) |
| `not-an-addr` | `203.0.113.9` | `not-an-addr` (unchanged from today's fallback) |
| `198.51.100.7:4000`, trusted `nil` | `203.0.113.9` | `198.51.100.7` (`NewClientIP(nil)` is today's `ClientIP`, byte for byte) |

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/httpapi -run TestNewClientIP -count=1`
Expected: FAIL, because `NewClientIP` is undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `NewClientIP(trusted)` returns a closure. Parse the peer with `net.SplitHostPort` then `netip.ParseAddr(...).Unmap()`; if either fails, or the peer isn't contained in any trusted prefix, return the peer host string (raw `RemoteAddr` on split failure, as today). Otherwise split `strings.Join(r.Header.Values("X-Forwarded-For"), ",")` on commas, trim each entry, and walk right to left. Skip entries contained in a trusted prefix; return the first untrusted one as `addr.Unmap().String()`. Return the peer if an entry fails to parse or every entry is trusted. Delete `ClientIP` and move its doc comment's reasoning onto `NewClientIP`, replacing the "gets designed then" sentence with Decision 1's rule. Update `ratelimit_test.go`'s callers to `NewClientIP(nil)`.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/httpapi -run 'TestNewClientIP|TestRateLimit' -count=1 && \
  git add internal/httpapi/middleware.go internal/httpapi/ws_handlers.go internal/httpapi/clientip_test.go internal/httpapi/ratelimit_test.go && \
  git commit -m "feat: resolve client IP from X-Forwarded-For behind a trusted proxy"
```

Expected: PASS, then one commit. So that the package compiles once `ClientIP` is deleted, this checkpoint also changes its two callers to `NewClientIP(nil)`: `authThrottle`'s `KeyFn` and `wsThrottleKey`'s fallback. Behavior is identical, since `nil` means "trust no peer". Checkpoint 3 wires the real list. Add `ws_handlers.go` to this checkpoint's `git add`.

**Checkpoint 3: the auth and socket throttles key on the resolved address**

- [ ] **Step 1: Write the failing test, then run it**

Spec, an integration test in `ratelimit_test.go` against Redis DB 15, using that file's existing mux/store fixture with `Deps.TrustedProxies = [127.0.0.1/32]`:
- 10 `POST /api/v1/auth/login` with bad credentials, `RemoteAddr = "127.0.0.1:5555"`, `X-Forwarded-For: 203.0.113.1` → all 401.
- 11th from the same `203.0.113.1` → 429 `rate_limit_exceeded`.
- 1 from `X-Forwarded-For: 203.0.113.2`, same peer → 401, **not** 429.
- `GET /api/v1/socket` with no token: 30 from `203.0.113.3` → none are 429 (they fail auth with 401); the 31st → 429; then one from `203.0.113.4` → 401, not 429. (`wsConnectLimit = 30`, `ws_handlers.go:13`.)

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./internal/httpapi -run TestThrottle_TrustedProxy -count=1`
Expected: FAIL. `Deps.TrustedProxies` is undefined; once added and still unwired, the `203.0.113.2` and `203.0.113.4` requests return 429.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: add `Deps.TrustedProxies`. `NewMux` builds `clientIP := NewClientIP(d.TrustedProxies)` once and passes it to `authThrottle(d.Store, clientIP)` and, via `wsThrottle`, to `wsThrottleKey(issuer, clientIP)`, whose IP fallback becomes `"ip:" + clientIP(r)`. `cmd/api/main.go` sets `TrustedProxies: cfg.TrustedProxies`. Add `TRUSTED_PROXIES` to README.md's configuration section beside `CORS_ALLOWED_ORIGINS`, with the 0/0 rule.

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./internal/httpapi ./internal/config -count=1 && go build ./... && \
  git add internal/httpapi/health.go internal/httpapi/middleware.go internal/httpapi/ws_handlers.go internal/httpapi/auth_handlers.go internal/httpapi/ratelimit_test.go cmd/api/main.go ../README.md && \
  git commit -m "feat: key auth and socket throttles on the proxy-resolved client IP"
```

Expected: PASS, then one commit.

**Task boundary:** `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make test` (full stack up) must pass, including the `-coverpkg=./...` figure for `internal/httpapi` staying at or above its pre-phase value.

---

## Task 2: `callit-cli -secure`

**Files:**
- Modify: `backend/cmd/callit-cli/main.go:24-35,78`
- Create: `backend/cmd/callit-cli/main_test.go`

**Interfaces:**
- Produces: `endpoints(addr string, secure bool) (httpBase, wsBase string)` in package `main`. Task 8's `smoke.sh` runs `callit-cli -secure -addr <host>`.

**Checkpoint 1: `-secure` selects https/wss**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `endpoints("localhost:8080", false)` → `("http://localhost:8080", "ws://localhost:8080")`; `endpoints("callit.duckdns.org", true)` → `("https://callit.duckdns.org", "wss://callit.duckdns.org")`.

Run: `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && go test ./cmd/callit-cli -count=1`
Expected: FAIL, because `endpoints` is undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: a `-secure` bool flag (default `false`, so today's behavior is unchanged). `base` and the socket URL are built from `endpoints`. There is no flag that disables certificate verification (Decision 8).

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && cd backend && \
  go test ./cmd/callit-cli -count=1 && go vet ./cmd/callit-cli && \
  git add cmd/callit-cli/main.go cmd/callit-cli/main_test.go && \
  git commit -m "feat: add -secure to callit-cli for https/wss targets"
```

Expected: PASS, then one commit.

---

## Task 3: Container image

Needs Docker (Global Constraints).

**Files:**
- Create: `backend/Dockerfile`, `backend/.dockerignore`, `deploy/check-image.sh`

**Interfaces:**
- Produces: image `callit-backend:<ref>` with `/app/api`, `/app/relay`, `/app/ledger-worker`, `/app/migrate`, no entrypoint, and a non-root user. Task 4 references it as `build: { context: ../backend }` with a per-service `command`.

**Checkpoint 1: the image builds, runs as non-root, and every binary starts**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `deploy/check-image.sh` (bash, `set -euo pipefail`) runs `docker build -t callit-backend:check backend` and then asserts:
- `docker inspect -f '{{.Config.User}}'` is non-empty and neither `0` nor `root`.
- `docker run --rm --network none callit-backend:check /app/api` exits non-zero with stderr containing `JWT_SECRET is required`.
- The same for `/app/migrate` and `/app/ledger-worker` → `POSTGRES_DSN is required`.
- `docker run --rm --network none -e ENV=bogus callit-backend:check /app/relay` → `is not one of development|production|test`.
- The image is under 100 MB (`docker image inspect -f '{{.Size}}'`).

Each assertion prints `ok: …` or `FAIL: …`, and the script exits 1 on the first failure. These exact config-error strings come from `internal/config/config.go`. They prove the binary is present, executable and linked for this architecture, with no infrastructure running.

Run: `bash deploy/check-image.sh`
Expected: FAIL at `docker build`, because there is no Dockerfile.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract:
- **Build stage:** `golang:1.26.7-alpine`. `go mod download` runs on `go.mod`/`go.sum` alone so the layer caches, then `CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/ ./cmd/api ./cmd/relay ./cmd/ledger-worker ./cmd/migrate`.
- **Final stage:** `gcr.io/distroless/static-debian12:nonroot`, `COPY --from=build /out/ /app/`, `USER nonroot`.
- **`.dockerignore`:** excludes `bin/`, `*_test.go`, `coverage*`.

The Lua scripts and migrations are already `go:embed`ed (`scripts/lua/embed.go`, `migrations/embed.go`), so nothing else needs copying.

```bash
bash deploy/check-image.sh && \
  git add backend/Dockerfile backend/.dockerignore deploy/check-image.sh && \
  git commit -m "feat: build all four backend binaries into one distroless image"
```

Expected: every line `ok:`, then one commit.

---

## Task 4: Production Compose topology

**Files:**
- Create: `deploy/compose.prod.yml`, `deploy/check-compose.sh`, `deploy/tests/fixture.env`

**Interfaces:**
- Consumes: Task 3's image; Task 1's `TRUSTED_PROXIES`.
- Produces: services `caddy`, `api`, `relay`, `ledger-worker`, `migrate`, `redis`, `postgres`, `kafka`. Caddy's static `edge` address is `172.30.0.10` (subnet `172.30.0.0/24`). `deploy/.env` keys: `CALLIT_HOSTNAME`, `JWT_SECRET`, `POSTGRES_PASSWORD`, `DUCKDNS_TOKEN` (optional), `ACME_EMAIL` (optional).

**Checkpoint 1: the topology satisfies every isolation and fail-fast invariant**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `deploy/check-compose.sh` renders `docker compose -f deploy/compose.prod.yml --env-file deploy/tests/fixture.env config --format json` and asserts each of the following with `jq`, printing `ok:`/`FAIL:` per line:
1. Only `caddy` has `ports`; its published ports are exactly `80` and `443`.
2. Network membership is exact: `caddy`=[edge], `api`=[edge,state], `relay`=[events,state], `ledger-worker`=[events,ledger], `migrate`=[ledger], `redis`=[state], `postgres`=[ledger], `kafka`=[events].
3. `state`, `events`, `ledger` have `internal: true`; `edge` does not.
4. Every `image` value has an explicit tag that isn't `latest`; the four Go services use `build` with context `../backend`.
5. `api` environment: `ENV=production`, `CORS_ALLOWED_ORIGINS=https://callit.example.invalid` (the fixture's hostname), `TRUSTED_PROXIES=172.30.0.10/32`, `REDIS_ADDR=redis:6379`, and **no** `METRICS_ADDR` key.
6. `relay` and `ledger-worker` have `KAFKA_BROKERS=kafka:9092` and `ENV=production`; `ledger-worker` and `migrate` have a `POSTGRES_DSN` whose host is `postgres`.
7. `kafka` has `KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://kafka:9092` and `KAFKA_HEAP_OPTS=-Xms1g -Xmx1g`.
8. `ledger-worker.depends_on.migrate.condition == "service_completed_successfully"`; `api` and `relay` depend on `redis` being `service_healthy`.
9. `restart` is `unless-stopped` on every service except `migrate`, which is `"no"`.
10. Rendering with `JWT_SECRET` removed from a temp copy of the fixture exits non-zero (`${JWT_SECRET:?…}` interpolation). The same holds for `POSTGRES_PASSWORD` and `CALLIT_HOSTNAME`.

`fixture.env` holds `CALLIT_HOSTNAME=callit.example.invalid` and placeholder secrets that are clearly fake: a 64-character `JWT_SECRET` of repeated `f`, and `POSTGRES_PASSWORD=fixture-not-a-secret`.

Run: `bash deploy/check-compose.sh`
Expected: FAIL, because `compose.prod.yml` doesn't exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `compose.prod.yml` implements exactly the assertions above.
- **Redis and Postgres:** Redis keeps `--appendonly yes`. Both keep their dev healthchecks. Volumes are named `redis-data`, `postgres-data`, `kafka-data`, `caddy-data` and `caddy-config`.
- **Postgres:** `POSTGRES_USER=callit`, `POSTGRES_DB=callit`, `POSTGRES_PASSWORD=${POSTGRES_PASSWORD:?…}`. The DSN is `postgres://callit:${POSTGRES_PASSWORD}@postgres:5432/callit?sslmode=disable`; plaintext is acceptable on an internal network, per Decision 3.
- **Kafka:** keeps dev's KRaft settings except the two in assertion 7, plus a `kafka-data` volume at `/var/lib/kafka/data`.
- **api:** `JWT_TTL` keeps its default.
- **caddy:** mounts `./Caddyfile` read-only and passes `CALLIT_HOSTNAME`/`ACME_EMAIL` through.

```bash
bash deploy/check-compose.sh && \
  git add deploy/compose.prod.yml deploy/check-compose.sh deploy/tests/fixture.env && \
  git commit -m "feat: production compose with network-isolated data stores"
```

Expected: every line `ok:`, then one commit.

---

## Task 5: Caddy, backup and restore, proven by a local rehearsal

These are one task because the restore drill is the only meaningful test of
either backup script, and it needs the whole stack running behind Caddy.

**Files:**
- Create: `deploy/Caddyfile`, `deploy/backup.sh`, `deploy/restore.sh`

**Interfaces:**
- Produces: `deploy/backup.sh [dest-dir]` (default `/var/backups/callit`). It writes `callit-YYYYmmdd-HHMMSS.tar.gz` containing `postgres.sql` (from `pg_dump`) and `redis/` (`dump.rdb` plus `appendonlydir/`, taken after a `BGSAVE` whose `LASTSAVE` has advanced), and keeps the newest 7.
- Produces: `deploy/restore.sh <archive>`. It stops `api`, `relay` and `ledger-worker`, restores both stores, then starts everything again.

**Checkpoint 1: Caddy serves the API over TLS and nothing else**

- [ ] **Step 1: Write the failing test, then run it**

Spec, with a local `deploy/.env` holding `CALLIT_HOSTNAME=localhost` and generated secrets (write it by hand for now; Task 6 automates it), and the stack started with `docker compose -f deploy/compose.prod.yml --env-file deploy/.env up -d --build`:
- `curl -sk https://localhost/healthz` → HTTP 200 with `{"status":"ok"}` (`internal/httpapi/health.go`'s body; confirm the exact JSON there before pinning it).
- `curl -sk -o /dev/null -w '%{http_code}' https://localhost/` → `404`.
- `curl -s -o /dev/null -w '%{http_code}' http://localhost/healthz` → `308` (redirect to https).
- `curl -sk -D- https://localhost/healthz` includes `Strict-Transport-Security`.

Run: the four `curl`s above.
Expected: FAIL, because the `caddy` service can't start without a `Caddyfile`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: a global block with `email {$ACME_EMAIL}`, omitted when empty. Caddy gets **no** `trusted_proxies` setting: it is the first hop, so no upstream's forwarded headers deserve trust. Then one site block `{$CALLIT_HOSTNAME}` containing:
- `log` (stdout access log)
- `header Strict-Transport-Security "max-age=31536000"`
- `handle /api/* { reverse_proxy api:8080 { stream_close_delay 5m } }`
- `handle /healthz { reverse_proxy api:8080 }`
- `handle { respond 404 }`

Validate with `docker run --rm -v "$PWD/deploy/Caddyfile:/etc/caddy/Caddyfile:ro" -e CALLIT_HOSTNAME=localhost caddy:2.8.4-alpine caddy validate --config /etc/caddy/Caddyfile`.

```bash
docker compose -f deploy/compose.prod.yml --env-file deploy/.env up -d && \
  [ "$(curl -sk -o /dev/null -w '%{http_code}' https://localhost/healthz)" = 200 ] && \
  [ "$(curl -sk -o /dev/null -w '%{http_code}' https://localhost/)" = 404 ] && \
  [ "$(curl -s -o /dev/null -w '%{http_code}' http://localhost/healthz)" = 308 ] && \
  curl -sk -D- -o /dev/null https://localhost/healthz | grep -qi strict-transport-security && \
  git add deploy/Caddyfile && \
  git commit -m "feat: Caddy TLS front for the API with HSTS and WebSocket close delay"
```

Expected: all checks pass, then one commit.

**Checkpoint 2: a backup restores a registered user onto empty volumes**

- [ ] **Step 1: Write the failing test, then run it**

Spec (the restore drill):
1. Register `drill@example.invalid` (password `drill-password-123`) via `curl -sk -X POST https://localhost/api/v1/auth/register`.
2. Run `bash deploy/backup.sh "$PWD/.backup-drill"`.
3. Run `docker compose … down -v`, which **deletes the volumes**. Run this only against the local rehearsal; the script is never run with `-v` on the server.
4. `up -d`; the same login now → 401 (proves the wipe).
5. `bash deploy/restore.sh .backup-drill/callit-*.tar.gz`.
6. The same login → 200, and `docker compose … exec -T postgres psql -U callit -tAc "select count(*) from schema_migrations"` → ≥ 1.

Run: steps 1–2.
Expected: FAIL at step 2, because `backup.sh` doesn't exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract:
- **backup.sh:** `set -euo pipefail`; uses `docker compose -f "$(dirname "$0")/compose.prod.yml" --env-file …/.env`.
  - Postgres: `exec -T postgres pg_dump -U callit --clean --if-exists callit`.
  - Redis: `exec -T redis redis-cli BGSAVE`, polls `LASTSAVE` until it changes (60 s timeout → exit 1), then `docker compose cp redis:/data` into a staging directory.
  - Writes the tar with `umask 077`, then prunes to the newest 7.
- **restore.sh:** checks the archive exists, then:
  1. Asks `type RESTORE to continue` on a TTY, or accepts `--yes` when non-interactive.
  2. Stops `api`, `relay` and `ledger-worker`.
  3. Pipes `postgres.sql` into `psql -U callit callit`.
  4. Stops `redis`, copies `redis/` back into its volume via `docker compose cp`, and starts `redis`.
  5. Waits for healthy, then starts the stopped services.

```bash
bash deploy/backup.sh "$PWD/.backup-drill" && \
  docker compose -f deploy/compose.prod.yml --env-file deploy/.env down -v && \
  docker compose -f deploy/compose.prod.yml --env-file deploy/.env up -d --wait && \
  bash deploy/restore.sh --yes .backup-drill/callit-*.tar.gz && \
  [ "$(curl -sk -o /dev/null -w '%{http_code}' -X POST https://localhost/api/v1/auth/login -H 'Content-Type: application/json' -d '{"email":"drill@example.invalid","password":"drill-password-123"}')" = 200 ] && \
  rm -rf .backup-drill && \
  git add deploy/backup.sh deploy/restore.sh && \
  git commit -m "feat: backup and restore for Redis and PostgreSQL, proven by a restore drill"
```

Expected: login 200 after restore, then one commit.

---

## Task 6: Server-side secrets and the DuckDNS updater

**Files:**
- Create: `deploy/gen-env.sh`, `deploy/tests/gen-env_test.sh`, `deploy/duckdns-update.sh`

**Interfaces:**
- Produces: `deploy/gen-env.sh <hostname> [duckdns-token]`, which writes `deploy/.env`.
- Produces: `deploy/duckdns-update.sh`, which reads `CALLIT_HOSTNAME`/`DUCKDNS_TOKEN` from `deploy/.env` and is run from cron every 5 minutes.

**Checkpoint 1: `gen-env.sh` generates strong secrets and never overwrites**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `gen-env_test.sh` runs `gen-env.sh` against a temp copy of `deploy/` (`mktemp -d`; the script writes beside itself) and asserts:
- `gen-env.sh callit.duckdns.org` → exit 0. `.env` has mode `600`, `CALLIT_HOSTNAME=callit.duckdns.org`, a `JWT_SECRET` of exactly 64 lowercase hex characters, and a `POSTGRES_PASSWORD` of exactly 48 hex characters.
- Two runs in fresh temp dirs → different `JWT_SECRET`s.
- A second run in the same dir → exit 1 with stderr `deploy/.env already exists; refusing to overwrite`, and the file is byte-identical afterward.
- No hostname argument → exit 2 with a usage line.
- Rendering `compose.prod.yml` with the generated `.env` passes `docker compose config -q`.

Run: `bash deploy/tests/gen-env_test.sh`
Expected: FAIL, because `gen-env.sh` doesn't exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract:
- Generates secrets with `openssl rand -hex 32` / `-hex 24`.
- Creates the file with `umask 077`, then writes it once using noclobber (`set -C`), so the overwrite refusal is atomic.
- Writes `DUCKDNS_TOKEN` only when the second argument is given.

`duckdns-update.sh` calls `curl -fsS "https://www.duckdns.org/update?domains=${CALLIT_HOSTNAME%.duckdns.org}&token=${DUCKDNS_TOKEN}&ip="` and exits non-zero unless the body is `OK`. It's not unit-tested, since its only behavior is that one call against DuckDNS. It gets exercised during go-live (README step 4).

```bash
bash deploy/tests/gen-env_test.sh && \
  git add deploy/gen-env.sh deploy/tests/gen-env_test.sh deploy/duckdns-update.sh && \
  git commit -m "feat: generate server secrets once and keep DuckDNS pointed at the VM"
```

Expected: every line `ok:`, then one commit.

---

## Task 7: Deploy, smoke and CI

**Files:**
- Create: `deploy/deploy.sh`, `deploy/smoke.sh`
- Modify: `.github/workflows/ci.yml` (new `deploy` job)

**Interfaces:**
- Consumes: Task 2's `-secure`; Tasks 3–6.
- Produces: `deploy/deploy.sh <git-ref>` and `deploy/smoke.sh <hostname>`. Both are used by the runbook.

**Checkpoint 1: `smoke.sh` proves a full round over TLS, and that a spoofed header can't escape a rate-limit bucket**

- [ ] **Step 1: Write the failing test, then run it**

Spec: with the Task 5 local stack up, `SSL_CERT_FILE=<caddy local root> bash deploy/smoke.sh localhost` asserts:
1. `GET https://<host>/healthz` → 200.
2. 11 bad logins sending a different spoofed `X-Forwarded-For` value each time → the 11th is 429. This proves the client-supplied prefix is ignored end to end, through real Caddy.
3. A full round with two `callit-cli -secure -addr <host>` processes, fed through named pipes: one host and one guest. The host opens a two-outcome round, the guest wagers, the host resolves, and both outputs contain the settlement reveal line. Base the stdin scripts on `cmd/callit-cli/main.go`'s prompts, and read those prompts before writing the feed.
4. Within 30 s, `docker compose … exec -T postgres psql -U callit -tAc "select count(*) from transactions"` has increased, which proves the outbox → Kafka → ledger path is live.

Get the root certificate with `docker compose … cp caddy:/data/caddy/pki/authorities/local/root.crt`; the README documents this for rehearsal only. Use a fresh email per run (`smoke-$(date +%s)@example.invalid`).

Run: `bash deploy/smoke.sh localhost`
Expected: FAIL, because `smoke.sh` doesn't exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `smoke.sh` as specified above, exiting non-zero on the first failed check. `deploy.sh <ref>` (`set -euo pipefail`):
1. Refuses if the working tree is dirty.
2. Runs `git fetch --tags origin` and `git checkout --detach <ref>`.
3. Runs `bash deploy/check-compose.sh`, then `docker compose … build`, then `up -d --wait`.
4. Runs `bash deploy/smoke.sh "$CALLIT_HOSTNAME"`.
5. Records the ref in `deploy/.deployed-ref`, which is gitignored.

On a smoke failure it prints the previous ref from `.deployed-ref` and the exact rollback command, `deploy/deploy.sh <previous>`. It doesn't roll back by itself: an automatic rollback right after `migrate` has applied a new schema could leave the database ahead of the older code.

```bash
SSL_CERT_FILE="$PWD/.caddy-root.crt" bash deploy/smoke.sh localhost && \
  git add deploy/smoke.sh deploy/deploy.sh .gitignore && \
  git commit -m "feat: tag-based deploy with an end-to-end TLS smoke test"
```

Expected: PASS, then one commit. Add `deploy/.deployed-ref` and `.caddy-root.crt` to `.gitignore` in this commit.

**Checkpoint 2: CI checks the image and topology on every push**

- [ ] **Step 1: Write the failing test, then run it**

Spec: a new `deploy` job in `ci.yml` that:
- runs `bash deploy/check-compose.sh` and `bash deploy/tests/gen-env_test.sh`
- uses `docker/setup-qemu-action` and `docker/setup-buildx-action`, then builds `backend/` for `linux/arm64` with `push: false`

All actions are pinned to a major tag, matching the file's existing `actions/checkout@v4` style.

The RED is the guard the CI job runs. `check-compose.sh` gains a `COMPOSE_FILE_OVERRIDE` env var, which renders a different file. `deploy/tests/check-compose_test.sh` copies `compose.prod.yml` to a temp file twice, mutating each copy once:
- add `ports: ["6379:6379"]` to `redis`
- add `ledger` to `api`'s networks

For each copy it asserts that `check-compose.sh` exits 1 and names the violated check (`FAIL: 1.` / `FAIL: 2.`). It also asserts that the unmodified file exits 0.

Run: `bash deploy/tests/check-compose_test.sh`
Expected: FAIL. The override variable doesn't exist yet, so both mutated runs render the real file and exit 0.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `check-compose.sh` renders `${COMPOSE_FILE_OVERRIDE:-deploy/compose.prod.yml}`. The `deploy` job also runs `check-compose_test.sh`.

```bash
bash deploy/tests/check-compose_test.sh && bash deploy/check-compose.sh && \
  git add deploy/check-compose.sh deploy/tests/check-compose_test.sh .github/workflows/ci.yml && \
  git commit -m "ci: check deploy topology and build the arm64 image on every push"
```

Expected: PASS, then one commit. The job's first real run happens when the branch is pushed during `finishing-a-development-branch`. That workflow already requires CI to be green before merge.

---

## Task 8: Runbook, docs, and security review

**Files:**
- Create: `deploy/README.md`
- Modify: `CLAUDE.md`, `README.md`, `docs/project-history.md`, `docs/specs/2026-08-21-callit-design.md` §8, `docs/plans/2026-08-21-implementation-plan.md` §9 (mark 8a ✅)

**Checkpoint 1: the runbook is complete and the phase is reviewed**

There's no RED for prose, so this is one verification checkpoint, not a TDD cycle.

- [ ] **Step 1: Write the runbook and run the review**

`deploy/README.md` sections, in order:
1. **Create the VM.** Oracle Cloud → Compute → Instance: shape `VM.Standard.A1.Flex`, 2 OCPU / 12 GB, Ubuntu 24.04 (aarch64), in the tenancy's home region (Always Free applies only there, per Oracle's docs).
2. **Open the firewall.** The VCN security list allows ingress on TCP 22, 80 and 443 only. On the VM, check that 80/443 are reachable from outside; Oracle's Ubuntu images can ship host `iptables` REJECT rules, so if a port is unreachable, inspect and adjust the `INPUT` chain.
3. **Install Docker** (Docker's official apt repository), then `git clone` the repo.
4. **Set up DuckDNS.** Create the subdomain at duckdns.org, run `bash deploy/gen-env.sh <name>.duckdns.org <token>`, run `deploy/duckdns-update.sh` once (expect `OK`), and add a cron entry every 5 minutes.
5. **First deploy:** `bash deploy/deploy.sh v0.2.0`. Caddy obtains the certificate on first start; `docker compose logs caddy` should show it.
6. **Backups.** Add a cron entry for `deploy/backup.sh` at 03:00, and enable Oracle's boot-volume backup policy. Off-VM copies matter because Oracle can reclaim an idle free instance (7 days with CPU p95, network and memory all under 20%, per Oracle's docs).
7. **Deploy a new version, and roll back.**
8. **Restore from a backup.**
9. **Swap in a real domain.** Change `CALLIT_HOSTNAME`, rerun `deploy.sh` with the current ref, and that's all.

Other updates:
- `CLAUDE.md`:
  - a Build & Test entry for the `deploy/` scripts
  - a Critical Invariants entry: *"Production network membership is an enforced invariant: `api` is never on the `ledger` or `events` network; `check-compose.sh` fails CI otherwise."*
  - update the Kafka-access invariant's "currently enforced by topology alone" sentence to name Decision 2's networks
  - `TRUSTED_PROXIES` added to the server-environment sentence
- `docs/project-history.md`: the 8a security review, Decisions 1–4, and the verification evidence (smoke output, restore drill).
- Spec §8: the live-deployment bullet becomes *"lifted by Phase 8a (2026-10)"*.

Then run the `security-reviewer` agent over the branch diff, focusing on `NewClientIP`, `compose.prod.yml`, the Caddyfile, `gen-env.sh` and `backup.sh` (archive permissions). Fix every CRITICAL and HIGH finding, each as its own `fix:` commit with a test where one is possible.

- [ ] **Step 2: Verify and commit**

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && make lint && make test && \
  bash deploy/check-compose.sh && bash deploy/tests/gen-env_test.sh && \
  git add deploy/README.md CLAUDE.md README.md docs/project-history.md docs/specs/2026-08-21-callit-design.md docs/plans/2026-08-21-implementation-plan.md && \
  git commit -m "docs: deployment runbook, 8a invariants, and security review record"
```

Expected: the branch is green and verified. This plan stops here; integration goes through `finishing-a-development-branch`.

---

## After merge: go-live (not a plan task)

Going live is a deploy of the merged `dev`. It's done after
`finishing-a-development-branch` because `deploy.sh` checks out a ref from
GitHub:
1. Tag the merge `v0.2.0` and push the tag.
2. Follow `deploy/README.md` §1–6 on the VM.
3. Run `deploy/smoke.sh <name>.duckdns.org` from the laptop, with no
   `SSL_CERT_FILE`, since this is a real Let's Encrypt certificate.
4. In `docker compose logs caddy`, confirm the access log shows the
   laptop's public IP, not a Docker gateway address. If it shows a gateway,
   Docker's userland proxy is rewriting the source address, and Decision 1's
   per-client buckets aren't working yet. Record the result in
   `docs/project-history.md` either way.
