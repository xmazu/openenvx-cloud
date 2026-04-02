# OpenEnvX Cloud Local Development Makefile

DOCKER_COMPOSE = local/docker-compose.yml
ORCHESTRATOR_MAIN = cmd/orchestrator/main.go
ENV_FILE = .env

.PHONY: help setup infra orchestrator clean stop

help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

setup: ## Initial setup - creates .env file from example
	@if [ ! -f $(ENV_FILE) ]; then \
		cp scripts/openenvx-orchestrator.env.example $(ENV_FILE); \
		echo "Created $(ENV_FILE) from example."; \
	else \
		echo "$(ENV_FILE) already exists."; \
	fi

infra: ## Start local infrastructure (PostgreSQL, MinIO)
	docker compose -f $(DOCKER_COMPOSE) up -d

orchestrator: ## Run the Orchestrator daemon
	go run $(ORCHESTRATOR_MAIN)

stop: ## Stop all local services (Docker)
	docker compose -f $(DOCKER_COMPOSE) down

clean: stop ## Alias for stop
