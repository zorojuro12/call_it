# Phase 8b — Web Frontend Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `https://callit.duckdns.org/` serves the Next.js web app from the
same origin as the API, so a browser can register, host, join and play a
full round at the public URL.

**Architecture:** The frontend runs as one more container, `web`, holding
Next.js's `standalone` server on port 3000. Caddy routes `/api/*` and
`/healthz` to `api` as today and sends everything else to `web`, adding the
browser security headers (CSP and others) on that route only. `web` sits on
its own internal network shared only with Caddy, so it can reach neither
the API nor any data store. The browser reaches the API the way it always
did: through Caddy, at the same origin. The API base URL baked into the
bundle is derived from `CALLIT_HOSTNAME` at build time, so the one-value
hostname rule from 8a still holds.

**Tech Stack:** Next.js 16.3.3 `output: "standalone"` · `node:24.14.0-bookworm-slim`
(matches CI's Node 24 and local `v24.14.0`) · Caddy 2.8.4 · Docker Compose
v2 · Playwright (existing). No new npm or Go dependencies.

**Spec:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md) §8 (live deployment, lifted by 8a)
**Parent plan:** [`docs/plans/2026-08-21-implementation-plan.md`](2026-08-21-implementation-plan.md) §9, row 8b, and the "Phase 8 split" note
**Builds on:** [`docs/plans/2026-10-17-phase-8a-backend-deploy.md`](2026-10-17-phase-8a-backend-deploy.md), whose Decision 5 reserved `/` for this phase

---

## Global Constraints

Every task's requirements implicitly include this section.

- **No new dependencies.** No npm package, no Go module. The toolchain
  holds from `CLAUDE.md` "Stack" stay unchanged.
- **Every image is pinned to an exact tag:** `node:24.14.0-bookworm-slim`
  for both frontend stages, plus 8a's existing pins. Never `latest` or a
  bare major.
- **No behaviour or visual change to the app.** 8b ships the 6a/6b
  frontend as it is. The page title ("Create Next App"), the visual design
  and the stray `public/*.svg` scaffold files are Phase 8c's job (parent
  plan §9, row 8c).
- **The anonymity invariant is untouched.** 8b changes no payload and no
  component (`CLAUDE.md`, "Wagers stay anonymous until the round is
  terminal").
- **`web` never joins `edge`, `state`, `events` or `ledger`.** It shares a
  network with Caddy and nothing else.
- **The hostname stays one value.** `deploy/.env`'s `CALLIT_HOSTNAME` drives
  the Caddy site address, the API's CORS origin, the CSP's `connect-src`
  and the frontend's baked API URL. No task may introduce a second copy.
- **Go commands need the PATH export:** `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin`
  (`CLAUDE.md`, Known Environment Gotchas).
- **Docker Desktop must be reachable from WSL** for every task. Tasks 1–4
  build images or run the local rehearsal stack.
- **Never run `deploy/deploy.sh` in this clone.** It checks out a detached
  tag over the working branch. Rehearsals use the "Local rehearsal" commands
  in `deploy/README.md`.
- **`deploy/smoke.sh` exhausts this machine's `auth` bucket** (10/min) in
  its step 4. Wait 60 s after a smoke run before running it again or before
  running Playwright against the rehearsal stack.
- **`security-reviewer` runs before the phase closes.** This phase opens a
  new network surface and sets the browser security policy.

---

## Decisions This Plan Fixes

1. **A Node `standalone` container, not a static export.** Next's own docs
   (`node_modules/next/dist/docs/01-app/02-guides/static-exports.md`,
   "Unsupported Features") rule out a static export for dynamic routes
   without `generateStaticParams()`. `app/room/[code]` is one, and a room
   code isn't known at build time. Faking it with a placeholder param plus
   Caddy rewrites would also have to fake the RSC payload fetches that
   client navigation makes. One extra container on a 12 GB VM costs
   nothing that matters. The `standalone` output ships only the traced
   files, so the image carries no `node_modules` tree.

2. **A dedicated internal `web` network: Caddy and `web`, nobody else.**
   The browser talks to the API through Caddy, never through `web`, so `web`
   has no reason to reach `api:8080`. Putting it on `edge` would give a
   compromised Node process a direct path to the API's unproxied port, and a
   route to the internet. `web` is `internal: true`, because it needs no
   egress at runtime. Fonts are downloaded at build time by
   `next/font/google`.

3. **The API base URL is a build argument, `https://${CALLIT_HOSTNAME}`.**
   `NEXT_PUBLIC_*` values are inlined at build time, and
   `lib/socket.ts` derives the `wss://` URL from that absolute base. A
   relative base would mean changing `lib/` for no gain. The existing
   fail-fast in `lib/config.ts` already breaks the build when the value is
   empty in production. A hostname change therefore needs a rebuild, and
   `deploy.sh` always rebuilds (`dc build`), so `deploy/README.md` §9's
   domain swap still works unchanged.

4. **Browser security headers live in the Caddyfile, one definition, on the
   frontend route only.** The CSP is Next's documented "Without Nonces"
   policy (`node_modules/next/dist/docs/01-app/02-guides/content-security-policy.md`)
   plus `connect-src 'self' wss://{$CALLIT_HOSTNAME}`. Script and style
   sources keep `'unsafe-inline'`:
   - A nonce-based CSP forces every page to render dynamically, per the
     same doc.
   - The hash-based alternative (SRI) is marked experimental there.

   So the CSP's job here is to restrict where scripts, connections and
   frames may come from. XSS defence still rests on React's escaping, and
   the codebase has no `dangerouslySetInnerHTML` (checked 2026-10-18).
   Alongside the CSP:
   - `X-Content-Type-Options: nosniff`
   - `Referrer-Policy: strict-origin-when-cross-origin`, so room-code URLs
     don't leak cross-origin
   - `Permissions-Policy: camera=(), microphone=(), geolocation=()`

   The existing site-wide HSTS header stays. `next.config.ts` sets no
   headers, so `next dev` and the e2e build are unaffected.

5. **`web` is hardened like the Go services:** it runs as the image's `node`
   user, with a read-only root filesystem, `cap_drop: [ALL]` and
   `no-new-privileges`. Two `tmpfs` mounts cover the paths Next may write to
   at runtime: `/app/.next/cache` and `/tmp`. The Debian-based (glibc) image
   matches the `-gnu` native binaries that the lockfile already lists for
   arm64 (`@next/swc`, `lightningcss`, `@tailwindcss/oxide`).

---

## Review Focus

Inputs and failure modes no task's tests reach naturally. Each line names
the test that now pins it and the task that owns it.

1. **Someone opens a shared room link cold**, e.g. `https://…/room/K7QF2M`
   pasted into a new tab, not reached by client-side navigation. Expected:
   200 with the app's HTML. Pinned by Task 1 (`check-image.sh`) and Task 3
   (`smoke.sh` step 5).
2. **The CSP blocks the app's own WebSocket, fonts or styles**, so the page
   loads but a round silently never connects. Expected: a full two-browser
   round works through Caddy, with no CSP violation logged. Pinned by Task
   4's CSP guard and its run of `round.spec.ts` through Caddy.
3. **The bundle is built with the wrong API URL**: empty, `http://`, or a
   stale hostname. Expected: an empty value fails the build (existing
   `lib/config.ts` behaviour), and the build argument is asserted to be
   exactly `https://$CALLIT_HOSTNAME`. Pinned by Task 2's `check-compose.sh`
   check 4.
4. **The arm64 build is missing a native binary** (swc, lightningcss,
   oxide), and the failure only shows up on the VM mid-deploy. Expected: CI
   fails first. Pinned by Task 1's `linux/arm64` image build in CI's
   `deploy` job.
5. **A mistaken edit puts `web` on `edge` or `state`, or publishes port
   3000.** Expected: CI fails. Pinned by Task 2's `check-compose.sh` checks 1
   and 2 and the new mutation case in `check-compose_test.sh`.

---

## File Structure

```
frontend/
├── Dockerfile                  # NEW  deps → build → standalone runtime, as user node
├── .dockerignore               # NEW
├── next.config.ts              # MOD  output: "standalone"
├── playwright.config.ts        # MOD  E2E_BASE_URL: run against a deployed origin, no webServer
└── e2e/
    ├── fixtures.ts             # NEW  `test` with an auto CSP-violation guard
    ├── csp-guard.spec.ts       # NEW  proves the guard fails a violating page (deployed mode only)
    ├── join.spec.ts            # MOD  import { test, expect } from "./fixtures"
    ├── round.spec.ts           # MOD  same
    └── logic-check.spec.ts     # MOD  same
deploy/
├── compose.prod.yml            # MOD  web service, web network, caddy joins it
├── Caddyfile                   # MOD  catch-all → web:3000, security headers
├── check-compose.sh            # MOD  web membership, internal web network, build arg, hardening
├── check-image.sh              # MOD  also builds and checks the web image
├── smoke.sh                    # MOD  step 5: pages, cold room link, headers, /api not shadowed
├── tests/check-compose_test.sh # MOD  mutation: web on state fails check 2
└── README.md                   # MOD  runbook: web in §5/§7, rehearsal, checks
.github/workflows/ci.yml        # MOD  deploy job builds the web image for linux/arm64
CLAUDE.md, docs/project-history.md, docs/plans/2026-08-21-implementation-plan.md  # MOD  Task 5
```

---

## Task 1: The web image

**Files:**
- Create: `frontend/Dockerfile`, `frontend/.dockerignore`
- Modify: `frontend/next.config.ts`, `deploy/check-image.sh`, `.github/workflows/ci.yml` (`deploy` job)

**Interfaces:**
- Produces: a `frontend/Dockerfile` taking `ARG NEXT_PUBLIC_API_BASE_URL`. Its
  final stage sets `WORKDIR /app`, `USER node`, `EXPOSE 3000`, and
  `ENV NODE_ENV=production PORT=3000 HOSTNAME=0.0.0.0 NEXT_TELEMETRY_DISABLED=1`,
  and runs `CMD ["node", "server.js"]`. Task 2's compose service builds it.

**Checkpoint 1: the web image builds, runs as non-root, and serves `/` and a cold room URL**

- [ ] **Step 1: Write the failing test, then run it**

Spec: append a web section to `deploy/check-image.sh`, after the backend
checks, reusing its `fail`/`ok` helpers:
- `docker build -q -t callit-web:check --build-arg NEXT_PUBLIC_API_BASE_URL=https://callit.example.invalid frontend`
  → on failure, `fail "docker build frontend"`.
- `.Config.User` must not be empty, `0`, `root` or `0:0` → otherwise
  `fail "web image runs as root (User=…)"`.
- Size under `300 MiB` (`max_web_bytes=$((300 * 1024 * 1024))`).
- `docker run -d --rm --network none --name callit-web-check callit-web:check`.
  Then, polling for up to 20 s, run
  `docker exec callit-web-check node -e "<fetch>"`, where the script fetches
  `http://127.0.0.1:3000<path>` and prints the status and whether the body
  contains `<html`:
  - `/` → `200` with `<html`
  - `/room/ABCDEF` → `200` with `<html` (Review Focus 1)
  - `/login` → `200`

  Always `docker rm -f callit-web-check` on exit, from a `trap`.
- Print `ok: web image …` lines matching the backend section's style.

Run: `bash deploy/check-image.sh`
Expected: FAIL with `FAIL: docker build frontend` (no `frontend/Dockerfile` yet).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract:
- **`next.config.ts`:** add `output: "standalone"` and keep `allowedDevOrigins`.
- **`frontend/Dockerfile`, three stages, all `FROM node:24.14.0-bookworm-slim`:**
  - `deps`: copy `package.json` and `package-lock.json`, then `npm ci`.
  - `build`: copy `node_modules` from `deps`, then the source. Set
    `ARG NEXT_PUBLIC_API_BASE_URL`, `ENV NEXT_PUBLIC_API_BASE_URL=$NEXT_PUBLIC_API_BASE_URL`
    and `NEXT_TELEMETRY_DISABLED=1`, then `npm run build`.
  - `runtime`: copy `.next/standalone/` to `/app/`, `.next/static/` to
    `/app/.next/static/` and `public/` to `/app/public/`, all
    `--chown=node:node`. Then the `ENV`/`USER`/`EXPOSE`/`CMD` from Interfaces.
  - A header comment states Decisions 1 and 5 in two lines.
- **`frontend/.dockerignore`:** `node_modules`, `.next`, `coverage`,
  `test-results`, `playwright-report`, `e2e`, `*.tsbuildinfo`, `.env*`.

```bash
bash deploy/check-image.sh && \
  git add frontend/Dockerfile frontend/.dockerignore frontend/next.config.ts deploy/check-image.sh && \
  git commit -m "feat: standalone web image for the Next.js frontend"
```

Expected: PASS (backend and web sections), then one commit.

- [ ] **Step 3: CI builds the web image for arm64 (Review Focus 4)**

In `.github/workflows/ci.yml`'s `deploy` job, after "Build backend image for
linux/arm64", add a `docker/build-push-action@v6` step named
`Build web image for linux/arm64`: `context: frontend`,
`platforms: linux/arm64`, `push: false`,
`build-args: NEXT_PUBLIC_API_BASE_URL=https://callit.example.invalid`.
Prove it locally before committing. The second command runs under QEMU and
takes several minutes.

```bash
docker run --rm -v "$PWD:/repo" -w /repo rhysd/actionlint:1.7.7 && \
  docker buildx build --platform linux/arm64 --build-arg NEXT_PUBLIC_API_BASE_URL=https://callit.example.invalid frontend && \
  git add .github/workflows/ci.yml && \
  git commit -m "ci: build the web image for linux/arm64"
```

**Task boundary:** first prove `next start` still works under
`output: "standalone"`, because Playwright's existing `webServer` uses
`npm run start`:

```bash
make fe-lint && make fe-test && make fe-build && make fe-e2e
```

If `fe-e2e` fails because `next start` refuses the standalone output,
change `playwright.config.ts`'s second `webServer.command` to
`npm run build && cp -r public .next/standalone/ && cp -r .next/static .next/standalone/.next/ && node .next/standalone/server.js`
(Next's documented standalone start, `output.md`). Then re-run `make fe-e2e`
and commit that change as `test: start the standalone server for e2e`. If it
passes as-is, change nothing.

---

## Task 2: `web` in the production topology

**Files:**
- Modify: `deploy/compose.prod.yml`, `deploy/check-compose.sh`, `deploy/tests/check-compose_test.sh`

**Interfaces:**
- Consumes: Task 1's `frontend/Dockerfile` and its `NEXT_PUBLIC_API_BASE_URL` build argument.
- Produces: compose service `web` (`image: callit-web:local`, port 3000 on
  network `web`), which Task 3's Caddyfile proxies to as `web:3000`.

**Checkpoint 1: the topology checks require `web`, isolated and hardened, and the real file satisfies them**

- [ ] **Step 1: Write the failing test, then run it**

Spec: changes to `deploy/check-compose.sh`.
- **Header comment:** add `web` to the invariants list.
- **Check 2:** `want_networks` becomes `caddy=edge,web` and adds `web=web`;
  the other lines are unchanged.
- **Check 3:** the loop covers `state events ledger web`.
- **Check 4, new assertions:**
  - `.services.web.build.context` ends with `/frontend`.
  - `.services.web.image == "callit-web:local"`.
  - `.services.web.build.args.NEXT_PUBLIC_API_BASE_URL == "https://callit.example.invalid"`.
  Failure messages: `4. web does not build from frontend/`,
  `4. web image is not callit-web:local`, and
  `4. web build arg NEXT_PUBLIC_API_BASE_URL is not https://$CALLIT_HOSTNAME`.
  Update the check's `ok` line.
- **Check 8:** `.services.caddy.depends_on.web.condition == "service_healthy"`,
  else `fail "8. caddy must wait for a healthy web"`.
- **Check 11:** the loop covers `api relay ledger-worker migrate web`. Add a
  `web`-only assertion that `.services.web.tmpfs` contains both
  `/app/.next/cache` and `/tmp`, else `fail "11. web lacks tmpfs for /app/.next/cache and /tmp"`.
  Update the `ok` line to `Go services and web: …`.

Spec: changes to `deploy/tests/check-compose_test.sh`. Add a mutation that
replaces `web`'s `networks: [web]` line with `networks: [web, state]` and
expects check 2. Its `ok` line is `the web container on the state network fails check 2`.
Choose the mutation anchor so it occurs exactly once in the finished file;
the `mutate` helper asserts this.

Run: `bash deploy/check-compose.sh`
Expected: FAIL with `FAIL: 2. network membership differs` (no `web` service yet).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract, in `deploy/compose.prod.yml`:
- Add `web` to the header's network list: `web — caddy, web (internal)`.
- A `web` service:
  - `build: { context: ../frontend, args: { NEXT_PUBLIC_API_BASE_URL: "https://${CALLIT_HOSTNAME:?CALLIT_HOSTNAME is required — run deploy/gen-env.sh}" } }`
  - `image: callit-web:local`, `pull_policy: build`, `restart: unless-stopped`
  - `read_only: true`, `cap_drop: [ALL]`, `security_opt: ["no-new-privileges:true"]`
  - `tmpfs: [/app/.next/cache, /tmp]`
  - `networks: [web]`
  - healthcheck: `test: ["CMD", "node", "-e", "fetch('http://127.0.0.1:3000/').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"]`,
    with `interval: 10s`, `timeout: 5s`, `retries: 6`
- `caddy` changes:
  - its `networks` gains `web:` with no fixed address; the `edge` entry keeps `ipv4_address: 172.30.0.10`
  - its `depends_on` becomes a map: `api: {condition: service_started}`, `web: {condition: service_healthy}`
- Top-level `networks` gains `web: {internal: true}`.

```bash
bash deploy/check-compose.sh && bash deploy/tests/check-compose_test.sh && \
  git add deploy/compose.prod.yml deploy/check-compose.sh deploy/tests/check-compose_test.sh && \
  git commit -m "feat: web service on its own internal network behind Caddy"
```

Expected: PASS (all 11 checks, and every mutation rejected), then one commit.

**Task boundary:** run the local rehearsal (`deploy/README.md`, "Local
rehearsal") up to `up -d --build --wait`. It must finish with `web`
reported healthy. That proves the read-only filesystem and the two `tmpfs`
mounts are enough for Next at runtime. Leave the stack up for Task 3.

---

## Task 3: Caddy serves the app, with security headers

**Files:**
- Modify: `deploy/Caddyfile`, `deploy/smoke.sh`

**Interfaces:**
- Consumes: compose service `web:3000` from Task 2.

**Checkpoint 1: `/` and cold room links reach the app with the security headers, and `/api` is not shadowed**

- [ ] **Step 1: Write the failing test, then run it**

Spec: append step 5 to `deploy/smoke.sh`, and add it to the header comment's
numbered list as `5. the web app answers at /, with its security headers`.
It uses `curl -sS -D <headers-file> -o <body-file> -w '%{http_code}'`:
- `https://$host/` → `200`, and the body contains `<html`.
- `https://$host/room/ABCDEF` → `200`, and the body contains `<html` (Review Focus 1).
- The `/` response headers, matched case-insensitively:
  - `content-security-policy` contains `frame-ancestors 'none'` and `connect-src 'self' wss://$host`
  - `x-content-type-options: nosniff`
  - `referrer-policy: strict-origin-when-cross-origin`
  - `strict-transport-security` is present
- `https://$host/api/v1/no-such-route` → `404`, the body does **not**
  contain `<html`, and the headers have **no** `content-security-policy`.
  This proves the API route still wins and the frontend headers stay off it.

Each failing assertion calls `fail "5. …"` naming the URL and what was
missing. On success: `ok "5. web app at /, cold room link, security headers, /api not shadowed"`.

Run (with Task 2's rehearsal stack up): `SSL_CERT_FILE="$PWD/.caddy-root.crt" bash deploy/smoke.sh localhost`
Expected: steps 1–4 pass, then FAIL at step 5 because `https://localhost/`
returned `404`. The Caddyfile still answers `respond 404`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: in `deploy/Caddyfile`, replace the final `handle { respond 404 }`
and its "arrives here in Phase 8b" comment with:

```
	# The web frontend (plan 8b). Browser security headers are set here and
	# only here (Decision 4): the API routes above never get them.
	handle {
		header {
			Content-Security-Policy "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' blob: data:; font-src 'self'; connect-src 'self' wss://{$CALLIT_HOSTNAME}; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; upgrade-insecure-requests"
			X-Content-Type-Options nosniff
			Referrer-Policy strict-origin-when-cross-origin
			Permissions-Policy "camera=(), microphone=(), geolocation=()"
		}
		reverse_proxy web:3000
	}
```

Update the top-of-file comment: `/` now goes to `web`.

```bash
docker compose -f deploy/compose.prod.yml --env-file deploy/.env up -d --wait && \
  sleep 60 && SSL_CERT_FILE="$PWD/.caddy-root.crt" bash deploy/smoke.sh localhost && \
  git add deploy/Caddyfile deploy/smoke.sh && \
  git commit -m "feat: Caddy serves the web app with browser security headers"
```

Expected: all five steps PASS, then one commit. The `sleep 60` clears the
`auth` bucket that Step 1's run exhausted. Caddy picks up the edited
Caddyfile through the bind mount when the container is recreated, so if
`up -d` reports nothing changed, run `docker compose … restart caddy`.

---

## Task 4: A browser plays a round through Caddy, under the CSP

**Files:**
- Create: `frontend/e2e/fixtures.ts`, `frontend/e2e/csp-guard.spec.ts`
- Modify: `frontend/playwright.config.ts`, `frontend/e2e/join.spec.ts`, `frontend/e2e/round.spec.ts`, `frontend/e2e/logic-check.spec.ts`

**Interfaces:**
- Produces: `frontend/e2e/fixtures.ts` exporting `test` (Playwright's `test`
  extended with an `auto: true` fixture `cspGuard`) and re-exporting
  `expect`. Every spec imports both from `./fixtures`.
- Produces: the `E2E_BASE_URL` environment variable for
  `playwright.config.ts`. When set, it becomes `baseURL`, `webServer` is
  omitted, and `ignoreHTTPSErrors` is `true` only when the URL's hostname is
  `localhost` (Caddy's local CA, rehearsal only). When unset, the config is
  exactly as today.

**Checkpoint 1: a CSP violation fails the test that caused it**

- [ ] **Step 1: Write the failing test, then run it**

Spec:
- `playwright.config.ts` gains the `E2E_BASE_URL` behaviour from Interfaces.
- `fixtures.ts` exports `test` and `expect`, initially with no guard.
- `csp-guard.spec.ts`:
  - `test.skip(!process.env.E2E_BASE_URL, "needs a deployed origin: the CSP is set by Caddy")`
  - one test, annotated `test.fail()`: `page.goto("/")`, then
    `page.evaluate(() => { new WebSocket("wss://csp-guard.invalid/"); })`,
    then `page.waitForTimeout(500)`
  - That WebSocket violates `connect-src`, so once the guard exists the test
    fails, which `test.fail()` turns into a pass.

Run (rehearsal stack up, 60 s after any smoke run): `cd frontend && E2E_BASE_URL=https://localhost npx playwright test e2e/csp-guard.spec.ts`
Expected: FAIL with `Expected to fail, but passed`, because nothing yet
fails a test on a CSP violation.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: the `cspGuard` fixture in `fixtures.ts` uses `{ auto: true }` and
depends on `browser` and `page`. `join.spec.ts` and `round.spec.ts` open
their own contexts with `browser.newContext()`, while `csp-guard.spec.ts`
uses the default `page`, so the guard watches both:
- For the duration of the test, it replaces `browser.newContext` with a
  wrapper that calls the original, then registers `context.on("page", watch)`
  on the result. It restores the original after `use()`.
- `watch(page)` subscribes to `page.on("console", …)` and records every
  message of type `error` whose text contains `Content Security Policy`. The
  default `page` is passed to `watch` directly.
- After `use()`, it runs `expect(violations, "CSP violations").toEqual([])`.

Then change the `@playwright/test` imports in `join.spec.ts`,
`round.spec.ts` and `logic-check.spec.ts` to `./fixtures`.

```bash
cd frontend && E2E_BASE_URL=https://localhost npx playwright test e2e/csp-guard.spec.ts && \
  npx tsc --noEmit && npm run lint && \
  git add playwright.config.ts e2e/fixtures.ts e2e/csp-guard.spec.ts e2e/join.spec.ts e2e/round.spec.ts e2e/logic-check.spec.ts && \
  git commit -m "test: fail e2e tests on CSP violations and run them against a deployed origin"
```

Expected: PASS (the expected failure occurs), then one commit.

**Task boundary: the evidence run for Review Focus 2.** With the rehearsal
stack up and 60 s after any smoke run:

```bash
cd frontend && E2E_BASE_URL=https://localhost npx playwright test e2e/join.spec.ts e2e/round.spec.ts e2e/csp-guard.spec.ts && \
  cd .. && make fe-e2e
```

Expected: both browser specs pass through Caddy over TLS with zero CSP
violations, and the ordinary `make fe-e2e` (dev mode, no `E2E_BASE_URL`,
`csp-guard` skipped) still passes. Record both results for Task 5. Then tear
the rehearsal down with `docker compose -f deploy/compose.prod.yml --env-file deploy/.env down -v`
and delete `deploy/.env` and `.caddy-root.crt`.

---

## Task 5: Runbook, invariants, security review, phase record

**Files:**
- Modify: `deploy/README.md`, `CLAUDE.md`, `docs/project-history.md`, `docs/plans/2026-08-21-implementation-plan.md`

There's no RED→GREEN here: this task records and reviews.

- [ ] **Step 1: Runbook.** In `deploy/README.md`:
  - §5: `deploy.sh` now builds two images, and the first build on the VM
    takes several minutes longer (`next build`). After the smoke test, open
    `https://<name>.duckdns.org/` on a phone and play a round.
  - §7: unchanged commands. Add one sentence: a frontend-only change is
    still a tag plus `deploy.sh`.
  - §9: add that the frontend bundle is rebuilt with the new hostname by the
    same `deploy.sh` run.
  - "Local rehearsal": add the Playwright command from Task 4's boundary.
  - "Checks": note that `check-image.sh` now covers both images.
- [ ] **Step 2: `CLAUDE.md`.**
  - "Production network membership" invariant: add that `web` shares only
    the internal `web` network with Caddy and can reach neither `api` nor
    any data store.
  - New invariant: browser security headers are defined once, in
    `deploy/Caddyfile`'s frontend route. Don't add a second CSP in
    `next.config.ts`.
  - Repository Layout: Phase 8b adds `frontend/Dockerfile`.
  - Build & Test: add `E2E_BASE_URL`.
- [ ] **Step 3: Security review.** Run the `security-reviewer` agent over the
  phase diff (`git diff dev...HEAD`). Ask it to cover:
  - the CSP's strength given `'unsafe-inline'`
  - `web`'s network reach and hardening
  - whether any `NEXT_PUBLIC_*` value could carry a secret
  - header leakage onto `/api`

  Fix every CRITICAL and HIGH finding before continuing, each as its own
  `fix:` commit with a test where one is possible. Record the MEDIUM and LOW
  findings, accepted or fixed, with reasons.
- [ ] **Step 4: `docs/project-history.md`.** Add a "Phase 8b" section in the
  shape of 8a's:
  - decisions, briefly, linking this plan
  - verification evidence: the `check-image.sh` output, `smoke.sh` 5/5, the
    Playwright run through Caddy, and the CI arm64 build
  - the security review record
  - deviations found during execution
  - the line "Go-live is appended here after merge"
- [ ] **Step 5: Parent plan.** In `docs/plans/2026-08-21-implementation-plan.md`
  §9, mark row 8b `✅ (code; go-live per the 8b plan's "After merge" section)`.
- [ ] **Step 6: Verify and commit.**

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
  bash deploy/check-compose.sh && bash deploy/tests/check-compose_test.sh && \
  make fe-lint && make fe-test && make test && \
  git add deploy/README.md CLAUDE.md docs/project-history.md docs/plans/2026-08-21-implementation-plan.md && \
  git commit -m "docs: web deploy runbook, 8b invariants, and security review record"
```

Expected: everything green, then one commit. The branch is now green and
verified. Hand off to `finishing-a-development-branch`.

---

## After merge: go-live (not a plan task)

Going live is a deploy of the merged `dev`, done on the VM after
`finishing-a-development-branch`:
1. Tag the merge `v0.3.0` and push the tag.
2. On the VM: `cd ~/call_it && bash deploy/deploy.sh v0.3.0`. Expect the
   first `next build` there to take a few minutes. `smoke.sh` runs all five
   steps.
3. From the laptop, 60 s later: `bash deploy/smoke.sh callit.duckdns.org`,
   with no `SSL_CERT_FILE`, since this is the real certificate.
4. On a phone and on the laptop, open `https://callit.duckdns.org/`. Register
   on one, join from the other with the room code, and play one round to the
   settlement reveal.
5. Optional: `E2E_BASE_URL=https://callit.duckdns.org npx playwright test e2e/round.spec.ts e2e/csp-guard.spec.ts`
   from the laptop. This creates real accounts on production, like the
   smoke test does.
6. Append the result to `docs/project-history.md`'s Phase 8b section.

The live site will still look like the 6a/6b frontend. Making it look
finished is Phase 8c, whose design inputs are being gathered in
`docs/design/8c-stitch-prompts.md`.
