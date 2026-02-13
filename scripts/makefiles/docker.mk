# server-price-tracker - Docker Targets
# Docker Compose local development environment (PostgreSQL + Ollama)

###################
##@ Docker Development

.PHONY: docker-up docker-down docker-clean docker-logs ollama-pull dev-setup mock-server

docker-up: ## Start PostgreSQL and Ollama containers
	@ $(MAKE) --no-print-directory log-$@
	@docker compose -f $(DOCKER_COMPOSE) up -d
	@echo "Waiting for services to be healthy..."
	@docker compose -f $(DOCKER_COMPOSE) exec -T postgres pg_isready -U tracker -q --timeout=30 || true
	@echo "✓ Development services started"

docker-down: ## Stop all containers
	@ $(MAKE) --no-print-directory log-$@
	@docker compose -f $(DOCKER_COMPOSE) down
	@echo "✓ Development services stopped"

docker-clean: ## Stop and remove containers, volumes, and images (full reset)
	@ $(MAKE) --no-print-directory log-$@
	@docker compose -f $(DOCKER_COMPOSE) down -v --rmi local
	@echo "✓ Development services cleaned"

docker-logs: ## View container logs
	@ $(MAKE) --no-print-directory log-$@
	@docker compose -f $(DOCKER_COMPOSE) logs -f

ollama-pull: ## Pull Ollama model (override: make ollama-pull OLLAMA_MODEL=mistral:7b)
	@ $(MAKE) --no-print-directory log-$@
	@docker compose -f $(DOCKER_COMPOSE) exec ollama ollama pull $(OLLAMA_MODEL)
	@echo "✓ Model $(OLLAMA_MODEL) pulled"

dev-setup: docker-up migrate ollama-pull ## Full local dev setup (containers + migrate + model)
	@ $(MAKE) --no-print-directory log-$@
	@echo "✓ Development environment ready"

mock-server: ## Start mock eBay server (port 8089)
	@ $(MAKE) --no-print-directory log-$@
	@go run ./tools/mock-server

###############
##@ Docker

.PHONY: docker-build docker-build-multiarch docker-bake-print docker-push

docker-build: ## Build local dev image (single-arch)
	@ $(MAKE) --no-print-directory log-$@
	@docker buildx bake dev

docker-build-multiarch: ## Validate multi-arch build (no push)
	@ $(MAKE) --no-print-directory log-$@
	@docker buildx bake ci

docker-bake-print: ## Print resolved bake config (debug)
	@ $(MAKE) --no-print-directory log-$@
	@docker buildx bake --print dev

docker-push: ## Build and push multi-arch image to registry
	@ $(MAKE) --no-print-directory log-$@
	@docker buildx bake release
