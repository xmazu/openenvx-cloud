# OpenEnvX Cloud Local Development Makefile

NOMAD_CONFIG = nomad/dev.hcl
NOMAD_JOB = nomad/terraform-worker.hcl
DOCKER_COMPOSE = local/docker-compose.yml
ORCHESTRATOR_MAIN = cmd/orchestrator/main.go
ENV_FILE = .env

.PHONY: help setup infra nomad orchestrator clean stop

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

nomad: ## Start Nomad agent in dev mode and register the worker job
	@echo "Starting Nomad agent..."
	nomad agent -dev -config=$(NOMAD_CONFIG) & \
	sleep 5 && \
	nomad job run $(NOMAD_JOB)

orchestrator: ## Run the Orchestrator daemon
	go run $(ORCHESTRATOR_MAIN)

stop: ## Stop all local services (Docker and Nomad)
	docker compose -f $(DOCKER_COMPOSE) down
	-pkill nomad

clean: stop ## Alias for stop
