# Phase 8f — Production Secrets from 1Password Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `JWT_SECRET` and `POSTGRES_PASSWORD` stop living in a plaintext `deploy/.env` on the VM. They live in a 1Password vault and are injected into Compose at run time by `op run`, authenticated by a read-only Service Account.

**Architecture:** One sourced helper, `deploy/lib/secrets.sh`, gives `deploy.sh`, `backup.sh` and `restore.sh` a single entry point. When the secrets aren't already loaded, it loads the Service Account token from a `0600` file and re-executes the calling script under `op run --env-file deploy/secrets.tpl`. Inside that process the two secrets are ordinary environment variables, which Compose interpolation prefers over `--env-file`. `compose.prod.yml` doesn't change: the env var names are the interface, so older tags still deploy. `deploy/.env` stays, but holds only host config (`CALLIT_HOSTNAME`, `DUCKDNS_TOKEN`). A laptop-side `init-secrets.sh` creates the 1Password item, or imports the live values, without putting a secret in argv or on disk.

**Tech Stack:** Bash · 1Password CLI v2 (`op run`, `op item get`, `op item create -`) · 1Password Service Accounts · Docker Compose · no Go, npm or Compose changes.

**Spec:** [`journal/2026-10-22_1935_ansh_1password-secrets-idea.md`](../../journal/2026-10-22_1935_ansh_1password-secrets-idea.md) (the idea and its open questions) · [`deploy/README.md`](../../deploy/README.md) §4–§8 (the runbook this changes) · parent plan [`2026-08-21-implementation-plan.md`](2026-08-21-implementation-plan.md) §9 row 8a (what 8a delivered)

## Global Constraints

- **No secret value in argv, ever.** 1Password's docs: command arguments "can be visible to other processes on your machine." Items are created by piping a JSON template to `op item create … -`, never with `field=value` assignments.
- **No secret value written to disk** by any script in this phase: no temp file, no `.env` line, no log line.
- **`compose.prod.yml` is unchanged.** It still reads `${JWT_SECRET:?…}` and `${POSTGRES_PASSWORD:?…}`, so `deploy.sh <pre-8f tag>` still works as a rollback.
- **The DuckDNS cron makes zero 1Password calls.** Service-account limits on Individual/Families plans are **1,000 requests per day per account** (1Password docs, "Rate limits", checked 2026-10-22). A 5-minute cron is 288 runs a day before counting references. `DUCKDNS_TOKEN` stays in the host-config `deploy/.env`.
- **The VM's Service Account is created with `read_items` on vault `callit-prod` only.** Service-account permissions are immutable after creation, and built-in Personal/Private vaults can't be granted, so a dedicated vault is required.
- **Fixed names:** vault `callit-prod`, item `callit`, fields `jwt_secret` and `postgres_password`. References: `op://callit-prod/callit/jwt_secret`, `op://callit-prod/callit/postgres_password`.
- **Token file:** `${CALLIT_OP_TOKEN_FILE:-$HOME/.config/callit/op-token}`, must be mode `600` and non-empty.
- **Existing deploy-script conventions hold:** `set -euo pipefail`; read `.env`, never `source` it; `fail()` prints `<script>: <message>` to stderr and exits 1; usage errors exit 2.
- **Live secrets are never regenerated during migration.** A new `POSTGRES_PASSWORD` won't match the existing volume, and a new `JWT_SECRET` logs everyone out. Migration imports the current values.
- **Tests never call the real `op`.** They put `deploy/tests/fake-op` on `PATH` as `op`.

## Review Focus

1. **1Password unreachable at 03:00:** `backup.sh` must fail loudly (non-zero, message in `backup.log`) rather than produce an archive or hang. It inherits `op run`'s exit status. Pinned in Task 1 CP3: the fake `op` exits 1 → the caller exits non-zero and never runs its body.
2. **Half-migrated VM:** `.env` still holds `JWT_SECRET=` and `op run` also supplies one. Two sources must be a hard error, not "whichever wins." Pinned in Task 1 CP3.
3. **`op` missing on PATH** (fresh VM, or a cron with a minimal PATH): clear error naming the CLI, exit 1. Pinned in Task 1 CP3.
4. **Token file readable by others** (copied with the wrong umask): refuse to use it. Pinned in Task 1 CP1.
5. **Import of malformed or partial input** (only one of the two keys, or a value that isn't the expected hex) must create nothing. A half-filled item would make the next deploy fail with a confusing error. Pinned in Task 3 CP2.

---

## File Structure

| File | Responsibility |
|---|---|
| `deploy/lib/secrets.sh` (create) | `require_secrets "$@"`: token checks, the two-sources guard, re-exec under `op run`, and a post-load check that both values are present |
| `deploy/secrets.tpl` (create) | The two `op://` references. Committed; holds no secret |
| `deploy/init-secrets.sh` (create) | Laptop-side: create item `callit` with generated values, or `--import` the live values from stdin |
| `deploy/deploy.sh`, `backup.sh`, `restore.sh` (modify) | Source the lib and call `require_secrets "$@"` before anything else; `deploy.sh`'s ".env missing" message changes |
| `deploy/gen-env.sh` (modify) | Writes host config only: `CALLIT_HOSTNAME`, optional `DUCKDNS_TOKEN` |
| `deploy/tests/fake-op` (create) | Test double for `op run`, `op item get`, `op item create -`, backed by a directory store; logs argv |
| `deploy/tests/secrets_test.sh`, `init-secrets_test.sh` (create); `gen-env_test.sh` (modify) | Tests |
| `.github/workflows/ci.yml` (modify) | Runs the new tests in the `deploy` job |
| `deploy/README.md`, parent plan §9 (modify) | Runbook and the 8f row (Task 4) |

### `deploy/tests/fake-op` contract

Store: `$FAKE_OP_STORE/<vault>/<item>/<field>`, one file per field holding the value. Every invocation appends its argv, one line, to `$FAKE_OP_LOG`.

- `op run --env-file <F> -- <cmd…>`: for each `KEY=op://v/i/f` line in F, export `KEY` with the stored value. A missing field → stderr `fake-op: no such reference op://v/i/f`, exit 1. If `$FAKE_OP_FAIL=1`, it exits 1 with `fake-op: unreachable` before running anything. Otherwise it `exec`s cmd.
- `op item get <item> --vault <v>`: exit 0 if `$FAKE_OP_STORE/v/item` exists, else exit 1.
- `op item create --vault <v> -`: reads a JSON template from stdin and writes each `fields[].label` → `value` into `$FAKE_OP_STORE/v/<title>/`. Uses `python3 -I` for JSON parsing; it's test-only code.

---

### Task 1: The secrets loader

**Files:**
- Create: `deploy/lib/secrets.sh`, `deploy/tests/fake-op`, `deploy/tests/secrets_test.sh`
- Modify: `.github/workflows/ci.yml` (add `bash deploy/tests/secrets_test.sh` to the `deploy` job, after "Test secret generation")

**Interfaces:**
- Produces: `require_secrets "$@"`, called as the first statement after `here=…` in a script that has done `. "$here/lib/secrets.sh"`. It returns only once `JWT_SECRET` and `POSTGRES_PASSWORD` are non-empty in the environment. Otherwise it re-execs the caller (`exec … op run --env-file "$here/secrets.tpl" -- bash "$0" "$@"`) or exits 1. It reads `CALLIT_OP_TOKEN_FILE` (default above) and sets `CALLIT_SECRETS_LOADED=1` across the re-exec.
- The test harness copies `deploy/` to a temp dir (same `fresh_copy` pattern as `gen-env_test.sh`) and writes a 3-line probe script into the copy that sources the lib, calls `require_secrets "$@"`, and prints `jwt=${#JWT_SECRET} pg=${#POSTGRES_PASSWORD}` (lengths only, never values).

**Checkpoint 1: refuses a bad token file**

- [ ] **Step 1: Write the failing test, then run it**

Spec, each case run against the probe with `fake-op` on PATH and a valid store:
- token file absent → exit 1, stderr contains `secrets: no service-account token at <path>`
- token file mode `644` → exit 1, stderr contains `must be mode 600`
- token file mode `600` but empty → exit 1, stderr contains `is empty`
- in all three, `$FAKE_OP_LOG` has no `run` line (op never called)

Run: `bash deploy/tests/secrets_test.sh`
Expected: FAIL. `deploy/lib/secrets.sh` doesn't exist, so sourcing it fails.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `require_secrets` checks existence, then `stat -c %a` = `600`, then non-empty, in that order, and exports the token as `OP_SERVICE_ACCOUNT_TOKEN` only for the `op` child (`env OP_SERVICE_ACCOUNT_TOKEN=… op run …`).

```bash
bash deploy/tests/secrets_test.sh && \
  git add deploy/lib/secrets.sh deploy/tests/fake-op deploy/tests/secrets_test.sh .github/workflows/ci.yml && \
  git commit -m "feat: refuse a missing, empty or world-readable 1Password token file"
```

Expected: PASS, then one commit.

**Checkpoint 2: loads both secrets through `op run` without writing them anywhere**

- [ ] **Step 1: Write the failing test, then run it**

Spec: store holds `callit-prod/callit/jwt_secret` = 64 `a`s and `postgres_password` = 48 `b`s; valid token file; host `.env` has only `CALLIT_HOSTNAME=x.example.invalid`.
- The probe prints exactly `jwt=64 pg=48` and exits 0.
- `$FAKE_OP_LOG` has exactly one `run --env-file <copy>/secrets.tpl -- …` line.
- `grep -rF "$(printf 'a%.0s' {1..64})" <copy>` finds nothing (no file under the copy holds the value).
- No line of `$FAKE_OP_LOG` contains either value or the token.

Run: `bash deploy/tests/secrets_test.sh`
Expected: FAIL. `deploy/secrets.tpl` doesn't exist, so fake-op errors on the missing env file.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: create `deploy/secrets.tpl` with the two references from Global Constraints and a header comment saying it holds references, not secrets. `require_secrets` re-execs under `op run` when `CALLIT_SECRETS_LOADED` is unset. In the re-exec'd process it returns after checking both vars are non-empty and don't start with `op://`; otherwise exit 1 with `secrets: JWT_SECRET was not resolved` (or the `POSTGRES_PASSWORD` equivalent).

```bash
bash deploy/tests/secrets_test.sh && \
  git add deploy/lib/secrets.sh deploy/secrets.tpl deploy/tests/secrets_test.sh && \
  git commit -m "feat: load JWT_SECRET and POSTGRES_PASSWORD from 1Password with op run"
```

Expected: PASS, then one commit.

**Checkpoint 3: hard errors for a half-migrated host, a missing CLI, and an unreachable 1Password**

- [ ] **Step 1: Write the failing test, then run it**

Spec:
- host `.env` contains a `JWT_SECRET=` line (or a `POSTGRES_PASSWORD=` line) → exit 1, stderr contains `deploy/.env still holds JWT_SECRET; finish the 8f migration (deploy/README.md §4)`; no `run` line in the log
- `PATH` without any `op` → exit 1, stderr contains `1Password CLI (op) not found`
- `FAKE_OP_FAIL=1` → exit non-zero, and the probe's `jwt=` line is never printed

Run: `bash deploy/tests/secrets_test.sh`
Expected: FAIL on the first case. Without the guard, the script runs and prints `jwt=64`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: both guards run before the token checks (`grep -q '^JWT_SECRET=\|^POSTGRES_PASSWORD='` on `$here/.env` if it exists; `command -v op`). The unreachable case needs no new code beyond `exec` propagating `op`'s status, but the test must exist.

```bash
bash deploy/tests/secrets_test.sh && \
  git add deploy/lib/secrets.sh deploy/tests/secrets_test.sh && \
  git commit -m "feat: refuse two sources of a secret and a missing op CLI"
```

Expected: PASS, then one commit.

---

### Task 2: Wire the loader into the deploy scripts; `.env` becomes host config

**Files:**
- Modify: `deploy/deploy.sh`, `deploy/backup.sh`, `deploy/restore.sh`, `deploy/gen-env.sh`, `deploy/tests/gen-env_test.sh`, `deploy/tests/secrets_test.sh`

**Interfaces:**
- Consumes: `require_secrets "$@"` and `deploy/secrets.tpl` (Task 1).
- Produces: `gen-env.sh <hostname> [duckdns-token]`, same usage and validation, writing only `CALLIT_HOSTNAME` and optional `DUCKDNS_TOKEN`.

**Checkpoint 1: `gen-env.sh` writes host config only**

- [ ] **Step 1: Write the failing test, then run it**

Spec: change `gen-env_test.sh`'s first case. After `gen-env.sh callit.duckdns.org`, the file is mode `600`, has `CALLIT_HOSTNAME=callit.duckdns.org`, and has **no** `JWT_SECRET=` or `POSTGRES_PASSWORD=` line. Delete the "secrets differ between installs" case. Replace the final "renders compose" case: under `fake-op` with the Task 1 store, run `op run --env-file <copy>/secrets.tpl -- docker compose -f <copy>/compose.prod.yml --env-file <copy>/.env config -q`, which must exit 0. Refusal, usage and validation cases stay as they are.

Run: `bash deploy/tests/gen-env_test.sh`
Expected: FAIL with `JWT_SECRET written to host config` (the new assertion's message).

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: drop the two `openssl rand` lines and update the header comment. The refusal reason is now "would discard the hostname and DuckDNS token", since secrets aren't in the file any more.

```bash
bash deploy/tests/gen-env_test.sh && \
  git add deploy/gen-env.sh deploy/tests/gen-env_test.sh && \
  git commit -m "feat: gen-env writes host config only; secrets come from 1Password"
```

Expected: PASS, then one commit.

**Checkpoint 2: every script that runs Compose loads secrets first**

- [ ] **Step 1: Write the failing test, then run it**

Spec: add a case to `secrets_test.sh`. For each of `deploy.sh`, `backup.sh`, `restore.sh`, the first non-comment line after `here=…` sources `lib/secrets.sh`, and the next is `require_secrets "$@"`. Both must come before the first `dc`, `git` or `[ -f` line. Check it with an `awk` over the file and report `<script>: require_secrets missing or after first use`.

Run: `bash deploy/tests/secrets_test.sh`
Expected: FAIL naming `deploy.sh`.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract:
- Add the two lines to each script.
- In `deploy.sh`, `[ -f "$here/.env" ]`'s message becomes `deploy/.env missing — run deploy/gen-env.sh first (host config only)`.
- `restore.sh`'s `--yes` parsing still sees the original `"$@"`, because the re-exec passes args through unchanged.
- No other behaviour changes.

```bash
bash deploy/tests/secrets_test.sh && bash deploy/check-compose.sh >/dev/null && \
  git add deploy/deploy.sh deploy/backup.sh deploy/restore.sh deploy/tests/secrets_test.sh && \
  git commit -m "feat: deploy, backup and restore load secrets through op run"
```

Expected: PASS, then one commit.

---

### Task 3: `init-secrets.sh`, creating or importing the 1Password item from the laptop

**Files:**
- Create: `deploy/init-secrets.sh`, `deploy/tests/init-secrets_test.sh`
- Modify: `.github/workflows/ci.yml` (add the test after `secrets_test.sh`)

**Interfaces:**
- Consumes: `fake-op` (Task 1).
- Produces: `deploy/init-secrets.sh` (generate) and `deploy/init-secrets.sh --import` (read `KEY=VALUE` lines from stdin). Exit 0 on create; 1 if item `callit` already exists in `callit-prod`; 2 on usage or validation errors. The item's JSON template, piped to `op item create --vault callit-prod -`, is `{"title":"callit","category":"API_CREDENTIAL","fields":[{"id":"jwt_secret","label":"jwt_secret","type":"CONCEALED","value":…},{"id":"postgres_password","label":"postgres_password","type":"CONCEALED","value":…}]}`.

**Checkpoint 1: generate mode creates the item, refuses a second run, and keeps values out of argv**

- [ ] **Step 1: Write the failing test, then run it**

Spec:
- Empty store → exit 0. The store now has `callit-prod/callit/jwt_secret` matching `^[0-9a-f]{64}$` and `postgres_password` matching `^[0-9a-f]{48}$`.
- No `$FAKE_OP_LOG` line contains either value.
- Run again → exit 1, stderr `init-secrets: item callit already exists in callit-prod; refusing to overwrite`, store byte-identical (compare `sha256sum` of both files).

Run: `bash deploy/tests/init-secrets_test.sh`
Expected: FAIL. `deploy/init-secrets.sh` doesn't exist.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: check existence with `op item get callit --vault callit-prod`. Generate with `openssl rand -hex 32` / `-hex 24` into shell variables, `printf` the template (hex values need no JSON escaping) straight into `op item create --vault callit-prod -`. Nothing touches a file.

```bash
bash deploy/tests/init-secrets_test.sh && \
  git add deploy/init-secrets.sh deploy/tests/init-secrets_test.sh .github/workflows/ci.yml && \
  git commit -m "feat: init-secrets creates the 1Password item without exposing values"
```

Expected: PASS, then one commit.

**Checkpoint 2: `--import` carries the live values over unchanged, or creates nothing**

- [ ] **Step 1: Write the failing test, then run it**

Spec:
- stdin `JWT_SECRET=<64 hex>\nPOSTGRES_PASSWORD=<48 hex>\n` → exit 0, and the store holds exactly those two values. Other keys on stdin (`CALLIT_HOSTNAME=…`) are ignored.
- Only one of the two keys → exit 2, stderr `init-secrets: --import needs both JWT_SECRET and POSTGRES_PASSWORD`, store empty
- `JWT_SECRET` that isn't 64 lowercase hex (e.g. `abc`, or a value containing `"`) → exit 2, stderr `init-secrets: JWT_SECRET is not 64 lowercase hex chars`, store empty. The `POSTGRES_PASSWORD` rule is the same, with 48.
- No log line contains a value in any case.

Run: `bash deploy/tests/init-secrets_test.sh`
Expected: FAIL. `--import` is treated as an unknown argument (exit 2 with a usage message), and the store stays empty.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: read stdin with `while IFS= read -r line`, match `^JWT_SECRET=` / `^POSTGRES_PASSWORD=`, and validate both before any `op` call. The existence check and the create path are shared with generate mode.

```bash
bash deploy/tests/init-secrets_test.sh && \
  git add deploy/init-secrets.sh deploy/tests/init-secrets_test.sh && \
  git commit -m "feat: init-secrets --import moves the live secrets into 1Password unchanged"
```

Expected: PASS, then one commit.

---

### Task 4: Runbook, parent plan row, and phase close

**Files:**
- Modify: `deploy/README.md`, `docs/plans/2026-08-21-implementation-plan.md` §9, `CLAUDE.md` (the "Deployment (Phase 8a)" paragraph: one sentence on 8f)

No checkpoint: documentation only. One commit at the end of the task.

- [ ] Update the README file table: `gen-env.sh` (host config), `init-secrets.sh`, `secrets.tpl`, `lib/secrets.sh`. Replace "`deploy/.env` holds every secret" with where each value now lives.
- [ ] Rewrite README §4 as: (1) DuckDNS, unchanged; (2) on the laptop, create vault `callit-prod` and run `bash deploy/init-secrets.sh`; (3) create a Service Account with `read_items` on `callit-prod` only, and save its token on the VM with `install -m 600 /dev/stdin ~/.config/callit/op-token` (paste the token into the terminal; it never goes in shell history or argv); (4) install the CLI from 1Password's apt repo (the arm64 build is picked automatically); (5) `bash deploy/gen-env.sh <host> <duckdns-token>`.
- [ ] Add README §4a, **Migrating a live 8a server** (in this order, with no step regenerating a secret):
  1. Laptop: `ssh <vm> "grep -E '^(JWT_SECRET|POSTGRES_PASSWORD)=' ~/call_it/deploy/.env" | bash deploy/init-secrets.sh --import`
  2. VM: install `op`; write the token file as in §4.
  3. VM: `cp -p deploy/.env ~/.env.pre-8f` (mode preserved), then delete the two secret lines from `deploy/.env`.
  4. VM: `bash deploy/deploy.sh <8f tag>`. The smoke test must pass.
  5. VM: `bash deploy/backup.sh`, which must write an archive.
  6. VM: `op service-account ratelimit` to see actual usage against the 1,000/day budget.
  7. Only then, `shred -u ~/.env.pre-8f`.
  Rollback: `deploy.sh <pre-8f tag>` still works, because the env var names didn't change.
- [ ] README §6: the backup cron line is unchanged. Add a note that it now needs 1Password reachable at 03:00, and that a failure shows in `backup.log`.
- [ ] Document the tradeoff in README: the token file is still a long-lived credential on the VM. The gain is one revocable, read-only, single-vault, audited credential instead of every secret in a file. `JWT_SECRET` rotation still logs everyone out, and that's unchanged by this phase.
- [ ] Parent plan §9: add row `8f | **Production secrets in 1Password** ✅ | …` with dependency `8a`, plus a one-paragraph note under the Phase 8 split notes explaining why it's out of sequence (it depends only on 8a, and it's independent of 8b–8e).
- [ ] Run the full deploy suite once, then run `security-reviewer`. This phase changes how production secrets are handled, which is the same trigger class as auth.

```bash
bash deploy/check-compose.sh >/dev/null && bash deploy/tests/check-compose_test.sh && \
  bash deploy/tests/gen-env_test.sh && bash deploy/tests/secrets_test.sh && \
  bash deploy/tests/init-secrets_test.sh && \
  git add deploy/README.md docs/plans/2026-08-21-implementation-plan.md CLAUDE.md && \
  git commit -m "docs: runbook and plan row for Phase 8f (secrets from 1Password)"
```

The branch is green and verified here. Merge, the go-live migration (§4a) and the journal entry come after, as in 8a.

---

## Decisions This Plan Fixes

1. **`op run` at the process boundary, not the Go SDK.** Compose already consumes env vars; the SDK would add a dependency and startup code for no gain.
2. **DuckDNS stays local.** This is the rate-limit constraint above. It's also the lowest-value secret: it can repoint DNS, but it can't read data.
3. **Re-exec instead of wrapping each `dc` call.** One `op run` per script run (one request burst) instead of one per Compose call. `backup.sh` alone calls `dc` six or more times.
4. **Hard error on two sources** instead of documenting precedence. Compose's shell-over-`--env-file` precedence would make a stale `.env` line silently lose, until the day someone runs Compose by hand without `op run` and gets the old value.
5. **Fixed vault and item names**, not configurable. There's one deployment; making them configurable is speculative.

## Out of Scope

- `JWT_SECRET` rotation without logging everyone out (needs two-key verification in `internal/auth`).
- 1Password in CI. No CI job needs a real secret today.
- The 1Password SSH agent for VM access: personal tooling, nothing in the repo changes.
