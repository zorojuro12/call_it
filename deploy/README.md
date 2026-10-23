# Deploying CallIt

The backend runs on one Linux VM with Docker Compose: Caddy terminates TLS
and is the only thing reachable from the internet, and Redis, PostgreSQL
and Kafka sit on internal networks that only the processes needing them
can reach. Design and reasoning: [`docs/plans/2026-10-17-phase-8a-backend-deploy.md`](../docs/plans/2026-10-17-phase-8a-backend-deploy.md).

| File | What it is |
|---|---|
| `compose.prod.yml` | The production topology. `check-compose.sh` enforces its invariants in CI. |
| `Caddyfile` | TLS, HSTS, routing (`/api/*`, `/healthz`; everything else 404 until Phase 8b). |
| `gen-env.sh` | Creates `deploy/.env`: the hostname and the secrets dir. No secrets. Run once per server. |
| `op-bootstrap.sh` | On your laptop: creates the secrets in the `callit-prod` 1Password vault, fresh or imported from an existing server. |
| `secrets.sh` | Fetches the secrets from 1Password into `/run/callit/secrets` (tmpfs). Run by `callit-secrets.service`. |
| `install-secrets.sh` | Installs `systemd/callit-secrets.service` and a root-owned copy of `secrets.sh`. |
| `preflight-secrets.sh` | `deploy.sh` runs it before starting anything: secrets complete, and on tmpfs. |
| `deploy.sh <ref>` | Deploys a git tag: checkout, build, migrate, start, smoke test. |
| `smoke.sh <host>` | Health check, a full round over `wss://`, ledger check, spoofed-header check. |
| `backup.sh` / `restore.sh` | Both money stores (Redis and PostgreSQL) into one archive, and back. |
| `duckdns-update.sh` | Keeps the DuckDNS record on the VM's IP (cron). |

**Secrets live in 1Password, not on the VM's disk.** The `callit-prod`
vault holds three items: `jwt-secret`, `postgres-password` and
`duckdns-token`. At boot and before every deploy, `callit-secrets.service`
fetches them with a read-only Service Account and writes one file per
secret into `/run/callit/secrets`, which lives in RAM. Compose mounts each
file into only the containers that need it (`check-compose.sh` check 12),
and the binaries read them through `*_FILE` variables. No secret is ever a
container environment variable, so none ends up in Docker's on-disk
container config or in `docker inspect`. Design: [`docs/plans/2026-10-22-phase-8a1-op-secrets.md`](../docs/plans/2026-10-22-phase-8a1-op-secrets.md).

`deploy/.env` now holds only `CALLIT_HOSTNAME` and `CALLIT_SECRETS_DIR`.
It's ignored by git, but it isn't secret.

---

## 1. Create the VM

Oracle Cloud Always Free (as of 2026-10, per Oracle's docs: Ampere A1, up to
2 OCPU / 12 GB in total, 200 GB of block storage, 10 TB/month outbound):

1. Console → **Compute → Instances → Create instance**.
2. Shape **`VM.Standard.A1.Flex`**, 2 OCPU, 12 GB memory.
3. Image **Ubuntu 24.04** (the aarch64 build).
4. Create it in your tenancy's **home region**. Always Free resources only
   exist there.
5. Add your SSH public key, and note the instance's public IP.

If creation fails with "out of capacity", free ARM capacity in that region
is exhausted for now. Retry later, or in another availability domain.

## 2. Open the firewall

1. On the instance's subnet, edit its **security list** (VCN → Security
   Lists) to allow ingress on TCP **22, 80, 443** only. Port 80 must stay
   open: it's how Let's Encrypt validates the certificate (HTTP-01), and
   where Caddy redirects to HTTPS.
2. Oracle's Ubuntu images can also ship host `iptables` rules that reject
   inbound traffic. After step 5 below, check from your laptop with
   `curl -I http://<public-ip>`. If it times out, inspect the host rules
   with `sudo iptables -L INPUT -n --line-numbers` and allow 80 and 443.

## 3. Install Docker, the 1Password CLI, and clone

```bash
# Docker Engine from Docker's official apt repository:
# https://docs.docker.com/engine/install/ubuntu/
sudo usermod -aG docker "$USER"   # then log out and back in
# 1Password CLI from 1Password's signed apt repository, choosing the
# **arm64** repository line (the VM is aarch64):
# https://developer.1password.com/docs/cli/get-started/  →  Linux → APT
op --version
git clone https://github.com/zorojuro12/call_it.git ~/call_it
cd ~/call_it
```

You also need the CLI on your laptop, signed in as yourself
(`op account add`, then `eval $(op signin)`).

## 4. DuckDNS and secrets

Moving a server that already runs with secrets in `deploy/.env`? Don't
follow this section: use [Moving an existing server to 1Password](#moving-an-existing-server-to-1password).
Generating new secrets for it would lock you out of its database.

1. Sign in at <https://www.duckdns.org>, create a subdomain (e.g.
   `callit`), and copy your token from the top of the page.
2. **On your laptop**, create a vault named exactly `callit-prod`, then the
   secrets. The script refuses to overwrite an item that already exists:
   ```bash
   bash deploy/op-bootstrap.sh --generate <duckdns-token>
   ```
3. In 1Password on the web, under **Developer → Service Accounts**, create
   `callit-prod-vm` with **read-only** access to `callit-prod` and nothing
   else. A Service Account's vault access can't be changed after it's
   created. The token is shown once: save it in your personal vault.
4. **On the VM**, store the token where only root can read it. Paste it
   at the prompt, so it never lands in your shell history:
   ```bash
   sudo install -d -m 0700 /etc/callit
   sudo bash -c 'umask 077; read -rsp "token: " t; printf %s "$t" > /etc/callit/op-service-account-token'; echo
   sudo bash deploy/install-secrets.sh
   sudo systemctl start callit-secrets
   bash deploy/gen-env.sh callit.duckdns.org
   bash deploy/preflight-secrets.sh /run/callit/secrets
   ```
   If `systemctl start` fails, `journalctl -u callit-secrets` says why.
   Rerun `install-secrets.sh` whenever a new tag changes `deploy/secrets.sh`
   or the unit.
5. Point the name at this VM, and keep it pointed:
   ```bash
   bash deploy/duckdns-update.sh        # silent on success
   crontab -e
   # */5 * * * * cd ~/call_it && bash deploy/duckdns-update.sh
   ```

## 5. First deploy

```bash
bash deploy/deploy.sh v0.2.0
```

This checks out the tag, builds the image natively on the VM, starts the
stack (`migrate` runs before the ledger worker starts), and runs
`smoke.sh`. On first start Caddy obtains the certificate;
`docker compose -f deploy/compose.prod.yml --env-file deploy/.env logs caddy`
shows it.

Then, **from your laptop**, run the smoke test against the real certificate
(steps 1, 2 and 4; the ledger step only runs on the server):

```bash
bash deploy/smoke.sh callit.duckdns.org
```

Finally, check that the API sees real client addresses. In
`docker compose … logs caddy`, the access-log entry for your laptop's
request should show your public IP as `remote_ip`. If it shows a Docker
gateway address (`172.x.0.1`) instead, Docker's port publishing is
rewriting source addresses, and every client shares one rate-limit bucket.
Fix that before announcing the URL.

## 6. Backups

Accounts and balances live permanently in Redis, so losing the VM without
a backup loses every account. Do both of these:

```bash
sudo mkdir -p /var/backups/callit && sudo chown "$USER" /var/backups/callit
crontab -e
# 0 3 * * * cd ~/call_it && bash deploy/backup.sh >> /var/backups/callit/backup.log 2>&1
```

and, in the Oracle console, attach a **backup policy** to the boot volume
(Block Storage → Boot Volumes). Always Free includes five volume backups,
so don't use the built-in Bronze policy, which keeps 12 monthly ones.
Create a custom policy under Block Storage → Backup Policies instead:
weekly, incremental, **27 days** of retention. That's 27 rather than 28,
so the oldest backup has expired before the next one starts, and the
policy never holds more than 4.
Off-VM copies matter here: Oracle can reclaim an idle Always Free instance
(7 days with CPU p95, network and memory all under 20%).

## 7. Deploy a new version, and roll back

```bash
git tag -a v0.3.0 -m "..." && git push origin v0.3.0    # on your laptop
bash deploy/deploy.sh v0.3.0                             # on the VM
```

If the smoke test fails, `deploy.sh` stops and prints the rollback command,
which is the same script with the previous tag. It doesn't roll back on its
own, because `migrate` may already have applied a newer schema. Check
whether the new tag added a migration (`backend/migrations/`) before
running older code against it.

Tags from before Phase 8a.1 read their secrets from `deploy/.env`. To roll
back past 8a.1, you'd need that old file back. `deploy.sh` stops at
`compose config` without it, before anything changes.

## 8. Restore from a backup

```bash
bash deploy/restore.sh /var/backups/callit/callit-YYYYmmdd-HHMMSS.tar.gz
```

This replaces **all** Redis and PostgreSQL data with the archive's (type
`RESTORE` to confirm). Kafka isn't backed up: the ledger worker replays
idempotently on each transaction's `idempotency_key`, so events delivered
after the backup point can't double-count.

## 9. Swap in a real domain later

1. Point the domain's DNS `A` record at the VM.
2. Change `CALLIT_HOSTNAME` in `deploy/.env`.
3. Rerun `bash deploy/deploy.sh <current-tag>`.

Caddy obtains the new certificate, and the API's CORS origin follows,
because both derive from that one value. Remove the DuckDNS cron entry.
Keep the `duckdns-token` item, or remove it from `secrets.sh` and
`preflight-secrets.sh` in the same change: both require it today.

## Moving an existing server to 1Password

For a server set up before Phase 8a.1, whose `deploy/.env` still holds
`JWT_SECRET` and `POSTGRES_PASSWORD`. The order matters: the live database
password must reach the vault before anything could replace it.

1. **Laptop:** create the `callit-prod` vault, then import the server's
   current values. They travel over SSH on stdin and are never written to
   your laptop's disk:
   ```bash
   ssh callit 'cat ~/call_it/deploy/.env' | bash deploy/op-bootstrap.sh --import
   ```
   Confirm the Postgres password matches, without printing either value.
   The two hashes must be identical:
   ```bash
   op read --no-newline op://callit-prod/postgres-password/password | sha256sum
   ssh callit "grep -m1 '^POSTGRES_PASSWORD=' ~/call_it/deploy/.env | cut -d= -f2- | tr -d '\n' | sha256sum"
   ```
2. **VM:** §4 steps 3–4, except `gen-env.sh`, which refuses while the old
   `.env` exists. Then `preflight-secrets.sh` passes.
3. **VM:** keep the old file until step 6, then rewrite `.env`:
   ```bash
   sudo install -m 0400 deploy/.env /root/env.pre-8a1
   rm deploy/.env && bash deploy/gen-env.sh callit.duckdns.org
   bash deploy/deploy.sh <the 8a.1 tag>
   ```
   The smoke test passes and existing accounts still log in. Existing
   tokens stay valid too, because the signing key didn't change.
4. **VM:** no container has a secret in its environment. This should
   print `0`:
   ```bash
   docker inspect $(docker compose -f deploy/compose.prod.yml --env-file deploy/.env ps -q) \
     | grep -ciE 'JWT_SECRET=|POSTGRES_PASSWORD=|POSTGRES_DSN='
   ```
5. **VM:** `sudo reboot`. Afterwards, `curl https://callit.duckdns.org/healthz`
   answers with no manual step, which proves the unit ran before Docker
   restarted the stack.
6. **VM:** `bash deploy/backup.sh` succeeds. Then `sudo shred -u /root/env.pre-8a1`.

## Rebuilding on a new VM

The secrets come from the vault, and the data from the newest
`backup.sh` archive. Neither depends on the old VM's disk.

1. §1–3 on the new VM, then §4 steps 4–5. The same Service Account token
   works; revoke and replace it if the old VM may be compromised.
2. Copy the newest archive over, deploy the current tag, then
   `bash deploy/restore.sh <archive>`.

## Rotating a secret

- **`jwt-secret`:** edit the item in 1Password (64 lowercase hex,
  `openssl rand -hex 32`), then `bash deploy/deploy.sh <current-tag>`.
  **Everyone is logged out**, because tokens signed with the old key stop
  verifying. Supporting two keys during a rotation window is deferred.
- **`postgres-password`:** not automated. Changing the item alone locks
  the services out of the existing database. First run
  `ALTER ROLE callit PASSWORD '...'` inside the `postgres` container, then
  edit the item to match, then redeploy.
- **The Service Account token:** create a new token in 1Password, replace
  `/etc/callit/op-service-account-token`, run
  `sudo systemctl restart callit-secrets`, then revoke the old token.

## What's still on disk

Being honest about what this does and doesn't remove:

- **The Service Account token** at `/etc/callit/op-service-account-token`
  (root, `0400`). It's a long-lived credential, but there's only one, it
  can only read one vault, it can be revoked in one click, and 1Password's
  activity log records every use. Before, there was a plaintext file holding
  every secret, with no audit and no revocation.
- **Root and the `docker` group can read the secrets** (`/run/callit/secrets`
  is `root:docker 0750`). The `docker` group is root-equivalent anyway: it
  can mount the host filesystem into a container.
- **1Password unreachable at boot means downtime.** The secret files don't
  exist until the unit succeeds, so the containers can't start. That's
  deliberate: a start with missing secrets is worse.
- **Backups contain no secrets,** but they contain every account's
  password hash and balance. Treat them as sensitive.

---

## Local rehearsal

The same stack runs on a laptop with `CALLIT_HOSTNAME=localhost`. Caddy then
issues the certificate from its own local CA.

Secrets come from random values in a RAM-backed dir instead of 1Password:

```bash
s=/dev/shm/callit-rehearsal; mkdir -p -m 0750 "$s"; pg="$(openssl rand -hex 24)"
printf %s "$(openssl rand -hex 32)" > "$s/jwt_secret"
printf %s "$pg" > "$s/postgres_password"
printf %s "postgres://callit:$pg@postgres:5432/callit?sslmode=disable" > "$s/postgres_dsn"
printf %s rehearsal > "$s/duckdns_token"; chmod 0444 "$s"/*
printf 'CALLIT_HOSTNAME=localhost\nCALLIT_SECRETS_DIR=%s\n' "$s" > deploy/.env
docker compose -f deploy/compose.prod.yml --env-file deploy/.env up -d --build --wait
docker compose -f deploy/compose.prod.yml --env-file deploy/.env \
  cp caddy:/data/caddy/pki/authorities/local/root.crt .caddy-root.crt
SSL_CERT_FILE="$PWD/.caddy-root.crt" bash deploy/smoke.sh localhost
# No secret value appears in any container's config. This should print 0:
docker inspect $(docker compose -f deploy/compose.prod.yml --env-file deploy/.env ps -q) \
  | grep -cF "$(cat "$s/jwt_secret")"
```

Tear down with `docker compose … down -v`, then `rm -rf "$s" deploy/.env .caddy-root.crt`.

Don't run `deploy.sh` in your development clone: it checks out a detached
tag over your working branch.

## Checks

```bash
bash deploy/check-compose.sh            # topology invariants (also in CI)
bash deploy/tests/check-compose_test.sh # the checks reject a broken layout
bash deploy/tests/gen-env_test.sh       # .env generation (non-secret settings)
bash deploy/tests/secrets_test.sh       # fetching secrets: all or nothing, nothing printed
bash deploy/tests/op-bootstrap_test.sh  # vault items: never overwritten, imported exactly
bash deploy/tests/preflight-secrets_test.sh
bash deploy/check-image.sh              # image: non-root, small, every binary starts and reads *_FILE
```
