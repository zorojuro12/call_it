#!/usr/bin/env bash
# Points the DuckDNS record at this machine's current public IP. Run from
# cron every 5 minutes (deploy/README.md); DuckDNS fills in the caller's
# address when ip= is left empty.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
# shellcheck disable=SC1091
. "$here/.env"
: "${CALLIT_HOSTNAME:?missing from deploy/.env}"
: "${DUCKDNS_TOKEN:?missing from deploy/.env — rerun setup with a token}"

domain="${CALLIT_HOSTNAME%.duckdns.org}"
reply="$(curl -fsS --max-time 15 "https://www.duckdns.org/update?domains=${domain}&token=${DUCKDNS_TOKEN}&ip=")"
[ "$reply" = OK ] || { echo "duckdns-update: DuckDNS replied '$reply'" >&2; exit 1; }
