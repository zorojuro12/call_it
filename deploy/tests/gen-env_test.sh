#!/usr/bin/env bash
# Tests deploy/gen-env.sh against throwaway copies of deploy/, so the real
# deploy/.env is never touched.
set -euo pipefail

src="$(cd "$(dirname "$0")/.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "ok: $*"; }

# fresh_copy prints the path of a new temp dir holding a copy of deploy/
# with no .env in it.
cleanup=()
trap 'rm -rf "${cleanup[@]}"' EXIT
fresh_copy() {
  local d
  d="$(mktemp -d)"
  cleanup+=("$d")
  cp -r "$src" "$d/deploy"
  rm -f "$d/deploy/.env"
  echo "$d/deploy"
}
val() { grep "^$2=" "$1/.env" | cut -d= -f2-; }

# A first run writes a 0600 .env with the hostname and strong secrets.
a="$(fresh_copy)"
bash "$a/gen-env.sh" callit.duckdns.org >/dev/null || fail "first run exited non-zero"
[ "$(stat -c %a "$a/.env")" = 600 ] || fail ".env mode is $(stat -c %a "$a/.env"), want 600"
[ "$(val "$a" CALLIT_HOSTNAME)" = callit.duckdns.org ] || fail "CALLIT_HOSTNAME not written"
[[ "$(val "$a" JWT_SECRET)" =~ ^[0-9a-f]{64}$ ]] || fail "JWT_SECRET is not 64 lowercase hex chars"
[[ "$(val "$a" POSTGRES_PASSWORD)" =~ ^[0-9a-f]{48}$ ]] || fail "POSTGRES_PASSWORD is not 48 lowercase hex chars"
grep -q '^DUCKDNS_TOKEN=' "$a/.env" && fail "DUCKDNS_TOKEN written without a token argument"
ok "first run writes a 0600 .env with generated secrets"

# The token argument is written when given.
t="$(fresh_copy)"
bash "$t/gen-env.sh" callit.duckdns.org tok-123 >/dev/null
[ "$(val "$t" DUCKDNS_TOKEN)" = tok-123 ] || fail "DUCKDNS_TOKEN not written"
ok "DUCKDNS_TOKEN written when given"

# Secrets differ between installs.
b="$(fresh_copy)"
bash "$b/gen-env.sh" callit.duckdns.org >/dev/null
[ "$(val "$a" JWT_SECRET)" != "$(val "$b" JWT_SECRET)" ] || fail "two installs generated the same JWT_SECRET"
ok "secrets differ between installs"

# A second run refuses to overwrite and leaves the file byte-identical.
before="$(sha256sum "$a/.env")"
if err="$(bash "$a/gen-env.sh" other.duckdns.org 2>&1 >/dev/null)"; then
  fail "second run succeeded, want a refusal"
fi
grep -qF "deploy/.env already exists; refusing to overwrite" <<<"$err" || fail "refusal message was: $err"
[ "$(sha256sum "$a/.env")" = "$before" ] || fail "second run modified .env"
ok "second run refuses and leaves .env untouched"

# No hostname is a usage error.
c="$(fresh_copy)"
set +e
bash "$c/gen-env.sh" >/dev/null 2>&1
code=$?
set -e
[ "$code" = 2 ] || fail "no-argument run exited $code, want 2"
[ ! -e "$c/.env" ] || fail "no-argument run created .env"
ok "missing hostname exits 2 without writing"

# Hostnames and tokens are validated before anything is written: .env is
# read by other scripts, so a value carrying shell syntax must never land
# in it.
for bad in 'evil;id' 'x$(id).duckdns.org' 'UPPER.duckdns.org' '-lead.duckdns.org' 'a b'; do
  d="$(fresh_copy)"
  set +e
  bash "$d/gen-env.sh" "$bad" >/dev/null 2>&1
  code=$?
  set -e
  [ "$code" = 2 ] || fail "hostname '$bad' exited $code, want 2"
  [ ! -e "$d/.env" ] || fail "hostname '$bad' created .env"
done
d="$(fresh_copy)"
set +e
bash "$d/gen-env.sh" callit.duckdns.org 'tok;id' >/dev/null 2>&1
code=$?
set -e
[ "$code" = 2 ] || fail "token 'tok;id' exited $code, want 2"
[ ! -e "$d/.env" ] || fail "invalid token created .env"
ok "invalid hostnames and tokens are rejected without writing"

# The generated file renders the production compose file.
docker compose -f "$a/compose.prod.yml" --env-file "$a/.env" config -q || fail "generated .env does not render compose.prod.yml"
ok "generated .env renders compose.prod.yml"
