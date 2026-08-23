.PHONY: up up-full down test lint build migrate loadtest

# Core services only (Redis, PostgreSQL) — what Phases 0-4 need.
up:
	docker compose up -d

# Everything, including Kafka — needed from Phase 5 onward.
up-full:
	docker compose --profile full up -d

down:
	docker compose down

test:
	cd backend && go test ./... -race -cover

lint:
	cd backend && go vet ./... && gofmt -l .

build:
	cd backend && go build ./...

migrate:
	@echo "no migrations yet — added in Phase 5"

loadtest:
	@echo "no k6 scripts yet — added in Phase 7"
