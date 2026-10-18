# Deploying CallIt

The backend runs on one Linux VM with Docker Compose: Caddy terminates TLS
and is the only thing reachable from the internet, and Redis, PostgreSQL
and Kafka sit on internal networks that only the processes needing them
can reach. Design and reasoning: [`docs/plans/2026-10-17-phase-8a-backend-deploy.md`](../docs/plans/2026-10-17-phase-8a-backend-deploy.md).

| File | What it is |
|---|---|
| `compose.prod.yml` | The production topology. `check-compose.sh` enforces its invariants in CI. |
| `Caddyfile` | TLS, HSTS, routing (`/api/*`, `/healthz`; everything else 404 until Phase 8b). |
| `gen-env.sh` | Creates `deploy/.env` with generated secrets. Run once per server. |
| `deploy.sh <ref>` | Deploys a git tag: checkout, build, migrate, start, smoke test. |
| `smoke.sh <host>` | Health check, a full round over `wss://`, ledger check, spoofed-header check. |
| `backup.sh` / `restore.sh` | Both money stores (Redis and PostgreSQL) into one archive, and back. |
| `duckdns-update.sh` | Keeps the DuckDNS record on the VM's IP (cron). |

`deploy/.env` holds every secret. It's created on the server, mode `0600`,
ignored by git, and never copied anywhere else.

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

## 3. Install Docker and clone

```bash
# Docker Engine from Docker's official apt repository:
# https://docs.docker.com/engine/install/ubuntu/
sudo usermod -aG docker "$USER"   # then log out and back in
git clone https://github.com/zorojuro12/call_it.git ~/call_it
cd ~/call_it
```

## 4. DuckDNS and secrets

1. Sign in at <https://www.duckdns.org>, create a subdomain (e.g.
   `callit`), and copy your token from the top of the page.
2. Generate `deploy/.env`. It refuses to run a second time, because new
   secrets would log everyone out and lock you out of the existing database:
   ```bash
   bash deploy/gen-env.sh callit.duckdns.org <duckdns-token>
   ```
3. Point the name at this VM, and keep it pointed:
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

---

## Local rehearsal

The same stack runs on a laptop with `CALLIT_HOSTNAME=localhost`. Caddy then
issues the certificate from its own local CA.

```bash
(umask 077; printf 'CALLIT_HOSTNAME=localhost\nJWT_SECRET=%s\nPOSTGRES_PASSWORD=%s\n' \
  "$(openssl rand -hex 32)" "$(openssl rand -hex 24)" > deploy/.env)
docker compose -f deploy/compose.prod.yml --env-file deploy/.env up -d --build --wait
docker compose -f deploy/compose.prod.yml --env-file deploy/.env \
  cp caddy:/data/caddy/pki/authorities/local/root.crt .caddy-root.crt
SSL_CERT_FILE="$PWD/.caddy-root.crt" bash deploy/smoke.sh localhost
```

Don't run `deploy.sh` in your development clone: it checks out a detached
tag over your working branch.

## Checks

```bash
bash deploy/check-compose.sh            # topology invariants (also in CI)
bash deploy/tests/check-compose_test.sh # the checks reject a broken layout
bash deploy/tests/gen-env_test.sh       # secret generation
bash deploy/check-image.sh              # image: non-root, small, every binary starts
```
