#!/usr/bin/env bash
# End-to-end check of a running deployment, over real TLS:
#   1. /healthz answers through Caddy
#   2. a host and a guest play a full round with callit-cli -secure
#   3. that round reaches the PostgreSQL ledger (only when run on the server
#      itself, where the compose stack is local)
#   4. spoofed X-Forwarded-For values cannot escape the auth rate limit
# Usage: deploy/smoke.sh <hostname>
# Against a local rehearsal (hostname localhost), trust Caddy's local CA with
# SSL_CERT_FILE=<root.crt> (deploy/README.md); there is no skip-verify option.
#
# Step 4 deliberately exhausts this machine's auth bucket (10/min), so a
# second run within a minute fails at step 2's registration.
set -euo pipefail

host="${1:?usage: deploy/smoke.sh <hostname>}"
here="$(cd "$(dirname "$0")" && pwd)"
lock_wait=45   # callit-cli opens rounds with a 30 s lockout

fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "ok: $*"; }

work="$(mktemp -d)"
pids=()
cleanup() {
  for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done
  rm -rf "$work"
}
trap cleanup EXIT

# wait_for <file> <fixed-string> <seconds>
wait_for() {
  for _ in $(seq "$3"); do
    grep -qF -- "$2" "$1" 2>/dev/null && return 0
    sleep 1
  done
  fail "timed out after $3 s waiting for \"$2\" in $(basename "$1"):
$(tail -n 20 "$1" 2>/dev/null)"
}

# --- 1. health
[ "$(curl -fsS "https://$host/healthz")" = '{"status":"ok"}' ] || fail "1. https://$host/healthz did not answer ok"
ok "1. healthz over TLS"

# --- 2. a full round through the public hostname
# Prefer a local Go build (laptop, rehearsal); on the server, run the copy
# shipped in the backend image, reaching the host's published 443 by name.
if command -v go >/dev/null; then
  (cd "$here/../backend" && go build -o "$work/callit-cli" ./cmd/callit-cli) || fail "2. building callit-cli"
  cli=("$work/callit-cli")
else
  cli=(docker run --rm -i --add-host "$host:host-gateway" callit-backend:local /app/callit-cli)
fi

ledger_before=""
dc=(docker compose -f "$here/compose.prod.yml" --env-file "$here/.env")
if [ -f "$here/.env" ] && [ -n "$("${dc[@]}" ps -q postgres 2>/dev/null)" ]; then
  ledger_before="$("${dc[@]}" exec -T postgres psql -U callit -tAc 'select count(*) from transactions')"
fi

mkfifo "$work/host.in" "$work/guest.in"
"${cli[@]}" -secure -addr "$host" -host <"$work/host.in" >"$work/host.log" 2>&1 &
pids+=($!)
exec 3>"$work/host.in"
printf '%s\n' "smoke-$(date +%s)-$RANDOM@example.invalid" "smoke-password-123" "SmokeHost" "1000" >&3
wait_for "$work/host.log" "Room created — code:" 20
code="$(grep -o 'code: [A-Z0-9]*' "$work/host.log" | head -1 | cut -d' ' -f2)"

"${cli[@]}" -secure -addr "$host" -join "$code" <"$work/guest.in" >"$work/guest.log" 2>&1 &
pids+=($!)
exec 4>"$work/guest.in"
echo "SmokeGuest" >&4
wait_for "$work/guest.log" "Connected to room" 20
wait_for "$work/host.log" "Connected to room" 20

echo "round Smoke test? | Yes | No" >&3
wait_for "$work/guest.log" "<< round_opened" 15
echo "bet 0 100" >&4
wait_for "$work/guest.log" "<< wager_accepted" 15
wait_for "$work/host.log" "<< round_locked" "$lock_wait"
echo "resolve 0" >&3
wait_for "$work/host.log" "<< round_resolved" 15
wait_for "$work/guest.log" "<< round_resolved" 15
exec 3>&- 4>&-
ok "2. host and guest played a full round over wss://$host (room $code)"

# --- 3. the round reached the ledger
if [ -n "$ledger_before" ]; then
  for _ in $(seq 30); do
    now="$("${dc[@]}" exec -T postgres psql -U callit -tAc 'select count(*) from transactions')"
    [ "$now" -gt "$ledger_before" ] && break
    sleep 1
  done
  [ "$now" -gt "$ledger_before" ] || fail "3. no new ledger transactions within 30 s ($ledger_before → $now)"
  ok "3. ledger recorded the round ($ledger_before → $now transactions)"
else
  echo "skip: 3. ledger check (no local compose stack — run on the server to include it)"
fi

# --- 4. spoofed X-Forwarded-For cannot mint fresh auth buckets
limited=false
for i in $(seq 11); do
  status="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "https://$host/api/v1/auth/login" \
    -H 'Content-Type: application/json' -H "X-Forwarded-For: 198.51.100.$i" \
    -d '{"email":"nobody@example.invalid","password":"smoke-password-123"}')"
  if [ "$status" = 429 ]; then limited=true; break; fi
done
$limited || fail "4. 11 logins with distinct spoofed X-Forwarded-For were never rate limited"
ok "4. spoofed X-Forwarded-For is ignored (rate limited by attempt $i)"
