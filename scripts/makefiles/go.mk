# server-price-tracjer - Go Backend Targets Go build, test, lint, and code generation targets

###############
##@ Go Development

.PHONY: build build-core build-spt
.PHONY: test test-all test-pkg test-report test-coverage test-integration test-integration-all
.PHONY: lint lint-fix fmt clean generate mocks postman postman-test
.PHONY: run run-local ci check
.PHONY: release-check release-local

## Build Targets

build: build-core build-spt ## Build everything (server + CLI)

build-core: ## Build server binary
	@ $(MAKE) --no-print-directory log-$@
	@mkdir -p $(BIN_DIR)
	@go build -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT_HASH)" -o $(BIN_DIR)/$(PROJECT_NAME) $(CMD)/$(PROJECT_NAME)
	@echo "✓ Server binary built"

build-spt: ## Build spt CLI client binary
	@ $(MAKE) --no-print-directory log-$@
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_DIR)/spt $(CMD)/spt
	@echo "✓ spt CLI built"

## Testing

test: ## Run all tests with race detector
	@ $(MAKE) --no-print-directory log-$@
	@go test -v -race ./...

test-all: test ## Run all tests (core + plugins)

test-pkg: ## Test specific package (usage: make test-pkg PKG=./pkg/api)
	@ $(MAKE) --no-print-directory log-$@
	@go test -v -race $(PKG)

test-report: ## Run tests with coverage report then open
	@ $(MAKE) --no-print-directory log-$@
	@go test -coverprofile=$(COVERAGE_OUT) ./...
	@go tool cover -html=$(COVERAGE_OUT)

test-coverage: ## Run tests with coverage report
	@ $(MAKE) --no-print-directory log-$@
	@go test -v -race -coverprofile=$(COVERAGE_OUT) -covermode=atomic ./...
	@echo "✓ Test coverage generated"

test-e2e: ## Run integration tests
	@ $(MAKE) --no-print-directory log-$@
	@go test -v -tags=integration ./tests/e2e...

test-integration: ## Run Ebay API integration tests
	@ $(MAKE) --no-print-directory log-$@
	@echo "Running integration tests..."
	@go test -tags integration -count=1 ./...
	@echo "✓ Integration tests passed"


## Code Quality

lint: ## Run golangci-lint
	@ $(MAKE) --no-print-directory log-$@
	@golangci-lint run ./...

lint-fix: ## Run golangci-lint with auto-fix
	@ $(MAKE) --no-print-directory log-$@
	@golangci-lint run --fix ./...

fmt: ## Format code with gofmt and goimports
	@ $(MAKE) --no-print-directory log-$@
	@gofmt -s -w .
	@goimports -w $(GOIMPORTS_LOCAL_ARG) .

clean: ## Remove build artifacts
	@ $(MAKE) --no-print-directory log-$@
	@rm -rf $(BIN_DIR)/
	@rm -f $(COVERAGE_OUT)
	@go clean -cache
	@find . -name "*.test" -delete
	@echo "✓ Build artifacts cleaned"


## Code Generation

generate: mocks ## Generate all code (mocks)
	@ $(MAKE) --no-print-directory log-$@
	@echo "✓ Code generation complete"

mocks: ## Generate mocks for testing
	@ $(MAKE) --no-print-directory log-$@
	@mockery --config .mockery.yaml
	@echo "✓ Mocks generated"

postman: ## Generate Postman collection with contract tests (requires running server)
	@ $(MAKE) --no-print-directory log-$@
	@portman -l http://localhost:8080/openapi.json \
		-c portman/portman-config.json \
		-o portman/postman_collection.json
	@echo "✓ Postman collection generated in portman/postman_collection.json"

postman-test: postman ## Run Postman collection tests via Newman (requires running server)
	@ $(MAKE) --no-print-directory log-$@
	@newman run portman/postman_collection.json \
		-e portman/environments/dev.json
	@echo "✓ Postman tests passed"

## Application Services

run: ## Go Run (override config: make run CONFIG=configs/other.yaml)
	@ $(MAKE) --no-print-directory log-$@
	@go run $(CMD)/$(PROJECT_NAME) serve --config $(CONFIG)

run-local: build ## Run built binary
	@ $(MAKE) --no-print-directory log-$@
	@$(BIN_DIR)/$(PROJECT_NAME) --config $(CONFIG)

## License Compliance

license-check: ## Check dependency licenses against allowed list
	@ $(MAKE) --no-print-directory log-$@
	@go-licenses check ./... --allowed_licenses=Apache-2.0,MIT,BSD-2-Clause,BSD-3-Clause,ISC,MPL-2.0

license-report: ## Generate CSV report of all dependency licenses
	@ $(MAKE) --no-print-directory log-$@
	@go-licenses report ./... --template=.github/licenses-csv.tpl

## CI/CD

ci: lint test build license-check ## Run CI pipeline (lint + test + build + license check)
	@ $(MAKE) --no-print-directory log-$@
	@echo "✓ CI pipeline complete"

check: lint test ## Quick pre-commit check (lint + test)
	@ $(MAKE) --no-print-directory log-$@
	@echo "✓ Pre-commit checks passed"

# =============================================================================
# Release Targets
# =============================================================================

release: ## Create release (use with TAG=v1.0.0)
	@ $(MAKE) --no-print-directory log-$@
	@if [ -z "$(TAG)" ]; then \
		echo "Error: TAG is required. Usage: make release TAG=v1.0.0"; \
		exit 1; \
	fi
	git tag -a $(TAG) -m "Release $(TAG)"
	git push origin $(TAG)

release-check: ## Validate goreleaser
	@ $(MAKE) --no-print-directory log-$@
	goreleaser check

release-local: ## Test goreleaser without publishing
	@ $(MAKE) --no-print-directory log-$@
	goreleaser release --snapshot --clean --skip=publish --skip=sign
	@echo "✓ Released local version"
