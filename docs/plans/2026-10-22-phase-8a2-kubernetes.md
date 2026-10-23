# Phase 8a.2 — Kubernetes Deployment and Chaos-Tested Delivery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use the `executing-plans` skill to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The full stack runs on a local Kubernetes cluster (kind). A chaos
run proves no wager is lost or double-counted when the relay, the ledger
worker and the Kafka broker are killed mid-load. It runs once against the
code before this phase's relay fix (expected FAIL) and once after (PASS).

**Architecture:**
- Plain manifests under `deploy/k8s/`, rendered with `kubectl kustomize`
  (built into `kubectl`, so there's no Helm). They translate
  `deploy/compose.prod.yml` one to one, with the same images:
  - Postgres, Redis and Kafka are single-replica StatefulSets on
    PersistentVolumeClaims;
  - the API, relay and ledger worker are Deployments;
  - `migrate` is a Job.
- Secrets reach the pods only as files, from a Kubernetes Secret volume,
  through the `*_FILE` variables Phase 8a.1 added. A static check, in the
  style of `check-compose.sh`, enforces the invariants offline in CI.
- `chaos.sh` drives k6, kills pods on a schedule, waits for the pipeline to
  drain, and runs Phase 7b's `TestReconcileAfterLoad` through port-forwards.

**Tech Stack:** kind (host tool, user-local, pinned) · `kubectl` 1.32
(installed) · kustomize (via `kubectl kustomize`) · k6 (installed) ·
Go 1.26.7 (`internal/relay` only) · bash.

**Spec:** [`docs/specs/2026-08-21-callit-design.md`](../specs/2026-08-21-callit-design.md)
(the outbox delivery guarantee, `CLAUDE.md` Critical Invariants) and the
user's brief (pasted in the session; recorded in the journal). That brief
asked for Helm, which this plan drops: see Decision 2.
**Parent plan:** [`2026-08-21-implementation-plan.md`](2026-08-21-implementation-plan.md) §9, new row 8a.2
**Builds on:** Phase 7b's reconciler (`backend/internal/ledger/reconcile_after_load_test.go`,
`loadtest/README.md` "Phase 7b reconciliation after a real load run") and
Phase 8a.1's `*_FILE` secrets.

---

## Decisions This Plan Fixes

1. **The relay claims orphaned outbox entries on startup.**
   - **The bug:** the relay picks a random consumer ID on every start
     (`cmd/relay/main.go`, `uuid.NewString()`), and `Recover` reads only
     that consumer's own pending entries. If a relay dies between
     `XREADGROUP` and `XACK`, its batch is pending under a dead consumer
     forever and never reaches Kafka or the ledger.
   - **This is reachable today, without Kubernetes.** A Kafka outage makes
     `Once` fail, `Run` returns the error, the process exits, and it
     restarts under a new ID.
   - **The fix:** `Run` first `XAUTOCLAIM`s every pending entry, from any
     consumer, with min-idle 0. It then drains them through the existing
     `Recover` loop, before any new work, so the ordering guarantee
     `Run`'s comment states still holds.
2. **No Helm.** Bitnami's free images and charts stopped working in
   September 2025, and the alternatives are a different chart, or an
   operator, per store. Single-replica StatefulSets on the images
   production already runs are less work and match production exactly.
3. **Exactly one relay, replaced with `Recreate`.** *(Superseded during
   execution: the relay is a one-replica StatefulSet. See "Execution
   Deviations" at the end.)* Claiming with min-idle 0
   is only safe if no other relay is alive. A `RollingUpdate` would briefly
   run two, and the new one would steal the old one's in-flight batch and
   could relay a `rounds-settled` ahead of its wagers. Compose already runs
   exactly one relay. The check in Task 3 enforces `replicas: 1` and
   `strategy: Recreate`.
4. **The data stores are on PersistentVolumeClaims.** Kafka especially:
   with `emptyDir`, deleting the broker pod would discard every
   unconsumed message and the consumer group's offsets, which the chaos
   run would report as lost wagers. That's the right outcome, but for the
   wrong reason.
5. **Kills are `--grace-period=0 --force` (SIGKILL).** A graceful SIGTERM
   lets the relay finish and ack its batch, which proves nothing about
   crashes.
6. **No NetworkPolicies, Ingress, TLS or multi-node cluster.** Out of
   scope (the user's minimum-effort brief). Listed as limits in
   `deploy/k8s/README.md`.
7. **Labelled 8a.2.** Like 8a.1, it's inserted ahead of 8b without
   renumbering, and it never touches the live VM.

## Global Constraints

- **No new Go modules or npm packages.** `XAutoClaim` is in the pinned
  `go-redis` v9.18.0 (`stream_commands.go:402`). Never run `go get`.
- **Host tools are user-local and pinned:** kind goes in `~/.local/bin`,
  at an exact release at least two weeks old, with its `sha256` checked
  against the release's published checksum. No sudo.
- **Images:** exactly those in `deploy/compose.prod.yml` (`postgres:16-alpine`,
  `redis:7.2-alpine`, `apache/kafka:3.7.0`), plus the locally built
  `callit-backend:local` loaded with `kind load` (`imagePullPolicy: Never`).
- **Secrets as files only.** No secret-named env key anywhere
  (`JWT_SECRET`, `POSTGRES_PASSWORD`, `POSTGRES_DSN`). The grants are
  exactly `api=jwt_secret`, `migrate=postgres_dsn`,
  `ledger-worker=postgres_dsn` and `postgres=postgres_password`, each a
  Secret-volume `items` entry mounted under `/run/secrets`. Secret values
  are generated at bring-up into `/dev/shm` and are never committed.
- **Port-forwards use non-default local ports:** API 18080, Redis 16379,
  Postgres 15432, Kafka 19092. The dev Compose stack (`make up`) holds
  6379/5432/9092, and a reconciler aimed at it would read the wrong
  databases.
- **Never touch the live VM, `deploy/compose.prod.yml` or `deploy/.env`.**

## Review Focus

1. **The broker is killed while the relay holds an unacked batch.** The
   relay exits, restarts under a new ID, and the batch is orphaned. Pinned
   in Task 1 (integration test) and in Task 5's chaos run.
2. **Two relays overlap during a rollout,** and the claim steals in-flight
   work and inverts ordering. Pinned in Task 3 (check: `replicas: 1`,
   `Recreate`), with a mutant.
3. **Kafka on ephemeral storage** loses unconsumed messages on a pod
   delete. Pinned in Task 3 (check: every data store has
   `volumeClaimTemplates`), with a mutant.
4. **The reconciler points at the dev Compose stack instead of the
   cluster,** and passes against the wrong data. Pinned in Task 5: the
   ports are explicit, and the room ID only exists in the cluster, so a
   mis-aimed run fails rather than passes.
5. **The reconciler runs before the pipeline drains,** giving a false FAIL
   on a correct build. Pinned in Task 5: wait for zero pending outbox
   entries and a stable ledger count, with a timeout, then reconcile.

---

### Task 1: The relay claims orphaned outbox entries (flagship, inline)

**Files:**
- Modify: `backend/internal/relay/relay.go`
- Test: `backend/internal/relay/relay_test.go`

**Interfaces:**
- Produces: `func (r *Relay) ClaimOrphans(ctx context.Context, count int64) (int, error)`.
  It `XAUTOCLAIM`s up to `count` pending entries from any consumer in
  `r.group` to `r.consumer`, with min-idle 0, starting from `"0-0"`, and
  follows the returned cursor until it is `"0-0"`. It returns how many it
  claimed. `Run` calls it after `EnsureGroup` and before the `Recover`
  drain.

**Checkpoint 1: entries pending under a dead consumer reach the producer**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `TestRunClaimsOrphanedEntries`, in the style of
`TestRunRecoversBeforeNewWork`:
- XADD two valid outbox entries, A then B.
- `XREADGROUP` them as consumer `dead` (now pending under `dead`) and
  don't ack.
- XADD entry C (new, unread).
- Start `Run` on a `Relay` with consumer `fresh` and the fake producer, and
  cancel once the producer has seen 3 events, or after 5 s.
- Assert:
  - the producer received A, B, C **in that order**;
  - `XPENDING` summary for the group is 0;
  - `fresh` owns no pending entries.

Run: `cd backend && go test ./internal/relay -run TestRunClaimsOrphanedEntries -count=1 -race`
Expected: FAIL. The producer sees only C, then the 5 s timeout fires.

- [ ] **Step 2: Implement, then verify-and-commit in one command**

Contract: `ClaimOrphans` as specified in Interfaces. In `Run`, after
`EnsureGroup`, loop `ClaimOrphans(ctx, 100)` until it returns 0, then run
the existing `Recover` drain. Update `Run`'s and `Recover`'s doc comments:
orphans from dead relays are claimed first, and the single-relay
requirement is Decision 3.

```bash
cd backend && go test ./internal/relay -count=1 -race && cd .. && \
  git add backend/internal/relay/relay.go backend/internal/relay/relay_test.go && \
  git commit -m "fix: relay claims outbox entries orphaned by a dead consumer"
```

Task boundary: `make lint && make test`.

---

### Task 2: kind, and the facts the manifests depend on (spike)

No RED→GREEN. **If any check fails, stop and amend Decisions 2–4 before
Task 3.**

**Files:**
- Create: `deploy/k8s/kind.yaml` (one control-plane node, cluster name `callit`)
- Modify: this plan, appending a `## Spike Results (Task 2)` section

- [ ] **Step 1: Install kind** to `~/.local/bin`, at the exact version
  chosen per Global Constraints, with the `sha256` verified. Then
  `kind create cluster --config deploy/k8s/kind.yaml` succeeds and
  `kubectl get nodes` shows `Ready`.
- [ ] **Step 2: PVCs bind, and the images can write to them.** Apply a
  scratch StatefulSet of `apache/kafka:3.7.0` with a 1Gi
  `volumeClaimTemplates` mount at `/var/lib/kafka/data`. Record:
  - that the default StorageClass (`standard`, local-path) binds it;
  - whether the broker starts, which means uid 1000 can write;
  - whether `fsGroup: 1000` is needed.

  Same for `postgres:16-alpine`, with `PGDATA` in a subdirectory of the
  mount.
- [ ] **Step 3: A Secret volume is tmpfs on the node.** `kubectl exec` into
  a pod with a Secret volume and run `findmnt -n -o FSTYPE --target /run/secrets`.
  Record the result: it's the Kubernetes counterpart of 8a.1's `/run/callit/secrets`.
- [ ] **Step 4: Delete the scratch resources, record the results, and commit**

```bash
git add deploy/k8s/kind.yaml docs/plans/2026-10-22-phase-8a2-kubernetes.md && \
  git commit -m "docs: record Phase 8a.2 spike results"
```

---

### Task 3: Manifests, and the offline check that enforces them

**Files:**
- Create:
  - `deploy/k8s/kustomization.yaml`
  - `deploy/k8s/namespace.yaml` (namespace `callit`)
  - `deploy/k8s/config.yaml` (ConfigMap: `ENV=production`, `REDIS_ADDR=redis:6379`, `KAFKA_BROKERS=kafka:9092`, `CORS_ALLOWED_ORIGINS=http://localhost:18080`)
  - `deploy/k8s/{postgres,redis,kafka}.yaml` (StatefulSet + headless Service each; settings copied from `compose.prod.yml`, including Kafka's `KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://kafka:9092` and the 1g heap)
  - `deploy/k8s/{api,relay,ledger-worker}.yaml` (Deployments; api also gets a ClusterIP Service on 8080)
  - `deploy/k8s/migrate.yaml` (Job, `backoffLimit: 10`)
  - `deploy/k8s/check-manifests.sh`
  - `deploy/k8s/tests/check-manifests_test.sh`
- Modify: `.github/workflows/ci.yml` (two steps in the `deploy` job: run both scripts)

**Interfaces:**
- Produces: the rendered manifest set, `kubectl kustomize deploy/k8s`,
  which Task 4 applies. The Secret is named `callit-secrets` with keys
  `jwt_secret`, `postgres_password` and `postgres_dsn`. It isn't in the
  kustomization: Task 4 creates it at bring-up.

**Checkpoint 1: the invariants hold and the check bites**

- [ ] **Step 1: Write the failing test, then run it**

Spec: `check-manifests.sh` renders with `kubectl kustomize deploy/k8s`
(override with `MANIFESTS_DIR_OVERRIDE` for mutants) and asserts, printing
`ok: N. …` or `FAIL: N. …`:
1. **No secret-named env key** in any container, and **the grant map is
   exact** (Global Constraints), read from each workload's Secret-volume
   `items[].key`.
2. **Images pinned:** every image is exactly one of the four allowed
   images, and `callit-backend:local` uses `imagePullPolicy: Never`.
3. **postgres, redis and kafka are StatefulSets with `volumeClaimTemplates`.**
4. **The relay has `replicas: 1` and `strategy.type: Recreate`.**
5. **The api has a `readinessProbe` `httpGet` on `/healthz`, port 8080.**

`check-manifests_test.sh` copies `deploy/k8s` to a temp dir, applies one
exact-text mutation, and expects the named check to fail. One mutant per
case:
- `JWT_SECRET` added to api's env: expect check 1.
- Kafka's `volumeClaimTemplates` swapped for an `emptyDir` volume: expect
  check 3.
- The relay's `Recreate` changed to `RollingUpdate`: expect check 4.

The real directory must pass.

Run: `bash deploy/k8s/tests/check-manifests_test.sh`
Expected: FAIL. `check-manifests.sh` doesn't exist yet.

- [ ] **Step 2: Write the manifests and the check, then verify-and-commit**

```bash
bash deploy/k8s/check-manifests.sh && bash deploy/k8s/tests/check-manifests_test.sh && \
  git add deploy/k8s .github/workflows/ci.yml && \
  git commit -m "feat: Kubernetes manifests for the full stack, with an offline invariant check"
```

---

### Task 4: Bring-up and tear-down

**Files:**
- Create: `deploy/k8s/up.sh`, `deploy/k8s/down.sh`

**Interfaces:**
- `up.sh [image-tag]` creates the kind cluster if it's absent, then:
  1. `docker build -t callit-backend:local backend`, unless an image tag
     is given (Task 5 passes a pre-fix build);
  2. `kind load docker-image`;
  3. generates the three secrets into a fresh `/dev/shm` dir (DSN derived
     as in `deploy/secrets.sh`);
  4. `kubectl create secret generic callit-secrets --from-file=…` and
     removes the `/dev/shm` dir;
  5. `kubectl apply -k deploy/k8s`;
  6. waits for the migrate Job to complete and every Deployment and
     StatefulSet to be ready, with a timeout of 5 min;
  7. restarts the backend Deployments when the image changed
     (`kubectl rollout restart`).
- `down.sh` runs `kind delete cluster --name callit`.

- [ ] **Step 1: Bring it up and prove a round works end to end.**
  - `bash deploy/k8s/up.sh`.
  - Port-forward `svc/api 18080:8080`, then
    `curl -fsS localhost:18080/healthz` returns `{"status":"ok"}`.
  - A host and a guest play a round with `callit-cli -addr localhost:18080`
    (`-host`, and `-join <code>`; same flow as `smoke.sh` step 2, without
    `-secure`).
  - The ledger count in the postgres pod increases.
  - No live pod has a secret-named env key:
    `kubectl get pods -n callit -o json | jq '[.items[].spec.containers[].env[]?.name] | map(select(IN("JWT_SECRET","POSTGRES_PASSWORD","POSTGRES_DSN"))) | length'`
    prints `0`. This is check 1 run against the live objects rather than
    the rendered files.
- [ ] **Step 2: Verify and commit**

```bash
bash -n deploy/k8s/up.sh deploy/k8s/down.sh && \
  git add deploy/k8s/up.sh deploy/k8s/down.sh && \
  git commit -m "feat: bring the Kubernetes stack up and down with one command"
```

---

### Task 5: The chaos run (flagship evidence, inline)

**Files:**
- Create: `deploy/k8s/chaos.sh`
- Modify: `deploy/k8s/README.md` (created in Task 6; results recorded there and in `docs/project-history.md`)

**Interfaces:**
- `chaos.sh` takes no arguments and runs against whatever `up.sh` deployed.
  1. Port-forwards api 18080, redis 16379, postgres 15432 and kafka 19092,
     all killed on exit.
  2. Runs `API_BASE_URL=http://localhost:18080 WAGER_TEST_DURATION_S=240 WAGER_PLAYERS=25 k6 run loadtest/wager_latency.js`
     in the background, and captures the `RECONCILE_ROOM_ID=` line.
  3. Kills at **t=60 s** the relay pod, at **t=90 s** the ledger-worker
     pod, and at **t=120 s** the kafka-0 pod, each with
     `kubectl delete pod --grace-period=0 --force`. It re-establishes any
     port-forward a kill broke.
  4. After k6 exits, waits until `XPENDING wager-outbox relay` reports 0,
     via `kubectl exec` into redis-0, and the ledger transaction count is
     unchanged for 15 s. Timeout 120 s, and on timeout it still proceeds.
  5. Runs `cd backend && RECONCILE_ROOM_IDS=<id> REDIS_ADDR=localhost:16379 POSTGRES_DSN=postgres://callit:<pw>@localhost:15432/callit?sslmode=disable KAFKA_BROKERS=localhost:19092 go test ./internal/ledger/ -run TestReconcileAfterLoad -count=1 -v`.
     It reads `<pw>` from the Secret with `kubectl get secret`, into a
     variable, never echoed.
  6. Prints: the kill times, the k6 summary line, the pending count at
     the end, PASS/FAIL, and the reconciler's transaction count. It exits
     with the reconciler's status.

- [ ] **Step 1: The before run must fail.** Build the image from the commit
  *before* Task 1's fix:
  `git archive <pre-fix-sha> backend | tar -x -C /tmp/callit-prefix`, then
  `docker build -t callit-backend:prefix /tmp/callit-prefix/backend`.
  Then `bash deploy/k8s/down.sh; bash deploy/k8s/up.sh callit-backend:prefix && bash deploy/k8s/chaos.sh`.

  Expected: reconciliation **FAILS**, and the pending count is above 0
  (orphaned entries). **If it passes, the kills missed the window.** Rerun
  it once. If it passes twice, record that honestly, and move the relay
  kill to while Kafka is down (t=125 s). Don't weaken the after run to
  match.
- [ ] **Step 2: The after run must pass.**
  `bash deploy/k8s/down.sh; bash deploy/k8s/up.sh && bash deploy/k8s/chaos.sh`.
  Expected: **PASS**, pending at 0, with a wager count comparable to 7b's
  (thousands). **A FAIL here is a real finding: stop and investigate. Never
  loosen the reconciler.**
- [ ] **Step 3: Record both runs' printed summaries, then commit**

```bash
bash -n deploy/k8s/chaos.sh && \
  git add deploy/k8s/chaos.sh && \
  git commit -m "test: chaos run kills relay, ledger worker and Kafka mid-load, then reconciles"
```

---

### Task 6: Docs, review, wrap-up

**Files:**
- Create: `deploy/k8s/README.md`
- Modify: `README.md` (Deployment section: one paragraph and a link),
  `CLAUDE.md`, the parent plan §9 (row 8a.2, 8b's dependency becomes
  8a.2), `docs/project-history.md` (Phase 8a.2 record), `journal/` (via
  the `journal` skill)

- [ ] **Step 1: `deploy/k8s/README.md`.**
  - What it demonstrates, in two lines suitable for a resume.
  - Prerequisites (kind, kubectl, k6), and `up.sh`, `chaos.sh`, `down.sh`.
  - The before/after chaos results from Task 5.
  - The Compose → Kubernetes mapping.
  - **Limits**, stated plainly:
    - single-node kind;
    - Secrets are base64 in etcd, not encrypted at rest;
    - no NetworkPolicies, so the Compose network isolation isn't
      reproduced;
    - one relay by design (Decision 3);
    - not used in production. The VM still runs Compose.
- [ ] **Step 2: `CLAUDE.md`.**
  - A Critical Invariant: *exactly one relay runs, and it claims orphaned
    outbox entries before new work (Phase 8a.2); never scale the relay,
    or switch its rollout to RollingUpdate*.
  - `deploy/k8s/` in Repository Layout, plus the commands under Build & Test.
- [ ] **Step 3: Security review** with the `security-reviewer` agent over
  `git diff dev...HEAD`, which touches money movement (the relay). Fix
  CRITICAL and HIGH findings as `fix:` commits, and record the rest.
- [ ] **Step 4: Verify and commit**

```bash
make lint && make test && bash deploy/k8s/check-manifests.sh && bash deploy/k8s/tests/check-manifests_test.sh && \
  git add deploy/k8s/README.md README.md CLAUDE.md docs/plans/2026-08-21-implementation-plan.md docs/project-history.md && \
  git commit -m "docs: Kubernetes deployment and chaos-run results (Phase 8a.2)"
```

Then write the journal entry. The branch is green: hand off to
`finishing-a-development-branch`.

---

## Spike Results (Task 2)

Run 2026-10-22 on Docker Desktop (16 CPUs, 7.4 GiB). Decisions 2–4 stand.
Two findings change Task 3's manifests (marked **→**).

| Check | Result |
|---|---|
| kind | **v0.33.0** (released 2026-08-26), `sha256` verified against the release's `.sha256sum`, in `~/.local/bin`. The node runs **Kubernetes v1.37.0**. |
| kubectl skew | The installed `/usr/local/bin/kubectl` was **v1.32.2**, five minors behind the node (supported skew is ±1). **v1.37.1** (released 2026-09-23) was installed to `~/.local/bin`, which is earlier on `PATH`, with its `sha256` verified. It bundles kustomize v5.8.1. CI only uses `kubectl kustomize` (rendering, no cluster), so the runner's kubectl version doesn't matter there. |
| PVCs | The default StorageClass `standard` (`rancher.io/local-path`, `WaitForFirstConsumer`) bound both 1Gi claims. The mount root is `777 root`, so **no `fsGroup` is needed**. Kafka runs as uid 1000 and wrote `meta.properties` and `__cluster_metadata-0`. Postgres initialized in `PGDATA=/var/lib/postgresql/data/pgdata`. |
| Kafka survives a force-kill | A message was produced, then `kubectl delete pod kafka-0 --grace-period=0 --force`. The broker restarted on the same PVC with no cluster-ID mismatch: end offset `1` before and after, and the message read back intact. |
| Secret volume | The mount at `/run/secrets` is **tmpfs**: the Kubernetes counterpart of 8a.1's `/run/callit/secrets`. |
| **→ ServiceAccount token collision** | A read-only Secret volume at `/run/secrets` made the container fail to start (`RunContainerError`). The kubelet's automatic token mount at `/var/run/secrets/kubernetes.io/serviceaccount` can't create its mount point inside it (`/var/run` → `/run`). **Every CallIt pod sets `automountServiceAccountToken: false`.** None of them talks to the Kubernetes API, so this is also least privilege: a compromised pod holds no cluster credential. The paths stay `/run/secrets/<name>`, as in Compose. |
| **→ Kafka needs a Service named `kafka`** | The broker advertises `PLAINTEXT://kafka:9092`. Without a Service by that name, every client, including the broker's own CLI tools, hangs on `UnknownHostException: kafka`. `kafka.yaml` must include a headless Service `kafka`, plus a readiness probe matching Compose's healthcheck (`kafka-broker-api-versions.sh --bootstrap-server localhost:9092`). |

---

## Execution Deviations

Recorded after execution (2026-10-22). Results: `deploy/k8s/README.md`;
full record: `docs/project-history.md`, Phase 8a.2.

1. **The relay is a one-replica StatefulSet, not a Deployment with
   `Recreate` (Decision 3).** The security review showed `Recreate` only
   governs rollouts. On a pod delete, eviction or drain, a Deployment
   starts the replacement while the old pod is still terminating, so two
   relays can overlap. A StatefulSet waits for `relay-0` to be gone. Check
   4 asserts the StatefulSet, and the "rolling relay" mutant became a
   "two relays" mutant (`f42834c`).
2. **The chaos schedule gained a second relay kill at 125 s, during the
   Kafka outage, and "outbox drains" became a pass condition (Task 5).**
   The first before run, with the relay killed at 60 s only, passed the
   reconciler while 25 entries sat stranded under a dead consumer. They'd
   all reached Kafka before the kill, and a ledger check can't see a stuck
   outbox. The plan's fallback (rerun once, then move the kill into the
   outage) was taken without the identical rerun, because the diagnosis
   already explained the pass.
3. **Port-forwards during the run: the API only (Task 5).** k6 needs only
   the API, which is never killed. The Redis, Postgres and Kafka forwards
   open after the load, for the reconciler, so no forward needs
   re-establishing after a kill.
4. **Manifest details found in the spike or at bring-up, not in the plan:**
   - `automountServiceAccountToken: false` on every pod;
   - a Service named `kafka`, plus a broker readiness probe;
   - `runAsUser: 65532`, because the image names its user;
   - a user-local `kubectl` 1.37.1, because the system 1.32 was five
     minors behind the node.
5. **Security-review fixes beyond Decision 3:**
   - check 1 now closes every route a Secret can take into a pod
     (`270dc66`);
   - the reconciler masks the DSN password in its failure messages
     (`e4d9615`).
