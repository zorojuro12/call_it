# 2026-10-18 — ansh — Phase 8a go-live: v0.2.0 running at callit.duckdns.org

**Status:** Backend live at `https://callit.duckdns.org`: `v0.2.0` deployed on the Oracle VM, smoke test 4/4 green on the server, reachable from the internet with a Let's Encrypt cert. The root path returns 404 by design until Phase 8b.
**Decided:** Home region **US West (Phoenix)** (permanent): 3 availability domains versus 1 in Montreal, Toronto and San Jose, per Oracle's region list, so more chances at free A1 capacity.
**Spec:** No change.
**Next:** Finish the two remaining go-live checks (Caddy `remote_ip` shows real client IPs; backup cron plus Bronze boot-volume policy), record them in `docs/project-history.md`, then plan Phase 8b (serve the frontend at the same hostname).
**Blocked on:** Nothing. Both remaining checks are commands for the user to run on the VM.
**Touches:** `deploy/README.md` (runbook followed end to end), `~/.ssh/config` (laptop, not in repo), Oracle console resources, DuckDNS

---

## What We Worked On
Followed `deploy/README.md` §1–5 from a new Oracle account to a live deploy,
step by step through the console with screenshots.

## Decisions Made
- **Phoenix as home region.** See "Decided"; the choice can't be changed
  without a new account, which Oracle forbids.
- **Shape `VM.Standard.A1.Flex` at 2 OCPU / 12 GB.** This exactly fits the
  free allowance for a 31-day month (1,488 of 1,500 OCPU-hours and 8,928 of
  9,000 GB-hours). Burstable stays off.
- **Boot volume 100 GB at 10 VPU (Balanced).** Inside the free 200 GB.
  Higher VPU is paid.
- **Separate SSH key** `~/.ssh/id_ed25519_callit` instead of reusing the
  GitLab key. `~/.ssh/config` has a `Host callit` entry with
  `IdentitiesOnly yes`.
- **IPv6 off** on the VCN and the VNIC. That keeps Docker's IPv6 path, which
  can hide client addresses (security review MEDIUM), out of play.
- **On-demand capacity, never preemptible.** Preemptible instances can be
  reclaimed at any time.
- **A $1 budget with an alert at 1% actual spend.** Alerts only; it doesn't
  stop anything. The account is Free Trial, not upgraded, so no card
  charges are possible.
- **The DuckDNS token never entered the chat.** `gen-env.sh` ran on the VM
  with a leading space to keep it out of bash history.

## What Worked
- **VM `callit`:** AD-2, FD-1, public IP `129.146.71.244`. `uname`: `aarch64`,
  Ubuntu 24.04.5, 2 CPUs, 11 GiB.
- **Docker** from Docker's apt repo, with Compose v5.6.0. Repo cloned at
  `~/call_it` on `v0.2.0`.
- **`deploy.sh v0.2.0`:** all 8 services healthy and `migrate` exited 0.
  Smoke results on the server:
  - healthz over TLS
  - a full round over `wss://callit.duckdns.org`
  - ledger 0 → 2 transactions
  - spoofed XFF rate-limited by attempt 11 (step 2's registration came from
    the CLI container and used a different auth bucket than step 4's host
    `curl`, so step 4 got a full bucket of 10)
- **External check from the laptop:**
  - `/healthz` gives 200 and `{"status":"ok"}`, `/` gives 404, http gives a
    308 to https, and HSTS is set
  - the cert is Let's Encrypt `YE2` for `callit.duckdns.org`, valid until
    2027-01-16
- **Host iptables on Oracle's Ubuntu image did not block Docker's published
  80/443.** No manual rule changes were needed, so runbook §2's iptables
  note was only a precaution here.
- **DuckDNS** resolves `callit.duckdns.org` to `129.146.71.244`, and the
  5-minute updater cron is installed.

## What Didn't Work
- **Creating the VCN inside the instance-create form:** the "Automatically
  assign public IPv4 address" toggle stayed greyed out even with "Create new
  public subnet" selected. Fix: create the network first with **Networking →
  VCN Wizard → "Create VCN with internet connectivity"** (`callit-vcn`), then
  select the existing VCN and public subnet. Re-picking the compartment
  dropdown refreshed the VCN list.
- **AD-1 was out of A1 capacity, and AD-2's early capacity check also failed,**
  but clicking **Create** in AD-2 a minute later succeeded. The console's
  early check isn't final; retry the actual create.
- **The first `ssh callit` failed with "Host key verification failed"**
  because the trust prompt needs the word `yes` typed, not just Enter.
- **The plain "Create VCN" button** makes an empty VCN with no gateway or
  public subnet. Use the wizard.
- **My `ssh`/`~/.ssh` commands from Claude Code were denied by the permission
  settings,** so the user ran the server commands themselves (with `!` or in
  their own terminal).
- **"HTTP ERROR 404" in Chrome at `/` is expected, not a failure.** It proves
  TLS and routing work. Visible only because the frontend (8b) isn't
  deployed yet.

## Test Coverage
- **Covered:** `smoke.sh` 4/4 on the live server, including the Docker-image
  CLI path (the no-Go server path), which was untested locally before this.
  External HTTPS checks from the laptop.
- **Not covered yet:**
  - Caddy's `remote_ip` for external clients, the security-review MEDIUM
    that only the real server can answer
  - backup and restore on the VM (the drill was local only)
  - `duckdns-update.sh` via cron (it ran once by hand, `OK`)

## Open Questions / Blockers
- **Run the client-IP check:** `docker compose -f deploy/compose.prod.yml
  --env-file deploy/.env logs caddy --since 15m | grep -o
  '"remote_ip":"[^"]*"' | sort | uniq -c` should list the laptop's
  `69.196.89.210`.
- **Set up backups:** create `/var/backups/callit`, add the 03:00
  `backup.sh` cron entry, run one backup by hand, and assign the Bronze
  boot-volume backup policy in the console.
- **3 kernel packages were kept back** by `apt upgrade`. Run a
  `full-upgrade` and reboot during a quiet window.
- **The Free Trial ends in about 30 days.** After that the account is Always
  Free only, and the VM keeps running.

## Relevant Commits
- None during go-live; it deployed tag `v0.2.0` (`61ca3ec`). This journal
  entry is the only repo change.
