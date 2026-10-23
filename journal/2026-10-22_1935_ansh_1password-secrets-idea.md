# 2026-10-22 — ansh — Idea: production secrets from 1Password instead of a plaintext .env

**Status:** Idea only. No plan file, no branch, no code. Recorded so it isn't lost; nothing in `deploy/` has changed.
**Decided:** If this goes ahead, use a 1Password Service Account plus `op run` at deploy time, not the Go SDK. The secrets are only ever read as env vars by Compose, so injecting them at the process boundary needs no backend code change.
**Spec:** No change.
**Next:** If picked up, write a phase plan with `writing-plans` and add a row to the parent plan §9 (after 8b/8c, which are already queued).
**Blocked on:** Nothing technical. Needs a 1Password account with Service Accounts, and a vault created by hand.
**Touches:** `deploy/gen-env.sh`, `deploy/compose.prod.yml`, `deploy/deploy.sh`, `deploy/backup.sh`, `deploy/restore.sh`, `deploy/README.md`, `.github/workflows/ci.yml`

---

## What We Worked On
Came up while preparing 1Password internship applications (Vault, Machine Identity). `deploy/gen-env.sh` generates `JWT_SECRET`, `POSTGRES_PASSWORD` and the DuckDNS token with `openssl rand` and writes them once to a plaintext `deploy/.env` on the Oracle VM (`umask 077`, noclobber, refuses to overwrite). That's sound for one host, but:
- the secrets sit on disk in plaintext, with no audit of reads;
- rotating one means SSH and a hand edit;
- `.env` lives only on the VM. `backup.sh` archives Redis and PostgreSQL but not the secrets, and `gen-env.sh`'s own comment says a regenerated `POSTGRES_PASSWORD` won't match the existing volume. So losing the VM loses the password a restore needs.

## Rough shape (not a plan)
- Store the secrets as items in a dedicated `callit-prod` vault.
- Replace `deploy/.env` with a committed `deploy/.env.tpl` holding `op://callit-prod/...` references. Non-secret values (hostname, `TRUSTED_PROXIES`) can stay plain.
- `deploy.sh` runs Compose under `op run --env-file deploy/.env.tpl -- docker compose ...`, so values exist only in the process environment.
- A read-only Service Account scoped to that one vault; its token is the only secret left on the VM.
- `gen-env.sh` becomes a one-time "create the items in 1Password" step, or goes away.
- Optional: `1password/load-secrets-action` in CI if a job ever needs a real secret (none does today), and the 1Password SSH agent for VM access.

## Decisions Made
- **`op run`, not the Go SDK** — reason: Compose already reads these as env vars; the SDK would add a dependency and startup code for no gain.
- **Doesn't change the Kafka/network invariants** — this is about where secrets live, not who can reach which store.

## Open Questions / Blockers
- Honest tradeoff to keep in the write-up: the Service Account token is still a long-lived credential on the VM. The gain is one scoped, revocable, audited credential instead of every secret in a file. Not "no secrets on disk."
- How `deploy/tests/gen-env_test.sh` and the CI `deploy` job change if `gen-env.sh` is retired.
- Whether `op` runs on the Ampere arm64 VM as-is (check the arm64 build of the CLI).
- Rotating `JWT_SECRET` still logs everyone out. Decide whether that's acceptable or whether to support two keys during a rotation window.
