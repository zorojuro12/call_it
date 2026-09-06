# 2026-08-31 — ansh — Got the app running end to end in a Windows browser over WSL2

**Status:** Backend + frontend both run and are reachable from a Windows
browser via the WSL2 instance's LAN IP; registered an account, logged in,
created a room, and reached the (unstyled but functional) host console. Full
gameplay loop (open round → wager → lock → resolve → reveal) not yet
exercised end to end in the browser — session ended before completing it.
**Decided:** `wsl --shutdown` was ruled out as a fix for WSL2's broken
`localhost`→Windows port forwarding, since it would kill this Claude Code
session along with every other WSL process. Worked around it instead with
two new `make` targets (`api-lan`, `fe-dev-lan`) that auto-detect the WSL2
IP and wire the backend's `CORS_ALLOWED_ORIGINS` and the frontend's
`NEXT_PUBLIC_API_BASE_URL` to it automatically.
**Spec:** No change.
**Next:** Finish exercising a full round in the browser (two windows: host +
guest) to confirm wagering, live odds, lockout, resolve, and the settlement
reveal all work — session ended right after reaching the host console.
**Blocked on:** Nothing technical. `localhost:3000`/`127.0.0.1:3000` still
don't work directly from the Windows browser (root cause not fixed, only
worked around) — worth revisiting if it becomes annoying enough to justify
the `wsl --shutdown` risk in a moment when no session needs to stay alive.
**Touches:** `Makefile`, `frontend/next.config.ts`.

---

## What We Worked On

Resumed from the prior session (CI typecheck fix, `dev` clean). User wanted
to actually run the app — backend, frontend, and try it in a browser —
which surfaced a chain of WSL2/Windows networking issues neither of us had
hit before in this project, since all prior verification was CLI/test-based
(`make test`, `make fe-test`, Playwright against `localhost`) rather than a
real Windows browser hitting a WSL2 dev server.

## Decisions Made

- **Diagnosed root cause before reaching for a fix each time, in three
  layers:**
  1. `localhost:3000` unreachable from Windows → WSL2's automatic
     `localhost` port-forwarding into the VM wasn't working (confirmed:
     `curl` from *inside* WSL to `127.0.0.1:3000` succeeded while the
     Windows browser couldn't reach either `localhost:3000` or
     `127.0.0.1:3000` — so the servers were healthy, only the Windows-side
     forwarding was broken). Switched to the WSL2 `eth0` IP
     (`172.18.27.65`), which worked immediately.
  2. Browsing via the IP instead of `localhost` then 403'd on Next's own
     `_next/static/...` chunk requests — not a login bug. Root cause: Next
     15+'s dev server rejects cross-origin asset requests by default (a
     DNS-rebinding protection) unless the origin is in
     `allowedDevOrigins`. Fixed in `frontend/next.config.ts`.
  3. Login then needed `CORS_ALLOWED_ORIGINS` (backend) and
     `NEXT_PUBLIC_API_BASE_URL` (frontend) to both point at the IP too,
     since both default to `localhost` — a mismatch either fails CORS or
     never reaches the backend at all from the Windows browser's
     perspective.
- **User explicitly declined `wsl --shutdown`** (the clean fix for the
  underlying port-forwarding break) because it would kill this Claude Code
  session. Chose the LAN-IP workaround as a deliberate trade: doesn't fix
  the root cause, but has zero session risk and is now a two-command
  routine (`make api-lan`, `make fe-dev-lan`) instead of a five-line manual
  env-var incantation each time.
- **Committed only the Makefile + `next.config.ts` changes**, not a large
  unrelated edit to `docs/plans/2026-08-21-implementation-plan.md` (a Phase
  7→7a/7b split) that appeared in `git diff` mid-session. `ListAgents`
  confirmed a separate peer session ("Project status and plan 6 blockers")
  was concurrently busy on this repo — that edit wasn't from this session
  and was deliberately left unstaged for the user to review separately.

## What Worked

- **`ip -4 addr show eth0` inside WSL** gives the LAN IP a Windows browser
  can reach (`172.18.27.65` this session — DHCP-assigned, will likely
  change on a WSL restart).
- **`make api-lan` / `make fe-dev-lan`** (new Makefile targets) — confirmed
  via `make -n` dry-run that the IP substitution resolves correctly, then
  confirmed live: registered an account, logged in, created a room, landed
  on the host console with balance 1000 and room code `78YU6Z` showing.

## What Didn't Work

- **`http://localhost:3000` and `http://127.0.0.1:3000` from the Windows
  browser** — WSL2's port-forwarding relay isn't proxying either into the
  VM, even though the servers are listening and healthy from inside WSL.
  Not fixed this session (see Blocked on) — `wsl --shutdown` is the known
  fix but was ruled out.
- **Reaching the frontend via the LAN IP without `allowedDevOrigins` set**
  — every `_next/static` chunk 403'd, which looked at first glance like it
  could be the login request itself failing; turned out to be Next's dev
  server rejecting the browser's origin, unrelated to auth. Worth
  remembering: a 403 in the Network tab on `_next/static/chunks/...`
  specifically (not the API call) points here, not at CORS or the backend.

## Test Coverage

No test files touched this session — this was a manual dev-environment
exercise, not a code change to `internal/` or `lib/`/`components/`. No
coverage impact.

## Open Questions / Blockers

- Full gameplay loop (wager → lock → resolve → settlement reveal) still
  needs to be exercised in the browser with two windows (host + guest) —
  the session ended right after the host console loaded.
- The WSL2 `eth0` IP is DHCP-assigned and can change across a WSL restart,
  which would require re-running `ip -4 addr show eth0` and updating
  `frontend/next.config.ts`'s `allowedDevOrigins` entry by hand (it's a
  static string, not auto-detected the way the Makefile targets are).

## Relevant Commits

- `41a755a` — fix: allow the WSL2 host IP for local dev server access
  (`Makefile`, `frontend/next.config.ts`)

## Next Step

Open two browser windows (one normal, one incognito to avoid
`sessionStorage` collision) against `http://172.18.27.65:3000`, one as host
(open a round) and one as guest (place a wager), and confirm the full
round lifecycle renders correctly through to the settlement reveal.
