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
# Every workload's pod spec, as {name, kind, spec}.
workloads='[.[] | select(.kind == "Deployment" or .kind == "StatefulSet" or .kind == "Job") | {name: .metadata.name, kind, spec: .spec.template.spec}]'

# 1. No secret in any container's environment, and each workload is
#    granted exactly the secret files it needs.
leaked=$(q "$workloads"' | [.[] | .name as $w | .spec.containers[] | (.env // [])[] | .name
  | select(IN("JWT_SECRET","POSTGRES_PASSWORD","POSTGRES_DSN","DUCKDNS_TOKEN")) | "\($w).\(.)"] | join(",")')
[ -z "$leaked" ] || fail "1. secret passed as an environment value: $leaked"
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

# 4. Exactly one relay, replaced rather than rolled: it claims every
#    pending outbox entry on startup, which a second live relay would have
#    taken from it.
relay=$(q '[.[] | select(.kind == "Deployment" and .metadata.name == "relay")][0] // {}')
[ "$(jq -r '.spec.replicas // 1' <<<"$relay")" = 1 ] || fail "4. relay replicas is not 1"
[ "$(jq -r '.spec.strategy.type // "RollingUpdate"' <<<"$relay")" = Recreate ] || fail "4. relay strategy is not Recreate"
ok "4. one relay, Recreate strategy"

# 5. The API only receives traffic once /healthz answers.
probe=$(q '[.[] | select(.kind == "Deployment" and .metadata.name == "api") | .spec.template.spec.containers[0].readinessProbe.httpGet | "\(.path):\(.port)"][0] // ""')
[ "$probe" = "/healthz:8080" ] || fail "5. api readinessProbe is not httpGet /healthz on 8080 (got '$probe')"
ok "5. api readiness probe on /healthz"
