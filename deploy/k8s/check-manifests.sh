#!/usr/bin/env bash
# Renders the Kubernetes manifests offline (no cluster needed) and asserts
# the invariants the stack's safety rests on (plan 8a.2): secrets reach
# pods only as files granted to exactly the workloads that need them,
# images are pinned, the data stores are persistent, exactly one relay
# runs and is never rolled, and the API is only routed traffic once
# /healthz answers. tests/check-manifests_test.sh proves each check bites.
set -euo pipefail

cd "$(dirname "$0")"
# MANIFESTS_DIR_OVERRIDE renders a different directory; the test uses it
# to prove each check rejects a deliberately broken copy.
dir="${MANIFESTS_DIR_OVERRIDE:-.}"

fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "ok: $*"; }

rendered=$(kubectl kustomize "$dir") || fail "0. $dir does not render with kubectl kustomize"
cfg=$(python3 -I -c 'import json, sys, yaml; json.dump([d for d in yaml.safe_load_all(sys.stdin) if d], sys.stdout)' <<<"$rendered") \
  || fail "0. rendered manifests are not valid YAML"
q() { jq -r "$@" <<<"$cfg"; }
# Every pod-bearing object's pod spec, as {name, kind, spec}, with init
# containers folded into containers so no check can be dodged through one.
workloads='[.[] | {name: .metadata.name, kind, spec: (
    if .kind == "Pod" then .spec
    elif .kind == "CronJob" then .spec.jobTemplate.spec.template.spec
    elif IN(.kind; "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job") then .spec.template.spec
    else empty end)}
  | .spec.containers = ((.spec.containers // []) + (.spec.initContainers // []))]'

# 1. No secret in any container's environment, and each workload is
#    granted exactly the secret files it needs. Every other route a Secret
#    can take into a pod is refused outright: a volume with no items
#    (it mounts every key, yet would count as no grant), a projected
#    volume, a secretKeyRef env var, or envFrom.secretRef.
leaked=$(q "$workloads"' | [.[] | .name as $w | .spec.containers[] | (.env // [])[] | .name
  | select(IN("JWT_SECRET","POSTGRES_PASSWORD","POSTGRES_DSN","DUCKDNS_TOKEN")) | "\($w).\(.)"] | join(",")')
[ -z "$leaked" ] || fail "1. secret passed as an environment value: $leaked"
routes=$(q "$workloads"' | [.[] | .name as $w |
  ((.spec.volumes // [])[] | select(.secret != null and ((.secret.items // []) | length) == 0) | "\($w): secret volume \(.name) has no items"),
  ((.spec.volumes // [])[] | select(any(.projected.sources[]?; .secret != null)) | "\($w): projected secret volume \(.name)"),
  (.spec.containers[] | (.env // [])[] | select(.valueFrom.secretKeyRef != null) | "\($w): env \(.name) from secretKeyRef"),
  (.spec.containers[] | (.envFrom // [])[] | select(.secretRef != null) | "\($w): envFrom secretRef")] | join("; ")')
[ -z "$routes" ] || fail "1. secret reaches a pod outside its granted files: $routes"
want_grants="api=jwt_secret
kafka=
ledger-worker=postgres_dsn
migrate=postgres_dsn
postgres=postgres_password
redis=
relay="
got_grants=$(q "$workloads"' | map("\(.name)=\([.spec.volumes[]?.secret.items[]?.key] | sort | join(","))") | .[]' | sort)
[ "$got_grants" = "$(sort <<<"$want_grants")" ] \
  || fail "1. secret grants differ:
$(diff <(sort <<<"$want_grants") <(echo "$got_grants") || true)"
ok "1. secrets are files, granted only where needed"

# 2. Images are exactly the ones production runs, and the locally built
#    backend image is never pulled (there is no registry).
bad_images=$(q "$workloads"' | [.[] | .spec.containers[] | .image
  | select(IN("postgres:16-alpine","redis:7.2-alpine","apache/kafka:3.7.0","callit-backend:local") | not)] | join(",")')
[ -z "$bad_images" ] || fail "2. unexpected or unpinned image: $bad_images"
pulled=$(q "$workloads"' | [.[] | .name as $w | .spec.containers[] | select(.image == "callit-backend:local" and .imagePullPolicy != "Never") | $w] | join(",")')
[ -z "$pulled" ] || fail "2. callit-backend:local may be pulled by: $pulled"
ok "2. images pinned; the backend image is never pulled"

# 3. The data stores are StatefulSets on persistent volumes: Kafka on an
#    emptyDir would lose unconsumed messages when its pod is deleted.
for s in postgres redis kafka; do
  [ "$(q --arg s "$s" '[.[] | select(.kind == "StatefulSet" and .metadata.name == $s and ((.spec.volumeClaimTemplates // []) | length) > 0)] | length')" = 1 ] \
    || fail "3. $s is not a StatefulSet with volumeClaimTemplates"
done
ok "3. postgres, redis and kafka are persistent StatefulSets"

# 4. At most one relay pod ever exists: it claims every pending outbox
#    entry on startup, which a second live relay would have taken from
#    it. A Deployment can't promise that (on a pod delete, eviction or
#    drain its replacement starts while the old pod is still terminating,
#    whatever the strategy); a one-replica StatefulSet waits for the old
#    pod to be gone first.
relay=$(q '[.[] | select(.metadata.name == "relay" and (.kind == "Deployment" or .kind == "StatefulSet"))]')
[ "$(jq -r 'map(.kind) | join(",")' <<<"$relay")" = StatefulSet ] || fail "4. relay must be exactly one StatefulSet (got: $(jq -r 'map(.kind) | join(",")' <<<"$relay"))"
[ "$(jq -r '.[0].spec.replicas // 1' <<<"$relay")" = 1 ] || fail "4. relay replicas is not 1"
ok "4. relay is a one-replica StatefulSet"

# 5. The API only receives traffic once /healthz answers.
probe=$(q '[.[] | select(.kind == "Deployment" and .metadata.name == "api") | .spec.template.spec.containers[0].readinessProbe.httpGet | "\(.path):\(.port)"][0] // ""')
[ "$probe" = "/healthz:8080" ] || fail "5. api readinessProbe is not httpGet /healthz on 8080 (got '$probe')"
ok "5. api readiness probe on /healthz"
