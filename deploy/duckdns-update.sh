#!/usr/bin/env bash
# Points the DuckDNS record at this machine's current public IP. Run from
# cron every 5 minutes (deploy/README.md); DuckDNS fills in the caller's
# address when ip= is left empty.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
# Read values out of .env, never source it: sourcing would execute
# anything in the file. The token itself is a file that
# callit-secrets.service fetched from 1Password (plan 8a.1).
env_get() { grep -m1 "^$1=" "$here/.env" | cut -d= -f2-; }
CALLIT_HOSTNAME="$(env_get CALLIT_HOSTNAME)"
CALLIT_SECRETS_DIR="$(env_get CALLIT_SECRETS_DIR)"
: "${CALLIT_HOSTNAME:?missing from deploy/.env}"
: "${CALLIT_SECRETS_DIR:?missing from deploy/.env}"
DUCKDNS_TOKEN="$(cat "$CALLIT_SECRETS_DIR/duckdns_token")"
: "${DUCKDNS_TOKEN:?empty — check: systemctl status callit-secrets}"

domain="${CALLIT_HOSTNAME%.duckdns.org}"
reply="$(curl -fsS --max-time 15 "https://www.duckdns.org/update?domains=${domain}&token=${DUCKDNS_TOKEN}&ip=")"
[ "$reply" = OK ] || { echo "duckdns-update: DuckDNS replied '$reply'" >&2; exit 1; }
