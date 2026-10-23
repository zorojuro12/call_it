#!/usr/bin/env bash
# Proves check-manifests.sh actually bites: each mutant breaks one
# invariant in a copy of deploy/k8s, and the check must reject it with the
# matching check number. The real manifests must pass.
set -euo pipefail

src="$(cd "$(dirname "$0")/.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "ok: $*"; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# mutant <name> <file> <old> <new>: sets m to a copy of deploy/k8s with one
# exact text replacement in <file>.
mutant() {
  m="$tmp/$1"
  cp -r "$src" "$m"
  python3 -I - "$m/$2" "$3" "$4" <<'PY'
import sys
path, old, new = sys.argv[1:]
text = open(path).read()
assert text.count(old) == 1, f"mutation anchor not found exactly once in {path}: {old!r}"
open(path, "w").write(text.replace(old, new))
PY
}

# expect_rejected <check-number>: the current mutant $m must fail that check.
expect_rejected() {
  local out
  if out="$(MANIFESTS_DIR_OVERRIDE="$m" bash "$src/check-manifests.sh" 2>&1)"; then
    fail "check-manifests.sh accepted mutant $(basename "$m")"
  fi
  grep -qF "FAIL: $1." <<<"$out" || fail "mutant $(basename "$m"): expected check $1 to fail, got: $out"
}

mutant jwt-in-env api.yaml '            - {name: JWT_SECRET_FILE, value: /run/secrets/jwt_secret}' \
  '            - {name: JWT_SECRET_FILE, value: /run/secrets/jwt_secret}
            - {name: JWT_SECRET, value: x}'
expect_rejected 1
ok "a secret in the API environment fails check 1"

mutant relay-all-secrets relay.yaml '            capabilities: {drop: [ALL]}' \
  '            capabilities: {drop: [ALL]}
          volumeMounts: [{name: s, mountPath: /run/secrets, readOnly: true}]
      volumes: [{name: s, secret: {secretName: callit-secrets}}]'
expect_rejected 1
ok "a Secret volume without items (every key) fails check 1"

mutant secret-key-ref relay.yaml '          envFrom: [{configMapRef: {name: callit-config}}]' \
  '          envFrom: [{configMapRef: {name: callit-config}}]
          env: [{name: PG, valueFrom: {secretKeyRef: {name: callit-secrets, key: postgres_password}}}]'
expect_rejected 1
ok "a secretKeyRef env var fails check 1"

mutant kafka-emptydir kafka.yaml '  volumeClaimTemplates:' '  volumeClaimTemplatesDisabled:'
expect_rejected 3
ok "Kafka without persistent storage fails check 3"

mutant relay-scaled relay.yaml '  replicas: 1' '  replicas: 2'
expect_rejected 4
ok "two relays fail check 4"

bash "$src/check-manifests.sh" >/dev/null || fail "the real deploy/k8s manifests are rejected"
ok "the real manifests pass"
