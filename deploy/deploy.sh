#!/usr/bin/env bash
# Deploys a git ref (normally a release tag) on the server:
#   deploy/deploy.sh v0.2.0
# Rollback is the same command with the previous ref, which this script
# prints when a deploy's smoke test fails. It does not roll back by
# itself: migrate may already have applied a newer schema, and silently
# starting older code against it is worse than stopping for a human.
set -euo pipefail

ref="${1:?usage: deploy/deploy.sh <git-ref>}"
here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/.." && pwd)"
state="$here/.deployed-ref"

fail() { echo "deploy: $*" >&2; exit 1; }

[ -f "$here/.env" ] || fail "deploy/.env missing — run deploy/gen-env.sh first"
[ -z "$(git -C "$repo" status --porcelain --untracked-files=no)" ] \
  || fail "working tree has local changes; refusing to check out over them"

previous="$(cat "$state" 2>/dev/null || echo none)"

git -C "$repo" fetch --tags --quiet origin
git -C "$repo" checkout --quiet --detach "$ref"
echo "deploy: checked out $ref ($(git -C "$repo" rev-parse --short HEAD)); previous: $previous"

bash "$here/check-compose.sh" >/dev/null || fail "check-compose.sh failed for $ref"

dc() { docker compose -f "$here/compose.prod.yml" --env-file "$here/.env" "$@"; }
dc build
dc up -d --wait

# shellcheck disable=SC1091
hostname="$(. "$here/.env" && echo "$CALLIT_HOSTNAME")"
if ! bash "$here/smoke.sh" "$hostname"; then
  echo "deploy: smoke test FAILED for $ref" >&2
  if [ "$previous" != none ]; then
    echo "deploy: to roll back, run: deploy/deploy.sh $previous" >&2
  fi
  exit 1
fi

echo "$ref" >"$state"
echo "deploy: $ref is live"
