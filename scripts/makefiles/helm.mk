# server-price-tracker - Helm Chart Targets
# Chart linting, testing, templating, packaging, and documentation

CHART_DIR  := charts/server-price-tracker
CHARTS_DIR := charts

###############
##@ Helm Development

.PHONY: helm-lint helm-template helm-template-ci helm-package

helm-lint: ## Lint the Helm chart
	@ $(MAKE) --no-print-directory log-$@
	@helm lint $(CHART_DIR)
	@echo "✓ Helm lint passed"

helm-template: ## Render chart templates with default values
	@ $(MAKE) --no-print-directory log-$@
	@helm template test $(CHART_DIR)

helm-template-ci: ## Render chart templates with CI values (stub image, no probes)
	@ $(MAKE) --no-print-directory log-$@
	@helm template test $(CHART_DIR) --values $(CHART_DIR)/ci/ci-values.yaml

helm-package: ## Package the Helm chart into a .tgz archive
	@ $(MAKE) --no-print-directory log-$@
	@helm package $(CHART_DIR)
	@echo "✓ Chart packaged"

###############
##@ Helm Testing

.PHONY: helm-unittest helm-test

helm-unittest: ## Run helm-unittest plugin tests
	@ $(MAKE) --no-print-directory log-$@
	@helm unittest $(CHART_DIR)
	@echo "✓ Helm unit tests passed"

helm-test: helm-lint helm-unittest ## Run all Helm tests (lint + unit tests)
	@ $(MAKE) --no-print-directory log-$@
	@echo "✓ All Helm tests passed"

###############
##@ Helm Chart Testing (ct)

.PHONY: helm-ct-lint helm-ct-list-changed helm-ct-install

helm-ct-lint: ## Lint charts with chart-testing (ct lint)
	@ $(MAKE) --no-print-directory log-$@
	@ct lint --config ct.yaml --all
	@echo "✓ Chart-testing lint passed"

helm-ct-list-changed: ## List charts changed since target branch
	@ $(MAKE) --no-print-directory log-$@
	@ct list-changed --config ct.yaml

helm-ct-install: ## Install and test charts in kind cluster (ct install)
	@ $(MAKE) --no-print-directory log-$@
	@ct install --config ct.yaml --all
	@echo "✓ Chart-testing install passed"

###############
##@ Helm Tools

.PHONY: helm-docs helm-diff-check helm-cr-package

helm-docs: ## Generate chart documentation with helm-docs
	@ $(MAKE) --no-print-directory log-$@
	@helm-docs --chart-search-root $(CHARTS_DIR)
	@echo "✓ Chart docs generated"

helm-diff-check: ## Show diff between installed release and local chart (usage: make helm-diff-check RELEASE=spt)
	@ $(MAKE) --no-print-directory log-$@
	@if [ -z "$(RELEASE)" ]; then \
		echo "Error: RELEASE is required. Usage: make helm-diff-check RELEASE=spt"; \
		exit 1; \
	fi
	@helm diff upgrade $(RELEASE) $(CHART_DIR)

helm-cr-package: ## Package chart with chart-releaser (cr package)
	@ $(MAKE) --no-print-directory log-$@
	@cr package $(CHART_DIR)
	@echo "✓ Chart-releaser package complete"


