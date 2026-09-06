# Phase 3 — Auth + REST Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the authenticated REST surface — account registration and
login, JWT issuance, host room creation with a generated short code,
join-by-code for guests and account holders, manual refill claims, and the
shared sliding-window rate limiter that throttles all of it.

**Architecture:** Three layers, each testable on its own terms.
`internal/auth` is pure — argon2id hashing, credential validation, and JWT
issue/verify, with no I/O, so it unit-tests with nothing running.
`internal/redisstore` gains the account, rate-limiter, and top-up writers,
keeping its rule that it is the only package permitted to construct a Redis
key. `internal/account` and `internal/room` are thin orchestrators that wrap
the store and the auth primitives; `internal/httpapi` owns the wire format,
the error envelope, and the middleware chain. Every operation that must not
interleave — claiming a unique email, claiming a room code, admitting a
rate-limited request, topping a balance up to the refill target — is a
single Lua script, for the same reason the wager path is.

**Tech Stack:** Go 1.22.10 · `github.com/redis/go-redis/v9` v9.18.0 ·
`golang.org/x/crypto` **v0.33.0** (argon2id) · `github.com/golang-jwt/jwt/v5`
**v5.3.1** (HS256) · `github.com/google/uuid` **v1.6.0** · Redis 7.2 ·
`net/http` (Go 1.22 method-and-pattern routing, no third-party router).

**Spec:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md)
§3 (identity, guests, account holders, buy-in, refills), §6 (auth), §7
(latency targets). Parent plan:
[`docs/plans/2026-08-21-implementation-plan.md`](2026-08-21-implementation-plan.md)
§2 (auth mechanism), §3 (layout), §4 (key schema), §8 (economy constants),
§9 (phase table, Phase 3 note).

---

## Global Constraints

- **Go 1.22.10.** CI pins `go-version: "1.22"`. Do not raise it.
- **Every new dependency is pinned, and the pins are load-bearing:**
  - `golang.org/x/crypto` **v0.33.0** — the last release declaring `go 1.20`.
    v0.34.0 declares `go 1.23.0`, and `go get` will silently rewrite this
    module's `go` directive from `1.22.10` to `1.23.0` to accept it, which
    breaks CI. Verified 2026-08-25 by observing exactly that rewrite.
  - `github.com/golang-jwt/jwt/v5` **v5.3.1** — declares `go 1.21`. Safe.
  - `github.com/google/uuid` **v1.6.0** — declares no `go` directive at all
    and has no transitive dependencies. Safe.
  - The whole set was built and run together under Go 1.22.10 before this
    plan was written; `go mod tidy` left the `go` directive untouched.
- **All amounts are integer token units.** `domain.Tokens` everywhere. No
  float reaches Redis or a JSON response body except odds, which this phase
  does not emit.
- **`internal/domain` stays free of I/O**, and this phase adds nothing to it.
  Credential rules live in `internal/auth`, which is equally I/O-free but is
  about identity rather than the economy.
- **`internal/redisstore/keys.go` remains the only place a Redis key is
  constructed.** New keys get builders there; no other file concatenates.
- **Lockout and every rate-limit window are judged by `redis.call('TIME')`
  inside the script**, never a timestamp supplied by Go. One clock, shared
  by every API instance — the same rule that makes the wager lockout
  exploit-proof applies to the refill quota.
- **One sliding-window rate limiter, two call sites.** `rate_limit.lua` is
  the only sliding-window implementation in the codebase. The refill quota
  and the HTTP throttle both call it with different scopes, limits, and
  windows. Do not fork a second one for either.
- **Passwords are never logged, never echoed in a response, and never stored
  in plaintext.** The `password_hash` field never leaves `internal/redisstore`
  and `internal/account`.
- **No user enumeration on the login path.** An unknown email and a wrong
  password must produce byte-identical responses.
- **80% coverage minimum**, TDD RED→GREEN→IMPROVE, AAA test structure,
  table-driven where the cases are homogeneous.
- **`gofmt` clean.** CI fails on any output from `gofmt -l .`.

---

## Amendments to the parent plan

These change decisions recorded in
`docs/plans/2026-08-21-implementation-plan.md` and, for B1 and B5, in the
design spec. Task 12 folds them back into the committed docs — the same
close-out mechanism Phases 1 and 2 used.

### B1 — Persistent accounts live in Redis for this phase, not PostgreSQL

Spec §2 assigns "user accounts" to PostgreSQL. Parent plan §9 lists Phase 3's
dependencies as **0 and 2** — deliberately not 5 — and gates all migrations,
the `accounts`/`transactions`/`ledger_entries` schema, and the deferred
constraint trigger to Phase 5. Phase 3 therefore cannot store credentials in
PostgreSQL without pulling most of Phase 5 forward.

**Adopted:** accounts live in Redis for now, as `user:{userID}` (hash) plus
`email:{normalizedEmail}` (a unique secondary index). Phase 5's planning pass
decides whether they migrate to PostgreSQL alongside the ledger or stay in
Redis with the ledger holding only monetary history — recorded as an open
question there, not decided here.

Note the two senses of "account" do not collide: PostgreSQL's `accounts`
table in parent plan §6 holds **ledger** accounts (`user_wallet`,
`room_escrow`, `system_dust`), which are bookkeeping entities, not
credentials. Nothing in this amendment touches that table.

### B2 — Redis gains AOF persistence

Until this phase, everything in Redis was session state — rooms, rounds,
pools, wallets — all of it disposable if the container restarts. Persistent
account balances are the first data in Redis that must survive a restart, and
`redis:7.2-alpine` defaults to RDB snapshots alone, which can lose up to a
minute of writes.

**Adopted:** the compose service runs `redis-server --appendonly yes`. The
existing `redis-data` volume already mounts `/data`, so this is a one-line
change. It is not a substitute for the Phase 5 ledger — it is the floor under
B1 for as long as B1 holds.

### B3 — New package: `internal/account`

Parent plan §3's layout names `internal/auth` but no package that owns
account *lifecycle*. Folding registration, login, and refill claims into
`internal/auth` would put Redis calls inside the package whose value is being
I/O-free and unit-testable with nothing running.

**Adopted:** `internal/auth` keeps only pure primitives (argon2id,
credential validation, JWT issue/verify). `internal/account` orchestrates
them against `internal/redisstore`, exactly as `internal/room` orchestrates
room lifecycle. Layout §3 gains the package.

### B4 — `CreateRoom` and `JoinRoom` gain compare-and-set semantics

Phase 2 wrote both as unconditional writes (A4). Both have a defect that only
becomes reachable once Phase 3 puts a real caller in front of them:

- **`CreateRoom`** issues `SET code:{code} roomID` inside a `TxPipelined`.
  A generated code that collides with a live room **silently repoints that
  room's code at the new room** — every subsequent join-by-code lands the
  joiner in the wrong room. The generator cannot detect the collision to
  retry, because the write always reports success.
- **`JoinRoom`** issues `HSET room:{roomID}:wallets userID balance`
  unconditionally. A participant who rejoins — a page refresh, a dropped
  connection — has their session wallet **reset to the full buy-in**, wiping
  their losses. That is free money, reachable by anyone, with no exploit
  required beyond pressing reload.

**Adopted:** `CreateRoom` claims the code through `claim_unique.lua` and
returns `ErrAlreadyExists` on collision, which the short-code generator
retries. `JoinRoom` becomes `HSETNX` semantics and returns the *effective*
balance — the pre-existing one on a rejoin, the newly seeded one on a first
join — so its signature grows a return value.

### B5 — Display names travel in the JWT; no Redis key holds them

An earlier draft of this plan added `room:{roomID}:names`. It is not needed.
Spec §6 already requires the JWT to carry the participant's identity and be
"verified server-side without a per-message database hit" — so the display
name belongs in the signed token, where Phase 4's WebSocket handler reads it
from the verified claims at connection time. Guests, who have no `user:{id}`
hash at all, are covered by exactly the same mechanism.

The parent plan's §4 key schema is therefore unchanged in this respect: no
name key is added.

### B6 — Three new Lua scripts

Parent plan §5 names four scripts, all on the wager path. This phase adds
three, none of which touches wagers:

| Script | Why it must be atomic |
|---|---|
| `claim_unique.lua` | Claiming an email or a room code and creating the entity it points at must commit together, or a crash leaves a claimed index pointing at nothing. Two call sites (B4, B1). |
| `rate_limit.lua` | Check-then-record is the whole point of a limiter; split it and the limit is advisory. Two call sites (refill quota, HTTP throttle). |
| `top_up_balance.lua` | Read-balance-then-write-target across two round trips lets two concurrent claims both credit. Setting to the target under one lock makes the second a no-op. |

### B7 — The shared rate limiter lands here, as Phase 2's A5 deferred it

No change of decision — recording that A5's deferral is discharged by Task 6
of this plan. `ratelimit:{scope}:{id}` was already in parent plan §4; this is
its implementation, next to its first two callers.

---

## File Structure

```
backend/
├── go.mod                                   MODIFY — three new pinned deps
├── go.sum                                   MODIFY
├── scripts/lua/
│   ├── embed.go                             MODIFY — three new embeds
│   ├── claim_unique.lua                     CREATE
│   ├── rate_limit.lua                       CREATE
│   └── top_up_balance.lua                   CREATE
├── internal/config/config.go                MODIFY — JWTSecret, JWTTTL
├── internal/auth/                            (new package — pure, no I/O)
│   ├── errors.go                            CREATE
│   ├── password.go                          CREATE — argon2id, PHC encoding
│   ├── credentials.go                       CREATE — email/password/name rules
│   └── token.go                             CREATE — Claims, Issuer
├── internal/redisstore/
│   ├── keys.go                              MODIFY — UserKey, EmailKey, RateLimitKey
│   ├── errors.go                            MODIFY — ErrAlreadyExists
│   ├── room.go                              MODIFY — B4 compare-and-set
│   ├── user.go                              CREATE — CreateUser, User, UserByEmail, TopUpBalance
│   └── ratelimit.go                         CREATE — Allow, Revoke
├── internal/account/service.go              CREATE — Register, Login, ClaimRefill
├── internal/room/
│   ├── code.go                              CREATE — short-code generation
│   └── service.go                           CREATE — Create, Join
├── internal/httpapi/
│   ├── respond.go                           CREATE — envelope + error mapping
│   ├── middleware.go                        CREATE — RequireAuth, RateLimit
│   ├── auth_handlers.go                     CREATE
│   ├── room_handlers.go                     CREATE
│   ├── account_handlers.go                  CREATE
│   └── health.go                            MODIFY — NewMux takes dependencies
├── cmd/api/main.go                          MODIFY — construct and inject deps
└── docker-compose.yml (repo root)           MODIFY — B2 appendonly
```

**Why `internal/auth` splits into three files.** They have different test
characters: `password.go` is slow (argon2id is deliberately expensive),
`credentials.go` is a pure table-driven validator, `token.go` needs a clock
seam. Keeping them apart keeps each test file focused, and matches the
one-file-per-concern split `internal/redisstore` already uses.

**Constructors take concrete types, not interfaces.** `account.Service` holds
a `*redisstore.Store`; `httpapi` handlers hold `*account.Service`. This
codebase has no mock layer and its integration tests run against real Redis
on DB 15 — introducing interfaces purely to enable mocks would add
indirection that nothing consumes. Accept-interfaces is the right default
when there are two implementations; here there is one.

---

## Key schema additions

Appended to parent plan §4. Brace placeholders are substitutions, **not**
Redis Cluster hash tags, exactly as in §4.

| Key | Type | Contents |
|---|---|---|
| `user:{userID}` | HASH | `email`, `display_name`, `password_hash`, `balance`, `created_at` |
| `email:{normalizedEmail}` | STRING | → `userID` (unique index, claimed via `claim_unique.lua`) |
| `ratelimit:{scope}:{id}` | ZSET | sliding window — score is the hit's ms timestamp, member is a per-attempt UUID |

`balance` is the **persistent** account balance in integer token units, seeded
to `domain.StartingBalance` (1,000) at registration. It is distinct from a
session wallet in `room:{roomID}:wallets`, which this phase seeds from it but
never debits — spec §3 applies the session's net delta to the persistent
balance at session end, which is Phase 4's concern, not this phase's.

`ratelimit` scopes used in this phase:

| Scope | ID | Limit | Window |
|---|---|---|---|
| `auth` | client IP | 10 | 1 minute |
| `api` | user ID | 60 | 1 minute |
| `refill` | user ID | `domain.RefillQuota` (3) | 7 days |

---

## HTTP API surface

Versioned under `/api/v1` per the `api-design` skill's recommendation: start
at v1, don't version again until a breaking change forces it. `GET /healthz`
stays unversioned — it is an operational probe, not part of the API contract.

| Method | Path | Auth | Success | Purpose |
|---|---|---|---|---|
| POST | `/api/v1/auth/register` | none | 201 | Create an account, return a token |
| POST | `/api/v1/auth/login` | none | 200 | Exchange credentials for a token |
| POST | `/api/v1/rooms` | account token | 201 | Host creates a room, gets its code |
| POST | `/api/v1/rooms/{code}/participants` | optional | 201 | Join by code — guest or account holder |
| POST | `/api/v1/accounts/me/refills` | account token | 201 | Claim a manual refill |

Joining is modelled as creating a participant sub-resource rather than a
`POST /rooms/join` verb, per the skill's resource-naming rules. The room code
is the path identifier because it is what the joiner actually holds — they
were handed a code or a link, never a room UUID.

**Envelope.** Success bodies are `{"data": ...}`. Error bodies are
`{"error": {"code": "...", "message": "..."}}`. No `details` array: this
API has at most three input fields per endpoint, and a specific `code` per
failure carries the same information a field list would. Add `details` if and
when an endpoint grows enough fields to need it.

**Error codes.** The full mapping, which Task 8 implements as one table:

| Code | Status | Raised by |
|---|---|---|
| `validation_error` | 400 | malformed JSON, bad email/password/display name, bad buy-in, bad outcome count |
| `unauthorized` | 401 | missing, malformed, expired, or badly-signed token |
| `invalid_credentials` | 401 | login: unknown email **or** wrong password — identical either way |
| `not_found` | 404 | unknown room code |
| `email_taken` | 409 | register: email already claimed |
| `room_not_joinable` | 409 | room exists but is not `open` |
| `refill_not_eligible` | 409 | balance already at or above `domain.RefillTarget` |
| `refill_quota_exhausted` | 429 | 3 refills already claimed this window; carries `Retry-After` |
| `rate_limit_exceeded` | 429 | HTTP throttle; carries `Retry-After` |
| `internal_error` | 500 | anything unmapped — message is generic, never the wrapped error text |

**Rate-limit headers.** Every response passing through the throttle carries
`X-RateLimit-Limit`, `X-RateLimit-Remaining`, and `X-RateLimit-Reset` (Unix
seconds). 429s additionally carry `Retry-After` in seconds.

**Client IP** comes from `r.RemoteAddr` only. `X-Forwarded-For` is **not**
consulted: it is caller-supplied, so trusting it here would let any client
mint unlimited fresh rate-limit buckets by varying a header. When a real
proxy sits in front of this service (Phase 7 or deployment), the trusted-proxy
configuration to parse it correctly gets designed then.

---

## Lua conventions

Unchanged from Phase 2, restated so this plan stands alone:

- Every script documents its `KEYS`, `ARGV`, and possible replies in a header
  comment, in that order.
- Replies are always a **flat array of strings**, first element a status code.
  `tostring()` everything before returning — the Go side's `toStringSlice`
  treats a non-string element as a script bug, not a runtime condition.
- Scripts never call `TIME` indirectly through Go. Any script needing the
  current instant calls `redis.call('TIME')` itself.
- Scripts stay small and single-purpose; every branch gets an integration
  test (parent plan §10's mitigation for "Lua script complexity hiding
  subtle bugs").

## Test harness conventions

Unchanged from Phase 2:

- Integration tests talk to a **real Redis**. No fake, no `//go:build
  integration` tag — a tag is precisely how a suite rots unnoticed.
- Address from `REDIS_ADDR`, defaulting to `localhost:6379`.
- **Unreachable Redis is a test failure, never a skip.**
- **DB 15**, never DB 0. `FLUSHALL` forbidden; `FLUSHDB` on 15 permitted in
  `TestMain`.
- Every test namespaces its keys off `t.Name()` via the existing `testID`
  helper, so tests stay independent under `-race`.

New for this phase:

- `internal/httpapi`'s handler tests are **integration tests** against real
  Redis, driven through `httptest`. They get their own `TestMain` mirroring
  `redisstore`'s. This keeps one testing style in the repo rather than
  introducing a mock layer for one package.
- **argon2id is deliberately slow.** Tests that only need *a* hash must reuse
  one package-level fixture hash rather than calling `HashPassword` per case,
  or the suite's runtime balloons. Tests that specifically exercise hashing
  call it directly.

---

## Task 1: Dependencies, configuration, and Redis durability

Nothing in this phase compiles until the three dependencies are pinned, and
nothing runs until the process has a signing secret. This task ends with a
build that has the crypto it needs and a config loader that refuses to start
without a usable JWT secret.

**Files:**
- Modify: `backend/go.mod`, `backend/go.sum`
- Modify: `docker-compose.yml`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  ```go
  // package config
  type Config struct {
      Port      int
      Env       string
      LogLevel  string
      RedisAddr string
      RedisDB   int
      JWTSecret string        // REQUIRED — no default, min 32 bytes
      JWTTTL    time.Duration // default 2h, valid 1m..24h
  }
  ```

### Setup steps (one commit, no test cycle — this is scaffolding)

- [ ] **Step 1: Add the three dependencies at their pinned versions**

```bash
cd backend
go get golang.org/x/crypto@v0.33.0
go get github.com/golang-jwt/jwt/v5@v5.3.1
go get github.com/google/uuid@v1.6.0
go mod tidy
```

Then verify the `go` directive in `backend/go.mod` still reads `go 1.22.10`.
If it now reads `1.23.0` or higher, a dependency was resolved above its pin —
`go get` rewrites the directive silently rather than failing. Revert and
re-pin; see Global Constraints.

- [ ] **Step 2: Turn on AOF persistence for Redis (Amendment B2)**

In the root `docker-compose.yml`, give the `redis` service:

```yaml
    command: ["redis-server", "--appendonly", "yes"]
```

The `redis-data` volume already mounts `/data`, so no volume change is needed.

- [ ] **Step 3: Verify and commit**

Run: `make lint && make build` — expect both clean. Then
`docker compose up -d redis && docker exec call-it-redis-1 redis-cli CONFIG GET appendonly`
— expect `appendonly yes`.

```bash
git add backend/go.mod backend/go.sum docker-compose.yml
git commit -m "chore: pin argon2, JWT, and UUID dependencies and enable Redis AOF"
```

**Checkpoint 1: the process refuses to start without a usable JWT secret**

- [ ] **Step 1: Write a failing test**

Extend the existing table-driven test in `config_test.go`. Note that every
*existing* case must now also supply `JWT_SECRET`, since it becomes required —
add a valid 32-byte secret to the shared base environment those cases use.

New cases:
- `JWT_SECRET` absent entirely → error mentioning `JWT_SECRET`
- `JWT_SECRET=""` → error mentioning `JWT_SECRET`
- `JWT_SECRET` of 31 bytes → error naming the 32-byte minimum
- `JWT_SECRET` of exactly 32 bytes → accepted, `cfg.JWTSecret` equals it
- No `JWT_TTL` → `cfg.JWTTTL == 2 * time.Hour`
- `JWT_TTL=45m` → `cfg.JWTTTL == 45 * time.Minute`
- `JWT_TTL=30s` → error naming the valid range `1m-24h`
- `JWT_TTL=25h` → error naming the valid range `1m-24h`
- `JWT_TTL=notaduration` → error mentioning `JWT_TTL`

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/config/ -run TestLoad -v`
Expected: FAIL — compile error, `cfg.JWTSecret` and `cfg.JWTTTL` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: add `JWTSecret string` and `JWTTTL time.Duration` to `Config`.
`JWTSecret` has **no default** — absent or empty is an error, and so is any
value under 32 bytes. `JWTTTL` parses with `time.ParseDuration`, defaults to
`2 * time.Hour`, and must fall in `[1m, 24h]`. Follow the file's existing
shape exactly: `lookup`, validate, assign, with `config: KEY %q ...` error
text.

Why 32 bytes: HS256's HMAC key should be at least as long as the hash output
it produces, or the signature's effective strength drops below the algorithm's.
Why a 2-hour default TTL: the token outlives a watch party, which a 15-minute
default would not, and it is bounded so a leaked token expires the same day.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/config/ -cover`
Expected: PASS, coverage still 100%.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config/config.go backend/internal/config/config_test.go
git commit -m "feat: require a signing secret and token TTL in config"
```

---

## Task 2: argon2id password hashing

The first half of `internal/auth`. Pure, no I/O, and the only place in the
codebase that turns a plaintext password into something storable.

**Files:**
- Create: `backend/internal/auth/errors.go`
- Create: `backend/internal/auth/password.go`
- Create: `backend/internal/auth/password_test.go`

**Interfaces:**
- Consumes: `golang.org/x/crypto/argon2` (Task 1).
- Produces:
  ```go
  // package auth
  var ErrPasswordMismatch = errors.New("auth: password does not match")
  var ErrMalformedHash    = errors.New("auth: malformed password hash")

  func HashPassword(plain string) (string, error)
  func VerifyPassword(encoded, plain string) error
  ```

**Checkpoint 1: hashing produces a PHC string with a fresh random salt**

- [ ] **Step 1: Write a failing test**

Spec:
- `HashPassword("correct horse battery staple")` returns `(s, nil)` where `s`
  begins with the literal prefix `$argon2id$v=19$m=19456,t=2,p=1$` and, split
  on `$`, has exactly 6 parts (the leading empty one, `argon2id`, `v=19`,
  the parameter triple, the base64 salt, the base64 hash).
- The salt segment decodes with `base64.RawStdEncoding` to exactly 16 bytes;
  the hash segment to exactly 32 bytes.
- Two calls with the *same* input return **different** strings — the salt is
  random, not derived from the password.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/auth/ -run TestHashPassword -v`
Expected: FAIL — no non-test Go files in the package; `HashPassword` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: `HashPassword` draws a 16-byte salt from `crypto/rand`, derives a
32-byte key with `argon2.IDKey(plain, salt, t=2, m=19456, p=1, 32)`, and
encodes the result in PHC format:
`$argon2id$v=19$m=19456,t=2,p=1$<RawStdEncoding salt>$<RawStdEncoding hash>`.
Parameters are named constants in the file, not inline literals. A
`crypto/rand` read failure returns a wrapped error, never a zero salt.

These are the OWASP-recommended argon2id parameters for a 19 MiB memory
budget. They are encoded *into* the stored string, not just applied, so that
raising them later still verifies every hash written under the old ones.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/auth/ -run TestHashPassword -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/auth/errors.go backend/internal/auth/password.go backend/internal/auth/password_test.go
git commit -m "feat: hash passwords with argon2id in PHC encoding"
```

**Checkpoint 2: verification accepts the right password and rejects the wrong one**

- [ ] **Step 1: Write a failing test**

Spec, using one package-level fixture hash of `"correct horse battery staple"`
computed once in the test file (argon2id is deliberately slow — do not
re-hash per case):
- `VerifyPassword(fixture, "correct horse battery staple")` → `nil`
- `VerifyPassword(fixture, "Correct horse battery staple")` → error satisfying
  `errors.Is(err, ErrPasswordMismatch)`
- `VerifyPassword(fixture, "")` → `ErrPasswordMismatch`
- A hash produced by a *second* `HashPassword` call on the same password still
  verifies — i.e. verification uses the embedded salt, not a fixed one.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/auth/ -run TestVerifyPassword -v`
Expected: FAIL — `VerifyPassword` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: parse the encoded string's parameters, salt, and hash; re-derive
with `argon2.IDKey` using the parameters *read from the string*; compare with
`crypto/subtle.ConstantTimeCompare`, returning `ErrPasswordMismatch` on a
non-match.

**Deliberately out of scope for this checkpoint:** malformed input. For now,
any parse failure may return `ErrPasswordMismatch` — Checkpoint 3 is what
forces the distinction, and it must have a real failing test to do so.

Constant-time comparison is not separately checkpointed because it produces no
observable difference at this interface — a timing-attack test would be flaky
and prove little. It is an implementation requirement here and a line item on
Task 12's security review instead.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/auth/ -run TestVerifyPassword -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/auth/password.go backend/internal/auth/password_test.go
git commit -m "feat: verify passwords against their embedded argon2id parameters"
```

**Checkpoint 3: a malformed stored hash is distinguishable from a wrong password**

- [ ] **Step 1: Write a failing test**

Spec — each of these returns an error satisfying
`errors.Is(err, ErrMalformedHash)`, and specifically **not**
`errors.Is(err, ErrPasswordMismatch)`:
- `""`
- `"notahash"`
- `"$argon2id$v=19$m=19456,t=2,p=1$onlyfourparts"`
- `"$argon2i$v=19$m=19456,t=2,p=1$YWJjZGVmZ2hpamtsbW5vcA$YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY"` (wrong algorithm)
- `"$argon2id$v=16$m=19456,t=2,p=1$YWJjZGVmZ2hpamtsbW5vcA$YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY"` (wrong version)
- `"$argon2id$v=19$m=notanumber,t=2,p=1$YWJjZGVmZ2hpamtsbW5vcA$YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY"`
- `"$argon2id$v=19$m=19456,t=2,p=1$!!!notbase64!!!$YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY"`

None of them may panic.

Why this is a real RED and not a regression pin: Checkpoint 2's contract
explicitly permits returning `ErrPasswordMismatch` on a parse failure, so the
minimal implementation that satisfies it fails every case here.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/auth/ -run TestVerifyPassword_Malformed -v`
Expected: FAIL — `ErrMalformedHash` undefined, or returned error is
`ErrPasswordMismatch`.

- [ ] **Step 3: Implement to satisfy the test**

Contract: add `ErrMalformedHash`. Split the encoded string into exactly 6
`$`-separated parts and reject any other count; require part 1 to be exactly
`argon2id` and part 2 exactly `v=19`; parse `m=`, `t=`, `p=` with
`fmt.Sscanf` and reject a parse failure; decode both base64 segments and
reject a decode failure. Every rejection wraps `ErrMalformedHash` with enough
context to debug, and none of them re-derives a key.

The distinction matters operationally: a mismatch is a user typing the wrong
password, while a malformed hash is corrupted stored data. Collapsing them
would let a storage bug present as a login failure forever.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/auth/ -cover`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/auth/errors.go backend/internal/auth/password.go backend/internal/auth/password_test.go
git commit -m "feat: distinguish a malformed password hash from a mismatch"
```

---

## Task 3: Credential validation

The rules for what an acceptable email, password, and display name are. Pure
and table-driven; the endpoints in Tasks 9 and 10 call these before touching
Redis.

**Files:**
- Create: `backend/internal/auth/credentials.go`
- Create: `backend/internal/auth/credentials_test.go`
- Modify: `backend/internal/auth/errors.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  ```go
  // package auth
  var ErrInvalidEmail       = errors.New("auth: invalid email address")
  var ErrWeakPassword       = errors.New("auth: password does not meet requirements")
  var ErrInvalidDisplayName = errors.New("auth: invalid display name")

  const MinPasswordLen = 12
  const MaxPasswordLen = 128
  const MaxDisplayNameLen = 32

  func NormalizeEmail(raw string) string
  func ValidateEmail(normalized string) error
  func NormalizeDisplayName(raw string) string
  func ValidateDisplayName(normalized string) error
  func ValidatePassword(plain string) error
  ```

**Checkpoint 1: emails normalize to a single canonical form and validate**

- [ ] **Step 1: Write a failing test**

Spec — `NormalizeEmail` is `strings.ToLower(strings.TrimSpace(raw))`:
- `"  Alice@Example.COM  "` → `"alice@example.com"`
- `"alice@example.com"` → unchanged

`ValidateEmail`, applied to the **normalized** value, returns `nil` for:
- `"a@b.co"`, `"alice.smith+tag@example.co.uk"`

and an error satisfying `errors.Is(err, ErrInvalidEmail)` for:
- `""`, `"nope"`, `"a@"`, `"@b.c"`, `"a@b"` (no dot in domain),
  `"a b@c.d"` (space), `"a@@b.c"`, `"a@b..c"`, `"a@.b.c"`, `"a@b.c."`,
  a 255-character address, and a 65-character local part

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/auth/ -run TestEmail -v`
Expected: FAIL — `NormalizeEmail` and `ValidateEmail` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: `ValidateEmail` requires total length 3–254; exactly one `@`; a
local part of 1–64 characters containing no space; a domain containing at
least one `.`, with no leading dot, no trailing dot, no `..`, no space, and
every dot-separated label 1–63 characters.

Hand-rolled rather than `net/mail.ParseAddress`, which accepts display-name
forms like `Alice <a@b.c>` that must never become a lookup key. The
normalized form is what `email:{normalizedEmail}` is keyed on, so
normalization and validation must agree on exactly one canonical string per
address — otherwise the same person can register twice.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/auth/ -run TestEmail -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/auth/errors.go backend/internal/auth/credentials.go backend/internal/auth/credentials_test.go
git commit -m "feat: normalize and validate email addresses"
```

**Checkpoint 2: password length is bounded on both ends**

- [ ] **Step 1: Write a failing test**

Spec — `ValidatePassword` returns `nil` for a 12-character password and a
128-character one; returns an error satisfying `errors.Is(err,
ErrWeakPassword)` for `""`, an 11-character password, and a 129-character one.
The error message names the permitted range.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/auth/ -run TestValidatePassword -v`
Expected: FAIL — `ValidatePassword` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: reject any password whose **byte** length is below
`MinPasswordLen` (12) or above `MaxPasswordLen` (128).

Length only, no character-class rules: NIST SP 800-63B recommends length over
composition, since composition rules push users toward predictable
substitutions. The upper bound is not a security rule but a cost bound —
argon2id's work is proportional to input, so an unbounded password is a cheap
way to make the server do expensive work.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/auth/ -run TestValidatePassword -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/auth/credentials.go backend/internal/auth/credentials_test.go
git commit -m "feat: bound password length at both ends"
```

**Checkpoint 3: display names are trimmed, bounded, and free of control characters**

- [ ] **Step 1: Write a failing test**

Spec — `NormalizeDisplayName` is `strings.TrimSpace`:
- `"  Alice  "` → `"Alice"`

`ValidateDisplayName`, applied to the normalized value, returns `nil` for
`"Alice"`, `"J"`, a 32-rune name, and `"あかり"` (multi-byte runes count as
runes, not bytes); returns an error satisfying
`errors.Is(err, ErrInvalidDisplayName)` for `""`, a 33-rune name,
`"Alice\nBob"`, and `"Alice\x00"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/auth/ -run TestDisplayName -v`
Expected: FAIL — `NormalizeDisplayName` and `ValidateDisplayName` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: length measured in runes via `utf8.RuneCountInString`, permitted
range 1–`MaxDisplayNameLen` (32); reject if any rune satisfies
`unicode.IsControl`.

Runes not bytes, because the bound is what a person sees in a participant
list. Control characters are rejected because the display name is echoed into
WebSocket payloads in Phase 4 and rendered in Phase 6 — a newline in a name
is a formatting break at best and a log-injection vector at worst.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/auth/ -cover`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/auth/credentials.go backend/internal/auth/credentials_test.go
git commit -m "feat: validate display names by rune count and reject control characters"
```

---

## Task 4: JWT issue and verify

The token half of `internal/auth`. Spec §6 requires a short-lived signed JWT
carrying identity and room id, verifiable server-side with no database hit.

**Files:**
- Create: `backend/internal/auth/token.go`
- Create: `backend/internal/auth/token_test.go`
- Modify: `backend/internal/auth/errors.go`

**Interfaces:**
- Consumes: `github.com/golang-jwt/jwt/v5` (Task 1).
- Produces:
  ```go
  // package auth
  var ErrWeakSecret   = errors.New("auth: signing secret is too short")
  var ErrInvalidToken = errors.New("auth: token is invalid")
  var ErrTokenExpired = errors.New("auth: token has expired")

  const MinSecretLen = 32
  const Issuer_      = "callit" // token "iss" claim

  type Claims struct {
      UserID      string
      DisplayName string
      RoomID      string // empty on an account-scoped token
      Guest       bool
  }

  type Issuer struct{ /* unexported; has a `now func() time.Time` test seam */ }

  func NewIssuer(secret []byte, ttl time.Duration) (*Issuer, error)
  func (i *Issuer) Issue(c Claims) (string, error)
  func (i *Issuer) Verify(token string) (Claims, error)
  ```

**Checkpoint 1: a short signing secret is refused at construction**

- [ ] **Step 1: Write a failing test**

Spec:
- `NewIssuer(bytes.Repeat([]byte("a"), 31), time.Hour)` → `(nil, err)` with
  `errors.Is(err, ErrWeakSecret)`
- `NewIssuer(nil, time.Hour)` → `ErrWeakSecret`
- `NewIssuer(bytes.Repeat([]byte("a"), 32), time.Hour)` → non-nil issuer, nil error
- `NewIssuer(validSecret, 0)` → error mentioning the TTL
- `NewIssuer(validSecret, -time.Second)` → error mentioning the TTL

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/auth/ -run TestNewIssuer -v`
Expected: FAIL — `NewIssuer` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: reject a secret shorter than `MinSecretLen` (32) bytes and a
non-positive TTL. Store the secret, the TTL, and a `now` field defaulted to
`time.Now`. `config` already enforces the same 32-byte floor at startup; this
enforces it at the type that actually signs, so a future caller constructing
an `Issuer` from somewhere other than config cannot weaken it.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/auth/ -run TestNewIssuer -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/auth/errors.go backend/internal/auth/token.go backend/internal/auth/token_test.go
git commit -m "feat: refuse to construct a token issuer with a weak secret"
```

**Checkpoint 2: claims survive an issue-verify round trip**

- [ ] **Step 1: Write a failing test**

Spec — with a valid issuer, for each of these `Claims` values, `Issue` then
`Verify` returns a `Claims` deeply equal to the input:
- account token: `{UserID: "u1", DisplayName: "Alice", RoomID: "", Guest: false}`
- room token, account holder: `{UserID: "u1", DisplayName: "Alice", RoomID: "r1", Guest: false}`
- room token, guest: `{UserID: "g1", DisplayName: "Bob", RoomID: "r1", Guest: true}`

Additionally, the issued token has three `.`-separated segments, and its
decoded header (segment 1, `base64.RawURLEncoding`) contains `"alg":"HS256"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/auth/ -run TestIssueVerifyRoundTrip -v`
Expected: FAIL — `Issue` and `Verify` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: `Issue` builds `jwt.MapClaims` with `sub`=UserID, `name`=DisplayName,
`room_id`=RoomID, `guest`=Guest, `iss`=`Issuer_`, `iat`=`i.now()`,
`exp`=`i.now().Add(i.ttl)`, signs with `jwt.SigningMethodHS256`. `Verify`
parses, then reads the claims back into a `Claims`. Both directions must
agree on the `room_id` and `guest` claim names — they are what Phase 4's
WebSocket handshake will read.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/auth/ -run TestIssueVerifyRoundTrip -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/auth/token.go backend/internal/auth/token_test.go
git commit -m "feat: issue and verify HS256 tokens carrying room-scoped identity"
```

**Checkpoint 3: an expired token is rejected as expired**

- [ ] **Step 1: Write a failing test**

Spec — construct an issuer with a 1-hour TTL whose `now` seam returns a fixed
instant `T`. Issue a token. Move the seam to `T + 2h`. `Verify` returns an
error satisfying `errors.Is(err, ErrTokenExpired)`, and specifically **not**
`errors.Is(err, ErrInvalidToken)`.

A token verified at `T + 59m` still succeeds, so the test pins the boundary
rather than just "eventually fails".

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/auth/ -run TestVerifyExpired -v`
Expected: FAIL — no `now` seam to move, or the returned error is
`ErrInvalidToken` rather than `ErrTokenExpired`.

- [ ] **Step 3: Implement to satisfy the test**

Contract: expose an unexported `now func() time.Time` field on `Issuer`,
settable from within the package's tests, defaulting to `time.Now`. Pass
`jwt.WithTimeFunc(i.now)` to the parser so verification and issuance share one
clock. Map golang-jwt's `jwt.ErrTokenExpired` to this package's
`ErrTokenExpired`.

Expiry is distinguished from invalidity because the HTTP layer may eventually
want to tell a client "re-authenticate" rather than "your token is
suspicious"; collapsing them throws that away at the only layer that knows.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/auth/ -run TestVerifyExpired -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/auth/token.go backend/internal/auth/token_test.go
git commit -m "feat: reject expired tokens distinctly from invalid ones"
```

**Checkpoint 4: forged and algorithm-swapped tokens are rejected**

- [ ] **Step 1: Write a failing test**

Spec — every case returns an error satisfying
`errors.Is(err, ErrInvalidToken)`, and none returns usable claims:
- a token issued by a *different* issuer with a different 32-byte secret
- a well-formed token whose signature segment has one character altered
- an `alg: none` token, hand-built as
  `base64url({"alg":"none","typ":"JWT"}) + "." + base64url(validClaims) + "."`
  with the claims copied from a genuinely issued token
- a token whose header claims `"alg":"HS512"` but is signed with the same
  secret under HS512
- `""`, `"notatoken"`, `"a.b.c"`

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/auth/ -run TestVerifyForged -v`
Expected: FAIL — the `alg: none` and HS512 tokens verify successfully, because
Checkpoint 2's parser does not constrain the accepted algorithm.

- [ ] **Step 3: Implement to satisfy the test**

Contract: pass `jwt.WithValidMethods([]string{"HS256"})` to the parser, and
also assert `iss == Issuer_`. Map every remaining parse or validation failure
to `ErrInvalidToken`, wrapping the underlying cause for logs but never
surfacing it to a caller.

This is the checkpoint the whole task exists for. Algorithm confusion — a
verifier that honours the *attacker-supplied* `alg` header — is the classic
JWT vulnerability, and `alg: none` is its degenerate case. golang-jwt v5
requires methods to be declared explicitly rather than defaulting to
permissive, and this pins that we actually did.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/auth/ -cover`
Expected: PASS, `internal/auth` coverage ≥ 90%.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/auth/token.go backend/internal/auth/token_test.go
git commit -m "fix: pin token verification to HS256 and reject algorithm swaps"
```

---

## Task 5: Account storage and unique-index claiming

`claim_unique.lua` plus the account writers, and the `CreateRoom` fix it
enables (Amendment B4, first half).

**Files:**
- Create: `backend/scripts/lua/claim_unique.lua`
- Modify: `backend/scripts/lua/embed.go`
- Modify: `backend/internal/redisstore/keys.go`
- Modify: `backend/internal/redisstore/keys_test.go`
- Modify: `backend/internal/redisstore/errors.go`
- Create: `backend/internal/redisstore/user.go`
- Create: `backend/internal/redisstore/user_test.go`
- Modify: `backend/internal/redisstore/room.go`
- Modify: `backend/internal/redisstore/room_test.go`

**Interfaces:**
- Consumes: `domain.Tokens`, `domain.StartingBalance` (Phase 1).
- Produces:
  ```go
  // package redisstore
  var ErrAlreadyExists = errors.New("redisstore: unique index already claimed")

  func UserKey(userID string) string          // "user:" + userID
  func EmailKey(normalizedEmail string) string // "email:" + normalizedEmail
  func RateLimitKey(scope, id string) string   // "ratelimit:" + scope + ":" + id

  type User struct {
      ID           string
      Email        string
      DisplayName  string
      PasswordHash string
      Balance      domain.Tokens
      CreatedAt    time.Time
  }

  func (s *Store) CreateUser(ctx context.Context, u User) error
  func (s *Store) User(ctx context.Context, userID string) (User, error)
  func (s *Store) UserByEmail(ctx context.Context, normalizedEmail string) (User, error)

  // CreateRoom's signature is unchanged; its collision behaviour is not.
  func (s *Store) CreateRoom(ctx context.Context, roomID, code, hostID string, buyIn domain.Tokens) error
  ```

`RateLimitKey` is defined here, with the other key builders, even though its
first caller arrives in Task 6 — `keys.go` is the single definition site for
the schema, and splitting the table across two tasks would undercut that.

**Checkpoint 1: an account round-trips through Redis by ID and by email**

- [ ] **Step 1: Write a failing test**

Spec, against real Redis on DB 15:
- `CreateUser` with `{ID: <uuid>, Email: "alice@example.com", DisplayName:
  "Alice", PasswordHash: "$argon2id$...", Balance: domain.StartingBalance}`
  → `nil`
- `User(ctx, id)` returns every field equal to what was written, with
  `CreatedAt` non-zero and within 5 seconds of now
- `UserByEmail(ctx, "alice@example.com")` returns the same `User`
- `User(ctx, "no-such-user")` → error satisfying `errors.Is(err, ErrNotFound)`
- `UserByEmail(ctx, "nobody@example.com")` → `ErrNotFound`

Also extend `keys_test.go`'s table: `UserKey("u1")` → `user:u1`,
`EmailKey("a@b.c")` → `email:a@b.c`, `RateLimitKey("auth", "1.2.3.4")` →
`ratelimit:auth:1.2.3.4`.

Create and read-back land in one checkpoint deliberately: a writer with no
reader has nothing observable to assert against, so splitting them would leave
the first half unfalsifiable.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run 'TestCreateUser|TestKeys' -v`
Expected: FAIL — `CreateUser`, `User`, `UserByEmail`, and the three key
builders are undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract for `claim_unique.lua`:

```
-- KEYS[1] unique index key    (email:{email} | code:{code})
-- KEYS[2] entity hash key     (user:{userID} | room:{roomID})
-- ARGV[1] the id the index points at
-- ARGV[2..] field/value pairs written to the entity hash
-- reply: {'OK'} | {'TAKEN', existingID}
```

`SETNX KEYS[1] ARGV[1]`; on 0, return `{'TAKEN', GET KEYS[1]}` having mutated
nothing; on 1, `HSET KEYS[2]` with the remaining ARGV pairs and return
`{'OK'}`. One script serves both call sites because both are the same
operation — claim a unique secondary index and create the entity it points at,
atomically — and a second copy would be duplication, not clarity.

Go side: `CreateUser` calls it with `EmailKey(u.Email)`, `UserKey(u.ID)`, and
the hash fields, setting `created_at` to `time.Now().UnixMilli()`. `User` and
`UserByEmail` follow `room.go`'s existing read shape exactly — `HGetAll`,
empty means `ErrNotFound`, malformed numeric fields produce a wrapped parse
error naming the field. `UserByEmail` resolves the index with `GET` then
delegates to `User`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && make test-unit` (Redis must be up)
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/scripts/lua/claim_unique.lua backend/scripts/lua/embed.go backend/internal/redisstore/keys.go backend/internal/redisstore/keys_test.go backend/internal/redisstore/user.go backend/internal/redisstore/user_test.go
git commit -m "feat: store accounts in Redis behind a unique email index"
```

**Checkpoint 2: a duplicate email is refused without touching the first account**

- [ ] **Step 1: Write a failing test**

Spec:
- Create user A with email `dup@example.com` and display name `"First"`.
- `CreateUser` for user B — different ID, **same** email, display name
  `"Second"`, balance 42 → error satisfying `errors.Is(err, ErrAlreadyExists)`
- `UserByEmail(ctx, "dup@example.com")` still returns **A**: A's ID, display
  name `"First"`, balance `domain.StartingBalance`
- `User(ctx, bID)` → `ErrNotFound` — B's hash was never written

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run TestCreateUser_DuplicateEmail -v`
Expected: FAIL — `ErrAlreadyExists` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: add `ErrAlreadyExists` to `errors.go`. `CreateUser` maps the
script's `TAKEN` status onto it, wrapped with the email for context. The
script already guarantees the no-write half — this checkpoint is what proves
it, which is why it asserts on A's untouched fields and B's absence rather
than only on the returned error.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/redisstore/ -run TestCreateUser -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/redisstore/errors.go backend/internal/redisstore/user.go backend/internal/redisstore/user_test.go
git commit -m "feat: refuse a duplicate email without disturbing the existing account"
```

**Checkpoint 3: a colliding room code is refused instead of hijacking the room**

- [ ] **Step 1: Write a failing test**

Spec:
- `CreateRoom(roomA, "ABC123", hostA, 500)` → `nil`
- `CreateRoom(roomB, "ABC123", hostB, 900)` → error satisfying
  `errors.Is(err, ErrAlreadyExists)`
- `RoomByCode(ctx, "ABC123")` still returns **roomA**
- `Room(ctx, roomA)` still reports host `hostA` and buy-in 500
- `Room(ctx, roomB)` → `ErrNotFound`

Every existing test in `room_test.go` must still pass unchanged — this
changes collision behaviour, not the happy path.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run TestCreateRoom -v`
Expected: FAIL — the second `CreateRoom` returns `nil` and `RoomByCode` now
resolves `ABC123` to roomB, because the current implementation `SET`s the code
mapping unconditionally.

- [ ] **Step 3: Implement to satisfy the test**

Contract: replace `CreateRoom`'s `TxPipelined` with a `claim_unique.lua` call —
`RoomCodeKey(code)` as the index, `RoomKey(roomID)` as the entity, and the
existing `host_id` / `buy_in` / `status` / `created_at` fields as the pairs.
Buy-in validation via `domain.ValidateBuyIn` stays exactly where it is, before
the script runs. Map `TAKEN` to `ErrAlreadyExists`.

This is Amendment B4's first half. The old behaviour was not merely
unhelpful — it repointed a live room's code at a different room, sending every
subsequent joiner to the wrong place, and reported success while doing it.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && make test-unit`
Expected: PASS — the whole package, including Phase 2's suites.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/redisstore/room.go backend/internal/redisstore/room_test.go
git commit -m "fix: refuse a colliding room code instead of repointing the existing room"
```

---

## Task 6: The shared sliding-window rate limiter

Discharges Phase 2's Amendment A5. One implementation, called with different
scopes by the refill quota (Task 7) and the HTTP throttle (Task 8).

**Files:**
- Create: `backend/scripts/lua/rate_limit.lua`
- Modify: `backend/scripts/lua/embed.go`
- Create: `backend/internal/redisstore/ratelimit.go`
- Create: `backend/internal/redisstore/ratelimit_test.go`

**Interfaces:**
- Consumes: `RateLimitKey` (Task 5).
- Produces:
  ```go
  // package redisstore
  type Decision struct {
      Allowed    bool
      Remaining  int
      RetryAfter time.Duration // zero when Allowed
      Member     string        // the ZSET member recorded; "" when denied
      ResetAt    time.Time     // when the window's oldest hit ages out
  }

  func (s *Store) Allow(ctx context.Context, scope, id string, limit int, window time.Duration) (Decision, error)
  func (s *Store) Revoke(ctx context.Context, scope, id, member string) error
  ```

`Member` is part of `Decision` from the first checkpoint rather than being
added later, because `Revoke` is meaningless without it — a caller cannot
un-record a hit it cannot name.

**Checkpoint 1: requests under the limit are allowed and decrement the remainder**

- [ ] **Step 1: Write a failing test**

Spec — scope `"test"`, a per-test unique id, limit 3, window 1 minute:
- 1st `Allow` → `Allowed: true`, `Remaining: 2`, `RetryAfter: 0`, non-empty `Member`
- 2nd → `Allowed: true`, `Remaining: 1`
- 3rd → `Allowed: true`, `Remaining: 0`
- Each call's `Member` differs from the others
- `ResetAt` is within 1 minute of now and after now

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run TestAllow_UnderLimit -v`
Expected: FAIL — `Allow` and `Decision` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract for `rate_limit.lua`:

```
-- KEYS[1] ratelimit:{scope}:{id}
-- ARGV[1] window in milliseconds
-- ARGV[2] limit
-- ARGV[3] member id, unique per attempt
-- reply: {'ALLOWED', remaining, member, resetAtMs}
--      | {'DENIED', '0', '', retryAfterMs, resetAtMs}
```

Read `now` from `redis.call('TIME')` (seconds and microseconds → ms).
`ZREMRANGEBYSCORE KEYS[1] 0 (now - window)` to evict aged-out hits, then
`ZCARD`. If the count is below the limit: `ZADD` at score `now` with member
`ARGV[3]`, `PEXPIRE KEYS[1] ARGV[1]`, and return `ALLOWED` with
`limit - count - 1` remaining. `resetAtMs` is the oldest remaining score plus
the window, or `now + window` when the set is empty.

Go side: `Allow` builds the member as `uuid.NewString()`, converts the
millisecond fields to `time.Duration` / `time.Time`, and returns a `Decision`.

The clock is Redis's, not Go's — the same rule that makes wager lockout
immune to skew across API instances applies to a quota that must count the
same way from every instance. `PEXPIRE` means an idle bucket evicts itself
rather than accumulating forever.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/redisstore/ -run TestAllow -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/scripts/lua/rate_limit.lua backend/scripts/lua/embed.go backend/internal/redisstore/ratelimit.go backend/internal/redisstore/ratelimit_test.go
git commit -m "feat: add the shared sliding-window rate limiter"
```

**Checkpoint 2: the request over the limit is denied with a retry hint**

- [ ] **Step 1: Write a failing test**

Spec — limit 3, window 1 minute, after three allowed calls:
- 4th `Allow` → `Allowed: false`, `Remaining: 0`, `Member: ""`,
  `RetryAfter > 0` and `<= 1 minute`
- A 5th call is also denied — denial does not itself consume a slot, so the
  bucket does not drift
- `ZCARD` on `RateLimitKey("test", id)` equals 3, not 5

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run TestAllow_OverLimit -v`
Expected: FAIL — Checkpoint 1's script has no denial branch, so the 4th call
is allowed and `ZCARD` reaches 5.

- [ ] **Step 3: Implement to satisfy the test**

Contract: when `ZCARD` is at or above the limit, return `DENIED` **without**
`ZADD`ing. `retryAfterMs` is `oldestScore + window - now`, floored at 1 so a
caller never sees a zero-length wait on a denial.

Not recording denied attempts is deliberate: a limiter that counts rejections
would extend its own window under sustained load and lock out a client far
longer than the stated limit.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/redisstore/ -run TestAllow -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/scripts/lua/rate_limit.lua backend/internal/redisstore/ratelimit_test.go
git commit -m "feat: deny over-limit requests with a retry-after hint"
```

**Checkpoint 3: the window actually slides**

- [ ] **Step 1: Write a failing test**

Spec — limit 2, window **300ms**, one unique id:
- Two `Allow` calls → both allowed; third → denied
- `time.Sleep(400 * time.Millisecond)`
- A fourth `Allow` → `Allowed: true`, `Remaining: 1` — both earlier hits have
  aged out
- `ZCARD` on the key is 1, not 3 — the evicted members are gone, not just
  ignored

Use a short real window rather than a clock seam: the window is measured by
Redis's own clock inside the script, which Go cannot inject into. 400ms is the
price of testing the real mechanism.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run TestAllow_WindowSlides -v`
Expected: FAIL if `ZREMRANGEBYSCORE` is missing or its bound is wrong — the
fourth call is denied and `ZCARD` stays at 2.

- [ ] **Step 3: Implement to satisfy the test**

Contract: the `ZREMRANGEBYSCORE KEYS[1] 0 (now - window)` eviction runs on
**every** invocation, before the count, including denied ones. If Checkpoint 1
already implemented it correctly this checkpoint's test may pass immediately —
in that case do not manufacture a failure: note it in the commit message,
verify by temporarily removing the eviction line that the test does go red,
restore it, and commit the test as a pinned regression.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/redisstore/ -run TestAllow -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/scripts/lua/rate_limit.lua backend/internal/redisstore/ratelimit_test.go
git commit -m "test: pin that the rate-limit window evicts aged-out hits"
```

**Checkpoint 4: a recorded hit can be handed back**

- [ ] **Step 1: Write a failing test**

Spec — limit 3, window 1 minute:
- Three `Allow` calls; capture the third's `Member`
- `Revoke(ctx, "test", id, thirdMember)` → `nil`
- A fourth `Allow` → `Allowed: true`, `Remaining: 0` — the slot came back
- `Revoke` with a member that was never recorded → `nil`, and `ZCARD` is
  unchanged (revoking is idempotent, not an error)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run TestRevoke -v`
Expected: FAIL — `Revoke` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: `Revoke` is `ZREM RateLimitKey(scope, id) member`, returning nil
whether or not the member was present. No script needed — a single `ZREM` is
already atomic.

Task 7 is the caller: a refill claim that turns out to credit nothing must not
burn one of the account's three weekly slots. Without this, a user
double-clicking the refill button loses a third of their weekly quota to a
request that moved no tokens.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && make test-unit`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/redisstore/ratelimit.go backend/internal/redisstore/ratelimit_test.go
git commit -m "feat: allow a rate-limit slot to be handed back"
```

---

## Task 7: Refill claims

`top_up_balance.lua` plus the `internal/account` service that composes it with
the limiter and `domain.CanRefill`.

**Files:**
- Create: `backend/scripts/lua/top_up_balance.lua`
- Modify: `backend/scripts/lua/embed.go`
- Modify: `backend/internal/redisstore/user.go`
- Modify: `backend/internal/redisstore/user_test.go`
- Create: `backend/internal/account/service.go`
- Create: `backend/internal/account/service_test.go`
- Create: `backend/internal/account/testmain_test.go`

**Interfaces:**
- Consumes: `CreateUser`/`User` (Task 5), `Allow`/`Revoke` (Task 6),
  `domain.CanRefill`, `domain.RefillTarget`, `domain.RefillQuota` (Phase 1).
- Produces:
  ```go
  // package redisstore
  func (s *Store) TopUpBalance(ctx context.Context, userID string, target domain.Tokens) (credited domain.Tokens, newBalance domain.Tokens, err error)

  // package account
  const RefillScope  = "refill"
  const RefillWindow = 7 * 24 * time.Hour

  type Service struct{ /* unexported */ }

  func NewService(store *redisstore.Store, issuer *auth.Issuer) *Service

  type RefillResult struct {
      Credited  domain.Tokens
      Balance   domain.Tokens
      Remaining int       // refill claims left in the window
      ResetAt   time.Time
  }

  func (s *Service) ClaimRefill(ctx context.Context, userID string) (RefillResult, error)
  ```

**Checkpoint 1: topping up brings a balance exactly to the target**

- [ ] **Step 1: Write a failing test**

Spec, against real Redis:
- User with balance 300; `TopUpBalance(ctx, id, 1000)` → `credited == 700`,
  `newBalance == 1000`, and `User(ctx, id).Balance == 1000`
- User with balance 0 → `credited == 1000`, `newBalance == 1000`
- `TopUpBalance(ctx, "no-such-user", 1000)` → error satisfying
  `errors.Is(err, ErrNotFound)`

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run TestTopUpBalance -v`
Expected: FAIL — `TopUpBalance` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract for `top_up_balance.lua`:

```
-- KEYS[1] user:{userID}
-- ARGV[1] target balance
-- reply: {'OK', credited, newBalance} | {'NOT_FOUND'}
```

`HGET KEYS[1] balance`; a false/nil result returns `{'NOT_FOUND'}`. Otherwise,
if the balance is already at or above the target, return `{'OK', '0', balance}`
without writing. Else `HSET` the balance to the target and return the
difference as `credited`.

Setting to the target rather than incrementing by a Go-computed delta is what
makes a concurrent double-claim safe: the second call reads the already-topped
balance and credits nothing, instead of adding a second delta computed from a
stale read.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/redisstore/ -run TestTopUpBalance -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/scripts/lua/top_up_balance.lua backend/scripts/lua/embed.go backend/internal/redisstore/user.go backend/internal/redisstore/user_test.go
git commit -m "feat: top an account balance up to the refill target atomically"
```

**Checkpoint 2: a balance already at the target is left alone**

- [ ] **Step 1: Write a failing test**

Spec:
- User with balance exactly 1000; `TopUpBalance(ctx, id, 1000)` →
  `credited == 0`, `newBalance == 1000`
- User with balance 2500 (above target); `TopUpBalance(ctx, id, 1000)` →
  `credited == 0`, `newBalance == 2500` — the balance is **not** reduced to
  the target

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run TestTopUpBalance_AtOrAboveTarget -v`
Expected: FAIL if the script sets unconditionally — the 2500 case comes back
as 1000, silently burning 1,500 tokens.

If Checkpoint 1's implementation already guards this, do not manufacture a
failure: verify by temporarily removing the guard that the test goes red,
restore it, and commit the test as a pinned regression, noting so in the
commit message.

- [ ] **Step 3: Implement to satisfy the test**

Contract: the `balance >= target` branch returns `credited = 0` and the
**existing** balance, writing nothing. A refill is a floor, never a ceiling —
an account holder who is up on the week must not be levelled back down by
pressing a button meant to help them.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/redisstore/ -run TestTopUpBalance -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/scripts/lua/top_up_balance.lua backend/internal/redisstore/user_test.go
git commit -m "test: pin that a refill never reduces a balance above the target"
```

**Checkpoint 3: an eligible account claims a refill and consumes quota**

- [ ] **Step 1: Write a failing test**

Create `internal/account`'s `testmain_test.go` mirroring `redisstore`'s —
DB 15, fail-not-skip on unreachable Redis, `t.Name()`-derived IDs.

Spec:
- Register a user directly through the store with balance 250
- `ClaimRefill(ctx, id)` → `Credited: 750`, `Balance: 1000`, `Remaining: 2`,
  `ResetAt` within 7 days and in the future
- `store.User(ctx, id).Balance == 1000`

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/account/ -run TestClaimRefill_Eligible -v`
Expected: FAIL — package `account` does not exist; `NewService` and
`ClaimRefill` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract for `ClaimRefill`, in this order:

1. `store.User(ctx, userID)` — propagate `ErrNotFound`.
2. `domain.CanRefill(u.Balance, 0)` — the eligibility half only, passing 0 for
   the claim count so only the balance rule can reject here. A rejection
   returns `domain.ErrRefillNotEligible` **without touching the limiter**, so
   an ineligible request never costs quota.
3. `store.Allow(ctx, RefillScope, userID, domain.RefillQuota, RefillWindow)`.
   A denial returns `domain.ErrRefillQuotaExhausted`, wrapped so the caller can
   read `RetryAfter` (see Checkpoint 5's error type note).
4. `store.TopUpBalance(ctx, userID, domain.RefillTarget)`.
5. Return a `RefillResult` built from the decision and the credit.

The two-step order — check eligibility, then spend quota, then credit — is
what keeps a doomed request from consuming a slot. Step 5's residual race is
closed by Checkpoint 5.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/account/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/account/service.go backend/internal/account/service_test.go backend/internal/account/testmain_test.go
git commit -m "feat: claim a refill against the shared quota limiter"
```

**Checkpoint 4: the fourth claim in a window is refused**

- [ ] **Step 1: Write a failing test**

Spec:
- User with balance 100. Claim a refill (balance → 1000).
- Reset the balance to 100 directly through the store, then claim again.
  Repeat until three claims have succeeded.
- Reset the balance to 100 a fourth time; `ClaimRefill` → error satisfying
  `errors.Is(err, domain.ErrRefillQuotaExhausted)`
- `store.User(ctx, id).Balance == 100` — the refused claim credited nothing

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/account/ -run TestClaimRefill_QuotaExhausted -v`
Expected: FAIL if the limiter result is ignored — the fourth claim succeeds
and the balance reads 1000.

- [ ] **Step 3: Implement to satisfy the test**

Contract: a `Decision.Allowed == false` from step 3 returns
`domain.ErrRefillQuotaExhausted` immediately, before `TopUpBalance` runs.
`domain.RefillQuota` (3) and `RefillWindow` (7 days) come from the domain and
this package's constant respectively — never inline literals, so the policy
has one definition.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/account/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/account/service.go backend/internal/account/service_test.go
git commit -m "feat: enforce three refill claims per rolling seven-day window"
```

**Checkpoint 5: a refill that credits nothing hands its quota slot back**

- [ ] **Step 1: Write a failing test**

Spec — this exercises the narrow race that survives Checkpoint 3's ordering:
two concurrent claims can both pass the eligibility pre-check, both consume a
slot, and only one actually credit.

- User with balance 100.
- Launch two `ClaimRefill` calls concurrently with a `sync.WaitGroup`.
- Exactly one returns `Credited == 750` (or both succeed with one crediting
  900 and the other 0 — assert on the **sum**: the two calls' `Credited`
  values sum to 900, and the final balance is exactly 1000).
- Reset the balance to 100 and claim twice more, both succeeding. Then reset
  and claim once more → this **succeeds**, because the no-op claim returned
  its slot. Under a naive implementation it would be refused as the fourth.

Run this test with `-race`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/account/ -run TestClaimRefill_NoOpReturnsQuota -race -count=3 -v`
Expected: FAIL — the final claim is refused with
`domain.ErrRefillQuotaExhausted`, because the no-op claim consumed a slot it
never used.

- [ ] **Step 3: Implement to satisfy the test**

Contract: after `TopUpBalance` returns, if `credited == 0`, call
`store.Revoke(ctx, RefillScope, userID, decision.Member)` and return
`domain.ErrRefillNotEligible` — the balance was already at the target, so this
request was ineligible after all, it just could not be known until the atomic
step ran. A `Revoke` failure is logged, not returned: the user's claim
genuinely credited nothing, and failing the request would be a worse answer
than a slightly conservative quota.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/account/ -race -count=3 -cover`
Expected: PASS on all three runs.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/account/service.go backend/internal/account/service_test.go
git commit -m "fix: return the quota slot when a refill credits nothing"
```

---

## Task 8: HTTP envelope, error mapping, and middleware

The wire contract every handler in Tasks 9–11 is written against.

**Files:**
- Create: `backend/internal/httpapi/respond.go`
- Create: `backend/internal/httpapi/respond_test.go`
- Create: `backend/internal/httpapi/middleware.go`
- Create: `backend/internal/httpapi/middleware_test.go`
- Create: `backend/internal/httpapi/testmain_test.go`

**Interfaces:**
- Consumes: `auth.Issuer`/`auth.Claims` (Task 4), `redisstore.Store.Allow`
  (Task 6), the domain and store sentinels.
- Produces:
  ```go
  // package httpapi
  type APIError struct {
      Status  int
      Code    string
      Message string
  }
  func (e *APIError) Error() string

  func WriteData(w http.ResponseWriter, status int, v any)
  func WriteError(w http.ResponseWriter, err error)

  func RequireAuth(issuer *auth.Issuer) func(http.Handler) http.Handler
  func OptionalAuth(issuer *auth.Issuer) func(http.Handler) http.Handler
  func ClaimsFrom(ctx context.Context) (auth.Claims, bool)

  type LimitPolicy struct {
      Scope  string
      Limit  int
      Window time.Duration
      KeyFn  func(*http.Request) string
  }
  func RateLimit(store *redisstore.Store, p LimitPolicy) func(http.Handler) http.Handler

  func ClientIP(r *http.Request) string
  ```

**Checkpoint 1: success bodies are wrapped in a data envelope**

- [ ] **Step 1: Write a failing test**

Spec, via `httptest.NewRecorder`:
- `WriteData(rec, 201, map[string]any{"id": "abc"})` → status 201,
  `Content-Type: application/json`, body exactly
  `{"data":{"id":"abc"}}` after whitespace normalization
- `WriteData(rec, 200, nil)` → status 200, body `{"data":null}`

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestWriteData -v`
Expected: FAIL — `WriteData` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: set `Content-Type` **before** `WriteHeader`, write the status, then
encode `struct{ Data any `json:"data"` }{v}`. A failed encode is logged with
`slog.Error`, never re-written as a status — the header is already sent, the
same reasoning `health.go` already documents.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ -run TestWriteData -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/respond.go backend/internal/httpapi/respond_test.go
git commit -m "feat: wrap successful responses in a data envelope"
```

**Checkpoint 2: every known error maps to its documented code and status**

- [ ] **Step 1: Write a failing test**

Spec — table-driven over the HTTP API surface's error table. For each input
error, `WriteError` produces the listed status and a body whose
`error.code` matches:

| Input error | Status | `error.code` |
|---|---|---|
| `auth.ErrInvalidEmail` | 400 | `validation_error` |
| `auth.ErrWeakPassword` | 400 | `validation_error` |
| `auth.ErrInvalidDisplayName` | 400 | `validation_error` |
| `domain.ErrInvalidBuyIn` | 400 | `validation_error` |
| `auth.ErrInvalidToken` | 401 | `unauthorized` |
| `auth.ErrTokenExpired` | 401 | `unauthorized` |
| `account.ErrInvalidCredentials` | 401 | `invalid_credentials` |
| `redisstore.ErrNotFound` | 404 | `not_found` |
| `account.ErrEmailTaken` | 409 | `email_taken` |
| `room.ErrNotJoinable` | 409 | `room_not_joinable` |
| `domain.ErrRefillNotEligible` | 409 | `refill_not_eligible` |
| `domain.ErrRefillQuotaExhausted` | 429 | `refill_quota_exhausted` |

Each must also match when **wrapped**: `fmt.Errorf("context: %w", sentinel)`
maps identically, since every layer wraps on the way up.

Two new sentinels this checkpoint introduces in their own packages:
`account.ErrInvalidCredentials`, `account.ErrEmailTaken`, and
`room.ErrNotJoinable` (declared now, first raised in Tasks 9 and 10).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestWriteError_Mapping -v`
Expected: FAIL — `WriteError` and `APIError` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: an unexported `apiErrorFor(err error) *APIError` walks the table
with `errors.Is`, in the order listed. `WriteError` calls it, sets
`Content-Type`, writes the status, and encodes
`{"error":{"code":...,"message":...}}`. A `*APIError` passed in directly is
used as-is, so a handler can raise a one-off without extending the table.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ -run TestWriteError -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/respond.go backend/internal/httpapi/respond_test.go backend/internal/account/service.go backend/internal/room/service.go
git commit -m "feat: map domain and store errors onto the API error envelope"
```

**Checkpoint 3: an unmapped error becomes a 500 that leaks nothing**

- [ ] **Step 1: Write a failing test**

Spec:
- `WriteError(rec, errors.New("dial tcp 10.0.0.5:6379: connection refused"))`
  → status 500, `error.code == "internal_error"`
- The response body does **not** contain `"dial tcp"`, `"10.0.0.5"`, `"6379"`,
  or `"connection refused"` — assert with `strings.Contains` on the raw body
- `error.message` equals the fixed string `"an internal error occurred"`
- The same holds for a wrapped unknown error:
  `fmt.Errorf("redisstore: place wager: %w", errors.New("EOF"))`

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestWriteError_Unmapped -v`
Expected: FAIL — Checkpoint 2's fallthrough has no default branch, or its
default echoes `err.Error()` into the message.

- [ ] **Step 3: Implement to satisfy the test**

Contract: the default branch returns
`&APIError{500, "internal_error", "an internal error occurred"}`, and
`WriteError` logs the original error with `slog.Error` before responding.

The error text this suppresses is exactly the kind that names internal
hostnames, ports, and query shapes. It belongs in the server's logs, and
nowhere in a response body.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ -run TestWriteError -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/respond.go backend/internal/httpapi/respond_test.go
git commit -m "fix: return a generic 500 rather than leaking internal error text"
```

**Checkpoint 4: bearer tokens gate protected handlers**

- [ ] **Step 1: Write a failing test**

Spec — wrap a probe handler that writes `ClaimsFrom(r.Context())` as JSON:
- `Authorization: Bearer <valid account token>` → 200, and the probe sees
  `UserID` and `DisplayName` matching the issued claims
- No `Authorization` header → 401, `error.code == "unauthorized"`, and the
  probe handler **never runs** (assert with a flag the probe sets)
- `Authorization: Bearer garbage` → 401 `unauthorized`
- `Authorization: <valid token>` with no `Bearer ` prefix → 401
- `Authorization: Basic dXNlcjpwYXNz` → 401
- `Authorization: Bearer <expired token>` → 401
- `OptionalAuth` with no header → 200, and `ClaimsFrom` reports `ok == false`
- `OptionalAuth` with a garbage token → 401 (present-but-invalid is still an
  error; only *absent* is optional)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestRequireAuth -v`
Expected: FAIL — `RequireAuth`, `OptionalAuth`, and `ClaimsFrom` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: split the header on the exact prefix `"Bearer "`, verify with the
issuer, and store the resulting `auth.Claims` under an unexported context-key
type (never a bare string key). `ClaimsFrom` type-asserts it back.
`OptionalAuth` differs from `RequireAuth` only in that a **missing** header
proceeds without claims; a malformed or invalid one is rejected identically.

The distinction matters at the join endpoint, which serves both guests (no
token) and account holders (token) through one route. Letting a bad token
silently degrade to a guest join would mean an expired session quietly hands
someone a fresh guest wallet instead of telling them to log in again.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ -run 'TestRequireAuth|TestOptionalAuth' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/middleware.go backend/internal/httpapi/middleware_test.go backend/internal/httpapi/testmain_test.go
git commit -m "feat: gate handlers behind bearer-token authentication"
```

**Checkpoint 5: the throttle admits, annotates, and refuses**

- [ ] **Step 1: Write a failing test**

Spec — a policy of scope `"test"`, limit 2, window 1 minute, `KeyFn` returning
a per-test constant:
- 1st request → 200, `X-RateLimit-Limit: 2`, `X-RateLimit-Remaining: 1`,
  `X-RateLimit-Reset` parseable as a Unix second in the future
- 2nd → 200, `X-RateLimit-Remaining: 0`
- 3rd → 429, `error.code == "rate_limit_exceeded"`, `Retry-After` present and
  a positive integer, and the wrapped handler **never runs**
- Two different `KeyFn` values get independent buckets: a request under key
  `"other"` still returns 200 after key `"main"` is exhausted

Plus `ClientIP`:
- `r.RemoteAddr = "192.0.2.1:54321"` → `"192.0.2.1"`
- `r.RemoteAddr = "[2001:db8::1]:443"` → `"2001:db8::1"`
- `X-Forwarded-For: 1.2.3.4` with `RemoteAddr = "192.0.2.1:54321"` →
  `"192.0.2.1"` — the header is ignored

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run 'TestRateLimit|TestClientIP' -v`
Expected: FAIL — `RateLimit`, `LimitPolicy`, and `ClientIP` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: `RateLimit` calls `store.Allow` with the policy's scope, the
`KeyFn` result, limit, and window. On allow, set the three `X-RateLimit-*`
headers and call through. On deny, set them plus `Retry-After` (whole seconds,
rounded up, minimum 1) and write `&APIError{429, "rate_limit_exceeded", ...}`.
A store error is a 500 via `WriteError` — a limiter that cannot reach Redis
must **fail closed**, never wave traffic through.

`ClientIP` is `net.SplitHostPort(r.RemoteAddr)` with the raw value as a
fallback when it has no port. `X-Forwarded-For` is deliberately not consulted;
see the HTTP API surface section for why, and revisit when a real proxy exists.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ -cover`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/middleware.go backend/internal/httpapi/middleware_test.go
git commit -m "feat: throttle requests through the shared limiter with rate-limit headers"
```

---

## Task 9: Registration and login endpoints

**Files:**
- Modify: `backend/internal/account/service.go`
- Modify: `backend/internal/account/service_test.go`
- Create: `backend/internal/httpapi/auth_handlers.go`
- Create: `backend/internal/httpapi/auth_handlers_test.go`
- Modify: `backend/internal/httpapi/health.go` (mux gains dependencies)

**Interfaces:**
- Consumes: everything from Tasks 2–8.
- Produces:
  ```go
  // package account
  var ErrEmailTaken         = errors.New("account: email is already registered")
  var ErrInvalidCredentials = errors.New("account: email or password is incorrect")

  type Account struct {
      ID          string
      Email       string
      DisplayName string
      Balance     domain.Tokens
  }

  func (s *Service) Register(ctx context.Context, email, password, displayName string) (Account, string, error)
  func (s *Service) Login(ctx context.Context, email, password string) (Account, string, error)
  // second return value is a signed account-scoped token

  // package httpapi
  type Deps struct {
      Accounts *account.Service
      Rooms    *room.Service
      Store    *redisstore.Store
      Issuer   *auth.Issuer
  }
  func NewMux(d Deps) *http.ServeMux
  ```

**Checkpoint 1: registering creates a funded account and returns a token**

- [ ] **Step 1: Write a failing test**

Spec — `POST /api/v1/auth/register` with body
`{"email":"  Alice@Example.COM  ","password":"correct horse battery","display_name":"  Alice  "}`:
- status 201
- `data.account.id` is a non-empty UUID
- `data.account.email == "alice@example.com"` (normalized)
- `data.account.display_name == "Alice"` (trimmed)
- `data.account.balance == 1000` (`domain.StartingBalance`)
- `data.token` verifies with the test issuer to claims with `UserID` equal to
  `data.account.id`, `Guest: false`, `RoomID: ""`
- The raw response body contains neither `"password"` nor `"correct horse"`
  nor `"argon2"` — assert with `strings.Contains`
- `store.UserByEmail(ctx, "alice@example.com")` finds the account, and its
  `PasswordHash` verifies against the submitted password

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestRegister -v`
Expected: FAIL — the route does not exist; `NewMux` takes no dependencies.

- [ ] **Step 3: Implement to satisfy the test**

Contract: `NewMux` grows a `Deps` parameter and registers
`POST /api/v1/auth/register`. `account.Register` normalizes and validates all
three inputs via `internal/auth`, hashes the password, generates a UUIDv4 ID,
calls `store.CreateUser` with `domain.StartingBalance`, then issues an
account-scoped token (`RoomID: ""`, `Guest: false`). The handler decodes the
body with `json.Decoder` and `DisallowUnknownFields`, and never logs the
decoded struct.

`GET /healthz` keeps its existing registration and behaviour.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ ./internal/account/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/account/service.go backend/internal/account/service_test.go backend/internal/httpapi/auth_handlers.go backend/internal/httpapi/auth_handlers_test.go backend/internal/httpapi/health.go
git commit -m "feat: add the account registration endpoint"
```

**Checkpoint 2: a taken email and malformed input are refused distinctly**

- [ ] **Step 1: Write a failing test**

Spec:
- Register `dup@example.com`, then register it again with a different display
  name → 409, `error.code == "email_taken"`
- Registering `"  DUP@EXAMPLE.com "` (different case and spacing) also → 409 —
  normalization is what makes one address one account
- `{"email":"nope","password":"correct horse battery","display_name":"A"}` →
  400 `validation_error`
- `{"email":"a@b.co","password":"short","display_name":"A"}` → 400
- `{"email":"a@b.co","password":"correct horse battery","display_name":""}` → 400
- Body `not json at all` → 400 `validation_error`
- Body `{"email":"a@b.co","password":"correct horse battery","display_name":"A","admin":true}`
  → 400 (unknown field rejected)
- After every failing case, `store.UserByEmail` finds no new account

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestRegister_Rejections -v`
Expected: FAIL — `redisstore.ErrAlreadyExists` surfaces as an unmapped 500
rather than a 409 `email_taken`.

- [ ] **Step 3: Implement to satisfy the test**

Contract: `account.Register` translates `redisstore.ErrAlreadyExists` into
`account.ErrEmailTaken`. Validation runs **before** hashing, so a request that
cannot succeed never pays argon2id's cost — that ordering is also what stops
the register endpoint being a cheap CPU-exhaustion lever.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ -run TestRegister -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/account/service.go backend/internal/httpapi/auth_handlers.go backend/internal/httpapi/auth_handlers_test.go
git commit -m "feat: refuse duplicate and malformed registrations"
```

**Checkpoint 3: logging in returns a token for the right password**

- [ ] **Step 1: Write a failing test**

Spec — after registering `alice@example.com`:
- `POST /api/v1/auth/login` with the right email and password → 200,
  `data.token` verifies to claims with the registered account's `UserID`,
  `data.account.balance == 1000`
- `"  ALICE@example.COM "` with the right password → 200 (normalization
  applies on login too)
- The body contains no password material

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestLogin_Success -v`
Expected: FAIL — the route does not exist; `Login` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: `account.Login` normalizes the email, loads via
`store.UserByEmail`, verifies with `auth.VerifyPassword`, and issues an
account-scoped token. Register `POST /api/v1/auth/login` returning 200 — not
201, since logging in creates no resource.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ -run TestLogin -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/account/service.go backend/internal/httpapi/auth_handlers.go backend/internal/httpapi/auth_handlers_test.go
git commit -m "feat: add the login endpoint"
```

**Checkpoint 4: login does not reveal whether an email is registered**

- [ ] **Step 1: Write a failing test**

Spec — capture the full response for each and compare:
- Right email, wrong password → 401
- Unknown email, any password → 401
- The two responses have **identical** status, identical `error.code`
  (`invalid_credentials`), and identical `error.message`. Assert on the
  normalized raw bodies being byte-equal.
- A malformed-hash account (write a user whose `password_hash` is `"garbage"`
  directly through the store, then attempt login) → also 401
  `invalid_credentials`, **not** 500 — a corrupted record must not become an
  oracle either

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestLogin_NoEnumeration -v`
Expected: FAIL — the unknown-email path returns 404 `not_found` (from
`redisstore.ErrNotFound`) while the wrong-password path returns 401, so the
two are trivially distinguishable.

- [ ] **Step 3: Implement to satisfy the test**

Contract: `account.Login` collapses `redisstore.ErrNotFound`,
`auth.ErrPasswordMismatch`, and `auth.ErrMalformedHash` into the single
`ErrInvalidCredentials`, logging the real cause server-side at debug level.

An endpoint that answers "no such user" differently from "wrong password" is a
free account-enumeration oracle: an attacker learns which addresses are
registered without ever guessing a password. The malformed-hash case is
included because it is the same leak by another route.

Timing remains a residual signal — the unknown-email path skips argon2id and
so returns faster. Closing that means verifying against a dummy hash on the
miss path, which is worth doing but is a distinct change; it is recorded as an
open question for Task 12's security review rather than folded in here.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ ./internal/account/ -cover`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/account/service.go backend/internal/httpapi/auth_handlers_test.go
git commit -m "fix: give identical responses for unknown email and wrong password"
```

---

## Task 10: Rooms — short codes, creation, and joining

**Files:**
- Create: `backend/internal/room/code.go`
- Create: `backend/internal/room/code_test.go`
- Create: `backend/internal/room/service.go`
- Create: `backend/internal/room/service_test.go`
- Create: `backend/internal/room/testmain_test.go`
- Modify: `backend/internal/redisstore/room.go` (B4, second half)
- Modify: `backend/internal/redisstore/room_test.go`
- Create: `backend/internal/httpapi/room_handlers.go`
- Create: `backend/internal/httpapi/room_handlers_test.go`

**Interfaces:**
- Consumes: `CreateRoom`, `RoomByCode`, `Room`, `JoinRoom` (Tasks 5 and here),
  `domain.GuestSessionBalance`, `domain.AccountSessionBalance`,
  `domain.IsPartialBuyIn`, `domain.ValidateBuyIn` (Phase 1), `auth.Issuer`.
- Produces:
  ```go
  // package redisstore — signature change (Amendment B4)
  func (s *Store) JoinRoom(ctx context.Context, roomID, userID string, balance domain.Tokens) (effective domain.Tokens, err error)

  // package room
  const CodeLen = 6
  const CodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

  var ErrNotJoinable    = errors.New("room: room is not open for joining")
  var ErrCodeExhausted  = errors.New("room: could not generate an unused room code")

  func GenerateCode(r io.Reader) (string, error)

  type Service struct{ /* unexported */ }
  func NewService(store *redisstore.Store, issuer *auth.Issuer) *Service

  type Created struct {
      RoomID string
      Code   string
      BuyIn  domain.Tokens
      Token  string // host's room-scoped token
  }
  func (s *Service) Create(ctx context.Context, hostID, hostName string, buyIn domain.Tokens) (Created, error)

  type Joined struct {
      RoomID         string
      Code           string
      BuyIn          domain.Tokens
      SessionBalance domain.Tokens
      PartialBuyIn   bool
      Guest          bool
      Token          string
  }
  func (s *Service) Join(ctx context.Context, code string, c JoinRequest) (Joined, error)

  type JoinRequest struct {
      UserID      string // empty for a guest — the service generates one
      DisplayName string
      Guest       bool
      AccountBalance domain.Tokens // ignored when Guest
  }
  ```

**Checkpoint 1: generated codes are the right shape and unambiguous**

- [ ] **Step 1: Write a failing test**

Spec:
- `GenerateCode(crypto/rand.Reader)` returns a 6-character string, nil error
- Every character is in `CodeAlphabet`
- The alphabet contains none of `0`, `O`, `1`, `I`, `L`, is 31 characters
  long, and has no repeated character
- 1,000 successive calls produce at least 990 distinct codes
- `GenerateCode(bytes.NewReader(nil))` → error (a short read is reported, not
  silently padded)
- `GenerateCode` fed a reader of constant `0x00` bytes returns
  `strings.Repeat(string(CodeAlphabet[0]), 6)` — the mapping is deterministic
  in its input, which is what makes Checkpoint 3's collision test possible

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/room/ -run TestGenerateCode -v`
Expected: FAIL — package `room` does not exist.

- [ ] **Step 3: Implement to satisfy the test**

Contract: read `CodeLen` bytes from `r` with `io.ReadFull`, propagating a
short read as an error. Map each byte to `CodeAlphabet[b % 31]`.

The alphabet omits `0`/`O` and `1`/`I`/`L`: codes get read aloud across a room
and typed from a glance at someone's screen, so visually confusable characters
turn into failed joins. Digits `2`–`9` and the remaining 23 letters leave 31
characters.

The modulo introduces a slight bias toward the first `256 % 31 = 8` characters.
That is acceptable here and deliberately not corrected: the code is a
short-lived lookup handle for a room whose participants were invited out of
band, not a secret. Room *authorization* rests on the JWT, not on code
unguessability.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/room/ -run TestGenerateCode -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/room/code.go backend/internal/room/code_test.go
git commit -m "feat: generate unambiguous six-character room codes"
```

**Checkpoint 2: creating a room seeds the host's wallet and resolves by code**

- [ ] **Step 1: Write a failing test**

Create `internal/room`'s `testmain_test.go` mirroring `redisstore`'s.

Spec:
- `Create(ctx, hostID, "Host", 500)` → `Created` with a 6-character `Code`,
  a UUID `RoomID`, `BuyIn: 500`, and a `Token` that verifies to claims with
  `RoomID` equal to `Created.RoomID` and `Guest: false`
- `store.RoomByCode(ctx, created.Code)` returns `created.RoomID`
- `store.Room(ctx, created.RoomID)` reports host `hostID`, buy-in 500,
  status `"open"`
- `store.Balance(ctx, created.RoomID, hostID)` returns 500 — the host has a
  wallet
- `store.PlayerCount(ctx, created.RoomID)` returns **0** — the host is
  excluded from the denominator
- `Create` with buy-in 50 → error satisfying
  `errors.Is(err, domain.ErrInvalidBuyIn)`; with 20,000 → same

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/room/ -run TestCreate -v`
Expected: FAIL — `NewService` and `Create` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: generate a UUIDv4 room ID and a code, call `store.CreateRoom`, then
`store.JoinRoom` for the host at the room's buy-in, then issue a room-scoped
token.

Seeding the host's wallet is not cosmetic. `redisstore.PlayerCount` is
`HLEN room:{roomID}:wallets - 1`, written in Phase 2 on the assumption that
the host holds a wallet. If room creation skipped it, the "N/M players have
wagered" denominator that spec §4 requires would be short by one for the
room's entire life. The host still cannot wager — that guard lives in
`place_wager.lua` and is keyed on `host_id`, not on wallet absence.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/room/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/room/service.go backend/internal/room/service_test.go backend/internal/room/testmain_test.go
git commit -m "feat: create rooms with a generated code and a seeded host wallet"
```

**Checkpoint 3: a code collision is retried rather than surfaced**

- [ ] **Step 1: Write a failing test**

Spec — `NewService` gains an unexported `rand io.Reader` field defaulting to
`crypto/rand.Reader`, settable from within the package's tests:
- Give the service a reader yielding 6 bytes of `0x00`, then 6 bytes of
  `0x01`. `Create` twice.
- Both calls succeed, and the two rooms have **different** codes — the first
  claims the all-`0x00` code, the second collides, retries, and takes the
  `0x01` code.
- `store.RoomByCode` resolves each code to its own room.

Then, for exhaustion: give the service a reader yielding `0x00` forever, call
`Create` twice → the second returns an error satisfying
`errors.Is(err, ErrCodeExhausted)`, and no partial room hash exists for the
second attempt's room ID.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/room/ -run TestCreate_CodeCollision -v`
Expected: FAIL — Checkpoint 2's `Create` generates one code and returns
`redisstore.ErrAlreadyExists` straight to the caller on collision.

- [ ] **Step 3: Implement to satisfy the test**

Contract: loop up to `maxCodeAttempts` (5, a named constant). Generate a code,
call `store.CreateRoom`, and on `errors.Is(err, redisstore.ErrAlreadyExists)`
try again; any other error returns immediately. Exhausting the attempts
returns `ErrCodeExhausted`.

This is the other half of Amendment B4: `CreateRoom` was made to *report* a
collision in Task 5 Checkpoint 3 precisely so that this loop can exist. Five
attempts against a 31^6 ≈ 887-million-code space is far beyond what live room
counts need; the bound exists so a pathological `rand` cannot spin forever.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/room/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/room/service.go backend/internal/room/service_test.go
git commit -m "feat: retry room creation on a code collision"
```

**Checkpoint 4: rejoining preserves the existing session balance**

- [ ] **Step 1: Write a failing test**

Spec, at the store level:
- `JoinRoom(ctx, roomID, "u1", 500)` → `(500, nil)`
- Simulate play by writing the wallet down to 120 through Redis directly
- `JoinRoom(ctx, roomID, "u1", 500)` again → `(120, nil)` — the effective
  balance is what they still hold
- `store.Balance(ctx, roomID, "u1")` is still 120 — the wallet was **not**
  reset to 500
- `JoinRoom(ctx, roomID, "u2", 500)` → `(500, nil)` — a genuinely new joiner
  is seeded normally
- `JoinRoom(ctx, roomID, "u3", 0)` → error satisfying
  `errors.Is(err, domain.ErrInvalidStake)`, unchanged from Phase 2

Then at the service level: joining the same room twice as the same account
returns the second call's `SessionBalance` equal to the surviving balance.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/redisstore/ -run TestJoinRoom -v`
Expected: FAIL — `JoinRoom` returns only `error`, so the call does not
compile; once the signature is widened, the balance still resets to 500.

- [ ] **Step 3: Implement to satisfy the test**

Contract: `JoinRoom` becomes `HSETNX`, then reads the field back and returns
it as the effective balance. The positive-balance validation stays where it
is, ahead of the write. Every existing Phase 2 caller and test updates to the
two-value signature.

This is Amendment B4's second half, and it is the one with a live exploit
attached: under the old unconditional `HSET`, any participant who was losing
could refresh the page, rejoin, and have their wallet topped back to the full
buy-in — unlimited tokens, no tooling required. Every one of those tokens
would flow into real pools and out through real settlements.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && make test-unit`
Expected: PASS — the whole backend, including Phase 2's concurrency suites.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/redisstore/room.go backend/internal/redisstore/room_test.go backend/internal/room/service.go backend/internal/room/service_test.go
git commit -m "fix: preserve a session wallet on rejoin instead of resetting it"
```

**Checkpoint 5: the host creates a room over HTTP**

- [ ] **Step 1: Write a failing test**

Spec — `POST /api/v1/rooms` with `{"buy_in":500}`:
- With a valid account token → 201, `data.code` is 6 characters, `data.room_id`
  a UUID, `data.buy_in == 500`, and `data.token` verifies to claims with that
  `RoomID`
- Without a token → 401 `unauthorized`
- With `{"buy_in":50}` → 400 `validation_error`
- With `{"buy_in":20000}` → 400 `validation_error`
- With a body missing `buy_in` entirely → 400 `validation_error`

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestCreateRoom -v`
Expected: FAIL — the route does not exist.

- [ ] **Step 3: Implement to satisfy the test**

Contract: register `POST /api/v1/rooms` behind `RequireAuth`. Take `hostID`
and `hostName` from the verified claims — never from the body, or any caller
could create rooms hosted by someone else. Decode `buy_in` as an integer,
reject a missing or non-integer value as `validation_error`, and delegate to
`room.Service.Create`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ -run TestCreateRoom -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/room_handlers.go backend/internal/httpapi/room_handlers_test.go backend/internal/httpapi/health.go
git commit -m "feat: add the room creation endpoint"
```

**Checkpoint 6: a guest joins by code with a session wallet**

- [ ] **Step 1: Write a failing test**

Spec — after a host creates a room with buy-in 500,
`POST /api/v1/rooms/{code}/participants` with `{"display_name":"Bob"}` and
**no** Authorization header:
- 201, `data.guest == true`, `data.session_balance == 500`,
  `data.partial_buy_in == false`, `data.room_id` equal to the created room
- `data.token` verifies to claims with `Guest: true`, `DisplayName: "Bob"`,
  `RoomID` equal to the room, and a non-empty `UserID` that differs between
  two successive guest joins
- `store.Balance(ctx, roomID, claims.UserID) == 500`
- `store.PlayerCount(ctx, roomID) == 1` — the host is still excluded
- Unknown code → 404 `not_found`
- Missing `display_name` → 400 `validation_error`
- A 33-rune `display_name` → 400 `validation_error`
- Lowercase code for an uppercase room → 404 (codes are case-sensitive, and
  the alphabet is uppercase-only)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestJoinRoom_Guest -v`
Expected: FAIL — the route does not exist; `Join` undefined.

- [ ] **Step 3: Implement to satisfy the test**

Contract: register `POST /api/v1/rooms/{code}/participants` behind
`OptionalAuth`, reading `{code}` with `r.PathValue("code")` (Go 1.22 pattern
routing — no third-party router). With no claims present, validate the
display name, generate a UUIDv4 guest ID, resolve the code via
`store.RoomByCode`, load the room, require `status == "open"` (else
`ErrNotJoinable`), compute `domain.GuestSessionBalance(room.BuyIn)`, call
`store.JoinRoom`, and issue a room-scoped token with `Guest: true`.

Guests hold no `user:{id}` hash at all — their whole identity is the signed
token, which is exactly what spec §3 describes as session-scoped and wiped
when the session ends.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ -run TestJoinRoom -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/room/service.go backend/internal/httpapi/room_handlers.go backend/internal/httpapi/room_handlers_test.go
git commit -m "feat: let guests join a room by code"
```

**Checkpoint 7: an account holder joins under the 3x cap with partial buy-in surfaced**

- [ ] **Step 1: Write a failing test**

Spec — room buy-in 500 throughout, joining with an account token:
- Account balance 10,000 → `data.session_balance == 1500`
  (`min(3 x 500, 10000)`), `data.partial_buy_in == false`, `data.guest == false`
- Account balance 900 → `session_balance == 900`, `partial_buy_in == false`
  (900 ≥ the 500 buy-in)
- Account balance 200 → `session_balance == 200`, `partial_buy_in == true`
- The issued token carries `Guest: false`, the account's `UserID`, and the
  account's stored display name — **not** any name supplied in the body
- `store.User(ctx, accountID).Balance` is **unchanged** in every case — joining
  does not debit the persistent balance
- Joining the same room twice with balance 10,000, having spent down to 300 in
  between, returns `session_balance == 300` on the second call

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestJoinRoom_Account -v`
Expected: FAIL — the account branch does not exist; the handler treats a
token-bearing request as a guest join and seeds the flat buy-in.

- [ ] **Step 3: Implement to satisfy the test**

Contract: when `OptionalAuth` yields claims, load the account with
`store.User`, take the display name from the stored record, compute
`domain.AccountSessionBalance(u.Balance, room.BuyIn)` and
`domain.IsPartialBuyIn(u.Balance, room.BuyIn)`, and issue a token with
`Guest: false`.

The persistent balance is deliberately untouched. Spec §3 applies the
session's **net delta** at session end via `domain.ApplySessionResult`, not
the session's opening stake — debiting at join and crediting the final balance
at close would double-count. A consequence: an account holder can hold
concurrent sessions in several rooms totalling more than their persistent
balance. That is out of scope here and is safe by construction, because
`ApplySessionResult` floors the persistent balance at 0. Recorded as an open
question for Phase 4, which owns session close.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ ./internal/room/ -cover`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/room/service.go backend/internal/httpapi/room_handlers.go backend/internal/httpapi/room_handlers_test.go
git commit -m "feat: join account holders under the 3x stake cap with partial buy-in"
```

---

## Task 11: The refill endpoint and process wiring

**Files:**
- Create: `backend/internal/httpapi/account_handlers.go`
- Create: `backend/internal/httpapi/account_handlers_test.go`
- Modify: `backend/internal/httpapi/health.go`
- Modify: `backend/cmd/api/main.go`

**Interfaces:**
- Consumes: `account.Service.ClaimRefill` (Task 7), the middleware (Task 8).
- Produces: no new exported Go surface — this task wires what exists.

**Checkpoint 1: an eligible account claims a refill over HTTP**

- [ ] **Step 1: Write a failing test**

Spec — register an account, write its balance down to 250 through the store,
then `POST /api/v1/accounts/me/refills` with its token:
- 201, `data.credited == 750`, `data.balance == 1000`, `data.remaining == 2`,
  `data.reset_at` an RFC 3339 timestamp in the future
- `X-RateLimit-Limit: 3` and `X-RateLimit-Remaining: 2`
- Without a token → 401 `unauthorized`
- The user ID comes from the token: a body naming a different `user_id` is
  rejected as an unknown field (400), never honoured

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestClaimRefill -v`
Expected: FAIL — the route does not exist.

- [ ] **Step 3: Implement to satisfy the test**

Contract: register `POST /api/v1/accounts/me/refills` behind `RequireAuth`,
taking the user ID from the verified claims only. Delegate to
`account.ClaimRefill` and render the `RefillResult`. Set the `X-RateLimit-*`
headers from the result's `Remaining` and `ResetAt` — the refill quota is a
rate limit, so it reports itself like one.

`me` rather than `{userID}` in the path is deliberate: an endpoint that takes
a user ID invites both the bug where it is trusted and the endless
authorization check that follows. There is exactly one account a caller may
refill, and the token already names it.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ -run TestClaimRefill -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/account_handlers.go backend/internal/httpapi/account_handlers_test.go backend/internal/httpapi/health.go
git commit -m "feat: add the manual refill endpoint"
```

**Checkpoint 2: ineligible and exhausted claims are refused distinctly**

- [ ] **Step 1: Write a failing test**

Spec:
- Account at balance 1000 (already at target) → 409 `refill_not_eligible`,
  balance unchanged
- Account driven through three successful claims, then a fourth with balance
  reset to 100 → 429 `refill_quota_exhausted`, `Retry-After` present and a
  positive integer, balance still 100
- The 409 case does **not** consume quota: after it, a genuine claim from
  balance 100 still succeeds

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestClaimRefill_Rejections -v`
Expected: FAIL — the exhausted case returns 409 rather than 429 with a
`Retry-After`, since the handler has no branch that reads the retry hint.

- [ ] **Step 3: Implement to satisfy the test**

Contract: `domain.ErrRefillNotEligible` and `domain.ErrRefillQuotaExhausted`
already map to 409 and 429 through Task 8's table. The handler additionally
sets `Retry-After` on the 429 path, which needs the retry duration — carry it
on an exported `account.QuotaError` wrapping `domain.ErrRefillQuotaExhausted`
with a `RetryAfter time.Duration` field, so `errors.Is` keeps working and
`errors.As` retrieves the hint.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ ./internal/account/ -cover`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/account/service.go backend/internal/httpapi/account_handlers.go backend/internal/httpapi/account_handlers_test.go
git commit -m "feat: report refill quota exhaustion with a retry hint"
```

### Wiring steps (one commit, no test cycle — `cmd/api` is thin by design)

- [ ] **Step 1: Construct the dependency graph in `main.go`**

In `run()`, after `config.Load`: build the `redisstore.Store` with
`cfg.RedisAddr` / `cfg.RedisDB`, the `auth.Issuer` with `[]byte(cfg.JWTSecret)`
and `cfg.JWTTTL`, then `account.NewService` and `room.NewService`, and pass
them to `httpapi.NewMux` as `Deps`. Close the store on shutdown alongside the
existing graceful-shutdown path. Every construction error returns a wrapped
error from `run()`, matching the file's existing fail-fast shape.

`cmd/api` stays untested (0% coverage) on purpose — the plan's own note on
`main.go` — because everything with a branch now lives behind `NewMux`, which
Tasks 8–11 cover.

- [ ] **Step 2: Apply the throttle to the routes**

In `NewMux`, wrap `/api/v1/auth/*` with
`RateLimit(store, LimitPolicy{Scope: "auth", Limit: 10, Window: time.Minute,
KeyFn: func(r *http.Request) string { return ClientIP(r) }})`, and the
authenticated routes with the `"api"` scope at 60 per minute keyed on the
claims' user ID. `GET /healthz` is not throttled — an operational probe that
can be rate-limited out is not a probe.

- [ ] **Step 3: Smoke-test the running server by hand**

```bash
make up
cd backend && JWT_SECRET=0123456789abcdef0123456789abcdef go run ./cmd/api &
curl -s localhost:8080/healthz
curl -s -XPOST localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"a@b.co","password":"correct horse battery","display_name":"Alice"}'
# capture data.token from the response as $TOKEN, then:
curl -s -XPOST localhost:8080/api/v1/rooms -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"buy_in":500}'
# capture data.code as $CODE, then:
curl -s -XPOST localhost:8080/api/v1/rooms/$CODE/participants \
  -H 'Content-Type: application/json' -d '{"display_name":"Bob"}'
```

Expect: `{"status":"ok"}`, then a 201 with a token and a balance of 1000, then
a 201 with a room code, then a 201 guest join with `session_balance` 500.
Finally confirm the fail-fast path: run `go run ./cmd/api` with no
`JWT_SECRET` and expect it to exit non-zero naming `JWT_SECRET`.

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/api/main.go backend/internal/httpapi/health.go
git commit -m "feat: wire the REST API into the server entrypoint"
```

---

## Task 12: Coverage, security review, documentation, and close-out

The phase's last task. It ends at "branch is green and verified" — the merge
decision belongs to `finishing-a-development-branch`, which
`executing-plans` hands off to, not to this plan.

**Files:**
- Modify: `docs/plans/2026-08-21-implementation-plan.md`
- Modify: `docs/specs/2026-08-21-callit-design.md`
- Modify: `CLAUDE.md`
- Create: `journal/YYYY-MM-DD_HHMM_<name>_phase-3-auth-rest-execution.md`

- [ ] **Step 1: Verify coverage across every package**

Run: `cd backend && go test ./... -race -cover`

Expected floors: `internal/config` 100%, `internal/domain` 100% (unchanged —
this phase adds nothing to it), `internal/auth` ≥ 90%, `internal/redisstore`
≥ 85%, `internal/account` ≥ 85%, `internal/room` ≥ 85%, `internal/httpapi`
≥ 85%, `cmd/api` 0% (expected). Any package under 80% gets tests added before
this task continues; record the actual figures, they go into CLAUDE.md in
Step 4.

- [ ] **Step 2: Run a security review of the auth surface**

Parent plan §12 requires a security review of the auth and wager-placement
paths; this phase is the auth path. Invoke the `security-review` skill (or the
`security-reviewer` agent) against `internal/auth`, `internal/account`,
`internal/httpapi`, and the three new Lua scripts.

Specific items to confirm, each already asserted by a test above — this step
is a second pair of eyes, not the first check:

- `VerifyPassword` compares with `crypto/subtle.ConstantTimeCompare`
  (Task 2 CP2's implementation note — deliberately not separately
  checkpointed, because it has no observable signal at that interface)
- Token verification pins `HS256` and rejects `alg: none` (Task 4 CP4)
- Login gives byte-identical responses for unknown email and wrong password
  (Task 9 CP4)
- No password material appears in any response body or log line
- 500s carry no internal error text (Task 8 CP3)
- The rate limiter fails closed when Redis is unreachable (Task 8 CP5)
- `X-Forwarded-For` is not trusted (Task 8 CP5)

And two known-open items to record rather than fix here:

- **Login timing.** The unknown-email path skips argon2id and returns faster
  than the wrong-password path, so timing still distinguishes them even though
  the responses do not. The fix — verifying against a fixed dummy hash on the
  miss path — is a contained change; decide whether it lands now or in Phase 7
  hardening.
- **Room code bias.** `byte % 31` slightly favours the first 8 alphabet
  characters (Task 10 CP1). Accepted: the code is a lookup handle, not a
  secret, and authorization rests on the JWT.

Fix anything CRITICAL or HIGH before continuing; commit each fix on its own.

- [ ] **Step 3: Fold Amendments B1–B7 back into the parent plan and spec**

In `docs/plans/2026-08-21-implementation-plan.md`:
- §3 layout — add `internal/account/`, note that `internal/auth` is I/O-free
- §4 key schema — add `user:{userID}`, `email:{normalizedEmail}`; annotate
  `ratelimit:{scope}:{id}` as implemented in Phase 3 with the scope table
- §5 — add the three new scripts with their contracts (B6)
- §9 phase table — mark Phase 3 done; add a Phase 4 note that room and account
  writers, the limiter, and the token issuer already exist and must be wrapped,
  not reimplemented; add a Phase 5 note carrying B1's open question about
  migrating accounts to PostgreSQL

In `docs/specs/2026-08-21-callit-design.md`:
- §3 — one sentence recording that persistent accounts live in Redis until
  Phase 5 (B1), and that a session's opening stake does not debit the
  persistent balance
- §6 — one sentence that the token carries the display name, so no per-message
  lookup is needed for identity (B5)

- [ ] **Step 4: Update CLAUDE.md**

- **Stack** — the three new pinned dependencies and, for `x/crypto`, the same
  "raising it means moving the toolchain first" warning go-redis carries
- **Critical Invariants** — add: the shared limiter is the only sliding-window
  implementation; tokens are HS256-only and verification pins the algorithm;
  login must not distinguish unknown email from wrong password; unique indexes
  (email, room code) are claimed through `claim_unique.lua`, never a bare `SET`
- **Repository Layout** — `internal/auth`, `internal/account`, `internal/room`
  move from "(Phase 3)" to "(exists)" with their coverage
- **Testing** — the measured per-package coverage from Step 1
- **Installed Tooling** — record that `api-design` was used in this phase and
  note the next phase's imports per plan §9

- [ ] **Step 5: Commit the documentation close-out**

```bash
git add docs/plans/2026-08-21-implementation-plan.md docs/specs/2026-08-21-callit-design.md CLAUDE.md
git commit -m "docs: fold Phase 3 amendments into the plan, spec, and conventions"
```

- [ ] **Step 6: Verify the branch is green from a cold start**

```bash
make down && make up && make test && make lint && make build
```

Expected: all green, from a freshly created Redis with no leftover state. A
suite that only passes against a warm database is not passing.

- [ ] **Step 7: Write the journal entry**

Use the `journal` skill. Record the actual commit count against this plan's
estimate, which checkpoints combined or split during execution and why, and
whether the observable-signal rule (this phase is its first real trial —
see `.claude/skills/writing-plans/SKILL.md`) prevented a checkpoint that could
not go RED, or was itself found wanting.

Three checkpoints in this plan were written *because* of that rule and are
worth reporting on specifically: Task 2's constant-time comparison, which was
demoted from a checkpoint to an implementation note plus a review item; Task 6
CP3 and Task 7 CP2, both of which may pass on first run and carry explicit
instructions for what to do when they do.

- [ ] **Step 8: Stop here**

Do not merge. `executing-plans` hands off to
`finishing-a-development-branch`, which verifies the tests and presents the
merge/PR/keep choice — that decision stays with the user.

---

## Self-Review

**1. Spec coverage.**

| Spec requirement | Task |
|---|---|
| §3 short code + shareable link join | 10 CP1, CP6 |
| §3 guests: display name only, session-scoped balance | 10 CP6 |
| §3 account holders: persistent identity and balance | 5, 9 |
| §3 host-configurable buy-in, bounds enforced | 10 CP2, CP5 |
| §3 stake cap `min(3 x buy-in, balance)` | 10 CP7 |
| §3 partial buy-in surfaced | 10 CP7 |
| §3 net profit/loss applied at session end | **Phase 4** — not this phase; noted in 10 CP7 |
| §3 refill below target, top up to fixed amount | 7 CP1–CP3 |
| §3 max 3 refills per rolling 7 days | 7 CP4 |
| §3 refills use the shared sliding-window limiter | 6, 7 |
| §6 short-lived signed JWT with identity + room id | 4 CP2 |
| §6 verified server-side, no per-message DB hit | 4, B5 |
| Parent §2 email + password, argon2id, JWT HS256 | 2, 3, 4 |
| Parent §2 guests get a JWT with `guest: true`, no DB row | 10 CP6 |
| Parent §8 starting balance 1,000 | 9 CP1 |
| Parent §9 rate-limit middleware | 8 CP5 |

Two requirements are deliberately **not** in this phase, both recorded above
rather than dropped: session-end profit/loss application (Phase 4 owns session
close) and the wager-placement throttle's call site (Phase 4 owns the
WebSocket path — the limiter it will call is built here).

**2. Placeholder scan.** No "TBD", no "add appropriate validation", no
"similar to Task N". Every checkpoint names exact inputs and exact expected
outputs or sentinel errors. Two checkpoints — Task 6 CP3 and Task 7 CP2 — may
pass on first run because an earlier checkpoint's implementation already
satisfies them; both say so explicitly and give the procedure (verify the test
can go red by removing the guard, restore, commit as a pinned regression)
rather than leaving an executor to hit the contradiction cold.

**3. Type consistency.** Checked across tasks:
- `domain.Tokens` is the amount type everywhere; no bare `int64` crosses a
  package boundary.
- `JoinRoom`'s widened signature `(domain.Tokens, error)` is introduced in
  Task 10 CP4 and used consistently in Task 10 CP2, CP6, and CP7. Task 10 CP2
  is written before CP4 and calls the *old* signature — the executor updates
  it at CP4, which is also when Phase 2's existing callers change. Flagged
  here so that is a known edit, not a surprise.
- `Decision.Member` appears in Task 6 CP1's struct and is consumed in CP4 and
  Task 7 CP5 — one name, one meaning.
- `auth.Claims` field names (`UserID`, `DisplayName`, `RoomID`, `Guest`) are
  identical in Tasks 4, 8, 9, 10, and 11.
- `RefillScope` / `RefillWindow` are `account` package constants; the HTTP
  throttle's scopes (`auth`, `api`) are `httpapi`'s. No scope string is
  written as a literal at a call site.
- Error sentinels are declared in the package that raises them —
  `account.ErrEmailTaken`, `account.ErrInvalidCredentials`,
  `room.ErrNotJoinable`, `room.ErrCodeExhausted`,
  `redisstore.ErrAlreadyExists`, `auth.Err*` — and Task 8 CP2's mapping table
  is the single place they meet HTTP status codes.

**4. Size.** 12 tasks, 38 test-cycle checkpoints, plus 3 non-cycle setup
commits and 3 close-out commits — roughly 44 commits, against Phase 2's 28.
This is the largest phase so far because it is four deliverables in one:
credentials, tokens, the shared limiter, and the REST surface over rooms and
refills. The parent plan's §9 groups them, and splitting them would leave
half-wired endpoints at a phase boundary. If execution runs long, Tasks 1–10
are a coherent stopping point — registration, login, rooms, and joining all
work; only the refill endpoint and process wiring would remain.
