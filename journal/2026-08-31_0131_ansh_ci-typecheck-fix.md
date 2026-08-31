# 2026-08-31 — ansh — Fixed a pre-existing CI break: Typecheck ran before route types existed

**Status:** `dev`'s CI is green again (`fda40cc`, run 24, `conclusion: success`).
The push that merged Phase 6b (`0da3147`) had failed CI on the `frontend` job's
`Typecheck` step; fixed and pushed directly to `dev` as a hotfix, not a phase
branch.
**Decided:** Fold `next typegen` into the `typecheck` npm script rather than
reordering `ci.yml`'s steps — it's Next's own purpose-built command for
generating just the route/layout ambient types (`LayoutProps`, `PageProps`,
etc.) without running a full build, so it fixes the root cause in one place
(CI, `make fe-lint`, and a bare local `npm run typecheck` all call that
script) without weakening CI's fail-fast ordering.
**Spec:** No change.
**Next:** None from this fix. Phase 7 (load test + hardening) is still the
next planned phase.
**Blocked on:** Nothing.
**Touches:** `frontend/package.json`, `Makefile`.

---

## What We Worked On

User flagged a red GitHub Actions run (screenshot: `frontend` job failed at
"Typecheck", 2 errors — "Process completed with exit code 2" and "Cannot find
name 'LayoutProps'" — while `backend` and `frontend-e2e` stayed green) on the
push that landed Phase 6b's merge + journal commit. Root-caused and fixed.

## Decisions Made

- **Root cause, confirmed by exact local reproduction, not guessed:**
  `app/layout.tsx` (Phase 6a's original `create-next-app` scaffold,
  `d1e5b083`, untouched since) types its props as `LayoutProps<"/">` — an
  ambient type Next.js only declares in `.next/types/routes.d.ts`, generated
  as a side effect of `next dev` or `next build`. `ci.yml`'s `frontend` job
  runs `Typecheck` (`tsc --noEmit`) *before* `Build`, on a bare `actions/checkout`
  with no prior Next command ever run — so `.next/types` and `next-env.d.ts`
  don't exist yet, and `LayoutProps` is genuinely undefined at that point.
  Confirmed locally: `rm -rf .next next-env.d.ts && npx tsc --noEmit`
  reproduced `error TS2304: Cannot find name 'LayoutProps'` exactly; running
  `next build` (or `next typegen`) first, then `tsc --noEmit`, passed clean.
  This is not new — it predates 6b entirely (6a's own scaffold + 6a's
  original `ci.yml`, unmodified by 6b) — it had just never been exercised
  under CI's exact fresh-checkout ordering.
- **Why `next typegen` over reordering `Build` before `Typecheck`:** the
  ordering exists for fail-fast — a broken build shouldn't hide behind a
  slow test suite finishing first, and a `Typecheck` failure should surface
  before spending a full `next build`. `next typegen` produces only the
  route-type declarations Next's build would produce as a byproduct, with
  none of the compile/bundle work, so it preserves that ordering intent
  while making `tsc --noEmit` accurate on a bare checkout.
- **Fixed in `package.json`'s `typecheck` script itself, not `ci.yml`**,
  because `Makefile`'s `fe-lint` target called `npx tsc --noEmit` directly,
  bypassing the npm script — so fixing only `ci.yml` would have left local
  `make fe-lint` still broken on a fresh clone. Changed `fe-lint` to call
  `npm run typecheck` instead of duplicating the `tsc --noEmit` invocation,
  so there is exactly one place this logic lives.

## What Worked

- **Reproducing the CI failure locally, exactly, before touching anything.**
  The GitHub Actions UI screenshot named the failing step ("Typecheck") and
  the exact TS error, but not which file. Rather than guess, fetched the
  run's job/step breakdown via the GitHub REST API (`gh` CLI isn't installed
  in this environment) to confirm *which* step failed, then reproduced by
  clearing every artifact a prior `npm run build` in this same session had
  left behind (`.next/`, `next-env.d.ts`) — the first two "fresh install"
  attempts didn't reproduce it, because `next-env.d.ts` from an earlier
  `npm run build` call earlier this session was still sitting at the repo
  root and `rm -rf node_modules .next` doesn't touch it.
- **Verified the fix against the full CI sequence end to end locally**
  (`npm ci` → lint → typecheck → test:coverage → build, from a
  fully-cleared `.next`/`next-env.d.ts`/`node_modules`/`coverage`) before
  pushing, then confirmed the real run went green via the API rather than
  assuming from the local repro alone.

## What Didn't Work

- **First two "clean" reproduction attempts didn't reproduce the bug**,
  because `rm -rf .next node_modules && npm ci && npm run build` still
  succeeded — `next-env.d.ts` (a repo-root file, not inside `.next/` or
  `node_modules/`) was left over from builds run earlier in this same
  session (during Phase 6b's Task 10 verification) and neither `rm`
  command touched it. Only reproduced once `next-env.d.ts` was explicitly
  removed too, matching what a truly fresh `actions/checkout` actually
  looks like.
- **`gh` CLI is not installed in this environment** — used the GitHub REST
  API directly via `curl` instead (`.../actions/runs/{id}/jobs` for the
  step breakdown, `.../actions/runs/{id}` for status/conclusion polling).
  Worth remembering for future CI debugging sessions here.

## Test Coverage

No test behavior changed — this is a build/CI config fix. Confirmed the
full frontend suite (108 tests, coverage) and `next build` both still pass
under the corrected sequence.

## Open Questions / Blockers

None.

## Relevant Commits

- `fda40cc` — fix: generate Next.js route types before typechecking, in CI
  and locally (`frontend/package.json`, `Makefile`)

## Next Step

None pending from this fix. Phase 7 planning is the next real work whenever
the user is ready to start it.
