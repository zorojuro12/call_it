#!/usr/bin/env bash
# Tests deploy/preflight-secrets.sh with throwaway dirs: one on tmpfs
# (/dev/shm) and one on disk (inside deploy/).
set -euo pipefail

src="$(cd "$(dirname "$0")/.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "ok: $*"; }

cleanup=()
trap 'rm -rf "${cleanup[@]}"' EXIT

# filled <parent>: sets d to a new dir under <parent> holding the four
# secret files. It sets d rather than printing it: called as $(filled),
# the cleanup+= would happen in a subshell and the dir, which may sit
# inside deploy/, would never be removed.
filled() {
  d="$(mktemp -d "$1/preflight-test.XXXXXX")"
  cleanup+=("$d")
  for f in jwt_secret postgres_password postgres_dsn duckdns_token; do printf 'x' >"$d/$f"; done
}

# expect_refused <dir> <message>
expect_refused() {
  local out
  if out="$(bash "$src/preflight-secrets.sh" "$1" 2>&1)"; then
    fail "accepted $1, want: $2"
  fi
  grep -qF -- "$2" <<<"$out" || fail "for $1 said: $out, want: $2"
}

filled /dev/shm
bash "$src/preflight-secrets.sh" "$d" >/dev/null || fail "a complete tmpfs dir was refused"
ok "a complete set on tmpfs passes"

filled "$src"
expect_refused "$d" "preflight: $d is not on tmpfs"
ok "a disk-backed dir is refused"

filled /dev/shm
rm "$d/postgres_dsn"
expect_refused "$d" "preflight: $d/postgres_dsn missing or empty"
filled /dev/shm
: >"$d/jwt_secret"
expect_refused "$d" "preflight: $d/jwt_secret missing or empty"
ok "a missing or empty secret is refused"
