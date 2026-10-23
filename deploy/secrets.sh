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

# The service account token comes from the systemd credential when run by
# callit-secrets.service (LoadCredential=op-token:...), else from the
# environment. It reaches op only through op's environment, never argv.
if [ -n "${CREDENTIALS_DIRECTORY:-}" ] && [ -s "$CREDENTIALS_DIRECTORY/op-token" ]; then
  OP_SERVICE_ACCOUNT_TOKEN="$(<"$CREDENTIALS_DIRECTORY/op-token")"
fi
[ -n "${OP_SERVICE_ACCOUNT_TOKEN:-}" ] || fail "no 1Password service account token (see deploy/README.md §4)"
export OP_SERVICE_ACCOUNT_TOKEN

# fetch <item>: the item's password, exactly, with no trailing newline.
fetch() { op read --no-newline "op://$vault/$1/password"; }

# Everything is fetched before anything is written.
jwt="$(fetch jwt-secret)" || fail "could not read jwt-secret"
pg="$(fetch postgres-password)" || fail "could not read postgres-password"
duck="$(fetch duckdns-token)" || fail "could not read duckdns-token"

# ...and validated as a set before anything is written. Messages name the
# item, never its value. The Postgres pattern keeps the password safe to
# embed in the DSN without escaping.
[[ "$jwt" =~ ^[0-9a-f]{64}$ ]] || fail "jwt-secret must be 64 lowercase hex characters"
[[ "$pg" =~ ^[A-Za-z0-9]{16,}$ ]] || fail "postgres-password must be 16+ letters and digits"
[[ "$duck" =~ ^[A-Za-z0-9-]+$ ]] || fail "duckdns-token may contain only letters, digits and '-'"
dsn="postgres://callit:$pg@postgres:5432/callit?sslmode=disable"

names=(jwt_secret postgres_password postgres_dsn duckdns_token)
# A missing bind source can leave a directory where a file belongs; mv
# would then move the new secret into it and report success.
for name in "${names[@]}"; do
  if [ -e "$dir/$name" ] && [ ! -f "$dir/$name" ]; then
    fail "$dir/$name exists and is not a regular file; remove it and rerun"
  fi
done

# Parents this run creates (on first boot, /run/callit) must stay
# traversable, so they're made under umask 022; mkdir -p leaves existing
# dirs' modes alone. The secrets dir itself is then narrowed to 0750.
(umask 022; mkdir -p "$dir")
umask 077
chmod 0750 "$dir"
# The docker group may read the dir (deploy.sh, the DuckDNS cron job). It
# is root-equivalent already, so this grants nothing new; containers read
# their files through the bind mount, which needs the files 0444.
if [ "$(id -u)" = 0 ]; then chgrp docker "$dir"; fi

# Two phases, so a failure writing any one file (a full tmpfs, a kill)
# replaces none of them: first every value goes to a temp file in the
# same dir, then each is renamed over its final name. A rename can't run
# out of space, and a reader sees an old file or a new one, never a
# partial write. Staged temp files are removed on any exit, so none is
# left on tmpfs holding a secret.
staged=()
trap 'rm -f "${staged[@]}"' EXIT
# stage <value>: prints the path of a new 0444 temp file holding value.
stage() {
  local tmp
  tmp="$(mktemp "$dir/.tmp.XXXXXX")"
  staged+=("$tmp")
  printf '%s' "$1" >"$tmp"
  chmod 0444 "$tmp"
}

values=("$jwt" "$pg" "$dsn" "$duck")
for i in "${!names[@]}"; do stage "${values[$i]}"; done
for i in "${!names[@]}"; do mv -f "${staged[$i]}" "$dir/${names[$i]}"; done
staged=()
echo "secrets: wrote 4 secrets to $dir"
