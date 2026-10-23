#!/usr/bin/env bash
# Tests deploy/op-bootstrap.sh against a fake op (tests/fake-op), so no
# test talks to 1Password.
set -euo pipefail

src="$(cd "$(dirname "$0")/.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "ok: $*"; }

cleanup=()
trap 'rm -rf "${cleanup[@]}"' EXIT
items=(jwt-secret postgres-password duckdns-token)

# setup prints a fresh work dir holding an empty fake vault and an op on PATH.
setup() {
  local w
  w="$(mktemp -d)"
  cleanup+=("$w")
  mkdir -p "$w/vault" "$w/bin"
  ln -s "$src/tests/fake-op" "$w/bin/op"
  : >"$w/vault/calls.log"
  echo "$w"
}

# run_bootstrap <work-dir> <args...>: stdin passes through; output goes to
# <work-dir>/out.
run_bootstrap() {
  local w=$1
  shift
  env PATH="$w/bin:$PATH" FAKE_OP_DIR="$w/vault" bash "$src/op-bootstrap.sh" "$@" >"$w/out" 2>&1
}

# created <work-dir> <title>: the password field of the item created.
created() { jq -r '.fields[] | select(.id == "password") | .value' "$1/vault/created-$2.json"; }

# assert_item <work-dir> <title>: the item is a Password item, and its
# value never appeared in op's argv.
assert_item() {
  local f="$1/vault/created-$2.json" v
  [ -f "$f" ] || fail "item $2 was not created"
  [ "$(jq -r '.title + " " + .category' "$f")" = "$2 PASSWORD" ] || fail "item $2 has the wrong title or category: $(jq -c '{title, category}' "$f")"
  v="$(created "$1" "$2")"
  ! grep -qF -- "$v" "$1/vault/calls.log" || fail "item $2's value appeared in op's argv"
}

# assert_none_created <work-dir> <case>
assert_none_created() {
  if ls "$1/vault"/created-*.json >/dev/null 2>&1; then
    fail "$2: created items: $(ls "$1/vault" | grep created-)"
  fi
}

# --generate creates all three items with fresh values, passed on stdin.
w="$(setup)"
run_bootstrap "$w" --generate tok-1 || fail "--generate exited non-zero: $(cat "$w/out")"
for i in "${items[@]}"; do assert_item "$w" "$i"; done
[[ "$(created "$w" jwt-secret)" =~ ^[0-9a-f]{64}$ ]] || fail "jwt-secret is not 64 lowercase hex"
[[ "$(created "$w" postgres-password)" =~ ^[0-9a-f]{48}$ ]] || fail "postgres-password is not 48 lowercase hex"
[ "$(created "$w" duckdns-token)" = tok-1 ] || fail "duckdns-token is $(created "$w" duckdns-token)"
ok "--generate creates three Password items, values never in argv"

# An existing item is never overwritten, and nothing is created at all:
# every item is checked before the first create. A replaced
# postgres-password would no longer open the existing database.
w="$(setup)"
printf 'existing' >"$w/vault/postgres-password"
if run_bootstrap "$w" --generate tok-1; then
  fail "--generate over an existing item exited 0"
fi
grep -qF "op-bootstrap: item postgres-password already exists in callit-prod; refusing to overwrite" "$w/out" \
  || fail "refusal message was: $(cat "$w/out")"
assert_none_created "$w" "an existing item"
ok "an existing item is refused before anything is created"
