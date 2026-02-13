# server-price-tracker - Database Migration Targets
# PostgreSQL schema migrations
# TODO: look into using golang-migrate.

#######################
##@ Database Migrations

.PHONY: migrate

## Migration Targets

migrate: ## Run database migrations
	@ $(MAKE) --no-print-directory log-$@
	@go run $(CMD)/$(PROJECT_NAME) migrate --config $(CONFIG)
