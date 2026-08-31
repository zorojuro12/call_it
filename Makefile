.PHONY: up up-full down test test-unit lint build migrate ledger-worker loadtest \
        fe-install fe-dev fe-test fe-lint fe-build fe-e2e

# Core services only (Redis, PostgreSQL) — what Phases 0-4 need.
up:
	docker compose up -d

# Everything, including Kafka — needed from Phase 5 onward.
up-full:
	docker compose --profile full up -d

# --profile full ensures Kafka is included in the teardown — a bare
# `docker compose down` only touches services in profiles the invocation
# activates, and leaves the Kafka container running otherwise.
down:
	docker compose --profile full down

# Brings the full stack up (Redis, PostgreSQL, Kafka) and waits for all
# three to report healthy before testing — internal/redisstore,
# internal/migrate, and internal/events' integration tests fail (never
# skip) without their respective dependency. -p 1 runs package test
# binaries one at a time: multiple integration packages (redisstore,
# account, ...) share Redis DB 15, and each one's TestMain does a FLUSHDB
# for a clean slate — under Go's default parallel -p, two of those could
# race and wipe each other's in-flight test data.
test:
	docker compose --profile full up -d
	@until [ "$$(docker inspect -f '{{.State.Health.Status}}' call-it-redis-1 2>/dev/null)" = "healthy" ] && \
	       [ "$$(docker inspect -f '{{.State.Health.Status}}' call-it-postgres-1 2>/dev/null)" = "healthy" ] && \
	       [ "$$(docker inspect -f '{{.State.Health.Status}}' call-it-kafka-1 2>/dev/null)" = "healthy" ]; do sleep 1; done
	cd backend && go test ./... -race -cover -p 1

# Assumes Redis is already up (e.g. via `make up`) — skips the Docker round trip.
test-unit:
	cd backend && go test ./... -race -cover -p 1

lint:
	cd backend && go vet ./... && gofmt -l .

build:
	cd backend && go build ./...

# Applies the ledger schema. Requires POSTGRES_DSN — e.g.
# postgres://callit:callit@localhost:5432/callit?sslmode=disable, matching
# docker-compose.yml's postgres service. `make migrate ARGS=down` reverts it.
migrate:
	cd backend && go run ./cmd/migrate $(ARGS)

# Runs the Kafka → PostgreSQL ledger writer. Requires POSTGRES_DSN — e.g.
# postgres://callit:callit@localhost:5432/callit?sslmode=disable. Apply the
# schema with `make migrate` first; this binary never migrates.
ledger-worker:
	cd backend && go run ./cmd/ledger-worker

loadtest:
	@echo "no k6 scripts yet — added in Phase 7"

fe-install:
	cd frontend && npm ci

fe-dev:
	cd frontend && npm run dev

fe-test:
	cd frontend && npx vitest run

fe-lint:
	cd frontend && npm run lint && npm run typecheck

fe-build:
	cd frontend && npm run build

fe-e2e:
	cd frontend && npx playwright test
