#!/usr/bin/env bash
# Renders the production compose file with fake values and asserts the
# invariants the deployment's safety rests on (plan 8a, Decisions 2-5):
# only Caddy is reachable from outside, each data store sits on an
# internal network shared only with the processes that need it, the API
# can never reach PostgreSQL or Kafka, and missing secrets fail the render
# instead of starting with blanks.
set -euo pipefail

cd "$(dirname "$0")/.."
compose_file="deploy/compose.prod.yml"
fixture="deploy/tests/fixture.env"

fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "ok: $*"; }

[ -f "$compose_file" ] || fail "0. $compose_file does not exist"

# Secrets from the caller's shell must not mask a missing one in the file.
unset CALLIT_HOSTNAME JWT_SECRET POSTGRES_PASSWORD DUCKDNS_TOKEN ACME_EMAIL
cfg=$(docker compose -f "$compose_file" --env-file "$fixture" config --format json) \
  || fail "0. compose file does not render with the fixture env"
q() { jq -r "$1" <<<"$cfg"; }

# 1. Only caddy publishes ports, and exactly 80 and 443.
[ "$(q '[.services | to_entries[] | select(.value.ports != null and (.value.ports | length) > 0) | .key] | join(",")')" = caddy ] \
  || fail "1. a service other than caddy publishes ports"
[ "$(q '[.services.caddy.ports[].published | tostring] | sort | join(",")')" = "443,80" ] \
  || fail "1. caddy must publish exactly 80 and 443"
ok "1. only caddy is published, on 80 and 443"

# 2. Network membership is exact.
want_networks="caddy=edge
api=edge,state
relay=events,state
ledger-worker=events,ledger
migrate=ledger
redis=state
postgres=ledger
kafka=events"
got_networks=$(q '.services | to_entries | map("\(.key)=\(.value.networks | keys | sort | join(","))") | .[]' | sort)
[ "$got_networks" = "$(sort <<<"$want_networks")" ] \
  || fail "2. network membership differs:
$(diff <(sort <<<"$want_networks") <(echo "$got_networks") || true)"
ok "2. network membership is exact"

# 3. Data networks are internal; edge is not.
for n in state events ledger; do
  [ "$(q ".networks.\"$n\".internal")" = true ] || fail "3. network $n is not internal"
done
[ "$(q '.networks.edge.internal // false')" = false ] || fail "3. edge must not be internal (Caddy needs ACME egress)"
ok "3. state, events, ledger are internal"

# 4. Images are pinned; the Go services build from ../backend.
untagged=$(q '[.services | to_entries[] | select(.value.image != null) | select((.value.image | test(":") | not) or (.value.image | test(":latest$"))) | .key] | join(",")')
[ -z "$untagged" ] || fail "4. unpinned image on: $untagged"
for s in api relay ledger-worker migrate; do
  [ "$(q ".services.\"$s\".build.context | endswith(\"/backend\")")" = true ] || fail "4. $s does not build from backend/"
done
ok "4. images pinned, Go services build from backend/"

# 5. api environment.
env_of() { q ".services.\"$1\".environment.\"$2\" // \"<unset>\""; }
[ "$(env_of api ENV)" = production ] || fail "5. api ENV is not production"
[ "$(env_of api CORS_ALLOWED_ORIGINS)" = "https://callit.example.invalid" ] || fail "5. api CORS origin is not https://\$CALLIT_HOSTNAME"
[ "$(env_of api TRUSTED_PROXIES)" = "172.30.0.10/32" ] || fail "5. api TRUSTED_PROXIES is not Caddy's address"
[ "$(env_of api REDIS_ADDR)" = "redis:6379" ] || fail "5. api REDIS_ADDR is not redis:6379"
[ "$(env_of api METRICS_ADDR)" = "<unset>" ] || fail "5. api must not set METRICS_ADDR"
[ "$(q '.services.caddy.networks.edge.ipv4_address')" = "172.30.0.10" ] || fail "5. caddy's edge address is not 172.30.0.10"
ok "5. api environment and caddy address"

# 6. relay and ledger-worker reach Kafka; Postgres DSNs point at postgres.
for s in relay ledger-worker; do
  [ "$(env_of "$s" KAFKA_BROKERS)" = "kafka:9092" ] || fail "6. $s KAFKA_BROKERS is not kafka:9092"
  [ "$(env_of "$s" ENV)" = production ] || fail "6. $s ENV is not production"
done
for s in ledger-worker migrate; do
  env_of "$s" POSTGRES_DSN | grep -q '@postgres:5432/' || fail "6. $s POSTGRES_DSN does not target postgres:5432"
done
ok "6. relay/ledger-worker/migrate wiring"

# 7. Kafka is advertised on the service name, with a bounded heap.
[ "$(env_of kafka KAFKA_ADVERTISED_LISTENERS)" = "PLAINTEXT://kafka:9092" ] || fail "7. kafka advertised listener"
[ "$(env_of kafka KAFKA_HEAP_OPTS)" = "-Xms1g -Xmx1g" ] || fail "7. kafka heap"
ok "7. kafka listener and heap"

# 8. Startup ordering.
[ "$(q '.services."ledger-worker".depends_on.migrate.condition')" = service_completed_successfully ] \
  || fail "8. ledger-worker must wait for migrate to complete"
for s in api relay; do
  [ "$(q ".services.\"$s\".depends_on.redis.condition")" = service_healthy ] || fail "8. $s must wait for healthy redis"
done
ok "8. startup ordering"

# 9. Restart policies.
bad_restart=$(q '[.services | to_entries[] | select(.key != "migrate" and .value.restart != "unless-stopped") | .key] | join(",")')
[ -z "$bad_restart" ] || fail "9. not unless-stopped: $bad_restart"
[ "$(q '.services.migrate.restart')" = "no" ] || fail "9. migrate must not restart"
ok "9. restart policies"

# 10. Each required value missing fails the render.
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
for key in JWT_SECRET POSTGRES_PASSWORD CALLIT_HOSTNAME; do
  grep -v "^$key=" "$fixture" >"$tmp"
  if docker compose -f "$compose_file" --env-file "$tmp" config -q 2>/dev/null; then
    fail "10. rendering without $key succeeded"
  fi
done
ok "10. missing JWT_SECRET, POSTGRES_PASSWORD or CALLIT_HOSTNAME fails the render"
