#!/usr/bin/env bash
# Refuses to let a deploy start unless the secrets dir is RAM-backed and
# complete: deploy/preflight-secrets.sh <dir>
# Run by deploy.sh before `compose up`, so Compose never meets a missing
# secret file, and a dir pointed at disk by mistake is caught before any
# secret is relied on there.
set -euo pipefail

dir="${1:?usage: deploy/preflight-secrets.sh <dir>}"
fail() { echo "preflight: $*" >&2; exit 1; }

[ -d "$dir" ] || fail "$dir does not exist; run: sudo systemctl restart callit-secrets"
[ "$(findmnt -n -o FSTYPE --target "$dir")" = tmpfs ] || fail "$dir is not on tmpfs"
for f in jwt_secret postgres_password postgres_dsn duckdns_token; do
  [ -f "$dir/$f" ] && [ -s "$dir/$f" ] || fail "$dir/$f missing or empty"
done
echo "preflight: secrets in $dir are complete and on tmpfs"
