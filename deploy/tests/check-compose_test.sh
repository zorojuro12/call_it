#!/usr/bin/env bash
# Proves check-compose.sh actually bites: it must reject a compose file
# that publishes a data store's port or puts the API on a data network
# it must never reach, and accept the real file unchanged.
set -euo pipefail

src="$(cd "$(dirname "$0")/.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "ok: $*"; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# mutate <name> <old> <new>: a copy of compose.prod.yml beside the real one
# (so its relative ../backend and ./Caddyfile paths still resolve), with one
# exact text replacement.
mutate() {
  local out="$src/.compose-mutant-$1.yml"
  python3 -I - "$src/compose.prod.yml" "$out" "$2" "$3" <<'PY'
import sys
src, out, old, new = sys.argv[1:]
text = open(src).read()
assert text.count(old) == 1, f"mutation anchor not found exactly once: {old!r}"
open(out, "w").write(text.replace(old, new))
PY
  echo "$out"
}

# expect_rejected <mutant> <check-number>
expect_rejected() {
  local out
  if out="$(COMPOSE_FILE_OVERRIDE="$1" bash "$src/check-compose.sh" 2>&1)"; then
    rm -f "$1"
    fail "check-compose.sh accepted $(basename "$1")"
  fi
  rm -f "$1"
  grep -qF "FAIL: $2." <<<"$out" || fail "expected check $2 to fail, got: $out"
}

expect_rejected "$(mutate redis-port '    image: redis:7.2-alpine
' '    image: redis:7.2-alpine
    ports: ["6379:6379"]
')" 1
ok "a published Redis port fails check 1"

expect_rejected "$(mutate api-on-ledger '    networks: [edge, state]' '    networks: [edge, state, ledger]')" 2
ok "the API on the ledger network fails check 2"

bash "$src/check-compose.sh" >/dev/null || fail "the real compose.prod.yml is rejected"
ok "the real compose.prod.yml passes"
