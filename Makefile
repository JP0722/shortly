.DEFAULT_GOAL := help
SHELL := /bin/bash

MYSQL_DSN ?= shortly:shortly@tcp(localhost:3307)/shortly

# Your shell profile exports a global COMPOSE_FILE, which makes `docker compose`
# look for that file instead of this project's. Pin it here so the Makefile works
# regardless of ambient environment.
export COMPOSE_FILE := docker-compose.yml

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

up: ## Start MySQL + Redis
	docker compose up -d --wait

up-full: ## Start MySQL + Redis + the app, all in containers
	docker compose --profile full up -d --build --wait

logs: ## Tail the containerised app's logs
	docker compose --profile full logs -f app

image: ## Build the production image
	docker build -t shortly:local .

down: ## Stop containers (keeps volumes)
	docker compose --profile full down

nuke: ## Stop containers AND delete data volumes
	docker compose down -v

run: ## Run the API server (loads .env automatically)
	@set -a; . ./.env; set +a; go run ./cmd/api

demo: ## Exercise the whole API against a running server
	@./scripts/demo.sh

build: ## Build binary to ./bin/api
	go build -o bin/api ./cmd/api

test: ## Run tests (store tests need `make up`)
	SHORTLY_TEST_DSN="$(MYSQL_DSN)?parseTime=true&loc=UTC" go test ./... -count=1

race: ## Run tests with the race detector
	go test ./... -race -count=1

vet: ## go vet
	go vet ./...

db: ## Open a mysql shell in the container
	docker compose exec mysql mysql -ushortly -pshortly shortly

migrate: ## Apply migrations manually (the server also does this on boot)
	@for f in internal/store/migrations/*.sql; do \
		echo "applying $$f"; \
		docker compose exec -T mysql mysql -ushortly -pshortly shortly < "$$f" || exit 1; \
	done
	@echo "migrations applied"

schema: ## Print the current schema
	@docker compose exec -T mysql mysql -ushortly -pshortly shortly -e "SHOW CREATE TABLE links\G"

redis: ## Open a redis-cli shell in the container
	docker compose exec redis redis-cli

.PHONY: help up up-full down nuke run demo logs image build test race vet db migrate schema redis
