#!/usr/bin/env bash
# Creates deploy/.env on the server with freshly generated secrets.
# Usage: deploy/gen-env.sh <hostname> [duckdns-token]
#
# Run once per install. It refuses to overwrite an existing .env: a new
# JWT_SECRET would log out every session, and a new POSTGRES_PASSWORD would
# no longer match the database volume it was first created with.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
env_file="$here/.env"

if [ $# -lt 1 ] || [ -z "$1" ]; then
  echo "usage: deploy/gen-env.sh <hostname> [duckdns-token]" >&2
  exit 2
fi
hostname="$1"
token="${2:-}"

# Validated before anything is written: other scripts read these values
# back out of .env, so nothing resembling shell syntax may reach the file.
if ! [[ "$hostname" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]]; then
  echo "gen-env: '$hostname' is not a lowercase DNS hostname" >&2
  exit 2
fi
if [ -n "$token" ] && ! [[ "$token" =~ ^[A-Za-z0-9-]+$ ]]; then
  echo "gen-env: the DuckDNS token may contain only letters, digits and '-'" >&2
  exit 2
fi

refuse() { echo "deploy/.env already exists; refusing to overwrite" >&2; exit 1; }
[ -e "$env_file" ] && refuse

umask 077
# noclobber makes the create itself fail if the file appeared since the
# check above, so two concurrent runs can't both write.
set -C
{
  echo "CALLIT_HOSTNAME=$hostname"
  echo "JWT_SECRET=$(openssl rand -hex 32)"
  echo "POSTGRES_PASSWORD=$(openssl rand -hex 24)"
  if [ -n "$token" ]; then echo "DUCKDNS_TOKEN=$token"; fi
} >"$env_file" 2>/dev/null || refuse
set +C

echo "wrote $env_file"
