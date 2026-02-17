# server-price-tracker - Root Makefile
# Orchestration and domain-specific makefile includes
#
# This root Makefile coordinates domain-specific makefiles:
#   - makefiles/common.mk    - Shared variables and patterns
#   - makefiles/go.mk        - Go backend targets
#   - makefiles/docker.mk    - Docker and LocalStack
#   - makefiles/db.mk        - Database migrations
#   - makefiles/helm.mk      - helm targets
#   - makefiles/docs.mk      - docs and task management
#   - makefiles/dashgen.mk   - Grafana dashboard and Prometheus rules generation
#   - makefiles/docgen.mk    - CLI documentation generation
#
# All domain targets are defined in their respective makefiles.
# This file only contains orchestration targets that coordinate across domains.

.DEFAULT_GOAL := help

## Include Domain Makefiles
## Order matters: common.mk must be first (defines shared variables and help system)

include scripts/makefiles/common.mk
include scripts/makefiles/go.mk
include scripts/makefiles/docker.mk
include scripts/makefiles/db.mk
include scripts/makefiles/helm.mk
include scripts/makefiles/docs.mk
include scripts/makefiles/dashgen.mk
include scripts/makefiles/docgen.mk

######################
##@ Orchestration

.PHONY: all clean-all

all: lint test build ## Build everything (lint + test + backend )
	@ $(MAKE) --no-print-directory log-$@
	@echo "✓ All targets complete"

clean-all: clean api-docs-clean ## Clean all build artifacts and generated files
	@ $(MAKE) --no-print-directory log-$@
	@echo "✓ All artifacts cleaned"

## Special Pattern to Capture Remaining Arguments
## Allows commands like: make adr "My Title" or make rfc "My RFC"
## This must be at the end of the Makefile
%:
	@:

## End of Root Makefile
## See scripts/makefiles/*.mk for domain-specific targets
