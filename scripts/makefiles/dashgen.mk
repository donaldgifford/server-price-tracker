# server-price-tracker - Grafana dashboard and Prometheus rules generation


######################
##@ Dashboards

.PHONY: dashboards dashboards-validate dashboards-test

dashboards: ## Generate Grafana dashboards and Prometheus rules
	@ $(MAKE) --no-print-directory log-$@
	@cd tools/dashgen && go run .

dashboards-validate: ## Validate generated dashboards and rules
	@ $(MAKE) --no-print-directory log-$@
	@cd tools/dashgen && go run . -validate

dashboards-test: ## Run dashgen tests
	@ $(MAKE) --no-print-directory log-$@
	@cd tools/dashgen && go test ./...
