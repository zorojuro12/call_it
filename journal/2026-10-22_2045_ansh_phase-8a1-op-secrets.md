# 2026-10-22 — ansh — Phase 8a.1: production secrets from 1Password (planned and built, not merged)

**Status:** **`v0.3.0` is live on the VM with secrets from 1Password.** Merged to `dev` (`5a72a88`) and `main` (`11f126c`), tagged and pushed. **The live switch-over is complete:** `smoke.sh` 4/4, no secret in `docker inspect`, the reboot test passed, `backup.sh` works, and the old plaintext copy was shredded. The only secret on the VM's disk is the Service Account token. Security review: no CRITICAL/HIGH, and all MEDIUMs fixed. The competing `op run` plan (`cd69502`) was deleted at the user's request.
**Decided:** Secrets reach containers as **file secrets bind-mounted from tmpfs**, read through `*_FILE`, not as env vars via `op run`. Docker persists container env in `config.v2.json` and `docker inspect`, which the spike proved (the canary appeared 1× as an env var, 0× as a file). See the plan's Decisions 1–7.
**Spec:** No change. The journal idea's "`op run`, no backend change" decision is withdrawn (plan Decision 1).
**Next:** Plan the Kubernetes phase the user wants (see Next Step).
**Blocked on:** Nothing. The full-stack local rehearsal passed after this entry was first written (see What Worked), using an env file outside the repo instead of a scratch `deploy/.env`.
**Touches:** `deploy/{secrets,op-bootstrap,preflight-secrets,install-secrets,gen-env,deploy,duckdns-update,check-compose,check-image}.sh`, `deploy/systemd/`, `deploy/compose.prod.yml`, `deploy/tests/`, `deploy/README.md`, `backend/internal/config/secret_files*.go`, `backend/cmd/{api,migrate,ledger-worker}/main.go`, `.github/workflows/ci.yml`, `CLAUDE.md`, `docs/plans/2026-10-22-phase-8a1-op-secrets.md`

---

## What We Worked On
Turned the 1Password idea (previous entry) into a phase, mainly as portfolio evidence for the 1Password internship applications (Vault, Machine Identity). It was inserted ahead of 8b as **8a.1**, so nothing else had to be renumbered. Set up on the user's side:
- `op` 2.41.1 on the laptop (WSL, apt), signed in as `my.1password.com`;
- `op` 2.41.1 on the VM (arm64 apt repo, installed by the spike script);
- the `callit-prod` vault, possibly with its Service Account (the user was walked through creating both; unconfirmed whether they finished).

No live secrets have been touched. The VM's `deploy/.env` and the running stack are unchanged.

## Decisions Made
- **Files on tmpfs, not `op run`.** Plan Decision 1. This is the main point of difference from the competing 8f plan, which keeps `compose.prod.yml` unchanged and injects env vars with `op run`. That's simpler and allows rollback to pre-8f tags, but puts the values back in Docker's on-disk config.
- **The `_FILE` convention, with both set being an error.** Plan Decision 2.
- **`root:docker 0750` dir, `0444` files.** Plan Decision 3. Non-swarm Compose ignores `mode`/`uid` on file secrets, and the containers run as 65532 and as 70 (Postgres).
- **The root unit runs a root-owned copy of `secrets.sh`** at `/usr/local/libexec/callit-secrets`, never the file in the user-writable clone.
- **Mid-session fix: `env_get` in `deploy.sh` tolerates a missing key.** Under `pipefail`, a `grep` miss exited the script silently, so its "missing from `.env`" messages could never print. The old hostname line had the same flaw.
- **The check-12 grant mutant gives `relay` the DSN, not `api`.** A second `secrets:` key on `api` would be a duplicate YAML key, not a valid mutant.

## What Worked
- **Spike, recorded in the plan's "Spike Results" section:**
  - The secret file is readable as uid 65532 and uid 70.
  - The canary appears 0× in `docker inspect`, and 0× in `config.v2.json` on the VM.
  - On the VM's native engine, a missing bind source fails closed for both `docker start` and `compose up`.
  - `op read` adds `\n` unless `--no-newline` is passed.
  - `op item create --vault V -` accepts a JSON template on stdin.
- **Every checkpoint went RED, then GREEN.** Two partial REDs were expected and noted in commits. The unreadable-file case was already covered by Checkpoint 1's error wrapping. The failed-second-read case was already covered because `secrets.sh` fetches everything before it writes.
- **`check-image.sh` proves all three binaries read `*_FILE`.**
- **`systemd-analyze verify` accepts the unit.**
- **The full-stack local rehearsal passed on file secrets** (Docker Desktop; `-p callit-rehearsal`, env file outside the repo):
  - all 8 containers came up healthy;
  - `migrate` applied the schema with the DSN file (exit 0);
  - `smoke.sh` steps 1, 2 and 4 passed: a full round over `wss://` with the file-based JWT key;
  - the ledger went 0 → 2 transactions, so the ledger worker connected using the DSN file;
  - each secret value appears 0× in `docker inspect`, with only the `*_FILE` pointers present.
- **The old Phase 8a rehearsal stack was paused, not deleted.** `docker compose -p callit-prod down` kept its 5 volumes, and its old-format `deploy/.env` and `.caddy-root.crt` are untouched in the repo. Bring it back from `dev`'s compose file, because this branch's needs `CALLIT_SECRETS_DIR`.

## What Didn't Work
- **Docker Desktop (the laptop) behaves differently from the VM when a bind source is missing:** `compose up` creates an empty root-owned directory at the path and starts the container anyway. That's why `secrets.sh` refuses a non-file at a secret's path (Task 3, Checkpoint 4). Don't treat Desktop results as proof of the VM's behaviour.
- **`preflight-secrets_test.sh` leaked two `deploy/preflight-test.*` dirs into the repo tree.** `cleanup+=` runs inside `$(filled …)`, a subshell, so the trap never sees those dirs. They held only fake `x` values and were deleted. The test itself still needs fixing. `secrets_test.sh` and `op-bootstrap_test.sh` use the same `setup()` pattern and leak under `/tmp` the same way, as did `gen-env_test.sh` before this phase.
- **The first attempt to run the local rehearsal was denied** (it reads and writes `deploy/.env`). It's still unrun.

## Test Coverage
- **Covered:**
  - `internal/config` at 93.2%: passthrough, the newline trim, conflict, empty path, empty content, unreadable file, and no content in any error;
  - `secrets.sh`: full set, all-or-nothing, no values in output, token from the credential or env and never from argv, and the non-file refusal;
  - `op-bootstrap.sh`: generate, never overwrite, exact import, never source its input;
  - preflight: tmpfs, completeness;
  - `check-compose.sh` check 12, with 3 mutants.
- **Not covered yet:**
  - the unit and `install-secrets.sh` on a real host;
  - `deploy.sh` and `duckdns-update.sh`, which have no tests, as before;
  - the `chgrp docker` branch in `secrets.sh`, which only runs as root.

## Open Questions / Blockers
- **Resolved: two plans for one feature.** `8a.1` (this branch: tmpfs files plus a Go change) versus `8f` (`op run` env injection, with no Go or Compose change and DuckDNS staying in `.env`). The user chose 8a.1 and had the 8f plan deleted. It's recoverable from `cd69502`.
- **Security review MEDIUMs, all fixed after this entry was first written:**
  1. **First boot fails.** `secrets.sh` uses `umask 077`, so `mkdir -p` creates `/run/callit` as root `0700`. The deploy user then can't traverse it, so preflight fails and the DuckDNS cron fails silently. Fix: `RuntimeDirectory=callit` with mode `0750` in the unit, or `install -d`.
  2. **`deploy.sh` trusts `.env`'s `CALLIT_SECRETS_DIR`.** A rehearsal `/dev/shm` dir on the server would pass preflight. Fix: pin it to `/run/callit/secrets`.
  3. **The four writes aren't atomic as a set, and nothing traps away leftover `.tmp.*` files.**
- **Security review LOWs:**
  - the DuckDNS token is in `curl`'s argv (pre-existing);
  - `--generate <token>` puts the token in argv on the laptop;
  - `op item get` treats any error as "absent";
  - there's no hardening on the root unit, and no owner/mode check on the token file;
  - the README's "What's still on disk" section is missing: swap, `/root/env.pre-8a1`, old container configs, and import transcripts.
- **The 8f plan raises the Service Account rate limit (1,000 requests/day on Individual/Families).** This branch makes 3 `op` calls per boot or deploy, and the DuckDNS cron makes none, so it's well clear of the limit.
- **Unconfirmed: did the user finish creating the `callit-prod` vault and the `callit-prod-vm` Service Account?**

## Go-live (2026-10-22, complete)
- **Released:** `dev` merged into `main` (`11f126c`), tagged **`v0.3.0`**, pushed; CI green.
- **Step 1, import:** `ssh callit 'cat …/.env' | op-bootstrap.sh --import` created all three items. The vault's `postgres-password` and the VM's `.env` have **identical sha256**.
- **Step 2, VM access:**
  - The user created Service Account `callit-prod-vm` (read-only, `callit-prod` only).
  - Its token is at `/etc/callit/op-service-account-token`.
  - The unit was installed from a `git archive v0.3.0 deploy` copy in `/tmp`, so the checkout stayed on `v0.2.0` and the nightly backup cron kept working meanwhile.
  - Preflight passed on the VM.
- **Step 3, switch:**
  - The old `.env` is kept as root `0400` at `/root/env.pre-8a1`.
  - The new two-line `.env` was written directly.
  - `deploy.sh v0.3.0` passed `smoke.sh` 4/4, with the ledger at 2 → 3.
- **Step 4:** `docker inspect` shows 0 secret matches, and only the `*_FILE` pointers are present.
- **Step 5:** after `sudo reboot`, `/healthz` answered with no manual step, and `callit-secrets.service` was `active (exited)` with status `0/SUCCESS` from boot.
- **Step 6:** `backup.sh` wrote a `0600` archive under the new `.env`, then `/root/env.pre-8a1` was shredded.
- **Found during the run, and fixed on `dev` after the tag:**
  - `/usr/local/libexec` is missing on stock Ubuntu (`c30f575`). The VM was worked around with `install -d`.
  - The token prompt needs `bash`, not `sh`/dash (`e002ded`).
  - The runbook's move-over steps ran the old tag's `gen-env.sh`, which writes fresh secrets (`472a2c8`).
- **Docs:** the root `README.md` (Deployment, Security posture, `*_FILE`) and `docs/project-history.md` now have a Phase 8a.1 record.

## Relevant Commits
- `8e3d11c` plan · `cb041b0` spike results
- `6637395`, `398d2b8`, `c38c067`: `*_FILE` in `internal/config` and the three mains
- `68cc937`, `4cccd78`, `e467c24`, `d49208f`: `secrets.sh`
- `4559446`, `d949ff0`: Compose file secrets with check 12, and `gen-env.sh` without secrets
- `88b89d3`, `f3e2943`: `op-bootstrap.sh`
- `ba878c2`: unit, installer, preflight, `deploy.sh`/DuckDNS wiring
- `7528d07`: runbook and project docs
- `cd69502`: **not this session's work.** The competing Phase 8f plan, deleted on this branch.

## Next Step
Plan the **Kubernetes phase** the user wants: kind cluster, Helm for Postgres/Redis/Kafka, and manifests for api/relay/ledger-worker plus a migrate Job. Its flagship evidence is the k6 load test while killing the relay, worker and Kafka pods, with every bet still reconciling. Mount Kubernetes Secrets as files (`*_FILE`), not env, to keep the 8a.1 invariant. It needs a §9 row and a `writing-plans` pass first.
