# CallIt on Kubernetes

The full stack runs on a local Kubernetes cluster (kind): Postgres,
Redis, Kafka, the API, the outbox relay, the ledger worker, and the
migration Job.
A chaos run kills the relay, the ledger worker and the Kafka broker in
the middle of a wager load test, then proves that every wager reached the
double-entry ledger exactly once. The first chaos runs found a real bug:
**a relay crash during a Kafka outage silently lost wagers from the
ledger.** The fix is in `backend/internal/relay`.

Production still runs Docker Compose on one VM (`deploy/README.md`). This
directory is the same topology, translated to Kubernetes.
Design: [`docs/plans/2026-10-22-phase-8a2-kubernetes.md`](../../docs/plans/2026-10-22-phase-8a2-kubernetes.md).

## Run it

Needs Docker, [kind](https://kind.sigs.k8s.io/) and a `kubectl` within one
minor version of kind's node (v0.33.0 runs Kubernetes 1.37), plus k6 and
Go for the chaos run.

```bash
bash deploy/k8s/up.sh       # create the cluster, build and load the image, deploy, wait (~80 s)
bash deploy/k8s/chaos.sh    # 4 min of load with kills, then drain and reconcile
bash deploy/k8s/down.sh     # delete the cluster and its volumes
```

To use the API by hand:

```bash
kubectl --context kind-callit -n callit port-forward svc/api 18080:8080
go run ./backend/cmd/callit-cli -addr localhost:18080 -host
```

## The chaos run

`chaos.sh` runs `loadtest/wager_latency.js` (25 players, 240 s) against
the cluster and SIGKILLs pods on a schedule, with no grace period, so a
process gets no chance to finish its batch:

| Time | Killed |
|---|---|
| 60 s | relay |
| 90 s | ledger worker |
| 120 s | Kafka broker |
| 125 s | relay again, during the Kafka outage |

It passes only if, afterwards:

1. **the outbox drains:** nothing left pending or unread in the
   `wager-outbox` Redis stream;
2. **the ledger reconciles:** Phase 7b's `TestReconcileAfterLoad` proves
   `redis_wallet − opening_stake == ledger_balance` for every player, and
   that every transaction balances.

### Results (2026-10-22)

| | Before the fix | After the fix |
|---|---|---|
| Result | **FAIL** | **PASS** |
| Outbox entries stranded | 50, under two dead relay consumers | 0, drained within 24 s |
| Wagers missing from the ledger | **25** (10 tokens each, so every player's ledger balance was 10 too high) | 0 |
| Ledger transactions | 5,931 | 5,967 |

**The bug:** the relay starts under a fresh random consumer ID each time,
and it only ever retried entries pending under its *own* ID. A batch it
had read but not acknowledged when it died was stranded under a consumer
that no longer existed:
- **Killed after sending the batch to Kafka:** the entries are stuck but
  not lost (the 60 s kill).
- **Killed before sending it, during the Kafka outage:** the wagers never
  reach the ledger (the 125 s kill).

**The fix:** on startup, the relay claims every pending entry in the
group with `XAUTOCLAIM`, then drains them ahead of new work, so ordering
holds. This is safe because exactly one relay ever runs (below).
Redelivery can't double-count, because the ledger is idempotent on each
event's `idempotency_key`.

**What the first run taught:** with only the 60 s relay kill, the
reconciler passed while 25 entries sat stranded, because they had all
reached the ledger already. A ledger check alone can't see a stuck
outbox, so draining became a pass condition, and the mid-outage kill was
added to hit the window where stuck means lost.

## Compose → Kubernetes

| `compose.prod.yml` | Here |
|---|---|
| `postgres`, `redis`, `kafka` services with named volumes | StatefulSets with `volumeClaimTemplates` (kind's local-path storage) and headless Services |
| `api` | Deployment, plus a ClusterIP Service, with a readiness probe on `/healthz` |
| `relay` | Deployment with `replicas: 1` and `strategy: Recreate` |
| `ledger-worker` | Deployment |
| `migrate` (`restart: "no"`) | Job (`backoffLimit: 10`) |
| `depends_on` with health conditions | Nothing: pods restart until their dependencies are up (the Kubernetes way) |
| File secrets from tmpfs (`/run/callit/secrets`) | A `callit-secrets` Secret, mounted per workload with `items`, at the same `/run/secrets/<name>` paths, read through `*_FILE`. The kubelet mounts Secret volumes on tmpfs. |
| `read_only`, `cap_drop: [ALL]`, `no-new-privileges` | `readOnlyRootFilesystem`, `capabilities.drop: [ALL]`, `allowPrivilegeEscalation: false`, `runAsNonRoot` (UID 65532) |

`automountServiceAccountToken: false` is set on every pod. None of them
talks to the Kubernetes API, so a compromised pod holds no cluster
credential. It's also required: the kubelet's token mount under
`/var/run/secrets` can't be created inside the read-only `/run/secrets`
volume.

## Invariants (`check-manifests.sh`, in CI)

It renders the manifests offline, with no cluster, and fails on:
1. a secret in any container's environment, or any secret grant beyond
   `api=jwt_secret`, `migrate`/`ledger-worker=postgres_dsn` and
   `postgres=postgres_password`;
2. an image other than the four production runs, or a pullable backend
   image;
3. a data store without persistent storage. Kafka on an `emptyDir` would
   lose unconsumed messages when its pod is deleted;
4. a relay with more than one replica, or with a rolling update. Its
   startup claim would take a live relay's in-flight batch;
5. an API without its `/healthz` readiness probe.

`tests/check-manifests_test.sh` proves checks 1, 3 and 4 reject a broken
copy.

## Limits

- A single-node kind cluster on a laptop. Not production, and not
  multi-node or highly available.
- Secrets are generated by `up.sh` and stored in etcd base64-encoded, not
  encrypted at rest. In production they come from 1Password
  (`deploy/README.md`).
- No NetworkPolicies, so Compose's network isolation (the API can't reach
  Postgres or Kafka) is not reproduced here.
- Exactly one relay, by design. Scaling it would need a minimum idle time
  on the claim, plus per-key ordering.
- `relay` and `ledger-worker` have no health endpoint, so they have no
  probes. They recover by crashing and being restarted.
- k6's `wager_ack_ms` p99 < 15 ms threshold fails during chaos runs (the
  broker is being killed). That's a latency target, not a correctness check.
