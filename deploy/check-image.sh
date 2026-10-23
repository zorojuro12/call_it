#!/usr/bin/env bash
# Builds the backend image and checks the invariants a deploy relies on:
# it runs as a non-root user, stays small, and every one of the four
# binaries is present and starts on this architecture. Each binary is
# started with no infrastructure and asserted to fail on its own config
# validation (internal/config) — a message only a binary that actually
# executed could print.
set -euo pipefail

cd "$(dirname "$0")/.."
image="callit-backend:check"
max_bytes=$((100 * 1024 * 1024))

fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "ok: $*"; }

docker build -q -t "$image" backend >/dev/null || fail "docker build backend"
ok "image builds"

user=$(docker image inspect -f '{{.Config.User}}' "$image")
case "$user" in
  "" | 0 | root | 0:0) fail "image runs as root (User=\"$user\")" ;;
esac
ok "runs as non-root user $user"

size=$(docker image inspect -f '{{.Size}}' "$image")
[ "$size" -lt "$max_bytes" ] || fail "image is $size bytes, over $max_bytes"
ok "image is $((size / 1024 / 1024)) MiB"

# expect_config_error <binary> <expected stderr substring> [docker run args...]
expect_config_error() {
  local bin=$1 want=$2 out
  shift 2
  if out=$(docker run --rm --network none "$@" "$image" "/app/$bin" 2>&1); then
    fail "/app/$bin exited 0 with no config, want a config error"
  fi
  grep -qF -- "$want" <<<"$out" || fail "/app/$bin output does not contain \"$want\": $out"
  ok "/app/$bin starts and fails fast on config"
}

expect_config_error api "JWT_SECRET is required"
expect_config_error migrate "POSTGRES_DSN is required"
expect_config_error ledger-worker "POSTGRES_DSN is required"
expect_config_error relay "is not one of development|production|test" -e ENV=bogus
# Production passes secrets as files (compose.prod.yml); each binary that
# takes one must actually read its *_FILE variable, not ignore it.
expect_config_error api "reading JWT_SECRET_FILE" -e JWT_SECRET_FILE=/nonexistent
expect_config_error migrate "reading POSTGRES_DSN_FILE" -e POSTGRES_DSN_FILE=/nonexistent
expect_config_error ledger-worker "reading POSTGRES_DSN_FILE" -e POSTGRES_DSN_FILE=/nonexistent

out=$(docker run --rm --network none "$image" /app/callit-cli 2>&1 || true)
grep -qF "specify exactly one of --host or --join" <<<"$out" || fail "/app/callit-cli did not start: $out"
ok "/app/callit-cli starts"
