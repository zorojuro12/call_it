#!/usr/bin/env bash
# Restores an archive written by deploy/backup.sh, replacing BOTH the
# PostgreSQL ledger and all Redis data (every account and balance).
# Usage: deploy/restore.sh [--yes] <archive>
# Without --yes it asks for confirmation, and refuses when not on a TTY.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
dc() { docker compose -f "$here/compose.prod.yml" --env-file "$here/.env" "$@"; }
fail() { echo "restore: $*" >&2; exit 1; }

yes=false
if [ "${1:-}" = "--yes" ]; then yes=true; shift; fi
archive="${1:-}"
[ -n "$archive" ] || fail "usage: deploy/restore.sh [--yes] <archive>"
[ -f "$archive" ] || fail "no such archive: $archive"

if ! $yes; then
  [ -t 0 ] || fail "not a terminal; pass --yes to restore non-interactively"
  read -r -p "This replaces ALL Redis and PostgreSQL data. Type RESTORE to continue: " answer
  [ "$answer" = RESTORE ] || fail "aborted"
fi

staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT
tar -xzf "$archive" -C "$staging"
[ -f "$staging/postgres.sql" ] && [ -d "$staging/redis" ] || fail "archive is missing postgres.sql or redis/"

# Nothing may write either store mid-restore.
dc stop api relay ledger-worker

dc exec -T postgres psql -q -v ON_ERROR_STOP=1 -U callit callit <"$staging/postgres.sql" >/dev/null \
  || fail "PostgreSQL restore failed"

# Redis must be stopped while its volume is replaced, or it would write its
# in-memory state back over the restored files on shutdown. The swap runs in
# a throwaway container on the same volume, using the redis image itself so
# the redis user exists for the chown.
dc stop redis
volume="$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}' "$(dc ps -aq redis)")"
[ -n "$volume" ] || fail "could not find the Redis data volume"
docker run --rm -v "$volume:/data" -v "$staging/redis:/restore:ro" redis:7.2-alpine \
  sh -c 'rm -rf /data/* && cp -a /restore/. /data/ && chown -R redis:redis /data' \
  || fail "Redis volume swap failed"

dc up -d --wait redis
dc up -d api relay ledger-worker
echo "restore: restored $archive"
