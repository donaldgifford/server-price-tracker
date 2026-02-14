# server-price-tracker - targets for docs


###############
##@ Repo Linting

.PHONY: lint-yaml lint-yaml-charts lint-yaml-fmt lint-md lint-actions lint-all

lint-yaml: ## Lint repo YAML files (excludes charts/)
	@ $(MAKE) --no-print-directory log-$@
	@yamllint -c .yamllint.yml . -e charts/
	@echo "✓ YAML lint passed"

lint-yaml-charts: ## Lint chart YAML files (relaxed rules)
	@ $(MAKE) --no-print-directory log-$@
	@yamllint -c $(CHARTS_DIR)/.yamllint.yml $(CHARTS_DIR)/
	@echo "✓ Chart YAML lint passed"

lint-yaml-fmt: ## Check YAML formatting (no modify)
	@ $(MAKE) --no-print-directory log-$@
	@yamlfmt -lint .
	@echo "✓ YAML formatting check passed"

lint-md: ## Lint markdown files with markdownlint-cli2
	@ $(MAKE) --no-print-directory log-$@
	@markdownlint-cli2 '**/*.md' '#node_modules' '#vendor'
	@echo "✓ Markdown lint passed"

lint-actions: ## Lint GitHub Actions workflow files
	@ $(MAKE) --no-print-directory log-$@
	@actionlint
	@echo "✓ Actions lint passed"

lint-all: lint lint-yaml lint-yaml-charts lint-md lint-actions helm-lint ## Run all linters (Go + YAML + Markdown + Actions + Helm)
	@ $(MAKE) --no-print-directory log-$@
	@echo "✓ All linters passed"
