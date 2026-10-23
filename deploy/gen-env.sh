#!/usr/bin/env bash
# Creates deploy/.env on the server: the non-secret settings compose.prod.yml
# interpolates. Usage: deploy/gen-env.sh <hostname>
#
# It holds no secret. Secrets live in the callit-prod 1Password vault
# (deploy/op-bootstrap.sh creates them) and reach the containers as files
# that deploy/secrets.sh writes into CALLIT_SECRETS_DIR (plan 8a.1).
#
# Run once per install. It refuses to overwrite an existing .env; change
# the hostname by editing the file (deploy/README.md §9).
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
env_file="$here/.env"

if [ $# -ne 1 ] || [ -z "$1" ]; then
  if [ $# -eq 2 ]; then
    echo "gen-env: the DuckDNS token now lives in 1Password (deploy/README.md §4)" >&2
  else
    echo "usage: deploy/gen-env.sh <hostname>" >&2
  fi
  exit 2
fi
hostname="$1"

# Validated before anything is written: other scripts read these values
# back out of .env, so nothing resembling shell syntax may reach the file.
if ! [[ "$hostname" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]]; then
  echo "gen-env: '$hostname' is not a lowercase DNS hostname" >&2
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
  echo "CALLIT_SECRETS_DIR=/run/callit/secrets"
} >"$env_file" 2>/dev/null || refuse
set +C

echo "wrote $env_file"
