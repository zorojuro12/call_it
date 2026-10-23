#!/usr/bin/env bash
# Brings the full stack up on a local kind cluster (plan 8a.2):
#   deploy/k8s/up.sh            build backend/ and deploy it
#   deploy/k8s/up.sh <image>    deploy an already built image instead
#                               (chaos.sh's before/after runs use this)
# Safe to rerun: it reuses the cluster and the Secret, and redeploys.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
cluster=callit
image="${1:-}"

fail() { echo "up: $*" >&2; exit 1; }
k() { kubectl --context "kind-$cluster" -n callit "$@"; }

for tool in kind kubectl docker openssl; do
  command -v "$tool" >/dev/null || fail "$tool is not installed (deploy/k8s/README.md)"
done

if ! kind get clusters 2>/dev/null | grep -qx "$cluster"; then
  kind create cluster --config "$here/kind.yaml"
fi

if [ -n "$image" ]; then
  docker image inspect "$image" >/dev/null 2>&1 || fail "no local image $image"
  docker tag "$image" callit-backend:local
else
  docker build -q -t callit-backend:local "$repo/backend" >/dev/null
fi
kind load docker-image callit-backend:local --name "$cluster" >/dev/null

kubectl --context "kind-$cluster" apply -f "$here/namespace.yaml" >/dev/null

# The Secret is created once and then kept: Postgres initializes its
# volume with the first password, so a new one on a rerun would lock the
# ledger worker out. Values are generated on tmpfs, handed to the API
# server, and the files removed; they are never written anywhere else.
if ! k get secret callit-secrets >/dev/null 2>&1; then
  s="$(mktemp -d /dev/shm/callit-k8s-secrets.XXXXXX)"
  trap 'rm -rf "$s"' EXIT
  pg="$(openssl rand -hex 24)"
  printf %s "$(openssl rand -hex 32)" >"$s/jwt_secret"
  printf %s "$pg" >"$s/postgres_password"
  printf %s "postgres://callit:$pg@postgres:5432/callit?sslmode=disable" >"$s/postgres_dsn"
  k create secret generic callit-secrets \
    --from-file="$s/jwt_secret" --from-file="$s/postgres_password" --from-file="$s/postgres_dsn" >/dev/null
  rm -rf "$s"
fi

# A Job's pod template is immutable, and a new image should migrate again.
k delete job migrate --ignore-not-found >/dev/null
kubectl --context "kind-$cluster" apply -k "$here" >/dev/null
# The backend image keeps one tag, so Kubernetes can't see that it
# changed: restart the Deployments onto the image just loaded.
k rollout restart deployment/api deployment/relay deployment/ledger-worker >/dev/null

k wait --for=condition=complete job/migrate --timeout=300s >/dev/null || fail "migrate did not complete (k logs job/migrate)"
for w in statefulset/postgres statefulset/redis statefulset/kafka deployment/api deployment/relay deployment/ledger-worker; do
  k rollout status "$w" --timeout=300s >/dev/null || fail "$w did not become ready"
done
echo "up: the stack is ready in namespace callit (kubectl --context kind-$cluster -n callit get pods)"
