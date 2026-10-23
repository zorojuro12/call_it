# 2026-10-22 — ansh — Phase 8a.2: Kubernetes, and a chaos test that found a lost-wager bug

**Status:** Branch `phase-8a2-kubernetes` is complete and green: lint, the full Go suite, and the manifest checks with their mutants. Not merged. The chaos run went **FAIL → PASS**: 25 wagers lost before the relay fix, none after, and PASS again on the final StatefulSet manifests. Security review: no CRITICAL/HIGH; 3 MEDIUMs fixed, and the rest documented as limits.
**Decided:** The relay claims orphaned outbox entries on startup (`XAUTOCLAIM`, min-idle 0). That's safe only with at most one relay pod, so in Kubernetes it's a **one-replica StatefulSet**, not a Deployment. See plan Decisions 1 and 3, and the security-review entry in `docs/project-history.md`.
**Spec:** No change.
**Next:** Nothing outstanding for this phase. Merged to `dev` (`d8cc4df`) and pushed with CI green. **The relay fix is live in production as `v0.3.1`** (`main` `e3de8f6`; `smoke.sh` 4/4 on the VM, ledger at 4 → 6). Optional: `bash deploy/k8s/down.sh` frees the local cluster.
**Blocked on:** Nothing.
**Touches:** `backend/internal/relay/relay.go`, `backend/internal/relay/relay_test.go`, `backend/internal/ledger/{reconcile_after_load_test,redact_dsn_test}.go`, `deploy/k8s/`, `.github/workflows/ci.yml`, `CLAUDE.md`, `README.md`, `docs/project-history.md`, `docs/plans/2026-10-22-phase-8a2-kubernetes.md`

---

## What We Worked On
The user wanted Kubernetes on their resume for the least work that still
counts. They pasted a plan from another session: kind, Helm for the data
stores, manifests, then a k6 run while killing pods, then a reconciliation
check. This session turned it into Phase 8a.2.
- **Helm was dropped:** Bitnami's free images and charts were withdrawn in
  September 2025.
- **NetworkPolicies were skipped,** for minimum effort.
- **Planning found the bug the chaos test would hit:** `cmd/relay` uses a
  fresh UUID consumer per start, and `Recover` reads only its own pending
  entries.

## Decisions Made
- **Plain kustomize manifests on production's own images,** not Helm. Less
  work, and dev/prod parity.
- **Relay as a one-replica StatefulSet.** The plan said Deployment plus
  `Recreate`. The security review showed `Recreate` doesn't prevent
  overlap on a pod delete or eviction.
- **Chaos pass condition = outbox drains AND the ledger reconciles.** This
  was added mid-run (see What Didn't Work).
- **Second relay kill at 125 s, during the Kafka outage.** That's the
  window where an orphaned batch is a *lost* batch.
- **`automountServiceAccountToken: false` on every pod.** It's required,
  because the token mount collides with the `/run/secrets` volume, and
  it's also least privilege.
- **A user-local `kubectl` 1.37.1 in `~/.local/bin`.** The system 1.32 was
  five minors behind kind's node, and `/usr/local/bin/kubectl` is
  untouched.

## What Worked
- `TestRunClaimsOrphanedEntries`, against real Redis: RED on the old code
  (only the new entry relayed), GREEN after the fix. Re-verified RED after
  the test was restructured.
- `up.sh` gets a fresh cluster to ready in about 80 s, and a full
  `callit-cli` round reaches the ledger through a port-forward.
- **Chaos before the fix:** FAIL.
  - 50 entries stranded under two dead consumers.
  - The 60 s kill's 25 were already in the ledger, so stuck, not lost.
  - The 125 s kill's 25 never got there: **25 wagers of 10 tokens
    missing**, every player's ledger balance 10 too high.
- **Chaos after the fix:** PASS, drained in 24 s, 5,967 transactions.
  Repeated on the StatefulSet relay: PASS, drained in 26 s, 5,962
  transactions.
- `check-manifests.sh`: 5 checks, and 5 mutants all rejected by the right
  check.

## What Didn't Work
- **The first before run passed** (single relay kill at 60 s). Its 25
  stranded entries had already reached Kafka, and the reconciler only
  looks at the ledger, so it can't see a stuck outbox. Don't rely on a
  ledger reconciliation alone to prove outbox health.
- **The spike's Kafka looked broken** (CLI tools hung) only because the
  scratch manifest had no Service named `kafka`, which the broker
  advertises. Not a persistence problem.
- **A mutant anchored mid-container** inserted a pod-level `volumes:` key
  in the wrong place and broke the render (check 0) instead of testing
  check 1. Anchor pod-level mutations at the end of the container block.

## Test Coverage
- **Covered:**
  - the orphan claim, including ordering and zero pending afterwards;
  - `redactDSN`;
  - the manifest invariants (secrets as files, with every bypass route,
    pinned images, persistent stores, one StatefulSet relay, the API
    readiness probe);
  - end-to-end chaos with reconciliation.
- **Not covered yet:**
  - the chaos run isn't in CI (it needs a kind cluster and k6);
  - `up.sh` and `down.sh` have no automated test;
  - the relay/worker have no probes (no health endpoint).

## Open Questions / Blockers
- **The relay fix is live-relevant.** Production Compose has the same
  orphaning bug on any relay crash during a Kafka outage. It needs a
  release and a VM deploy to reach production: the user's call.
- **The kind cluster is still running locally.** `bash deploy/k8s/down.sh`
  frees it.
- **The old Phase 8a local rehearsal stack is still paused.** Its volumes
  are kept, from the 8a.1 session.

## Relevant Commits
- `bd5d0e4` plan · `716d8a6` spike results
- `1cf0eb3`: relay claims orphaned outbox entries
- `fe045eb`: manifests and `check-manifests.sh` · `a697341`: `up.sh` and `down.sh`
- `9855606`: `chaos.sh`, with the before/after results in its message
- `94ed6d0`: docs
- `f42834c`, `270dc66`, `e4d9615`: security-review fixes (StatefulSet relay, secret-route check, DSN redaction)
- `7c0a9c9`: review record and the final chaos run

## Next Step
Merge into `dev`. Then offer the user `v0.3.1` (or `v0.4.0`) to ship the relay fix to the VM via `deploy.sh`.
