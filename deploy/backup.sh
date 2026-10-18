#!/usr/bin/env bash
# Backs up both stores that hold money state into one archive:
#   PostgreSQL — the ledger, via pg_dump
#   Redis      — accounts, balances and live sessions (they live permanently
#                in Redis, parent plan §4 / Amendment B1), via a fresh
#                BGSAVE plus the append-only files Redis loads on start
# Usage: deploy/backup.sh [dest-dir]   (default /var/backups/callit)
# Keeps the newest 7 archives in dest-dir. Restore with deploy/restore.sh.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
dest="${1:-/var/backups/callit}"
keep=7
save_timeout=60

dc() { docker compose -f "$here/compose.prod.yml" --env-file "$here/.env" "$@"; }
fail() { echo "backup: $*" >&2; exit 1; }

umask 077
mkdir -p "$dest"
# umask only covers a directory this run creates; tighten an existing one.
chmod 700 "$dest"
staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT

dc exec -T postgres pg_dump -U callit --clean --if-exists callit >"$staging/postgres.sql" \
  || fail "pg_dump failed"

before="$(dc exec -T redis redis-cli LASTSAVE)"
dc exec -T redis redis-cli BGSAVE >/dev/null || fail "BGSAVE failed"
for _ in $(seq "$save_timeout"); do
  [ "$(dc exec -T redis redis-cli LASTSAVE)" != "$before" ] && break
  sleep 1
done
[ "$(dc exec -T redis redis-cli LASTSAVE)" != "$before" ] \
  || fail "Redis BGSAVE did not finish within ${save_timeout}s"
dc cp redis:/data "$staging/redis" >/dev/null || fail "copying Redis data failed"

archive="$dest/callit-$(date +%Y%m%d-%H%M%S).tar.gz"
tar -czf "$archive" -C "$staging" postgres.sql redis
echo "backup: wrote $archive"

ls -1t "$dest"/callit-*.tar.gz | tail -n +$((keep + 1)) | xargs -r rm --
