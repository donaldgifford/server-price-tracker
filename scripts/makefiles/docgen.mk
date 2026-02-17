# server-price-tracker - CLI documentation generation


######################
##@ CLI Docs

.PHONY: cli-docs cli-docs-test

cli-docs: ## Generate CLI reference docs from the spt command tree
	@ $(MAKE) --no-print-directory log-$@
	@go run ./tools/docgen

cli-docs-test: ## Verify CLI docs are up to date
	@ $(MAKE) --no-print-directory log-$@
	@go run ./tools/docgen
	@git diff --exit-code docs/cli/ || (echo "CLI docs are out of date. Run 'make cli-docs' and commit." && exit 1)
