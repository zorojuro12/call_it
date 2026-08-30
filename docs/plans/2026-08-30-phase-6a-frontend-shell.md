# Phase 6a — Frontend Shell Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Delegation:** Tasks 3 and 4 are delegated, one subagent per task, via the
> `delegating-plan-tasks` skill — both are mechanical layers over contracts
> this plan states exactly (the REST envelope's three shapes; a two-key
> storage wrapper), with no cross-task continuity to lose. **Every other
> task is inline.** Task 1 is a security-sensitive network surface (origin
> admission) and Task 9 is this phase's acceptance evidence — neither is the
> right place to absorb delegation risk. Task 2 shells out to
> `create-next-app` against the network in a WSL2 environment with known
> gotchas, which a cold subagent cannot troubleshoot from the plan alone.

**Goal:** Put a real browser in front of the CallIt backend — a
TypeScript/Next.js app that registers an account, creates or joins a room,
opens the authenticated WebSocket, and shows a live presence roster — and
open the backend to browser origins so any of that is possible at all.

**Architecture:** A `frontend/` Next.js App Router app alongside `backend/`
in the existing monorepo. Two client modules own every backend contact
point: `lib/api.ts` (REST, unwrapping the `{data}` / `{error}` envelope) and
`lib/socket.ts` (one WebSocket, a typed `on(type, handler)` dispatch table).
`lib/protocol.ts` mirrors the Go wire structs as TypeScript types, so the
compiler — not runtime — catches contract drift. Pages stay thin: they
render state and call those two modules. On the backend, one new
`httpapi.CORS` middleware and the WebSocket upgrader read a **single**
config-driven origin allowlist.

**The 6a/6b seam.** This phase builds the transport and stops. `lib/socket.ts`
exposes `on(type, handler)` but registers **no gameplay handlers**; Phase 6b
registers `round_opened`, `odds_updated`, `round_locked`, `round_resolved`,
and `round_refunded` against that same dispatch table without reopening the
transport. This mirrors the backend's own 4a/4b split, so each frontend half
consumes exactly one backend half.

**Tech Stack:** TypeScript 5 (`strict: true`) · React 19 · Next.js (App
Router) · Tailwind CSS · Vitest + React Testing Library (component/unit) ·
Playwright (E2E) · Node 24.14.0 / npm 11.9.0 (verified present) · Go 1.22.10
(Task 1 only).

**Spec:** `docs/specs/2026-08-21-callit-design.md` (§2 stack, §3 identity and
buy-in, §6 auth) and `docs/plans/2026-08-21-implementation-plan.md` §9 (the
6a row and the Phase 6 split note). Both travel with this plan; read them
alongside it.

**Branch:** `git checkout -b phase-6a-frontend-shell dev`

---

## Global Constraints

Every task's requirements implicitly include this section. Values copied
verbatim from `CLAUDE.md`, the spec, and the parent plan.

- **Wagers stay anonymous until the round is terminal, and the frontend must
  not reconstruct per-user state client-side.** `CLAUDE.md` binds Phase 6 by
  name here. 6a renders identity and presence only; it must never accumulate,
  cache, or derive a per-user stake from anything it receives. There is
  nothing to accumulate yet — keep it that way.
- **A connection's room and identity come from the verified JWT, never from a
  path or payload.** The room page's URL segment (`/room/[code]`) is the
  **join code**, used for exactly one thing: the `POST
  /api/v1/rooms/{code}/participants` call. Once a room-scoped token exists,
  the socket is opened with that token and nothing else. No outbound socket
  message may carry a `room_id` field — the server ignores it, and sending
  one creates a second source for a fact that has exactly one.
- **All amounts are integer token units.** Balances and stakes are
  TypeScript `number` holding integers; never construct a fractional token.
  Odds/multipliers are the only floats and exist for display only.
- **Never `go get -u`** (Task 1 adds no Go dependency — if one seems needed,
  stop and re-read `CLAUDE.md`'s Stack section). On the npm side: commit
  `frontend/package-lock.json`, and never run `npm update`.
- **80% minimum coverage, TDD (RED → GREEN → IMPROVE), AAA structure.**
  Frontend coverage is measured by `vitest run --coverage` over `lib/` and
  `components/`; Next.js route files (`app/**/layout.tsx`, `app/**/page.tsx`)
  are thin wiring and are excluded from the floor, the same allowance
  `cmd/*` has on the Go side.
- **Query by accessible role first** (`getByRole(role, { name })`), then
  label, then text; `getByTestId` is an escape hatch only
  (`.claude/rules/ecc/react/testing.md`). Tests assert what a user sees, never
  internal state or which hooks ran.
- **No secrets in frontend code.** The only environment value the browser
  bundle may carry is `NEXT_PUBLIC_API_BASE_URL`. The JWT secret, Redis, and
  PostgreSQL never appear in `frontend/`.
- **`go` may report "not found" in a non-interactive shell.** Run
  `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin` before any Go command.
  No sudo is available in this environment — never retry a failure with it.

---

## Decisions This Plan Fixes

Recorded here because a later reader will otherwise re-litigate them.

**1. Stack: TypeScript + App Router.** Resolves the open question
`CLAUDE.md` records against the Phase 6 tooling import ("confirm or correct
this when Phase 6's plan fixes the actual stack"). Confirmed as assumed —
`.claude/rules/ecc/typescript/` stays installed. TypeScript earns its place
specifically because `lib/protocol.ts` mirrors Go structs that nothing else
checks it against.

**2. Token storage: `sessionStorage`, two separate keys.** The backend issues
two different JWTs — an **account token** (from register/login, no room
claim) and a **room-scoped token** (from create-room or join-room, carrying
`room_id`). They are stored under distinct keys and never substituted for one
another; the socket accepts only the room token.

`sessionStorage` over the alternatives: it dies with the tab, which matches a
session-scoped game, and it is never attached automatically to a request — so
enabling CORS in Task 1 does not simultaneously open a CSRF surface, as an
httpOnly cookie would (that path also needs backend `Set-Cookie` plus CSRF
tokens, out of scope here). The accepted trade-off is that an XSS on the page
can read the token, exactly as it could with `localStorage`; `sessionStorage`
narrows the window rather than closing the hole. **Flag this to
`security-reviewer` at phase close** rather than assuming it passes.

**3. No automatic socket reconnect in 6a.** Spec §4's known limitation:
`EndSession` fires on disconnect, so a reconnect ends the old session and
rejoins at the room's buy-in, silently discarding in-room profit or loss.
Auto-reconnecting would fire that repeatedly and invisibly. 6a surfaces a
disconnected state to the user and stops. The grace window that would make
reconnect safe is deferred to Phase 7, and reconnect ships no earlier.

**4. CORS allowlist has exactly one definition.** The REST middleware and the
WebSocket `CheckOrigin` read the same parsed `[]string` from config. Two
lists that must agree is the shape `CLAUDE.md` already rejects for Redis keys
and the rate limiter.

---

## File Structure

**Backend (Task 1 only):**
- Modify: `backend/internal/config/config.go` — `AllowedOrigins []string` on `Config`
- Create: `backend/internal/httpapi/cors.go` — `CORS(origins []string) func(http.Handler) http.Handler`
- Modify: `backend/internal/httpapi/router.go` (or wherever `NewMux`/`Deps` lives) — wrap the mux, carry origins on `Deps`
- Modify: `backend/internal/ws/handler.go` — replace `var upgrader = websocket.Upgrader{}` with an origin-checking constructor
- Test: `backend/internal/config/config_test.go`, `backend/internal/httpapi/cors_test.go`, `backend/internal/ws/handler_test.go`

**Frontend:**
```
frontend/
├── package.json · package-lock.json · tsconfig.json · next.config.ts
├── vitest.config.ts · vitest.setup.ts · playwright.config.ts
├── app/
│   ├── layout.tsx              # shell, Tailwind import
│   ├── page.tsx                # landing: join-by-code form + link to login
│   ├── login/page.tsx
│   ├── register/page.tsx
│   ├── host/page.tsx           # create a room (account token required)
│   └── room/[code]/page.tsx    # presence roster + own identity/balance
├── lib/
│   ├── protocol.ts             # types mirroring Go wire structs — no logic
│   ├── api.ts                  # REST client, envelope unwrap, ApiError
│   ├── session.ts              # sessionStorage wrapper, two token keys
│   └── socket.ts               # WebSocket client, typed on(type, handler)
├── components/
│   ├── PresenceRoster.tsx
│   └── ErrorBanner.tsx
└── e2e/
    └── join.spec.ts            # two browser contexts, one room
```

Each `lib/` module has one responsibility and is tested directly; pages
compose them and are tested through the DOM.

---

## Task 1: Browser origin admission (backend)

Nothing in this phase is reachable from a browser until this lands. The API
sets no CORS headers, and `internal/ws/handler.go:14` constructs
`websocket.Upgrader{}` — gorilla's zero value, whose default `CheckOrigin`
**rejects** any upgrade whose `Origin` host differs from the request `Host`.
A page served from `:3000` therefore fails at both doors.

**Design note — why the middleware wraps the mux rather than each route.** A
CORS preflight is an `OPTIONS` request to a path registered only for `POST`;
`http.ServeMux` answers that with 405 before any per-route middleware runs.
Wrapping outside the mux is what lets the preflight be answered at all. This
is the one place in this codebase where middleware is applied globally rather
than per route, and that is the reason.

**Files:**
- Modify: `backend/internal/config/config.go` — add `AllowedOrigins []string` to `Config`, parsed in `Load`
- Create: `backend/internal/httpapi/cors.go`
- Modify: `backend/internal/httpapi/health.go:32-41` — add `AllowedOrigins []string` to `Deps`
- Modify: `backend/internal/httpapi/ws_handlers.go:36` — pass the origins through to `ws.Handler`
- Modify: `backend/internal/ws/handler.go:14,31` — replace the package-level `upgrader` var with a per-handler upgrader built from options
- Modify: `backend/cmd/api/main.go:79` — wrap `httpapi.NewMux(...)` in `httpapi.CORS(cfg.AllowedOrigins)`
- Test: `backend/internal/config/config_test.go`, `backend/internal/httpapi/cors_test.go`, `backend/internal/ws/handler_test.go`

**Interfaces:**
- Consumes: `config.Load(lookup LookupFunc) (Config, error)`; `httpapi.NewMux(d Deps) *http.ServeMux`; `ws.Handler(hub *Hub, issuer *auth.Issuer, cfg ClientConfig, onMessage MessageHandler, sessions SessionEnder) http.HandlerFunc`
- Produces:
  - `config.Config.AllowedOrigins []string`
  - `httpapi.CORS(origins []string) func(http.Handler) http.Handler`
  - `httpapi.Deps.AllowedOrigins []string`
  - `ws.HandlerOption` and `ws.WithAllowedOrigins(origins []string) HandlerOption`
  - `ws.Handler(...)` gains a trailing variadic `opts ...HandlerOption`. **Variadic specifically so the six existing call sites compile unchanged** — this is the functional-options pattern `.claude/rules/ecc/golang/patterns.md` already prescribes.
  - `NewMux` keeps its current signature and return type.

**Preconditions:** `export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin` and
`make up` (the `httpapi` and `ws` suites use Redis DB 15).

---

**Checkpoint 1: the origin allowlist is parsed and validated from config**

- [ ] **Step 1: Write the failing test, then run it**

Add a table-driven case set to `config_test.go` covering `Load`'s handling of
`CORS_ALLOWED_ORIGINS`:

| env | `CORS_ALLOWED_ORIGINS` | expected |
|---|---|---|
| `ENV` unset (→ `development`) | unset | `AllowedOrigins == []string{"http://localhost:3000"}` |
| `ENV=development` | `"http://a.test,http://b.test"` | `[]string{"http://a.test", "http://b.test"}` |
| `ENV=development` | `"http://a.test , http://b.test"` | `[]string{"http://a.test", "http://b.test"}` (each element trimmed) |
| `ENV=production` | unset | error mentioning `CORS_ALLOWED_ORIGINS` |
| `ENV=production` | `""` | error mentioning `CORS_ALLOWED_ORIGINS` |
| `ENV=development` | `"*"` | error — a wildcard is rejected outright, in every env |
| `ENV=development` | `"http://a.test,*"` | error — rejected even mixed into a list |
| `ENV=development` | `"not-a-url"` | error — each entry must parse as an absolute URL with a scheme and host |

The production default is deliberately *absent* rather than
`http://localhost:3000`: a production deployment that forgets the variable
must fail fast, exactly as `JWT_SECRET` already does, not silently trust a
dev origin.

Run: `cd backend && go test ./internal/config/ -run TestLoad -count=1`
Expected: FAIL — `cfg.AllowedOrigins` is an undefined field.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `Load` populates `Config.AllowedOrigins []string`. Unset and
`ENV != "production"` yields `["http://localhost:3000"]`; unset or empty with
`ENV=production` is an error. The value splits on `,`, trims each element,
drops empties, and errors if any element is `*` or fails to parse as an
absolute URL with both scheme and host.

```bash
cd backend && go test ./internal/config/ -count=1 && \
  git add internal/config/config.go internal/config/config_test.go && \
  git commit -m "feat: parse and validate the browser origin allowlist"
```

Expected: PASS, then one commit.

---

**Checkpoint 2: an allowed origin gets CORS headers, a disallowed one does not**

- [ ] **Step 1: Write the failing test, then run it**

New `cors_test.go`. Build `CORS([]string{"http://localhost:3000"})` around a
stub handler that writes 200 and the body `ok`, then:

- `GET` with `Origin: http://localhost:3000` → response carries
  `Access-Control-Allow-Origin: http://localhost:3000` (the origin **echoed**,
  never `*`), `Access-Control-Allow-Credentials: true`, and `Vary: Origin`;
  status 200 and body `ok` (the inner handler still ran).
- `GET` with `Origin: http://evil.test` → **no** `Access-Control-Allow-Origin`
  header at all; status 200 and body `ok` (the request is not blocked
  server-side — the browser enforces the block, and a server-side 403 here
  would break every non-browser client, including `cmd/callit-cli`).
  `Vary: Origin` is still set, so a cache never serves one origin's response
  to another.
- `GET` with no `Origin` header → no `Access-Control-Allow-Origin`, status
  200, body `ok`.

Run: `cd backend && go test ./internal/httpapi/ -run TestCORS -count=1`
Expected: FAIL — `CORS` is undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `CORS(origins []string) func(http.Handler) http.Handler`. On every
request it sets `Vary: Origin`. If the request's `Origin` exactly matches an
allowlist entry it echoes that origin into `Access-Control-Allow-Origin` and
sets `Access-Control-Allow-Credentials: true`. Otherwise it adds no CORS
headers. It always calls the next handler (preflight handling is Checkpoint 3).

```bash
cd backend && go test ./internal/httpapi/ -run TestCORS -count=1 && \
  git add internal/httpapi/cors.go internal/httpapi/cors_test.go && \
  git commit -m "feat: echo allowlisted browser origins from a CORS middleware"
```

Expected: PASS, then one commit.

---

**Checkpoint 3: a preflight is answered 204 and never reaches the mux**

- [ ] **Step 1: Write the failing test, then run it**

Extend `cors_test.go`. Wrap a stub handler that records whether it ran, then:

- `OPTIONS` with `Origin: http://localhost:3000` and
  `Access-Control-Request-Method: POST` → status **204**, the inner handler
  **did not run**, and the response carries `Access-Control-Allow-Origin:
  http://localhost:3000`, `Access-Control-Allow-Methods` containing `POST`,
  `Access-Control-Allow-Headers` containing `Authorization` and
  `Content-Type`, and `Access-Control-Max-Age` set.
- `OPTIONS` with `Origin: http://evil.test` and
  `Access-Control-Request-Method: POST` → status **204**, inner handler did
  not run, and **no** `Access-Control-Allow-Origin` header (the browser
  rejects the preflight on the missing header).
- `OPTIONS` with **no** `Access-Control-Request-Method` header → not a
  preflight; the inner handler **does** run.

Run: `cd backend && go test ./internal/httpapi/ -run TestCORS -count=1`
Expected: FAIL — the preflight case reaches the inner handler and returns
200, not 204.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: a request is a preflight when its method is `OPTIONS` **and** it
carries `Access-Control-Request-Method`. `CORS` short-circuits a preflight
with 204 and no body, after applying Checkpoint 2's origin rules plus
`Access-Control-Allow-Methods: GET, POST, OPTIONS`,
`Access-Control-Allow-Headers: Authorization, Content-Type`, and
`Access-Control-Max-Age: 600`. Also wire it in `cmd/api/main.go`:
`Handler: httpapi.CORS(cfg.AllowedOrigins)(httpapi.NewMux(deps))`.

```bash
cd backend && go test ./internal/httpapi/ -count=1 && go build ./... && \
  git add internal/httpapi/cors.go internal/httpapi/cors_test.go cmd/api/main.go && \
  git commit -m "feat: answer CORS preflights ahead of the mux"
```

Expected: PASS, then one commit.

---

**Checkpoint 4: the socket upgrade honours the same allowlist**

- [ ] **Step 1: Write the failing test, then run it**

Extend `handler_test.go`. Serve
`Handler(hub, issuer, DefaultClientConfig(), nil, nil, WithAllowedOrigins([]string{"http://localhost:3000"}))`
with a valid room-scoped token, and dial with `websocket.Dialer` supplying an
explicit `Origin` request header:

- `Origin: http://localhost:3000` → upgrade **succeeds**; the client receives
  the `connected` envelope.
- `Origin: http://evil.test` → upgrade **fails** with HTTP **403**.
- no `Origin` header (a non-browser client such as `cmd/callit-cli`) →
  upgrade **succeeds**.
- a handler built with **no** `WithAllowedOrigins` option and dialed with
  `Origin: http://evil.test` → upgrade fails, i.e. the pre-existing
  same-origin default is preserved for the six call sites that pass no option.

Run: `cd backend && go test ./internal/ws/ -run TestHandler -count=1 -race`
Expected: FAIL — `WithAllowedOrigins` is undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `type HandlerOption func(*handlerOpts)` and
`WithAllowedOrigins(origins []string) HandlerOption`. `Handler` accepts
`opts ...HandlerOption` and builds its own `websocket.Upgrader` per call
instead of using a package-level var. `CheckOrigin` returns true when the
request has no `Origin` header, or when `Origin` exactly matches an allowlist
entry; with no option supplied it falls back to gorilla's same-origin default.
Then thread it through: `Deps.AllowedOrigins`, passed by `registerWSRoutes`
into `ws.Handler`, set from `cfg.AllowedOrigins` in `cmd/api/main.go`.

```bash
cd backend && go test ./internal/ws/ -count=1 -race && go build ./... && \
  git add internal/ws/handler.go internal/ws/handler_test.go internal/httpapi/health.go internal/httpapi/ws_handlers.go cmd/api/main.go && \
  git commit -m "feat: check the socket upgrade origin against the shared allowlist"
```

Expected: PASS, then one commit.

---

**Task 1 boundary — full suite:**

```bash
cd backend && go vet ./... && gofmt -l . && go test ./... -race -cover -p 1 -count=1
```

Expected: PASS, `gofmt -l` prints nothing. All six pre-existing `ws.Handler`
call sites must still compile untouched — if any needed editing, the variadic
option was not applied as specified.

## Task 2: Next.js scaffold and the frontend test harness

Everything after this task depends on a working `frontend/` with a test
runner. Per the skill's right-sizing rule, the scaffold, Tailwind, the Vitest
harness, the Makefile targets, `.gitignore`, and the CI job are all **setup
folded into this task** rather than tasks of their own — they exist to serve
this task's deliverable and nothing can test them independently.

**Environment notes.** Node **v24.14.0** and npm **11.9.0** are verified
present on this machine. `create-next-app` needs network access. It is
interactive by default — the flags below make it non-interactive; if a flag
is rejected by the installed version, run
`npx create-next-app@latest --help` and map it to the current equivalent
rather than falling back to the interactive prompts, which will stall a
non-interactive shell.

**Files:**
- Create: the `frontend/` tree (scaffold), `frontend/vitest.config.ts`, `frontend/vitest.setup.ts`, `frontend/lib/config.ts`
- Modify/Create: `frontend/app/layout.tsx`, `frontend/app/page.tsx`
- Modify: `Makefile` (repo root), `.gitignore` (repo root), `.github/workflows/ci.yml`
- Test: `frontend/app/page.test.tsx`, `frontend/lib/config.test.ts`

**Interfaces:**
- Produces:
  - `frontend/lib/config.ts` → `export const API_BASE_URL: string` and `export function resolveApiBaseUrl(env: Record<string, string | undefined>): string`
  - Makefile targets `fe-install`, `fe-dev`, `fe-test`, `fe-lint`, `fe-build`
  - npm scripts `dev`, `build`, `lint`, `test`, `test:coverage`, `typecheck`

---

**Checkpoint 1: the landing page renders the app shell**

- [ ] **Step 1: Write the failing test, then run it**

Scaffold and install the harness first (this is setup, not the behavior under
test):

```bash
\
npx --yes create-next-app@latest frontend \
  --ts --tailwind --eslint --app --no-src-dir --use-npm \
  --import-alias "@/*" --no-turbopack --skip-install && \
cd frontend && npm install && \
npm install --save-dev vitest @vitejs/plugin-react jsdom \
  @testing-library/react @testing-library/user-event @testing-library/jest-dom \
  @vitest/coverage-v8
```

Then add `vitest.config.ts` (React plugin, `environment: "jsdom"`,
`setupFiles: ["./vitest.setup.ts"]`, coverage provider `v8` including
`lib/**` and `components/**` and excluding `app/**`), and `vitest.setup.ts`
(`import "@testing-library/jest-dom/vitest"`). Set `"strict": true` in
`tsconfig.json` if the scaffold did not.

Now write `app/page.test.tsx`: render `<Page />` and assert

- `getByRole("heading", { name: /CallIt/i })` is present, and
- `getByRole("textbox", { name: /room code/i })` is present, and
- `getByRole("button", { name: /join/i })` is present.

Run: `cd frontend && npx vitest run app/page.test.tsx`
Expected: FAIL — the scaffold's generated `app/page.tsx` renders Next.js
boilerplate, so none of the three queries match. (Vitest re-runs on every
invocation; there is no cached-PASS hazard to defeat here.)

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `app/page.tsx` is a client component rendering the landing shell —
an `<h1>` naming CallIt, a labelled room-code text input, a Join submit
button, and a link to `/login`. **No submit behavior yet** (Task 6 wires it);
the form must not throw when submitted. `app/layout.tsx` sets `lang="en"` on
`<html>` and imports the Tailwind stylesheet.

Also fold in, in this same commit:
- Root `.gitignore`: `frontend/node_modules/`, `frontend/.next/`, `frontend/coverage/`, `frontend/test-results/`, `frontend/playwright-report/`
- Root `Makefile`: `fe-install` (`cd frontend && npm ci`), `fe-dev`, `fe-test` (`cd frontend && npx vitest run`), `fe-lint` (`cd frontend && npm run lint && npx tsc --noEmit`), `fe-build`
- `frontend/package.json` scripts: `typecheck` → `tsc --noEmit`, `test` → `vitest run`, `test:coverage` → `vitest run --coverage`
- `.github/workflows/ci.yml`: a `frontend` job using `actions/setup-node` pinned to Node 24 with `cache: npm` and `cache-dependency-path: frontend/package-lock.json`, running `npm ci`, `npm run lint`, `npm run typecheck`, `npm run test:coverage`, `npm run build`. It runs **alongside** the existing Go job, not replacing it; the Go job's steps and order are untouched.
- Commit `frontend/package-lock.json`.

```bash
cd frontend && npx vitest run app/page.test.tsx && npm run lint && npx tsc --noEmit && \
  cd .. && git add frontend .gitignore Makefile .github/workflows/ci.yml && \
  git commit -m "feat: scaffold the Next.js frontend with a Vitest harness"
```

Expected: PASS, then one commit.

---

**Checkpoint 2: the API base URL resolves from the environment**

- [ ] **Step 1: Write the failing test, then run it**

`lib/config.test.ts`, table-driven over `resolveApiBaseUrl(env)`:

| `env.NEXT_PUBLIC_API_BASE_URL` | `env.NODE_ENV` | expected |
|---|---|---|
| unset | `"development"` | returns `"http://localhost:8080"` |
| `"http://api.test:9000"` | `"development"` | returns `"http://api.test:9000"` |
| `"http://api.test:9000/"` | `"production"` | returns `"http://api.test:9000"` (one trailing slash stripped, so callers concatenate paths without doubling it) |
| unset | `"production"` | **throws** an `Error` whose message names `NEXT_PUBLIC_API_BASE_URL` |
| `""` | `"production"` | **throws**, same as unset |

Run: `cd frontend && npx vitest run lib/config.test.ts`
Expected: FAIL — `lib/config.ts` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `resolveApiBaseUrl(env)` returns the trimmed value with any single
trailing `/` removed; falls back to `http://localhost:8080` when unset or
empty and `env.NODE_ENV !== "production"`; throws otherwise. `API_BASE_URL`
is `resolveApiBaseUrl(process.env)` evaluated once at module load. Add
`frontend/.env.example` documenting the variable — **never** a `.env` with a
real value, and no secret ever goes in a `NEXT_PUBLIC_*` name.

```bash
cd frontend && npx vitest run lib/config.test.ts && npx tsc --noEmit && \
  cd .. && git add frontend/lib/config.ts frontend/lib/config.test.ts frontend/.env.example && \
  git commit -m "feat: resolve the API base URL from the environment"
```

Expected: PASS, then one commit.

---

**Task 2 boundary — full frontend suite:**

```bash
cd frontend && npm run lint && npx tsc --noEmit && npx vitest run && npm run build
```

Expected: all PASS. `npm run build` proves the App Router compiles, which no
unit test covers.

---

## Task 3: Typed protocol and the REST client

**Delegated** (see the header). Everything this task needs is stated below;
it touches no file another task is editing.

The backend answers **every** REST route with one of two envelopes:
`{"data": ...}` on success (`backend/internal/httpapi/respond.go:18`) and
`{"error": {"code": ..., "message": ...}}` on failure (same file, line 81).
This task builds the single client that knows that, so no page ever parses a
response body itself.

**Files:**
- Create: `frontend/lib/protocol.ts`, `frontend/lib/api.ts`
- Test: `frontend/lib/api.test.ts`

**Interfaces:**
- Consumes: `API_BASE_URL` from `lib/config.ts` (Task 2)
- Produces, in `lib/protocol.ts` — types only, no logic, each mirroring a Go struct:
  ```ts
  export type AccountResponse   = { id: string; email: string; display_name: string; balance: number }
  export type AuthResponse      = { account: AccountResponse; token: string }
  export type CreateRoomResponse= { room_id: string; code: string; buy_in: number; token: string }
  export type JoinRoomResponse  = { room_id: string; guest: boolean; session_balance: number; partial_buy_in: boolean; token: string }
  export type RefillResponse    = { credited: number; balance: number; remaining: number; reset_at: string }
  export type ConnectedEvent    = { user_id: string; display_name: string; room_id: string; guest: boolean }
  export type PresenceEvent     = { user_id: string; display_name: string; player_count: number }
  export type SocketErrorEvent  = { code: string; message: string }
  export type Envelope          = { type: string; data?: unknown }
  ```
  Every numeric field above is an **integer token count or a count of
  players** — never a fraction. `lib/protocol.ts` declares no odds or wager
  types; those arrive in Phase 6b.
- Produces, in `lib/api.ts`:
  ```ts
  export class ApiError extends Error { code: string; status: number }
  export function apiPost<T>(path: string, body: unknown, token?: string): Promise<T>
  export function apiGet<T>(path: string, token?: string): Promise<T>
  ```

---

**Checkpoint 1: a success envelope is unwrapped to its data**

- [ ] **Step 1: Write the failing test, then run it**

`lib/api.test.ts`. Stub `global.fetch` with `vi.fn()` (restore it in
`afterEach`). Arrange a resolved `Response` with status 200 and body
`{"data":{"room_id":"r1","code":"ABC123","buy_in":1000,"token":"t"}}`, then
call `apiPost<CreateRoomResponse>("/api/v1/rooms", { buy_in: 1000 })`.

Assert the promise resolves to exactly the inner object (`room_id === "r1"`,
`code === "ABC123"`, `buy_in === 1000`) — **not** the wrapper — and that
`fetch` was called with URL `http://localhost:8080/api/v1/rooms`, method
`POST`, header `Content-Type: application/json`, and a body that
`JSON.parse`s to `{ buy_in: 1000 }`.

Run: `cd frontend && npx vitest run lib/api.test.ts`
Expected: FAIL — `lib/api.ts` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `apiPost` issues `fetch(API_BASE_URL + path, { method: "POST",
headers, body: JSON.stringify(body) })` and, on a 2xx response, parses the
JSON and resolves with its `data` property. `apiGet` is the same without a
body or `Content-Type`. Write `lib/protocol.ts` with exactly the types listed
in Interfaces above.

```bash
cd frontend && npx vitest run lib/api.test.ts && npx tsc --noEmit && \
  cd .. && git add frontend/lib/api.ts frontend/lib/api.test.ts frontend/lib/protocol.ts && \
  git commit -m "feat: unwrap the REST success envelope in a typed client"
```

Expected: PASS, then one commit.

---

**Checkpoint 2: an error envelope becomes a typed ApiError**

- [ ] **Step 1: Write the failing test, then run it**

Extend `lib/api.test.ts`:

- status 401, body `{"error":{"code":"invalid_credentials","message":"invalid email or password"}}`
  → the promise **rejects** with an `ApiError` where `code ===
  "invalid_credentials"`, `status === 401`, and `message === "invalid email or
  password"`.
- status 429, body `{"error":{"code":"rate_limited","message":"too many requests"}}`
  → rejects with `ApiError`, `code === "rate_limited"`, `status === 429`.
- status 500 with a body that is **not** JSON (e.g. the string `Bad Gateway`)
  → rejects with an `ApiError` where `status === 500` and `code ===
  "internal_error"` — a proxy or a crash can return non-JSON, and the client
  must not throw a raw `SyntaxError` at a page.
- `fetch` itself rejects (network down) → rejects with an `ApiError` where
  `code === "network_error"` and `status === 0`.

Every rejection must be an `ApiError`, never a bare `Error` — pages branch on
`.code`, and a login page distinguishing `invalid_credentials` from
`rate_limited` is the concrete consumer (Task 5).

Run: `cd frontend && npx vitest run lib/api.test.ts`
Expected: FAIL — the non-2xx cases currently resolve with `undefined` rather
than rejecting.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `class ApiError extends Error { code: string; status: number }`. On
a non-2xx response the client parses the body and rejects with
`ApiError(error.message, error.code, status)`; if the body will not parse as
JSON or carries no `error` object it rejects with code `internal_error` and
the response's status. A thrown `fetch` becomes code `network_error`, status
`0`.

```bash
cd frontend && npx vitest run lib/api.test.ts && npx tsc --noEmit && \
  cd .. && git add frontend/lib/api.ts frontend/lib/api.test.ts && \
  git commit -m "feat: reject failed REST calls as a typed ApiError"
```

Expected: PASS, then one commit.

---

**Checkpoint 3: a supplied token is sent as a bearer header**

- [ ] **Step 1: Write the failing test, then run it**

Extend `lib/api.test.ts`:

- `apiPost("/api/v1/rooms", { buy_in: 1000 }, "tok123")` → `fetch` received
  header `Authorization: Bearer tok123`.
- `apiPost("/api/v1/rooms", { buy_in: 1000 })` with no token → the `fetch`
  call carries **no** `Authorization` header at all (assert absence, not an
  empty string — `POST /api/v1/rooms/{code}/participants` is behind
  `OptionalAuth`, where an empty bearer would be a malformed credential
  rather than an absent one).
- `apiGet("/api/v1/whatever", "tok123")` → same bearer header, method `GET`,
  and no request body.

Run: `cd frontend && npx vitest run lib/api.test.ts`
Expected: FAIL — the token parameter is not yet read.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: when `token` is a non-empty string, both helpers add
`Authorization: Bearer <token>`; when it is `undefined` or empty they add no
such header.

```bash
cd frontend && npx vitest run lib/api.test.ts && npx tsc --noEmit && \
  cd .. && git add frontend/lib/api.ts frontend/lib/api.test.ts && \
  git commit -m "feat: attach a bearer token to authenticated REST calls"
```

Expected: PASS, then one commit.

---

**Task 3 boundary:**

```bash
cd frontend && npx vitest run && npm run lint && npx tsc --noEmit
```

Expected: PASS.

---

## Task 4: Session token storage

**Delegated** (see the header). Small, self-contained, and specified
completely here.

The backend issues **two different JWTs** and this task is where the
difference is made explicit and unmixable. An **account token** comes from
`POST /api/v1/auth/register` and `POST /api/v1/auth/login` and carries no
room claim. A **room token** comes from `POST /api/v1/rooms` and `POST
/api/v1/rooms/{code}/participants` and carries `room_id`. The socket accepts
only a room token — `internal/ws/handler.go:45` rejects a token with an empty
`RoomID` with 403.

Storage is `sessionStorage`, per this plan's Decisions §2 — read that section
before changing anything here.

**Files:**
- Create: `frontend/lib/session.ts`
- Test: `frontend/lib/session.test.ts`

**Interfaces:**
- Produces:
  ```ts
  export function setAccountToken(token: string): void
  export function getAccountToken(): string | null
  export function setRoomToken(token: string): void
  export function getRoomToken(): string | null
  export function clearRoomToken(): void
  export function clearSession(): void   // clears both

  export type RoomSummary = {
    room_id: string
    guest: boolean
    session_balance: number   // integer tokens
    partial_buy_in: boolean
  }
  export function setRoomSummary(s: RoomSummary): void
  export function getRoomSummary(): RoomSummary | null
  ```

---

**Checkpoint 1: a token round-trips through storage**

- [ ] **Step 1: Write the failing test, then run it**

`lib/session.test.ts` (jsdom provides a real `sessionStorage`; clear it in
`beforeEach`):

- `getAccountToken()` on empty storage → `null`.
- `setAccountToken("acc1")` then `getAccountToken()` → `"acc1"`.
- `setRoomToken("room1")` then `getRoomToken()` → `"room1"`.
- After both setters, `sessionStorage` holds **two distinct keys** — assert
  `getAccountToken() === "acc1"` and `getRoomToken() === "room1"`
  simultaneously, which fails if one key overwrites the other.
- `getAccountToken()` when `sessionStorage.getItem` **throws** (stub it to
  throw, as a browser with site data blocked does) → returns `null` rather
  than propagating. A storage read must never crash a page render.

Run: `cd frontend && npx vitest run lib/session.test.ts`
Expected: FAIL — `lib/session.ts` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: the two tokens live under distinct constant keys
(`callit.account_token`, `callit.room_token`). Every read is wrapped in
`try/catch` returning `null` on throw; every write is wrapped in `try/catch`
and swallows a quota/security error. Nothing in this module logs a token
value.

```bash
cd frontend && npx vitest run lib/session.test.ts && npx tsc --noEmit && \
  cd .. && git add frontend/lib/session.ts frontend/lib/session.test.ts && \
  git commit -m "feat: store account and room tokens under separate session keys"
```

Expected: PASS, then one commit.

---

**Checkpoint 2: the two tokens clear independently**

- [ ] **Step 1: Write the failing test, then run it**

Extend `lib/session.test.ts`:

- With both tokens set, `clearRoomToken()` → `getRoomToken() === null` **and**
  `getAccountToken() === "acc1"`. This is the leaving-a-room case: the player
  drops their room session but stays logged in.
- With both tokens set, `clearSession()` → both getters return `null`. This
  is logout.
- `clearRoomToken()` on already-empty storage → does not throw.

Run: `cd frontend && npx vitest run lib/session.test.ts`
Expected: FAIL — `clearRoomToken` and `clearSession` are undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `clearRoomToken()` removes only the room key; `clearSession()`
removes both. Both are `try/catch`-wrapped and no-op on empty storage.

```bash
cd frontend && npx vitest run lib/session.test.ts && npx tsc --noEmit && \
  cd .. && git add frontend/lib/session.ts frontend/lib/session.test.ts && \
  git commit -m "feat: clear the room token without ending the account session"
```

Expected: PASS, then one commit.

---

**Checkpoint 3: the join result survives the navigation into the room**

- [ ] **Step 1: Write the failing test, then run it**

The `connected` socket event carries `user_id`, `display_name`, `room_id`,
and `guest` — it does **not** carry a balance. The session balance and the
partial-buy-in flag exist only in the REST join/create response, one
navigation earlier, so they must be carried across it. That is this
checkpoint.

Extend `lib/session.test.ts`:

- `getRoomSummary()` on empty storage → `null`.
- `setRoomSummary({ room_id: "r1", guest: true, session_balance: 200,
  partial_buy_in: true })` then `getRoomSummary()` → a deep-equal object,
  with `session_balance` still the **number** `200` (not `"200"`).
- With a summary and both tokens set, `clearRoomToken()` also clears the
  summary — it describes the room session and must not outlive it — while
  `getAccountToken()` still returns `"acc1"`.
- `clearSession()` clears the summary too.
- `getRoomSummary()` when the stored value is corrupt (write the literal
  string `not json` under the key directly) → returns `null` rather than
  throwing. `JSON.parse` on user-reachable storage is a real crash path.

Run: `cd frontend && npx vitest run lib/session.test.ts`
Expected: FAIL — `setRoomSummary` and `getRoomSummary` are undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: the summary is JSON under a third constant key
(`callit.room_summary`), written by `setRoomSummary` and read by
`getRoomSummary`, which returns `null` on absent, unparseable, or
non-object values. `clearRoomToken()` and `clearSession()` both remove it.

```bash
cd frontend && npx vitest run lib/session.test.ts && npx tsc --noEmit && \
  cd .. && git add frontend/lib/session.ts frontend/lib/session.test.ts && \
  git commit -m "feat: carry the join result into the room across navigation"
```

Expected: PASS, then one commit.

---

**Task 4 boundary:**

```bash
cd frontend && npx vitest run && npx tsc --noEmit
```

Expected: PASS.

---

## Task 5: Register and login pages

**Test approach for every page task (5, 6, 8).** Stub `global.fetch` — the
real network boundary — rather than mocking `lib/api`, so these tests
exercise the envelope handling Task 3 built instead of asserting that our own
module was called. Mock only `next/navigation`'s `useRouter` (jsdom has no
Next router): `vi.mock("next/navigation", () => ({ useRouter: () => ({ push }) }))`
with `push = vi.fn()`. A `push` call is observable navigation, not an
implementation detail.

**Files:**
- Create: `frontend/app/login/page.tsx`, `frontend/app/register/page.tsx`, `frontend/components/ErrorBanner.tsx`
- Test: `frontend/app/login/page.test.tsx`, `frontend/app/register/page.test.tsx`

**Interfaces:**
- Consumes: `apiPost`, `ApiError` (Task 3); `setAccountToken` (Task 4); `AuthResponse` (Task 3)
- Produces: `components/ErrorBanner.tsx` → `export function ErrorBanner({ message }: { message: string | null }): JSX.Element | null` — renders `role="alert"` with the message, or nothing when `message` is `null`. Tasks 6 and 8 reuse it.

---

**Checkpoint 1: a successful login stores the account token and navigates**

- [ ] **Step 1: Write the failing test, then run it**

`app/login/page.test.tsx`. Stub `fetch` to resolve 200 with
`{"data":{"account":{"id":"u1","email":"a@b.test","display_name":"Ann","balance":5000},"token":"acc-tok"}}`.
Render the page, then with `userEvent`: type `a@b.test` into
`getByLabelText(/email/i)`, type a 12+ character password into
`getByLabelText(/password/i)`, click `getByRole("button", { name: /log in/i })`.

Assert: `fetch` was called with URL ending `/api/v1/auth/login` and a body
parsing to `{ email: "a@b.test", password: "<the typed password>" }`;
`getAccountToken()` returns `"acc-tok"`; and `push` was called with `"/host"`.

The password input must have `type="password"` and the email input
`type="email"` — assert both, since `getByLabelText` alone would pass on a
plain text field that shows the password on screen.

Run: `cd frontend && npx vitest run app/login/page.test.tsx`
Expected: FAIL — `app/login/page.tsx` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: a client component with a labelled email input (`type="email"`), a
labelled password input (`type="password"`), and a submit button reading
"Log in". On submit it calls
`apiPost<AuthResponse>("/api/v1/auth/login", { email, password })`, stores
`token` via `setAccountToken`, and navigates to `/host`. The button is
disabled while the request is in flight, so a double-click cannot fire two
logins.

```bash
cd frontend && npx vitest run app/login/page.test.tsx && npx tsc --noEmit && \
  cd .. && git add frontend/app/login/page.tsx frontend/app/login/page.test.tsx && \
  git commit -m "feat: log in and store the account token"
```

Expected: PASS, then one commit.

---

**Checkpoint 2: a failed login shows the error and does not navigate**

- [ ] **Step 1: Write the failing test, then run it**

Extend `app/login/page.test.tsx`:

- `fetch` resolves 401 with
  `{"error":{"code":"invalid_credentials","message":"invalid email or password"}}`
  → after submitting, `findByRole("alert")` contains text matching
  `/invalid email or password/i`; `push` was **not** called; and
  `getAccountToken()` is `null`.
- `fetch` resolves 429 with
  `{"error":{"code":"rate_limited","message":"too many requests"}}` → the
  alert matches `/too many/i` and `push` was not called.
- After a failed submit, the submit button is **enabled again** (a user must
  be able to retry).

The backend deliberately returns the identical response for an unknown email
and a wrong password (`CLAUDE.md`: account-enumeration oracle). This page must
not undo that — it renders the server's message verbatim and never adds a
client-side hint distinguishing the two.

Run: `cd frontend && npx vitest run app/login/page.test.tsx`
Expected: FAIL — the rejected `ApiError` is currently unhandled, so no alert
renders.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: the submit handler catches `ApiError`, puts `err.message` into
component state, renders it through `<ErrorBanner />` (`role="alert"`), leaves
`push` uncalled, stores no token, and re-enables the button. Create
`components/ErrorBanner.tsx` per the Interfaces block.

```bash
cd frontend && npx vitest run app/login/page.test.tsx && npx tsc --noEmit && \
  cd .. && git add frontend/app/login/page.tsx frontend/app/login/page.test.tsx frontend/components/ErrorBanner.tsx && \
  git commit -m "feat: surface login failures without leaking which field was wrong"
```

Expected: PASS, then one commit.

---

**Checkpoint 3: registration takes a display name and reports validation errors**

- [ ] **Step 1: Write the failing test, then run it**

`app/register/page.test.tsx`:

- Happy path: `fetch` resolves 201 with the same `AuthResponse` shape as
  Checkpoint 1. Fill email, password, and `getByLabelText(/display name/i)`,
  submit → body parses to `{ email, password, display_name }` (all three
  fields, `display_name` in snake_case to match the Go struct tag);
  `getAccountToken()` returns the token; `push` called with `"/host"`.
- Server validation failure: `fetch` resolves 400 with
  `{"error":{"code":"validation_error","message":"password must be at least 12 characters"}}`
  → `findByRole("alert")` matches `/at least 12 characters/i`, no token
  stored, `push` not called.

Password rules are the server's (`internal/auth`); this page does not
reimplement them, so a rule change never needs two edits.

Run: `cd frontend && npx vitest run app/register/page.test.tsx`
Expected: FAIL — `app/register/page.tsx` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: same shape as the login page plus a labelled display-name input,
posting to `/api/v1/auth/register` with `{ email, password, display_name }`.
Success stores the account token and navigates to `/host`; failure renders
`<ErrorBanner />`. Add a link from `/login` to `/register` and back.

```bash
cd frontend && npx vitest run app/register/page.test.tsx && npx tsc --noEmit && \
  cd .. && git add frontend/app/register/page.tsx frontend/app/register/page.test.tsx frontend/app/login/page.tsx && \
  git commit -m "feat: register an account from the browser"
```

Expected: PASS, then one commit.

---

**Task 5 boundary:**

```bash
cd frontend && npx vitest run && npm run lint && npx tsc --noEmit
```

Expected: PASS.

---

## Task 6: Create a room, and join one by code

**Files:**
- Create: `frontend/app/host/page.tsx`
- Modify: `frontend/app/page.tsx` (wire the landing form's submit)
- Test: `frontend/app/host/page.test.tsx`, `frontend/app/page.test.tsx`

**Interfaces:**
- Consumes: `apiPost`, `ApiError` (Task 3); `getAccountToken`, `setRoomToken`, `setRoomSummary` (Task 4); `CreateRoomResponse`, `JoinRoomResponse`, `RoomSummary` (Tasks 3–4); `ErrorBanner` (Task 5)
- Produces: no new module exports. After this task, `getRoomToken()` returns a room-scoped JWT on both paths — that is what Task 7 consumes.

---

**Checkpoint 1: a host creates a room and gets a shareable code**

- [ ] **Step 1: Write the failing test, then run it**

`app/host/page.test.tsx`. Seed `setAccountToken("acc-tok")`, stub `fetch` to
resolve 201 with
`{"data":{"room_id":"r1","code":"ABC123","buy_in":1000,"token":"room-tok"}}`.
Render, type `1000` into `getByLabelText(/buy-in/i)`, click
`getByRole("button", { name: /create room/i })`.

Assert: `fetch` called with URL ending `/api/v1/rooms`, header
`Authorization: Bearer acc-tok`, body parsing to `{ buy_in: 1000 }` — a
**number**, not the string `"1000"`, since `buy_in` is `*int64` on the Go
side and a string fails `DisallowUnknownFields` decoding; `getRoomToken()`
returns `"room-tok"`; the page shows the code `ABC123`; and a shareable link
ending `/room/ABC123` is present as `getByRole("link", { name: /ABC123/i })`
**or** as the value of a readonly textbox — assert whichever the
implementation renders, and pick one now: a readonly textbox, so the host can
select and copy it without a clipboard permission.

Also assert `push` was **not** called: the host stays on this page to read
the code out loud, and navigates to the room deliberately.

Run: `cd frontend && npx vitest run app/host/page.test.tsx`
Expected: FAIL — `app/host/page.tsx` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: a client component that reads the account token via
`getAccountToken()`; when it is `null` it renders a prompt linking to
`/login` and no form. Otherwise it renders a labelled numeric buy-in input
(defaulting to `1000`) and a "Create room" button. On success it calls
`setRoomToken(res.token)` **and** `setRoomSummary({ room_id: res.room_id,
guest: false, session_balance: res.buy_in, partial_buy_in: false })` — the
host holds a full buy-in and is never partial — then renders the code
prominently, renders the join URL in a readonly textbox, and renders a link
to `/room/<code>`.

```bash
cd frontend && npx vitest run app/host/page.test.tsx && npx tsc --noEmit && \
  cd .. && git add frontend/app/host/page.tsx frontend/app/host/page.test.tsx && \
  git commit -m "feat: create a room and surface its shareable code"
```

Expected: PASS, then one commit.

---

**Checkpoint 2: a guest joins by code with a display name**

- [ ] **Step 1: Write the failing test, then run it**

Extend `app/page.test.tsx` (the landing page from Task 2). Stub `fetch` to
resolve 200 with
`{"data":{"room_id":"r1","guest":true,"session_balance":1000,"partial_buy_in":false,"token":"room-tok"}}`.
Type `ABC123` into the room-code textbox, type `Ann` into
`getByLabelText(/display name/i)`, click the Join button.

Assert: `fetch` called with URL ending `/api/v1/rooms/ABC123/participants`,
body parsing to `{ display_name: "Ann" }`, and **no** `Authorization` header
(a guest has no account token); `getRoomToken()` returns `"room-tok"`;
`getRoomSummary()` deep-equals `{ room_id: "r1", guest: true,
session_balance: 1000, partial_buy_in: false }`; and `push` called with
`"/room/ABC123"`.

Add a second case: with `setAccountToken("acc-tok")` seeded beforehand, the
same flow sends `Authorization: Bearer acc-tok` — the join route is behind
`OptionalAuth`, and an account holder joining must be recognised as one so
they get their real balance rather than a guest session.

Run: `cd frontend && npx vitest run app/page.test.tsx`
Expected: FAIL — the landing form has no submit behavior (Task 2 left it
deliberately inert).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: the landing form gains a labelled display-name input and a submit
handler posting to `/api/v1/rooms/{code}/participants` with
`{ display_name }`, passing `getAccountToken() ?? undefined` as the token. On
success it calls `setRoomToken(res.token)` **and** `setRoomSummary({ room_id:
res.room_id, guest: res.guest, session_balance: res.session_balance,
partial_buy_in: res.partial_buy_in })`, then navigates to `/room/<the code
the user typed>`. The code is uppercased before use.

```bash
cd frontend && npx vitest run app/page.test.tsx && npx tsc --noEmit && \
  cd .. && git add frontend/app/page.tsx frontend/app/page.test.tsx && \
  git commit -m "feat: join a room by code as a guest or an account holder"
```

Expected: PASS, then one commit.

---

**Checkpoint 3: a bad code and a partial buy-in are both surfaced**

- [ ] **Step 1: Write the failing test, then run it**

Extend `app/page.test.tsx`:

- `fetch` resolves 404 with
  `{"error":{"code":"room_not_found","message":"room not found"}}` → after
  submit, `findByRole("alert")` matches `/room not found/i`, `push` was not
  called, and `getRoomToken()` is `null`.
- `fetch` resolves 200 with `partial_buy_in: true` and
  `session_balance: 200` → `push` **is** called with `/room/ABC123`,
  `setRoomToken` stored the token, and `getRoomSummary()` has
  `partial_buy_in: true` and `session_balance: 200`. The partial buy-in is not an error; spec
  §3 requires it be surfaced transparently, and Task 8's room page is where
  the "joined with 200/…" line renders. Assert here only that a partial join
  still succeeds and navigates — this checkpoint's job is that it is not
  mistaken for a failure.

Run: `cd frontend && npx vitest run app/page.test.tsx`
Expected: FAIL — the error path currently has no `ErrorBanner`, so no
`role="alert"` renders.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: the join handler catches `ApiError`, renders `err.message` through
`<ErrorBanner />`, stores no room token, and does not navigate. A successful
response navigates regardless of `partial_buy_in`.

```bash
cd frontend && npx vitest run app/page.test.tsx && npx tsc --noEmit && \
  cd .. && git add frontend/app/page.tsx frontend/app/page.test.tsx && \
  git commit -m "feat: report a failed join without blocking a partial buy-in"
```

Expected: PASS, then one commit.

---

**Task 6 boundary:**

```bash
cd frontend && npx vitest run && npm run lint && npx tsc --noEmit
```

Expected: PASS.

---

## Task 7: The typed WebSocket client

This is the 6a/6b seam. It builds the transport and a dispatch table, and
registers **no gameplay handlers** — Phase 6b adds `round_opened`,
`odds_updated`, `round_locked`, `round_resolved`, and `round_refunded` here
without reopening the transport.

**jsdom has no `WebSocket`.** Install a fake on `globalThis` in the test
file: a class recording its constructor URL, exposing `onopen`, `onmessage`,
`onclose`, `onerror` and a `close()` that records the call, plus test helpers
to fire a message or a close. Keep the most recently constructed instance in
a module-level array so a test can assert **how many** sockets were ever
constructed — that is what proves the no-reconnect rule in Checkpoint 3.
Restore the original `globalThis.WebSocket` in `afterEach`.

**Files:**
- Create: `frontend/lib/socket.ts`
- Test: `frontend/lib/socket.test.ts`

**Interfaces:**
- Consumes: `API_BASE_URL` (Task 2); `Envelope`, `ConnectedEvent`, `PresenceEvent`, `SocketErrorEvent` (Task 3)
- Produces:
  ```ts
  export type SocketStatus = "connecting" | "open" | "closed"
  export type Handler = (data: unknown) => void
  export interface RoomSocket {
    on(type: string, handler: Handler): () => void  // returns an unsubscribe
    onStatus(handler: (s: SocketStatus) => void): () => void
    send(type: string, data: unknown): void
    close(): void
  }
  export function openRoomSocket(token: string): RoomSocket
  export function toWebSocketUrl(baseUrl: string, token: string): string
  ```

---

**Checkpoint 1: the socket URL is derived from the API base and carries the token**

- [ ] **Step 1: Write the failing test, then run it**

`lib/socket.test.ts`, table-driven over `toWebSocketUrl(base, token)`:

| base | token | expected |
|---|---|---|
| `"http://localhost:8080"` | `"t1"` | `"ws://localhost:8080/api/v1/socket?token=t1"` |
| `"https://api.test"` | `"t1"` | `"wss://api.test/api/v1/socket?token=t1"` (https ⇒ wss, never a downgrade to ws) |
| `"http://localhost:8080"` | `"a b+c/d"` | the token appears percent-encoded, i.e. the URL contains `token=a%20b%2Bc%2Fd` |

Then: `openRoomSocket("t1")` constructs exactly **one** `WebSocket`, with the
URL `toWebSocketUrl(API_BASE_URL, "t1")`.

The query parameter is the only option: `internal/ws/handler.go:89` reads the
`Authorization` header first and the `token` query parameter second, and a
browser cannot set headers on a WebSocket handshake.

Run: `cd frontend && npx vitest run lib/socket.test.ts`
Expected: FAIL — `lib/socket.ts` does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `toWebSocketUrl` swaps the scheme (`http`→`ws`, `https`→`wss`),
appends `/api/v1/socket`, and adds `token` via `URLSearchParams` so it is
percent-encoded. `openRoomSocket(token)` constructs one `WebSocket` at that
URL and returns a `RoomSocket`.

```bash
cd frontend && npx vitest run lib/socket.test.ts && npx tsc --noEmit && \
  cd .. && git add frontend/lib/socket.ts frontend/lib/socket.test.ts && \
  git commit -m "feat: derive the room socket URL from the API base"
```

Expected: PASS, then one commit.

---

**Checkpoint 2: envelopes dispatch by type, and junk is ignored**

- [ ] **Step 1: Write the failing test, then run it**

Extend `lib/socket.test.ts`. Open a socket, register handlers, fire messages
through the fake:

- Fire `{"type":"connected","data":{"user_id":"u1","display_name":"Ann","room_id":"r1","guest":true}}`
  → a handler registered via `on("connected", h)` was called exactly once
  with the **inner** `data` object (`user_id === "u1"`), not the envelope.
- Fire a `player_joined` envelope → only the `player_joined` handler runs;
  the `connected` handler's call count is unchanged.
- Fire an envelope whose `type` has **no** registered handler → nothing
  throws and no handler runs. Unhandled types are normal here by design: 6b's
  message types will arrive at a 6a-era client during development, and an
  unknown type must never break the socket.
- Fire a message whose payload is **not valid JSON** (the literal `not json`)
  → nothing throws, no handler runs.
- Fire an envelope with **no** `type` field → nothing throws, no handler runs.
- Two handlers registered for the same type → both run.
- The function returned by `on(...)` unsubscribes: call it, fire that type
  again, and the handler's call count does not increase.

Run: `cd frontend && npx vitest run lib/socket.test.ts`
Expected: FAIL — `on` is undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `onmessage` parses the payload inside `try/catch`, discards
anything that is not an object with a string `type`, looks the type up in a
`Map<string, Set<Handler>>`, and calls each handler with `envelope.data`. A
handler that itself throws is caught and logged so one bad handler cannot
kill the dispatch loop. `on` returns a function that deletes that handler
from the set. `send(type, data)` writes `JSON.stringify({ type, data })` —
and **never** adds a `room_id` field, per this plan's Global Constraints.

```bash
cd frontend && npx vitest run lib/socket.test.ts && npx tsc --noEmit && \
  cd .. && git add frontend/lib/socket.ts frontend/lib/socket.test.ts && \
  git commit -m "feat: dispatch socket envelopes to typed handlers"
```

Expected: PASS, then one commit.

---

**Checkpoint 3: closing is final — the client never reconnects**

- [ ] **Step 1: Write the failing test, then run it**

Extend `lib/socket.test.ts`:

- Register `onStatus(h)`. On construction `h` saw `"connecting"`; after the
  fake fires `onopen`, `h` saw `"open"`; after it fires `onclose`, `h` saw
  `"closed"`.
- After an **unexpected** `onclose` (server-side drop, no `close()` call),
  the number of `WebSocket` instances ever constructed is still **1**, and it
  is still 1 after advancing timers by 30 seconds with `vi.useFakeTimers()`
  and `vi.advanceTimersByTime(30_000)`. This is the no-reconnect rule from
  Decisions §3: reconnecting would re-fire the backend's `EndSession` cycle
  and silently reset the player to the room buy-in, discarding their in-room
  result.
- After `close()`, firing a further `onmessage` runs **no** handler — a
  closed socket is inert.
- `close()` called twice does not throw.

Run: `cd frontend && npx vitest run lib/socket.test.ts`
Expected: FAIL — `onStatus` is undefined.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: the client tracks status through `connecting` → `open` → `closed`
and notifies `onStatus` subscribers on each transition. `close()` sets a
closed flag, calls the underlying `close()`, and makes further dispatch a
no-op. **There is no reconnect timer, no backoff, and no retry** — add a
comment saying so and pointing at spec §4's known limitation, so the absence
reads as a decision rather than an omission.

```bash
cd frontend && npx vitest run lib/socket.test.ts && npx tsc --noEmit && \
  cd .. && git add frontend/lib/socket.ts frontend/lib/socket.test.ts && \
  git commit -m "feat: report socket status and never auto-reconnect"
```

Expected: PASS, then one commit.

---

**Task 7 boundary:**

```bash
cd frontend && npx vitest run && npm run lint && npx tsc --noEmit
```

Expected: PASS.

---

## Task 8: The room page and its presence roster

The phase's payoff: a real browser in a real room, showing who is in it.

**Files:**
- Create: `frontend/app/room/[code]/page.tsx`, `frontend/components/PresenceRoster.tsx`
- Test: `frontend/app/room/[code]/page.test.tsx`, `frontend/components/PresenceRoster.test.tsx`

**Interfaces:**
- Consumes: `openRoomSocket`, `RoomSocket`, `SocketStatus` (Task 7); `getRoomToken`, `getRoomSummary`, `clearRoomToken` (Task 4); `ConnectedEvent`, `PresenceEvent` (Task 3); `ErrorBanner` (Task 5)
- Produces: `components/PresenceRoster.tsx` → `export function PresenceRoster({ players, selfId }: { players: { user_id: string; display_name: string }[]; selfId: string | null })` — a `<ul>` of display names, marking the entry whose `user_id === selfId` as "(you)". Phase 6b reuses it unchanged.

---

**Checkpoint 1: the room page shows own identity, balance, and connection state**

- [ ] **Step 1: Write the failing test, then run it**

`app/room/[code]/page.test.tsx`. Seed `setRoomToken("room-tok")` and
`setRoomSummary({ room_id: "r1", guest: true, session_balance: 200,
partial_buy_in: true })`, install the fake `WebSocket` from Task 7, render
the page with route param `code = "ABC123"`, then fire
`{"type":"connected","data":{"user_id":"u1","display_name":"Ann","room_id":"r1","guest":true}}`.

Assert:
- the fake `WebSocket` was constructed with a URL containing
  `token=room-tok` — the socket is opened with the **room** token, never the
  account token, and never with the room code from the URL;
- `findByText(/Ann/)` is present;
- the session balance `200` is rendered;
- because `partial_buy_in` is true, text matching `/200\s*\/\s*1000/` **or**
  an explicit partial-join notice is rendered — pick the notice: render
  `Joined with a partial buy-in: 200 tokens`, and assert exactly that,
  satisfying spec §3's "surfaced transparently in the UI";
- with `partial_buy_in: false`, that notice is **absent**
  (`queryByText(/partial buy-in/i)` is `null`).

Second case: render with **no** room token seeded → the page renders a
message directing the user back to `/` to join, and constructs **no**
`WebSocket`.

Run: `cd frontend && npx vitest run "app/room/[code]/page.test.tsx"`
Expected: FAIL — the page does not exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: a client component that reads `getRoomToken()` in an effect. When
it is `null` it renders a link back to `/` and opens no socket. Otherwise it
calls `openRoomSocket(token)` once, registers a `connected` handler storing
`{ user_id, display_name }`, renders the display name, renders
`getRoomSummary()?.session_balance`, renders the partial-buy-in notice when
`partial_buy_in` is true, and renders the socket status. The effect's cleanup
calls `socket.close()` on unmount — a `useEffect` with a `[]` dependency
array, so React does not open a second socket on re-render.

**The room code in the URL is not used to open the socket.** It exists for
the join call in Task 6 and for display. The room identity is in the token.

```bash
cd frontend && npx vitest run "app/room/[code]/page.test.tsx" && npx tsc --noEmit && \
  cd .. && git add "frontend/app/room/[code]/page.tsx" "frontend/app/room/[code]/page.test.tsx" && \
  git commit -m "feat: open the room socket and show own identity and balance"
```

Expected: PASS, then one commit.

---

**Checkpoint 2: the roster tracks joins and leaves**

- [ ] **Step 1: Write the failing test, then run it**

First `components/PresenceRoster.test.tsx` (pure, no socket): given
`players: [{user_id:"u1",display_name:"Ann"},{user_id:"u2",display_name:"Bo"}]`
and `selfId: "u1"` → a `getByRole("list")` containing two
`getAllByRole("listitem")`, with the Ann item's text also matching `/you/i`
and the Bo item's not.

Then extend `app/room/[code]/page.test.tsx`. After the `connected` event for
`u1`, fire in order:

- `{"type":"player_joined","data":{"user_id":"u2","display_name":"Bo","player_count":2}}`
  → the roster shows both `Ann` and `Bo`, and the text `2` appears as the
  player count.
- the **same** `player_joined` for `u2` again → still exactly two list items.
  The backend broadcasts presence to the whole room, and a duplicate must not
  double an entry.
- `{"type":"player_left","data":{"user_id":"u2","display_name":"Bo","player_count":1}}`
  → `queryByText("Bo")` is `null`, one list item remains, count shows `1`.
- `player_left` for a `user_id` never seen → no throw, roster unchanged.

**Anonymity check, per Global Constraints:** assert that after all of the
above, no rendered text matches `/\btokens?\b.*\b(staked|wagered)\b/i` and
that the component holds no per-user amount. 6a receives no wager data at
all, so this is a pin on the invariant surviving Phase 6b's edits to this
same file, not a check that today's code fails.

Run: `cd frontend && npx vitest run components/PresenceRoster.test.tsx "app/room/[code]/page.test.tsx"`
Expected: FAIL — `PresenceRoster` does not exist and the page renders no
roster.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `PresenceRoster` renders a `<ul>` per the Interfaces block. The
page keeps players in React state keyed by `user_id`, seeds it with self on
`connected`, adds on `player_joined` (replacing any existing entry with the
same `user_id` rather than appending), removes on `player_left`, and renders
`player_count` from the most recent presence event. It stores **only**
`user_id` and `display_name` per player — no amounts, ever.

```bash
cd frontend && npx vitest run && npx tsc --noEmit && \
  cd .. && git add frontend/components/PresenceRoster.tsx frontend/components/PresenceRoster.test.tsx "frontend/app/room/[code]/page.tsx" "frontend/app/room/[code]/page.test.tsx" && \
  git commit -m "feat: track room presence as players join and leave"
```

Expected: PASS, then one commit.

---

**Task 8 boundary — coverage gate:**

```bash
cd frontend && npm run lint && npx tsc --noEmit && npx vitest run --coverage
```

Expected: PASS, and coverage over `lib/**` and `components/**` at **80% or
above**. If it is short, close the gap with a test for a real untested
behavior — never by lowering the threshold or adding a test that asserts
nothing.

---

## Task 9: End-to-end acceptance, and phase close-out

This is the phase's evidence: two independent browser contexts, one room,
each seeing the other. Executed **inline** — it is the acceptance proof, the
counterpart of Phase 4b's `cmd/callit-cli`.

**Environment risk, read before starting.** Playwright needs a browser
binary, and **no sudo is available in this environment** (`CLAUDE.md`), so
`npx playwright install --with-deps` **will fail** — it invokes the system
package manager. Run `npx playwright install chromium` instead, which
downloads only the browser into the user-local cache. Chromium may still fail
to launch if a shared library is missing from the distro image.

**If, and only if, the browser cannot be made to launch after a genuine
attempt:** do not delete the spec and do not weaken any other task. Commit
`e2e/join.spec.ts` as written, mark the `frontend-e2e` CI job
`continue-on-error: true` with a comment naming this environment limitation,
and carry out the same scenario manually — two browser windows, one room —
recording the outcome in the journal entry below. Report it plainly as a
partial result at phase close rather than as a pass. Never report an
unexecuted E2E as passing.

**Files:**
- Create: `frontend/playwright.config.ts`, `frontend/e2e/join.spec.ts`
- Modify: `Makefile`, `.github/workflows/ci.yml`, `README.md`, `CLAUDE.md`, `docs/plans/2026-08-21-implementation-plan.md`
- Create: `journal/YYYY-MM-DD_HHMM_ansh_phase-6a-frontend-shell.md`

**Interfaces:**
- Consumes: every prior task, through the browser only. This task adds no module.
- Produces: Makefile target `fe-e2e`.

---

**Checkpoint 1: two browsers in one room see each other**

- [ ] **Step 1: Write the failing test, then run it**

Install Playwright and its browser:

```bash
cd frontend && npm install --save-dev @playwright/test && npx playwright install chromium
```

Write `playwright.config.ts` with `testDir: "./e2e"`, project `chromium`, and
**two** `webServer` entries so the run is self-contained:

1. the API — command
   `cd ../backend && JWT_SECRET=$(openssl rand -hex 32) CORS_ALLOWED_ORIGINS=http://localhost:3000 go run ./cmd/api`,
   `url: "http://localhost:8080/healthz"`, `reuseExistingServer: true`
2. the frontend — command `npm run build && npm run start`,
   `url: "http://localhost:3000"`, `reuseExistingServer: true`

Redis must already be up (`make up`); the API fails fast without it.

`e2e/join.spec.ts` — one test, two `browser.newContext()` calls so the two
tabs have **separate `sessionStorage`** (the same context would share it and
the second join would overwrite the first's room token, which is exactly the
bug this test must be able to catch):

1. Host context: go to `/register`, register a unique email
   (`host-${Date.now()}@e2e.test`), a 12+ character password, display name
   `HostAnn` → lands on `/host`.
2. Create a room with buy-in `1000` → read the room code from the page.
3. Host navigates to `/room/<code>`; expect the roster to show `HostAnn`.
4. Guest context: go to `/`, type the code, display name `GuestBo`, join →
   lands on `/room/<code>`.
5. **Assert in the guest context:** the roster shows both `HostAnn` and
   `GuestBo`, and the player count reads `2`.
6. **Assert in the host context:** the roster now shows `GuestBo` too — this
   is the real proof, because it can only be true if the host's socket
   received a `player_joined` broadcast pushed from the server.
7. Close the guest context; assert the host's roster drops back to `HostAnn`
   alone and the count reads `1`.

Run: `cd frontend && npx playwright test`
Expected: FAIL — `e2e/join.spec.ts` and `playwright.config.ts` do not exist,
so the run reports no tests found; once they exist, expect the run to fail
against whatever is not yet wired, and iterate to green.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

There is no new application code here — the checkpoint passes by making the
already-built pages work together against a live backend. Any fix needed is
a genuine integration defect (a wrong path, an unencoded token, a missing
CORS origin); fix it where it lives rather than special-casing the test. Add
the `fe-e2e` Makefile target (`cd frontend && npx playwright test`) and a
`frontend-e2e` CI job that starts `docker compose up -d redis` before running
it.

```bash
cd frontend && npx playwright test && \
  cd .. && git add frontend/playwright.config.ts frontend/e2e frontend/package.json frontend/package-lock.json Makefile .github/workflows/ci.yml && \
  git commit -m "test: prove two browsers share one room end to end"
```

Expected: PASS, then one commit.

---

**Checkpoint 2: the phase's documentation matches what was built**

- [ ] **Step 1: Write the failing check, then run it**

This checkpoint's "test" is the repository's own gates plus a specific
factual check, since documentation has no unit test:

```bash
cd backend && go vet ./... && gofmt -l . && go test ./... -race -cover -p 1 -count=1 && \
  cd ../frontend && npm run lint && npx tsc --noEmit && npx vitest run --coverage && npm run build && \
  cd .. && grep -n "CORS_ALLOWED_ORIGINS" README.md CLAUDE.md
```

Expected: the Go and frontend suites PASS, and the `grep` **FAILS** (no
match) — the new required-in-production environment variable is documented
nowhere, which is the gap this checkpoint closes.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Make these edits:

- **`README.md`** — how to run the frontend (`make fe-install`, `make
  fe-dev`), the `NEXT_PUBLIC_API_BASE_URL` and `CORS_ALLOWED_ORIGINS`
  variables, and the fact that the API now requires `CORS_ALLOWED_ORIGINS` in
  `ENV=production`.
- **`CLAUDE.md`** — add `frontend/` to Repository Layout with the `lib/`
  module roles; add `fe-install` / `fe-dev` / `fe-test` / `fe-lint` /
  `fe-build` / `fe-e2e` to Build & Test; record in Installed Tooling that
  **Phase 6a fixed the stack as TypeScript + App Router**, resolving the open
  question that section currently leaves against the `typescript` rule pack;
  and add a Critical Invariant: *the browser origin allowlist has exactly one
  definition (`config.Config.AllowedOrigins`), read by both `httpapi.CORS`
  and the WebSocket `CheckOrigin` — don't fork a second list*.
- **`docs/plans/2026-08-21-implementation-plan.md`** — mark the 6a row ✅ and
  append any amendment this phase made to the plan's own decisions.
- **`docs/project-history.md`** — record the frontend coverage figure and how
  it is measured, and the Playwright/no-sudo outcome (worked, or the
  contingency above).
- **Journal entry** via the `journal` skill, including whether
  `writing-plans`' delegation check (its first real use, per the
  2026-08-28 journal entry) felt natural or needs another pass, and the
  measured token cost of the two delegated tasks against the inline ones.

```bash
grep -q "CORS_ALLOWED_ORIGINS" README.md && grep -q "CORS_ALLOWED_ORIGINS" CLAUDE.md && \
  git add README.md CLAUDE.md docs/plans/2026-08-21-implementation-plan.md docs/project-history.md journal/ && \
  git commit -m "docs: record Phase 6a's stack decision, targets, and origin invariant"
```

Expected: PASS, then one commit.

---

**Checkpoint 3: security review of the new network surface**

- [ ] **Step 1: Run the review**

`CLAUDE.md` requires the `security-reviewer` agent before closing any phase
that touches auth, money movement, or a network surface. This phase opens a
new network surface and introduces browser-side token storage, so it
qualifies twice.

Run the `security-reviewer` agent over the phase diff
(`git diff dev...HEAD`), directing it explicitly at:

1. `httpapi.CORS` — that a disallowed origin receives no
   `Access-Control-Allow-Origin`, that `*` cannot be configured, that
   `Vary: Origin` is always set, and that `Allow-Credentials: true` is only
   ever paired with a specific echoed origin.
2. `ws.WithAllowedOrigins` — that a missing `Origin` header is a deliberate
   allowance for non-browser clients and not a bypass for a browser (a
   browser always sends `Origin` on a WebSocket handshake).
3. **`sessionStorage` token storage** — this plan's Decisions §2 states the
   XSS trade-off explicitly. Ask whether it holds, or whether an httpOnly
   cookie plus CSRF tokens should be pulled forward from Phase 7.
4. That the token never reaches a log, an error message, or a URL the app
   itself renders back to the page.

- [ ] **Step 2: Act on the findings, then commit**

Fix every CRITICAL and HIGH finding before the phase closes. Record MEDIUM
and LOW findings in `docs/project-history.md` alongside the three items
already open by design, each with an explicit accept-or-defer decision — a
finding that is neither fixed nor recorded is a finding that was lost.

```bash
cd backend && go test ./... -race -cover -p 1 -count=1 && \
  cd ../frontend && npx vitest run && \
  cd .. && git add backend/internal backend/cmd frontend/lib frontend/app frontend/components && \
  git commit -m "fix: address Phase 6a security review findings"
```

(If the review produced no code changes, skip this commit and record the
clean result in `docs/project-history.md` as part of Checkpoint 2's commit
instead.)

Expected: PASS.

---

**Task 9 boundary — the branch is green and verified:**

```bash
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin && \
cd backend && go vet ./... && gofmt -l . && go test ./... -race -cover -p 1 -count=1 && \
cd ../frontend && npm run lint && npx tsc --noEmit && npx vitest run --coverage && npm run build && npx playwright test
```

The plan ends here. Integration is `finishing-a-development-branch`'s
decision, invoked by `executing-plans` Step 3 — do not merge from within this
plan.

---

## Self-Review

Run against the spec and the parent plan after the plan was complete.

**1. Spec coverage.**

| Requirement | Where |
|---|---|
| §2 React 19 / Next.js / Tailwind / WebSockets | Task 2 scaffold; Task 7 socket |
| §2 monorepo `/backend`, `/frontend` | Task 2 creates `frontend/` beside `backend/` |
| §3 join by short code + shareable link | Task 6 CP1 (link), CP2 (join) |
| §3 guest: display name only, session balance | Task 6 CP2; Task 8 CP1 |
| §3 account holder: persistent identity | Task 5 CP1, CP3 |
| §3 partial buy-in "surfaced transparently in the UI" | Task 6 CP3; Task 8 CP1 renders the notice |
| §3 host-configurable buy-in at creation | Task 6 CP1 |
| §6 JWT presented when opening the WebSocket | Task 7 CP1; Task 8 CP1 asserts the room token is used |
| §6 display name from the token claim | Task 8 CP1 reads it from the `connected` event |
| §4 no reconnect-with-resume (known limitation) | Task 7 CP3 makes the absence a tested rule |
| §2 Web Audio API | **Phase 6b** — not in scope here, per the §9 split |
| §4 rounds, wagers, odds, settlement | **Phase 6b** — the 6a/6b seam is Task 7's dispatch table |

No 6a requirement is unassigned. The §2 Web Audio and §4 gameplay rows are
deliberately deferred and named as such in the parent plan's 6b row.

**2. Placeholder scan.** No `TBD`, no "add validation", no "similar to Task
N", no "handle the edge case". Every checkpoint states exact inputs and exact
expected outputs or errors. The one conditional instruction — Task 9's
Playwright contingency — states a concrete trigger, a concrete fallback, and
an explicit ban on reporting an unexecuted test as passing.

**3. Type consistency.** `RoomSummary` is defined once (Task 4) and consumed
by name in Tasks 6 and 8. `ApiError` (Task 3) is the only rejection type and
is caught by name in Tasks 5, 6, 8. `RoomSocket`, `SocketStatus`, `Handler`
(Task 7) are consumed unchanged in Task 8. `setRoomToken` / `getRoomToken` /
`clearRoomToken` / `setRoomSummary` / `getRoomSummary` keep one spelling
throughout. On the Go side, `ws.WithAllowedOrigins` and
`httpapi.Deps.AllowedOrigins` both name the same `config.Config.AllowedOrigins`
value. Every field name in `lib/protocol.ts` was copied from the Go struct
tags (`snake_case`), not from the Go field names.

**4. Delegation eligibility.** Tasks 3 and 4 are mechanical layers over
contracts fully stated in this plan — a REST envelope with three shapes, a
storage wrapper with three keys — with no cross-task continuity to lose, so
both are tagged in the header. Task 1 is mechanical too but is a
security-sensitive network surface, and Task 9 is the phase's acceptance
evidence; both stay inline, matching Phase 5b's precedent of keeping the
flagship correctness work out of the delegation experiment. Task 2 stays
inline because it shells out to `create-next-app` over the network in a WSL2
environment whose gotchas a cold subagent cannot troubleshoot from the plan.

---

## Notes for the executor

- **Task 1 must land before Task 9 can pass**, and before any manual
  browser check works at all. Tasks 2–8 can be developed against stubbed
  `fetch`/`WebSocket` without a running backend; only Task 9 needs the live
  stack.
- **Run `make up` before any Go test.** `internal/httpapi` and `internal/ws`
  use Redis DB 15 and fail rather than skip when it is unreachable.
- **`export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin`** in every
  non-interactive shell before a Go command.
- **Never `npm audit fix --force` or `npm update`.** If an advisory appears,
  record it and decide deliberately, the same way the Go dependency pins are
  handled.
