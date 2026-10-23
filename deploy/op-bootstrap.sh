#!/usr/bin/env bash
# Creates the production secrets as items in the callit-prod 1Password
# vault. Run on your laptop, signed in to op as yourself; the server only
# ever gets read-only access (deploy/README.md §4).
#   deploy/op-bootstrap.sh --generate <duckdns-token>   fresh install
#
# It never overwrites an item: a replaced postgres-password would no longer
# open the existing database volume, and a replaced jwt-secret logs
# everyone out. Values reach op on stdin, never in argv.
set -euo pipefail

vault=callit-prod
items=(jwt-secret postgres-password duckdns-token)

fail() { echo "op-bootstrap: $*" >&2; exit 1; }
usage() { echo "usage: deploy/op-bootstrap.sh --generate <duckdns-token>" >&2; exit 2; }

# validate <jwt> <pg> <duck>: the same rules deploy/secrets.sh applies on
# the server, so nothing goes into the vault that the server would refuse.
validate() {
  [[ "$1" =~ ^[0-9a-f]{64}$ ]] || fail "JWT_SECRET must be 64 lowercase hex characters"
  [[ "$2" =~ ^[A-Za-z0-9]{16,}$ ]] || fail "POSTGRES_PASSWORD must be 16+ letters and digits"
  [[ "$3" =~ ^[A-Za-z0-9-]+$ ]] || fail "DUCKDNS_TOKEN may contain only letters, digits and '-'"
}

# create <title> <value>: a Password item. The value goes into the JSON
# template through jq's stdin, and the template into op's stdin, so it
# appears in no process's argv.
create() {
  printf '%s' "$2" \
    | jq -Rs --arg title "$1" \
        '{title: $title, category: "PASSWORD", fields: [{id: "password", type: "CONCEALED", purpose: "PASSWORD", label: "password", value: .}]}' \
    | op item create --vault "$vault" - >/dev/null \
    || fail "creating $1 failed"
  echo "op-bootstrap: created $1"
}

case "${1:-}" in
  --generate)
    [ $# -eq 2 ] || usage
    jwt="$(openssl rand -hex 32)"
    pg="$(openssl rand -hex 24)"
    duck="$2"
    ;;
  *) usage ;;
esac
validate "$jwt" "$pg" "$duck"

op whoami >/dev/null || fail "not signed in to op; run: eval \$(op signin)"
# Every item is checked before the first create, so a refusal creates
# nothing.
for item in "${items[@]}"; do
  if op item get "$item" --vault "$vault" >/dev/null 2>&1; then
    fail "item $item already exists in $vault; refusing to overwrite"
  fi
done

create jwt-secret "$jwt"
create postgres-password "$pg"
create duckdns-token "$duck"
