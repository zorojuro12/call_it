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

# A first run writes a 0600 .env holding exactly the two non-secret
# settings. Secrets live in 1Password and reach the server as files
# (deploy/secrets.sh), never in .env.
a="$(fresh_copy)"
bash "$a/gen-env.sh" callit.duckdns.org >/dev/null || fail "first run exited non-zero"
[ "$(stat -c %a "$a/.env")" = 600 ] || fail ".env mode is $(stat -c %a "$a/.env"), want 600"
[ "$(cat "$a/.env")" = "CALLIT_HOSTNAME=callit.duckdns.org
CALLIT_SECRETS_DIR=/run/callit/secrets" ] || fail ".env is not exactly the two settings: $(cat "$a/.env")"
ok "first run writes a 0600 .env with only non-secret settings"

# A DuckDNS token argument is refused: the token lives in 1Password now.
t="$(fresh_copy)"
set +e
err="$(bash "$t/gen-env.sh" callit.duckdns.org tok-123 2>&1 >/dev/null)"
code=$?
set -e
[ "$code" = 2 ] || fail "token argument exited $code, want 2"
grep -qF "gen-env: the DuckDNS token now lives in 1Password (deploy/README.md §4)" <<<"$err" || fail "token refusal message was: $err"
[ ! -e "$t/.env" ] || fail "token argument created .env"
ok "a DuckDNS token argument is refused without writing"

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

# Hostnames are validated before anything is written: .env is read by
# other scripts, so a value carrying shell syntax must never land in it.
for bad in 'evil;id' 'x$(id).duckdns.org' 'UPPER.duckdns.org' '-lead.duckdns.org' 'a b'; do
  d="$(fresh_copy)"
  set +e
  bash "$d/gen-env.sh" "$bad" >/dev/null 2>&1
  code=$?
  set -e
  [ "$code" = 2 ] || fail "hostname '$bad' exited $code, want 2"
  [ ! -e "$d/.env" ] || fail "hostname '$bad' created .env"
done
ok "invalid hostnames are rejected without writing"

# The generated file renders the production compose file.
docker compose -f "$a/compose.prod.yml" --env-file "$a/.env" config -q || fail "generated .env does not render compose.prod.yml"
ok "generated .env renders compose.prod.yml"
