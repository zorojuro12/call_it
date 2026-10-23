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
# Read, never source: sourcing .env would execute anything in it.
env_get() { { grep -m1 "^$1=" "$here/.env" || true; } | cut -d= -f2-; }
[ -z "$(git -C "$repo" status --porcelain --untracked-files=no)" ] \
  || fail "working tree has local changes; refusing to check out over them"

previous="$(cat "$state" 2>/dev/null || echo none)"

git -C "$repo" fetch --tags --quiet origin
# Resolve to a commit first, with option parsing ended, so a ref can never
# be read as a checkout option (e.g. "--orphan=x"); then check out the SHA.
sha="$(git -C "$repo" rev-parse --verify --quiet --end-of-options "$ref^{commit}")" \
  || fail "'$ref' is not a commit in this repository"
git -C "$repo" checkout --quiet --detach "$sha"
echo "deploy: checked out $ref (${sha:0:7}); previous: $previous"

bash "$here/check-compose.sh" >/dev/null || fail "check-compose.sh failed for $ref"

# Refetch the secrets so an edit in 1Password takes effect on this
# deploy, then refuse to start unless they're complete and on tmpfs.
sudo systemctl restart callit-secrets.service || fail "fetching secrets from 1Password failed (journalctl -u callit-secrets)"
secrets_dir="$(env_get CALLIT_SECRETS_DIR)"
[ -n "$secrets_dir" ] || fail "CALLIT_SECRETS_DIR missing from deploy/.env"
bash "$here/preflight-secrets.sh" "$secrets_dir" >/dev/null || fail "secrets preflight failed"

dc() { docker compose -f "$here/compose.prod.yml" --env-file "$here/.env" "$@"; }
dc build
dc up -d --wait

hostname="$(env_get CALLIT_HOSTNAME)"
[ -n "$hostname" ] || fail "CALLIT_HOSTNAME missing from deploy/.env"
if ! bash "$here/smoke.sh" "$hostname"; then
  echo "deploy: smoke test FAILED for $ref" >&2
  if [ "$previous" != none ]; then
    echo "deploy: to roll back, run: deploy/deploy.sh $previous" >&2
  fi
  exit 1
fi

echo "$ref" >"$state"
echo "deploy: $ref is live"
