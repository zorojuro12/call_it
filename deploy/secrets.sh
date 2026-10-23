#!/usr/bin/env bash
# Fetches the production secrets from the callit-prod 1Password vault and
# writes them, one file each, into $CALLIT_SECRETS_DIR (default
# /run/callit/secrets, which is tmpfs). compose.prod.yml mounts those files
# into the containers as file secrets, so no secret is ever a container
# environment variable, which Docker would persist in its on-disk config.
# Run as root by callit-secrets.service at boot and before every deploy.
set -euo pipefail

dir="${CALLIT_SECRETS_DIR:-/run/callit/secrets}"
vault=callit-prod

fail() { echo "secrets: $*" >&2; exit 1; }

# fetch <item>: the item's password, exactly, with no trailing newline.
fetch() { op read --no-newline "op://$vault/$1/password"; }

# Everything is fetched before anything is written.
jwt="$(fetch jwt-secret)" || fail "could not read jwt-secret"
pg="$(fetch postgres-password)" || fail "could not read postgres-password"
duck="$(fetch duckdns-token)" || fail "could not read duckdns-token"
dsn="postgres://callit:$pg@postgres:5432/callit?sslmode=disable"

umask 077
mkdir -p "$dir"
chmod 0750 "$dir"

# put <name> <value>: write to a temp file in the same dir, then rename
# over the final name, so a reader sees the old file or the new one, never
# a partial write.
put() {
  local tmp
  tmp="$(mktemp "$dir/.tmp.XXXXXX")"
  printf '%s' "$2" >"$tmp"
  chmod 0444 "$tmp"
  mv -f "$tmp" "$dir/$1"
}

put jwt_secret "$jwt"
put postgres_password "$pg"
put postgres_dsn "$dsn"
put duckdns_token "$duck"
echo "secrets: wrote 4 secrets to $dir"
