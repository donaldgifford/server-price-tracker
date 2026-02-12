# server-price-tracker - Docker Targets
# Docker Compose development environment
#
# NOTE: Docker targets are placeholder references from another project.
#   We eventually want to add docker-compose targets here to make local
#   dev easier (PostgreSQL, Ollama, etc.).

###################
##@ Docker Development

# .PHONY: docker-up docker-down docker-clean docker-logs

# docker-up: ## Start PostgreSQL
# 	@ $(MAKE) --no-print-directory log-$@
# 	@docker compose -f <path-to-docker-compose.yml> up -d
# 	@echo "Waiting for PostgreSQL to be ready..."
# 	@sleep 5
# 	@echo "✓ Development databases started"

# docker-down: ## Stop database container
# 	@ $(MAKE) --no-print-directory log-$@
# 	@docker compose -f <path-to-docker-compose.yml> down
# 	@echo "✓ Development databases stopped"

# docker-clean: ## Stop and remove containers, volumes, and images (full reset)
# 	@ $(MAKE) --no-print-directory log-$@
# 	@docker compose -f <path-to-docker-compose.yml> down -v --rmi local
# 	@echo "✓ Development databases cleaned"

# docker-logs: ## View database container logs
# 	@ $(MAKE) --no-print-directory log-$@
# 	@docker compose -f <path-to-docker-compose.yml> logs -f
