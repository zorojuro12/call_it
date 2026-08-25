.PHONY: up up-full down test test-unit lint build migrate loadtest

# Core services only (Redis, PostgreSQL) — what Phases 0-4 need.
up:
	docker compose up -d

# Everything, including Kafka — needed from Phase 5 onward.
up-full:
	docker compose --profile full up -d

down:
	docker compose down

# Brings Redis up and waits for it to report healthy before testing —
# internal/redisstore's integration tests fail (never skip) without it.
# -p 1 runs package test binaries one at a time: multiple integration
# packages (redisstore, account, ...) share Redis DB 15, and each one's
# TestMain does a FLUSHDB for a clean slate — under Go's default
# parallel -p, two of those could race and wipe each other's in-flight
# test data.
test:
	docker compose up -d redis
	@until [ "$$(docker inspect -f '{{.State.Health.Status}}' call-it-redis-1 2>/dev/null)" = "healthy" ]; do sleep 1; done
	cd backend && go test ./... -race -cover -p 1

# Assumes Redis is already up (e.g. via `make up`) — skips the Docker round trip.
test-unit:
	cd backend && go test ./... -race -cover -p 1

lint:
	cd backend && go vet ./... && gofmt -l .

build:
	cd backend && go build ./...

migrate:
	@echo "no migrations yet — added in Phase 5"

loadtest:
	@echo "no k6 scripts yet — added in Phase 7"
