# 2026-10-18 — ansh — Phase 8a go-live checks: real client IPs and backups

**Status:** Both remaining go-live checks pass and are recorded in `docs/project-history.md` (Phase 8a section, "Go-live"). The `deploy/README.md` §6 backup instructions are corrected. Committed and pushed `f163a25` on `dev`. Phase 8b planning was started but stopped before any plan file was written.
**Decided:** Boot-volume backups use a custom `callit_weekly` policy (weekly incremental, Monday 00:00 UTC, **27-day** retention) instead of Oracle's Bronze. Always Free includes 5 volume backups; Bronze keeps 12 monthly ones. 27 days rather than 28 means the policy never holds more than 4.
**Spec:** No change.
**Next:** Write the Phase 8b plan (frontend at `callit.duckdns.org`, same origin as the API) with `writing-plans`. Inputs: parent plan §9's 8b row (line 436) and its Phase 8 split note (~line 611), plus the 8a plan's Caddyfile decision (~line 121).
**Blocked on:** Nothing. To check on 2026-10-19: the first scheduled boot-volume backup (00:00 UTC) shows *Available* in the Oracle console, and `/var/backups/callit/backup.log` plus a second archive exist after the 03:00 UTC cron run.
**Touches:** `docs/project-history.md`, `deploy/README.md` §6, VM crontab, Oracle console (Backup Policies, boot volume)

---

## What Worked
- **Real-client-IP check:** a `/healthz` request from the laptop showed the laptop's public IP as `remote_ip` in Caddy's access log, not a `172.x.0.1` gateway address.
- **Manual `deploy/backup.sh`:** wrote a `0600` archive containing `postgres.sql` and Redis `dump.rdb` plus the append-only files. It's 5.1K, which is expected with almost no data.
- **Cron:** `0 3 * * * cd ~/call_it && bash deploy/backup.sh >> /var/backups/callit/backup.log 2>&1`, installed alongside the DuckDNS updater. Exactly one entry.
- **Boot-volume policy:** the volume's details page shows `callit_weekly`, with upcoming runs on 10-19, 10-26 and 11-02.

## What Didn't Work
- `curl -s …; echo` in **PowerShell** prompts for a `Uri`, because `curl` is an alias for `Invoke-WebRequest` there. Use `curl.exe`, or run it from WSL, which leaves through the same public IP.

## Open Questions / Blockers
- No manual boot-volume backup was taken, by choice: it would never expire and would take one of the 5 free slots. So the 2026-10-19 scheduled run is the first proof the policy works.

## Relevant Commits
- `f163a25` — docs: record the Phase 8a go-live checks and fix the backup runbook
