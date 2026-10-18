# 2026-10-17 — ansh — Phase 8a execution: deploy stack built, reviewed, merged

**Status:** Phase 8a code complete. All 8 tasks done and merged `phase-8a-backend-deploy` → `dev` → `main`; tagged `v0.2.0`; not yet live on a server.
**Decided:** Security review MEDIUMs: the `.env` injection was fixed; unsigned release tags and Docker possibly hiding client IPs were accepted, with reasons in `docs/project-history.md` (Phase 8a section).
**Spec:** Updated. §8's "live deployment deferred" is marked lifted by 8a, and Terraform is recorded as dropped.
**Next:** Go-live per `deploy/README.md` §1–6 (Oracle VM, firewall, DuckDNS, `deploy.sh v0.2.0`), then append the result to `docs/project-history.md`.
**Blocked on:** The user creating the Oracle Cloud account/VM and the DuckDNS subdomain, which need their own accounts.
**Touches:** `deploy/**`, `backend/Dockerfile`, `.github/workflows/ci.yml`, `CLAUDE.md`, `README.md`, `docs/project-history.md`, `docs/specs/2026-08-21-callit-design.md` §8

---

## What We Worked On
Continued from `journal/2026-10-17_1915_ansh_phase-8a-deploy-plan-and-start.md`
(Tasks 1–2 and the app-logic check). This session built Tasks 3–8:
- the container image
- the production compose topology
- Caddy, backup and restore
- server secrets
- deploy, smoke and CI
- the runbook and docs

Then the security review, and the merges.

## Decisions Made
- **The image also ships `callit-cli`.** `deploy.sh` runs `smoke.sh` on the
  server, which has no Go toolchain. The reviewer flagged this as LOW
  attack surface; it's accepted.
- **Only `api` builds the backend image.** The other three services reuse
  the tag with `pull_policy: never`. See "What Didn't Work".
- **No optional ACME email.** Let's Encrypt doesn't need one, and Caddy
  rejects an empty `email` option.
- **Security review outcome:** recorded in `docs/project-history.md`, Phase
  8a section. Not restated here.
- **Kept the phase branch after merging**, matching earlier phases.

## What Worked
- **`check-image.sh`:** 13 MiB image, `nonroot` user, and all five binaries
  start, each proven by failing on its own config validation.
- **`check-compose.sh`:** 11 checks pass, and `check-compose_test.sh`
  proves the guard rejects a published Redis port and `api` on the ledger
  network.
- **Local rehearsal behind Caddy's local CA:**
  - `/healthz` gives 200, `/` gives 404, http→https is a 308, and HSTS is
    set.
  - `smoke.sh` passes all 4 steps: a full round over `wss://`, the ledger
    recording it, and spoofed `X-Forwarded-For` rate-limited by attempt 10.
- **Restore drill:** login 200, then `down -v` (login 401, ledger empty),
  then restore, then login 200 with migrations back at version 2.
- **arm64 image:** builds under emulation (5m34s) and its binary starts.
- **`actionlint`:** clean (run via `rhysd/actionlint:1.7.1` in Docker,
  since no YAML parser is installed locally).
- **Merged tree green:** `make test`, plus Playwright with 2 passed and the
  logic check skipped (opt-in).
- **Coverage:** 89.0% excluding `cmd/*` (`-coverpkg=./...`).

## What Didn't Work
- **Four compose services sharing `build:` and one image tag** failed with
  `image "callit-backend:local": already exists`, because the parallel
  builds collide on export. Fix: only `api` builds.
- **A missing bind-mounted file** (`./Caddyfile`, before it existed) made
  Docker create a root-owned *directory* in its place. Remove it before
  writing the real file.
- **`email {$ACME_EMAIL}` with an empty value** failed with Caddy's "wrong
  argument count".
- **My first jq for check 4** had a precedence bug
  (`test(":") | not or test(...)` evaluated `test` on a boolean). The parts
  need explicit parentheses.
- **Testing `deploy.sh` in a temp clone with a `&&` setup chain:**
  - The setup's no-op `git commit` ("nothing to commit") silently stopped
    the chain, so the script never ran.
  - A later attempt wrapping `rm -rf "$D"` in a shell function was blocked
    by Claude Code's removal safety check. Fix: create the dir with
    `mktemp -d` in its own call and remove it by its literal path.
- **Don't run a valid tag through `deploy.sh` in a clone on this machine.**
  The clone uses the same compose project name (`callit-prod`), so it would
  rebuild and restart the rehearsal stack.
- **`smoke.sh` step 4 uses up the auth bucket for a minute,** so an
  immediate rerun fails at registration. For local reruns, clear
  `ratelimit:*` in the rehearsal Redis.

## Test Coverage
- **Covered:**
  - image invariants (`check-image.sh`)
  - topology invariants plus proof the guard bites (`check-compose.sh`,
    `check-compose_test.sh`)
  - secret generation, including rejecting injection-shaped
    hostnames/tokens (`gen-env_test.sh`)
  - end to end over TLS (`smoke.sh`)
  - backup and restore (the drill; manual, not in CI)
- **Not covered yet:**
  - whether the VM's Caddy sees real client IPs (manual go-live check,
    runbook §5)
  - `smoke.sh`'s Docker-image CLI path (the no-Go server path), which
    wasn't runnable locally because `localhost` resolves inside the
    container
  - `duckdns-update.sh` against real DuckDNS
  - `deploy.sh`'s full happy path; only its refusals were tested

## Open Questions / Blockers
- **Deferred security LOWs** (listed in `docs/project-history.md`):
  - a minimum prefix length for `TRUSTED_PROXIES`
  - the DuckDNS token visible in `ps`
  - pinning images and CI actions by digest/SHA
  - no `ReadHeaderTimeout` on `cmd/api`
- **The local rehearsal stack (`callit-prod`) may still be running** on
  ports 80/443. `docker compose -f deploy/compose.prod.yml --env-file
  deploy/.env down` stops it.

## Relevant Commits
- `c2a4fcc`: one distroless image for all backend binaries
- `2ae6168`: production compose with network-isolated data stores
- `f675702`: Caddy TLS front, plus the single-builder and no-email fixes
- `1baf509`: backup and restore, proven by the restore drill
- `6698313`: `gen-env.sh` and `duckdns-update.sh`
- `c6171e2`: tag-based deploy and the end-to-end TLS smoke test (CLI in the image)
- `718e7b8`: the CI `deploy` job (topology checks, arm64 build)
- `0c1ee70`: fix, validate gen-env inputs and stop sourcing `.env` (security MEDIUM)
- `7362e33`: fix, resolve deploy refs to a commit (security LOW)
- `6d2cacb`: fix, harden the Go containers, CI token and backup dir (security LOWs)
- `a8366c3`: runbook, `CLAUDE.md` invariants, security review record
- `3eeef78`: merge into `dev`; `7fd173b`: merge `dev` into `main`; tag `v0.2.0`

## Spec Changes
§8: the "Live public cloud deployment" bullet is struck through and marked
lifted by Phase 8a, with Terraform dropped (parent plan §9, "Phase 8 split"
note).
