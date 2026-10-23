# Phase 8a.1 — Production Secrets from 1Password Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Production secrets live in a 1Password vault and reach the
containers as files on a RAM-backed mount, so the VM's disk holds exactly
one long-lived credential (a read-only, vault-scoped Service Account
token) instead of every secret in `deploy/.env`.

**Architecture:** A root systemd unit, `callit-secrets.service`, runs
`deploy/secrets.sh` at boot and on every deploy. It fetches each secret with
`op read`, validates the whole set, then writes it atomically into
`/run/callit/secrets` (tmpfs). Compose mounts those files into the containers
as file-based `secrets:` (bind mounts). The Go binaries read them through new
`*_FILE` variables, and Postgres through its native `POSTGRES_PASSWORD_FILE`.
No secret value is ever a container environment variable, so none lands in
Docker's on-disk container config (`config.v2.json`, `docker inspect`).
`deploy/op-bootstrap.sh` runs on the laptop under your own 1Password sign-in.
It creates the vault items, either by generating them for a fresh install or
by importing the live VM's existing values for the cutover.

**Tech Stack:** 1Password CLI `op` v2 (host tool on the VM and laptop, not a
code dependency) · 1Password Service Accounts · systemd (`LoadCredential=`) ·
Docker Compose v2 file secrets · Go 1.26.7 (`internal/config` only) · bash.

**Spec:** [`journal/2026-10-22_1935_ansh_1password-secrets-idea.md`](../../journal/2026-10-22_1935_ansh_1password-secrets-idea.md)
(the idea, its decisions and open questions). This plan amends one of its
decisions; see Decision 1.
**Parent plan:** [`docs/plans/2026-08-21-implementation-plan.md`](2026-08-21-implementation-plan.md) §9, new row 8a.1
**Builds on:** [`docs/plans/2026-10-17-phase-8a-backend-deploy.md`](2026-10-17-phase-8a-backend-deploy.md)

---

## Decisions This Plan Fixes

1. **Files on tmpfs, not `op run` env vars.** The journal chose `op run`
   because "values exist only in the process environment". That isn't true
   under Docker: a container's environment is persisted in
   `/var/lib/docker/containers/<id>/config.v2.json` and printed by
   `docker inspect`. File secrets bind-mounted from tmpfs keep the values
   out of that config. Docker records only the source path. The cost is a
   small Go change (`*_FILE` support), which the journal's "no backend code
   change" decision didn't anticipate. That decision is withdrawn.
2. **The `_FILE` convention.** The pattern is the one the official Postgres
   image already uses: `NAME_FILE` names a file whose content is `NAME`.
   It's resolved once, at startup, by one helper in `internal/config`. Setting
   both `NAME` and `NAME_FILE` is a startup error rather than a precedence
   rule.
3. **Secrets dir permissions are `root:docker 0750`, and each file is `0444`.**
   Non-swarm Compose ignores `uid`/`gid`/`mode` on file secrets, so a
   container (uid 65532 for the Go images, 70 for Postgres) can only read a
   file that is world-readable. The directory is what stops other host users.
   The `docker` group is already root-equivalent, so granting it the
   directory adds no new access, and `deploy.sh` and the DuckDNS cron job can
   read the files without sudo.
4. **Fail closed when 1Password is unreachable.** After a reboot, `/run` is
   empty until `callit-secrets.service` succeeds. Until then the containers
   cannot start, because their bind sources are missing. An outage at boot
   means downtime, never a start with blank or stale secrets.
5. **The import never regenerates.** `op-bootstrap.sh` refuses to create an
   item that already exists. The cutover imports the VM's current
   `POSTGRES_PASSWORD`, because a new one would no longer open the existing
   database volume (`gen-env.sh`'s own warning).
6. **`JWT_SECRET` rotation stays logout-all.** Rotating means editing the
   item and redeploying, after which every session must log in again. That's
   accepted and documented. Two-key verification during a rotation window is
   deferred. Postgres password rotation (`ALTER ROLE` plus a vault edit) is
   documented as a manual procedure, not automated.
7. **No phase renumbering.** It's labelled 8a.1 so that 8b–8e and their plan
   files keep their numbers. 8b's dependency moves from 8a to 8a.1.

## Global Constraints

- **No new Go modules, npm packages or images.** `op` is a host binary,
  installed from 1Password's signed apt repository, never vendored.
- **A secret value never appears in:** argv (`ps`), stdout or stderr, a
  container's environment, git, or a file outside the tmpfs secrets dir.
  The one exception, by design, is the Service Account token at
  `/etc/callit/op-service-account-token` (root `0400`).
- **Production network membership is unchanged.** `check-compose.sh`
  check 2 keeps its exact map, and no service joins a new network.
- **Vault `callit-prod`. Items, all of category Password with field `password`:**
  `jwt-secret`, `postgres-password`, `duckdns-token`.
  References: `op://callit-prod/<item>/password`.
- **Secret files (in `$CALLIT_SECRETS_DIR`, default `/run/callit/secrets`):**
  `jwt_secret`, `postgres_password`, `postgres_dsn`, `duckdns_token`, each
  written with no trailing newline. `postgres_dsn` is derived as
  `postgres://callit:<password>@postgres:5432/callit?sslmode=disable`.
- **Validation:** JWT secret `^[0-9a-f]{64}$`; Postgres password
  `^[A-Za-z0-9]{16,}$` (safe inside a DSN without escaping); DuckDNS token
  `^[A-Za-z0-9-]+$`. Values `gen-env.sh` generated satisfy all three.
- **Shell tests stub `op`** with a fake on `PATH`. No test talks to real
  1Password, and CI needs no secret.

## Review Focus

1. **Cutover regenerates instead of importing:** the DB is locked out.
   Pinned in Task 5, Checkpoints 1–2 (refuse-if-exists; byte-exact import).
2. **A trailing newline sneaks into a secret** (`op read` adds one by
   default): the JWT key changes by one byte and the DSN breaks. Pinned in
   Task 2, Checkpoint 1 (Go strips exactly one) and Task 3, Checkpoint 1
   (files have none).
3. **`op` fails halfway through a fetch:** a mismatched set (new JWT, old
   Postgres) goes live. Pinned in Task 3, Checkpoint 2 (all or nothing).
4. **After a reboot, Docker created a directory at a missing bind path**
   (`jwt_secret/`), and the next materialization writes into it. Pinned in
   Task 3, Checkpoint 4.
5. **A secret leaks into logs** through a script's output or a Go error.
   Pinned in Task 2, Checkpoint 2 (errors name the path, never the content)
   and Task 3, Checkpoint 2 (no stdout or stderr carries a value).

---

### Task 1: Verify the assumptions this plan rests on

No code, and no RED→GREEN. This is a spike with explicit stop conditions.
**If any check fails, stop and amend Decisions 1–4 before Task 2.**

**Files:**
- Modify: this plan, appending a `## Spike Results (Task 1)` section

- [ ] **Step 1: File secrets stay out of the container config (laptop).**
  Use a scratch Compose file with one `busybox:1.36.1` service,
  `read_only: true`, `user: "65532"`, and a file secret from `/dev/shm/x/s`
  (mode 0444, dir 0750) containing a canary string. Run `up`, then:
  `cat /run/secrets/s` inside the container prints the canary;
  `docker inspect` does not contain the canary;
  `docker inspect -f '{{json .Mounts}}'` shows a `bind` mount of the source.
  Repeat with `user: "70"`.
- [ ] **Step 2: A missing bind source fails closed.** Stop the container,
  delete `/dev/shm/x/s`, and `docker start` it. Record whether Docker errors
  ("bind source path does not exist") or creates a directory. Either outcome
  is fail-closed. Record which one, because Task 3, Checkpoint 4 exists for
  the second.
- [ ] **Step 3: `op` CLI behaviour (laptop, signed in):**
  `op read --no-newline` exists and prints no trailing newline. Find out
  whether `op item create --vault V -` accepts a JSON item template on stdin.
  If it does, Task 5 passes values that way. If not, Task 5 uses
  `--template <file>` with the file under `/dev/shm`, mode 0600, removed
  after. Record which.
- [ ] **Step 4: `op` on the arm64 VM.** Install `op` from 1Password's apt
  repository (key fingerprint checked per 1Password's Linux install docs);
  `op --version` runs on aarch64. Record the version.
- [ ] **Step 5: Record and commit**

```bash
git add docs/plans/2026-10-22-phase-8a1-op-secrets.md && \
  git commit -m "docs: record Phase 8a.1 spike results"
```

---

### Task 2: `*_FILE` secrets in `internal/config`

**Files:**
- Create: `backend/internal/config/secret_files.go`, `backend/internal/config/secret_files_test.go`
- Modify: `backend/cmd/api/main.go`, `backend/cmd/migrate/main.go`, `backend/cmd/ledger-worker/main.go`, `deploy/check-image.sh`

**Interfaces:**
- Produces: `func ResolveSecretFiles(lookup LookupFunc, readFile func(string) ([]byte, error), names ...string) (LookupFunc, error)`.
  `Load`, `LoadMigrate` and `LoadLedger` keep their signatures. The mains
  wrap `os.LookupEnv` before calling them: api with `"JWT_SECRET"`, migrate
  and ledger-worker with `"POSTGRES_DSN"`. The relay has no secret and is
  unchanged.

**Checkpoint 1: `NAME_FILE` supplies `NAME`**

- [ ] **Step 1: Write the failing test, then run it**

Spec (table-driven, with `readFile` faked from a `map[string][]byte`):
- `JWT_SECRET_FILE=/s/jwt` with file content `"abc\n"`: the returned lookup
  gives `("abc", true)` for `JWT_SECRET`.
- Content `"abc"`: gives `"abc"`.
- Content `"abc\n\n"`: gives `"abc\n"`, because exactly one trailing `\n` is
  stripped.
- `JWT_SECRET_FILE` unset and `JWT_SECRET=xyz`: gives `"xyz"` (passthrough).
- Both unset: `("", false)`.
- Keys not in `names` pass through untouched.
- End to end: `Load` called on the resolved lookup, with a 64-char key in
  the file, sets `cfg.JWTSecret` to it.

Run: `cd backend && go test ./internal/config -run TestResolveSecretFiles -count=1`
Expected: FAIL, build error (`ResolveSecretFiles` undefined)

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: for each name whose `NAME_FILE` is set, read the file once and
overlay `NAME` with its content minus one trailing `\n`. Every other key
delegates to `lookup`.

```bash
cd backend && go test ./internal/config -run TestResolveSecretFiles -count=1 && cd .. && \
  git add backend/internal/config/secret_files.go backend/internal/config/secret_files_test.go && \
  git commit -m "feat: resolve NAME_FILE secrets in config"
```

**Checkpoint 2: ambiguous, empty and unreadable secret files fail fast**

- [ ] **Step 1: Write the failing test, then run it**

Spec, each case returning a non-nil error and a nil lookup:
- `JWT_SECRET=x` and `JWT_SECRET_FILE=/s/jwt` both set (even
  `JWT_SECRET=""`): error contains `set JWT_SECRET or JWT_SECRET_FILE, not both`.
- `JWT_SECRET_FILE=""`: `JWT_SECRET_FILE must not be empty`.
- `readFile` returns `fs.ErrNotExist`: the error contains
  `reading JWT_SECRET_FILE` and wraps `fs.ErrNotExist` (`errors.Is`).
- File content `""` or `"\n"`: the error contains `JWT_SECRET_FILE /s/jwt is empty`.
- In every error case, assert the error string does **not** contain the
  file's content. Use the content `"s3cret-canary"` in a variant where the
  file is fine but `JWT_SECRET` is also set.

Run: `cd backend && go test ./internal/config -run TestResolveSecretFiles -count=1`
Expected: FAIL, because the conflict, empty-path and empty-content cases return no error

- [ ] **Step 2: Implement, then verify-and-commit in one command**

```bash
cd backend && go test ./internal/config -count=1 && cd .. && \
  git add backend/internal/config/secret_files.go backend/internal/config/secret_files_test.go && \
  git commit -m "feat: reject ambiguous or empty secret files at startup"
```

**Checkpoint 3: the binaries honour `*_FILE`**

- [ ] **Step 1: Write the failing check, then run it**

Spec: in `deploy/check-image.sh`, add after the existing expectations:
`expect_config_error api "reading JWT_SECRET_FILE" -e JWT_SECRET_FILE=/nonexistent`,
and the same for `migrate` and `ledger-worker` with `POSTGRES_DSN_FILE`.

Run: `bash deploy/check-image.sh`
Expected: FAIL, because api prints `JWT_SECRET is required` (the variable is ignored)

- [ ] **Step 2: Wire the three mains, then verify-and-commit**

Contract: `lookup, err := config.ResolveSecretFiles(os.LookupEnv, os.ReadFile, "<NAME>")`
before the existing `Load*` call. On error, the main exits the same way an
existing config error does.

```bash
bash deploy/check-image.sh && \
  git add backend/cmd/api/main.go backend/cmd/migrate/main.go backend/cmd/ledger-worker/main.go deploy/check-image.sh && \
  git commit -m "feat: read production secrets from files in api, migrate and ledger-worker"
```

Task boundary: `make lint && make test`.

---

### Task 3: `deploy/secrets.sh`, the materializer

**Files:**
- Create: `deploy/secrets.sh`, `deploy/tests/secrets_test.sh`, `deploy/tests/fake-op` (stub)
- Modify: `.github/workflows/ci.yml` (add a `Test secret materialization` step to the `deploy` job)

**Interfaces:**
- Produces: `deploy/secrets.sh`, which takes no arguments. Environment:
  `CALLIT_SECRETS_DIR` (default `/run/callit/secrets`); token from
  `$CREDENTIALS_DIRECTORY/op-token` if present, else
  `OP_SERVICE_ACCOUNT_TOKEN`. Exits 0 with the full set written, or 1 with
  nothing changed.
- `fake-op`: answers `read --no-newline op://callit-prod/<item>/password`
  from `$FAKE_OP_DIR/<item>`. It exits 1 if that file is missing or
  `$FAKE_OP_FAIL_ON=<item>`, and appends its argv and
  `${OP_SERVICE_ACCOUNT_TOKEN:+token-set}` to `$FAKE_OP_DIR/calls.log`.

**Checkpoint 1: a good fetch writes the full set**

- [ ] **Step 1: Write the failing test, then run it**

Spec: fake values `jwt-secret`=64 hex, `postgres-password`=`abcDEF123456789xyz`,
`duckdns-token`=`tok-1`. After `secrets.sh` exits 0, the dir is mode 0750 and
holds exactly the four files, each mode 0444, with no trailing newline.
`postgres_dsn` equals
`postgres://callit:abcDEF123456789xyz@postgres:5432/callit?sslmode=disable`,
and no `.tmp*` files are left over.

Run: `bash deploy/tests/secrets_test.sh`
Expected: FAIL, because `deploy/secrets.sh` doesn't exist

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: read all three into shell variables first, then validate all
three (Global Constraints). Write each file to a temp name in the target
dir, `chmod 0444`, then `mv` over the final name. When running as root,
also `chgrp docker` the dir.

```bash
bash deploy/tests/secrets_test.sh && \
  git add deploy/secrets.sh deploy/tests/secrets_test.sh deploy/tests/fake-op && \
  git commit -m "feat: materialize production secrets from 1Password onto tmpfs"
```

**Checkpoint 2: any failure leaves the previous set untouched and prints no secret**

- [ ] **Step 1: Write the failing test, then run it**

Spec: start from a populated dir (Checkpoint 1's output, sha256 recorded).
Each of these exits 1, leaves every file byte-identical, adds no file, and
produces stdout+stderr that contains none of the fake values:
(a) `FAKE_OP_FAIL_ON=postgres-password` (the *second* read fails);
(b) the JWT value is 63 hex chars;
(c) the Postgres password contains `@`;
(d) the DuckDNS token contains `;`.

Run: `bash deploy/tests/secrets_test.sh`
Expected: FAIL on (a), because `jwt_secret` was already replaced (if the
Checkpoint 1 implementation writes as it reads), or on (b)–(d) through
missing validation

- [ ] **Step 2: Implement, then verify-and-commit**

```bash
bash deploy/tests/secrets_test.sh && \
  git add deploy/secrets.sh deploy/tests/secrets_test.sh && \
  git commit -m "fix: make secret materialization all-or-nothing"
```

(If Checkpoint 1's implementation already fetched and validated before any
write, this checkpoint goes RED only on the validation cases. That's still a
genuine cycle. If it goes RED on none, merge these cases into Checkpoint 1's
test per the writing-plans rule, and say so in the commit body.)

**Checkpoint 3: the token comes from the credential file or the env, and never from argv**

- [ ] **Step 1: Write the failing test, then run it**

Spec:
- No `CREDENTIALS_DIRECTORY` and no `OP_SERVICE_ACCOUNT_TOKEN`: exits 1 with
  `secrets: no 1Password service account token`, and `calls.log` is empty
  (`op` never ran).
- `CREDENTIALS_DIRECTORY=$d` with `$d/op-token`=`ops_fake`: succeeds,
  `calls.log` shows `token-set`, and no logged argv contains `ops_fake`.

Run: `bash deploy/tests/secrets_test.sh`
Expected: FAIL, because the credential-file path is unsupported

- [ ] **Step 2: Implement, then verify-and-commit**

```bash
bash deploy/tests/secrets_test.sh && \
  git add deploy/secrets.sh deploy/tests/secrets_test.sh && \
  git commit -m "feat: read the service account token from a systemd credential"
```

**Checkpoint 4: a non-file at a secret's path is refused**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `mkdir "$dir/jwt_secret"` (what Docker may create after a reboot;
Task 1, Step 2), then run. It exits 1 with
`secrets: $dir/jwt_secret exists and is not a regular file`, and nothing is
written inside that directory.

Run: `bash deploy/tests/secrets_test.sh`
Expected: FAIL, because `mv` moves the temp file *into* the directory and the run exits 0

- [ ] **Step 2: Implement, then verify-and-commit**

```bash
bash deploy/tests/secrets_test.sh && \
  git add deploy/secrets.sh deploy/tests/secrets_test.sh .github/workflows/ci.yml && \
  git commit -m "fix: refuse to materialize over a non-file secret path"
```

---

### Task 4: Compose consumes file secrets; `check-compose.sh` enforces it

**Files:**
- Modify: `deploy/compose.prod.yml`, `deploy/check-compose.sh`, `deploy/tests/check-compose_test.sh`, `deploy/tests/fixture.env`, `deploy/gen-env.sh`, `deploy/tests/gen-env_test.sh`

**Interfaces:**
- Consumes: Task 2's `JWT_SECRET_FILE` / `POSTGRES_DSN_FILE`; Task 3's file names.
- Produces: `deploy/.env` holds only `CALLIT_HOSTNAME` and
  `CALLIT_SECRETS_DIR`. The secret grants are `api=jwt_secret`,
  `migrate=postgres_dsn`, `ledger-worker=postgres_dsn`,
  `postgres=postgres_password`, and nothing for any other service.

**Checkpoint 1: no secret in any container environment, with least-privilege grants**

- [ ] **Step 1: Write the failing test, then run it**

Spec: add these to `check-compose_test.sh`:
- A mutant that adds `postgres_dsn` to api's `secrets:` must fail check 12.
- A mutant that adds `JWT_SECRET: x` to api's `environment:` must fail check 12.

Run: `bash deploy/tests/check-compose_test.sh`
Expected: FAIL (`check-compose.sh accepted ...`, because check 12 doesn't exist)

- [ ] **Step 2: Implement, then verify-and-commit**

Contract:
- In compose, add top-level `secrets:` with
  `file: ${CALLIT_SECRETS_DIR:?CALLIT_SECRETS_DIR is required — run deploy/gen-env.sh}/<name>`.
  Give api `JWT_SECRET_FILE: /run/secrets/jwt_secret`; migrate and
  ledger-worker `POSTGRES_DSN_FILE: /run/secrets/postgres_dsn`; postgres
  `POSTGRES_PASSWORD_FILE: /run/secrets/postgres_password`. Delete every
  `${JWT_SECRET}`/`${POSTGRES_PASSWORD}` interpolation.
- New check 12: no service's environment has a key in
  {`JWT_SECRET`, `POSTGRES_PASSWORD`, `POSTGRES_DSN`, `DUCKDNS_TOKEN`}; the
  grants exactly match the map in Interfaces; every top-level secret's
  `file` starts with the fixture's `CALLIT_SECRETS_DIR`.
- Rewrite check 6's DSN line to assert `POSTGRES_DSN_FILE=/run/secrets/postgres_dsn`.
- Check 10 now drops each of `CALLIT_HOSTNAME` and `CALLIT_SECRETS_DIR`.
- Extend the `unset` line with `CALLIT_SECRETS_DIR`.
- `fixture.env` keeps `CALLIT_HOSTNAME` plus
  `CALLIT_SECRETS_DIR=/run/callit/secrets`, and drops both secrets.

```bash
bash deploy/check-compose.sh && bash deploy/tests/check-compose_test.sh && \
  git add deploy/compose.prod.yml deploy/check-compose.sh deploy/tests/check-compose_test.sh deploy/tests/fixture.env && \
  git commit -m "feat: mount production secrets as files, never container env"
```

**Checkpoint 2: `gen-env.sh` writes no secret**

- [ ] **Step 1: Write the failing test, then run it**

Spec:
- `gen-env.sh callit.duckdns.org` writes a 0600 `.env` of exactly two
  lines: `CALLIT_HOSTNAME=callit.duckdns.org` and
  `CALLIT_SECRETS_DIR=/run/callit/secrets`.
- A second positional argument exits 2 with
  `gen-env: the DuckDNS token now lives in 1Password (deploy/README.md §4)`
  and writes nothing.
- The existing refusal and hostname-validation cases stay, and the
  generated `.env` still renders compose.
- Delete the secret-format, `DUCKDNS_TOKEN` and secrets-differ cases.

Run: `bash deploy/tests/gen-env_test.sh`
Expected: FAIL, because `.env` still contains `JWT_SECRET=`

- [ ] **Step 2: Implement, then verify-and-commit**

```bash
bash deploy/tests/gen-env_test.sh && bash deploy/check-compose.sh && \
  git add deploy/gen-env.sh deploy/tests/gen-env_test.sh && \
  git commit -m "refactor: gen-env.sh writes only non-secret settings"
```

---

### Task 5: `deploy/op-bootstrap.sh`, which creates the vault items from the laptop

**Files:**
- Create: `deploy/op-bootstrap.sh`, `deploy/tests/op-bootstrap_test.sh`
- Modify: `deploy/tests/fake-op` (add `item get` / `item create`), `.github/workflows/ci.yml` (add a `Test vault bootstrap` step)

**Interfaces:**
- Produces: `deploy/op-bootstrap.sh --generate <duckdns-token>` or
  `deploy/op-bootstrap.sh --import` (it reads `.env`-format lines on stdin).
  It runs under the caller's own `op` sign-in.
- `fake-op` additions: `item get <title> --vault callit-prod` exits 0 if
  `$FAKE_OP_DIR/<title>` exists. `item create` records argv to `calls.log`
  and stdin (or the `--template` file, per the Task 1 result) to
  `$FAKE_OP_DIR/created-<title>.json`.

**Checkpoint 1: `--generate` creates three items and never overwrites one**

- [ ] **Step 1: Write the failing test, then run it**

Spec:
- An empty fake vault with `--generate tok-1` exits 0 and creates
  `jwt-secret` (value matches `^[0-9a-f]{64}$`), `postgres-password`
  (`^[0-9a-f]{48}$`) and `duckdns-token` (`tok-1`), each of category
  Password in vault `callit-prod`.
- No value appears in `calls.log` (argv).
- With `postgres-password` already present in the fake vault, it exits 1
  with `op-bootstrap: item postgres-password already exists in callit-prod; refusing to overwrite`
  and creates **no** item (existence is checked for all three before any
  create).

Run: `bash deploy/tests/op-bootstrap_test.sh`
Expected: FAIL, because the script doesn't exist

- [ ] **Step 2: Implement, then verify-and-commit**

```bash
bash deploy/tests/op-bootstrap_test.sh && \
  git add deploy/op-bootstrap.sh deploy/tests/op-bootstrap_test.sh deploy/tests/fake-op && \
  git commit -m "feat: bootstrap the callit-prod vault with generated secrets"
```

**Checkpoint 2: `--import` copies the live values byte-for-byte**

- [ ] **Step 1: Write the failing test, then run it**

Spec:
- Stdin `CALLIT_HOSTNAME=h\nJWT_SECRET=<64hex>\nPOSTGRES_PASSWORD=<48hex>\nDUCKDNS_TOKEN=tok-9\n`
  creates the three items with exactly those values. `CALLIT_HOSTNAME` is
  ignored.
- Stdin missing `POSTGRES_PASSWORD` exits 1 with
  `op-bootstrap: POSTGRES_PASSWORD missing from input`, and nothing is created.
- A value failing Global Constraints validation (for example
  `JWT_SECRET=short`) exits 1, and nothing is created.
- The script never `source`s or `eval`s its input: a line
  `JWT_SECRET=$(touch $FAKE_OP_DIR/pwned)` is rejected by validation and
  `pwned` doesn't exist.

Run: `bash deploy/tests/op-bootstrap_test.sh`
Expected: FAIL (`--import` is an unknown mode)

- [ ] **Step 2: Implement, then verify-and-commit**

```bash
bash deploy/tests/op-bootstrap_test.sh && \
  git add deploy/op-bootstrap.sh deploy/tests/op-bootstrap_test.sh .github/workflows/ci.yml && \
  git commit -m "feat: import the live server's secrets into the vault"
```

---

### Task 6: Host wiring: preflight, the systemd unit, `deploy.sh`, DuckDNS

**Files:**
- Create: `deploy/preflight-secrets.sh`, `deploy/tests/preflight-secrets_test.sh`, `deploy/systemd/callit-secrets.service`, `deploy/install-secrets.sh`
- Modify: `deploy/deploy.sh`, `deploy/duckdns-update.sh`, `.github/workflows/ci.yml` (add a preflight test step)

**Interfaces:**
- Produces: `deploy/preflight-secrets.sh <dir>`, which exits 0 only when
  `<dir>` is on tmpfs (`findmnt -n -o FSTYPE --target`) and holds all four
  secret files as non-empty regular files.
- Unit: `Type=oneshot`, `RemainAfterExit=yes`,
  `After=network-online.target`, `Wants=network-online.target`,
  `Before=docker.service`,
  `LoadCredential=op-token:/etc/callit/op-service-account-token`,
  `ExecStart=/usr/local/libexec/callit-secrets`, `WantedBy=multi-user.target`.
- `install-secrets.sh` (run with sudo) copies `secrets.sh` to
  `/usr/local/libexec/callit-secrets` (root 0755), installs the unit, and
  runs `systemctl daemon-reload && systemctl enable callit-secrets`. It's
  rerun whenever `secrets.sh` changes. The installed copy is root-owned so
  that the root unit doesn't execute a file in a user-writable clone.

**Checkpoint 1: preflight rejects disk-backed or incomplete secret dirs**

- [ ] **Step 1: Write the failing test, then run it**

Spec:
- A dir under `/dev/shm` holding the four non-empty files exits 0.
- The same files in a dir under `deploy/` (disk-backed) exit 1 with
  `preflight: <dir> is not on tmpfs`.
- A tmpfs dir missing `postgres_dsn` exits 1 with
  `preflight: <dir>/postgres_dsn missing or empty`.
- An empty `jwt_secret` exits 1 in the same way.

Run: `bash deploy/tests/preflight-secrets_test.sh`
Expected: FAIL, because the script doesn't exist

- [ ] **Step 2: Implement, wire the host, then verify-and-commit**

Contract:
- After its existing `.env` check, `deploy.sh` runs
  `sudo systemctl restart callit-secrets.service`, reads
  `CALLIT_SECRETS_DIR` out of `.env` (read, never source), and fails the
  deploy unless `preflight-secrets.sh` passes.
- `duckdns-update.sh` reads the token from `$CALLIT_SECRETS_DIR/duckdns_token`
  instead of `.env`.
- The unit and the install script are as in Interfaces. They're verified
  on the VM in the After-merge section, not in CI.

```bash
bash deploy/tests/preflight-secrets_test.sh && bash -n deploy/deploy.sh deploy/duckdns-update.sh deploy/install-secrets.sh && \
  git add deploy/preflight-secrets.sh deploy/tests/preflight-secrets_test.sh deploy/systemd/callit-secrets.service deploy/install-secrets.sh deploy/deploy.sh deploy/duckdns-update.sh .github/workflows/ci.yml && \
  git commit -m "feat: materialize secrets at boot and before every deploy"
```

---

### Task 7: Runbook, project docs, security review

**Files:**
- Modify: `deploy/README.md`, `CLAUDE.md`, `docs/plans/2026-08-21-implementation-plan.md` (§9), `docs/plans/2026-10-18-phase-8b-web-deploy.md`
- Create: a journal entry via the `journal` skill (it picks the dated filename)

- [ ] **Step 1: `deploy/README.md`**
  - Replace §4 with a 1Password section:
    - create the vault;
    - `op-bootstrap.sh --generate` on the laptop;
    - create a Service Account (1Password web, Developer → Service
      Accounts) with **read-only** access to `callit-prod` only;
    - install `op` on the VM (arm64 apt repo);
    - store the token at `/etc/callit/op-service-account-token` (root
      0400, piped from the clipboard, never in shell history);
    - `sudo bash deploy/install-secrets.sh`;
    - move the DuckDNS cron entry to the user's crontab unchanged.
  - Add these sections:
    - **"Migrating an existing server"**: the cutover order from After merge.
    - **"Rebuilding on a new VM"**: secrets come from the vault and the data
      from `backup.sh`'s archive.
    - **"Rotating"**: Decision 6.
    - **"What's still on disk"**: the Service Account token (scoped,
      revocable, audited in 1Password's activity log), the `docker` group
      being root-equivalent, and the gap that 1Password unreachable at boot
      means downtime.
  - Update the local rehearsal to write the four secret files into a
    `/dev/shm` dir and set `CALLIT_SECRETS_DIR` to it. Add a canary check:
    `docker inspect $(docker compose … ps -q) | grep -c <jwt value>` prints `0`.
  - Update the Checks list with the new test scripts.
- [ ] **Step 2: Update the project docs.**
  - `CLAUDE.md`: Build & Test, "needs `JWT_SECRET` (or `JWT_SECRET_FILE`)";
    the Deployment paragraph names the secrets flow and `install-secrets.sh`;
    add an invariant: "production secrets reach containers only as tmpfs
    files; `check-compose.sh` check 12 enforces it".
  - Parent plan §9: insert row 8a.1 between 8a and 8b, and set 8b's
    dependency to 8a.1.
  - 8b plan: replace its `run deploy/gen-env.sh` error-message text and its
    rehearsal `.env` lines with the 8a.1 form, and add a note pointing here.
- [ ] **Step 3: Security review.** Run the `security-reviewer` agent over
  `git diff dev...HEAD` (secrets handling plus a root systemd unit). Fix
  CRITICAL and HIGH findings in their own `fix:` commits, and record the rest
  in the journal entry.
- [ ] **Step 4: Verify and commit**

```bash
make lint && make test && bash deploy/check-compose.sh && \
  for t in deploy/tests/*_test.sh; do bash "$t" || exit 1; done && \
  git add deploy/README.md CLAUDE.md docs/plans/2026-08-21-implementation-plan.md docs/plans/2026-10-18-phase-8b-web-deploy.md && \
  git commit -m "docs: 1Password secrets runbook and Phase 8a.1 project docs"
```

Then write the journal entry with the `journal` skill. The branch is green
and verified: hand off to `finishing-a-development-branch`.

---

## After merge: go-live on the VM (by hand, in this order)

The order matters: the live database password must reach the vault before
anything could replace it.

1. Laptop: `ssh <vm> 'cat ~/call_it/deploy/.env' | bash deploy/op-bootstrap.sh --import`.
   Then confirm the match without printing either value: compare the
   `sha256sum` of `op read --no-newline op://callit-prod/postgres-password/password`
   on the laptop with that of the `POSTGRES_PASSWORD` value on the VM.
2. VM: install `op`, store the Service Account token, run
   `sudo bash deploy/install-secrets.sh` and
   `sudo systemctl start callit-secrets`. Then
   `bash deploy/preflight-secrets.sh /run/callit/secrets` passes.
3. VM: rewrite `deploy/.env` to the two non-secret lines (keep a copy of the
   old file at `/root/env.pre-8a1`, root 0400, until step 6 passes), then
   `bash deploy/deploy.sh <8a.1 tag>`. The smoke test passes, and existing
   accounts can still log in. Existing JWTs stay valid, because the key is
   the same.
4. VM: `docker inspect $(docker compose … ps -q) | grep -ciE 'JWT_SECRET=|POSTGRES_PASSWORD='`
   prints `0`.
5. VM: `sudo reboot`. Afterwards `curl https://<host>/healthz` answers with
   no manual step, which proves the unit ran before Docker restarted the stack.
6. VM: `bash deploy/backup.sh` succeeds. Then `shred -u /root/env.pre-8a1`.
7. Laptop, disaster-recovery rehearsal: restore the newest archive into the
   local rehearsal stack, with its secret files fetched by `secrets.sh`
   into a `/dev/shm` dir (`OP_SERVICE_ACCOUNT_TOKEN` set for that one
   command only, copied from the vault item that stores the token). The ledger worker connects,
   which proves a restore no longer depends on the VM's disk.
8. Journal the go-live with the results of each step.
