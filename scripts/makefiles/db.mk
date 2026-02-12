# server-price-tracker - Database Migration Targets
# PostgreSQL schema migrations
# TODO: look into using golang-migrate.

#######################
##@ Database Migrations

.PHONY: migrate

## Migration Targets

migrate: ## run migrations
	go run $(CMD) migrate
