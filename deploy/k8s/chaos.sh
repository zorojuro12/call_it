#!/usr/bin/env bash
# The Phase 8a.2 chaos run: k6 wager load against the cluster while the
# relay, the ledger worker and the Kafka broker are SIGKILLed mid-run. It
# passes only if both hold afterwards:
#   - the outbox drains: nothing pending or unread in wager-outbox. An
#     entry stuck under a dead relay's consumer is never retried, even
#     when it happened to reach Kafka first;
#   - Phase 7b's TestReconcileAfterLoad passes: every wager reached the
#     ledger exactly once (redis_wallet - opening_stake == ledger_balance
#     for every player, every transaction balanced).
#   deploy/k8s/up.sh [image] && deploy/k8s/chaos.sh
# Needs k6 and Go on PATH.
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
duration=240
players=25
# Kill times, in seconds from the start of the load. The relay dies twice:
# once under normal load, and once 5 s into the Kafka outage, when it holds
# a batch it has read but can't produce. That second window is where an
# orphaned batch is a lost one, not just an unacked duplicate.
kill_relay_at=60
kill_worker_at=90
kill_kafka_at=120
kill_relay_again_at=125
drain_timeout=120

# Non-default local ports: the dev Compose stack (make up) holds 6379,
# 5432 and 9092, and a reconciler aimed at it would read the wrong data.
api_port=18080 redis_port=16379 pg_port=15432 kafka_port=19092

k() { kubectl --context kind-callit -n callit "$@"; }
log() { echo "chaos: $(date +%H:%M:%S) $*"; }
work="$(mktemp -d)"
pids=()
cleanup() {
  for p in "${pids[@]}"; do kill "$p" 2>/dev/null; done
  rm -rf "$work"
}
trap cleanup EXIT

# forward <target> <local-port> <remote-port>: a background port-forward,
# returned once the local port accepts connections.
forward() {
  k port-forward "$1" "$2:$3" >"$work/pf-$2.log" 2>&1 &
  pids+=($!)
  for _ in $(seq 40); do
    (exec 3<>"/dev/tcp/127.0.0.1/$2") 2>/dev/null && return 0
    sleep 0.25
  done
  echo "chaos: port-forward to $1 on $2 never came up: $(cat "$work/pf-$2.log")" >&2
  exit 1
}

# kill_pod <label-or-name>: SIGKILL, no grace period, so the process gets
# no chance to finish and ack its batch. A crash, not a shutdown.
kill_pod() {
  local pod
  pod="$(k get pods -l "app=$1" -o jsonpath='{.items[0].metadata.name}')"
  k delete pod "$pod" --grace-period=0 --force >/dev/null 2>&1
  log "killed $pod"
}

# outbox_state prints "<pending> <lag>" for the relay's consumer group.
outbox_state() {
  k exec redis-0 -- redis-cli XINFO GROUPS wager-outbox 2>/dev/null \
    | awk 'prev == "pending" {p = $0} prev == "lag" {l = $0} {prev = $0} END {print (p == "" ? "?" : p), (l == "" ? "?" : l)}'
}
ledger_count() { k exec postgres-0 -- psql -U callit -tAc 'select count(*) from transactions' 2>/dev/null; }

forward svc/api "$api_port" 8080
log "load: ${duration}s, $players players; kills at relay=${kill_relay_at}s worker=${kill_worker_at}s kafka=${kill_kafka_at}s relay=${kill_relay_again_at}s"
(cd "$repo" && API_BASE_URL="http://localhost:$api_port" WAGER_TEST_DURATION_S="$duration" WAGER_PLAYERS="$players" \
  k6 run loadtest/wager_latency.js) >"$work/k6.log" 2>&1 &
k6pid=$!
start=$SECONDS
sleep "$kill_relay_at"; kill_pod relay
sleep $((kill_worker_at - (SECONDS - start))); kill_pod ledger-worker
sleep $((kill_kafka_at - (SECONDS - start))); kill_pod kafka
sleep $((kill_relay_again_at - (SECONDS - start))); kill_pod relay
wait "$k6pid"
k6status=$?
room="$(grep -o 'RECONCILE_ROOM_ID=[A-Za-z0-9-]*' "$work/k6.log" | head -1 | cut -d= -f2)"
[ -n "$room" ] || { echo "chaos: k6 printed no RECONCILE_ROOM_ID:" >&2; tail -20 "$work/k6.log" >&2; exit 1; }
log "k6 finished (exit $k6status; a p99 threshold miss is not a correctness failure) for room $room"
grep -E "wager_ack_ms|iterations\.*:|checks\.*:" "$work/k6.log" | sed 's/^/chaos:   /'

# Drained means: nothing pending or unread in the outbox, and the ledger
# count unchanged for 15 s. A build that orphans entries never drains, so
# the wait is bounded and the reconciler runs regardless.
log "waiting up to ${drain_timeout}s for the pipeline to drain"
stable=0 last="" drained=false
for _ in $(seq "$drain_timeout"); do
  read -r pending lag <<<"$(outbox_state)"
  now="$(ledger_count)"
  if [ "$pending" = 0 ] && [ "$lag" = 0 ] && [ "$now" = "$last" ]; then
    stable=$((stable + 1))
  else
    stable=0
  fi
  last="$now"
  if [ "$stable" -ge 15 ]; then drained=true; break; fi
  sleep 1
done
log "drained=$drained outbox_pending=$pending outbox_lag=$lag ledger_transactions=$now"

forward svc/redis "$redis_port" 6379
forward svc/postgres "$pg_port" 5432
forward svc/kafka "$kafka_port" 9092
# Read into a variable, never echoed.
pg_password="$(k get secret callit-secrets -o jsonpath='{.data.postgres_password}' | base64 -d)"
log "reconciling room $room"
(cd "$repo/backend" && RECONCILE_ROOM_IDS="$room" REDIS_ADDR="localhost:$redis_port" \
  POSTGRES_DSN="postgres://callit:$pg_password@localhost:$pg_port/callit?sslmode=disable" \
  KAFKA_BROKERS="localhost:$kafka_port" \
  go test ./internal/ledger/ -run TestReconcileAfterLoad -count=1 -v) >"$work/reconcile.log" 2>&1
reconciled=$?
grep -vE "^=== (RUN|PAUSE|CONT)" "$work/reconcile.log" | tail -15 | sed 's/^/chaos:   /'
if [ "$reconciled" = 0 ] && $drained; then
  log "RESULT: PASS (outbox drained, every wager reconciled)"
  exit 0
fi
$drained || log "outbox did not drain: $pending entries stuck pending (stranded under a dead relay consumer)"
[ "$reconciled" = 0 ] || log "reconciliation failed"
log "RESULT: FAIL"
exit 1
