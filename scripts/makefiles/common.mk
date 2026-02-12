# Forge - Cli tool for managing project scaffolding.

## Project Variables

PROJECT_NAME  := server-price-tracker
PROJECT_OWNER := donaldgifford
DESCRIPTION   := An API-first Go service that monitors eBay listings for server hardware, extracts structured attributes via LLM , scores listings against historical baselines, and alerts on deals via Discord webhooks.
PROJECT_URL   := https://github.com/$(PROJECT_OWNER)/$(PROJECT_NAME)

## Go Variables

GO          ?= go
GO_PACKAGE  := github.com/$(PROJECT_OWNER)/$(PROJECT_NAME)
GOOS        ?= $(shell $(GO) env GOOS)
GOARCH      ?= $(shell $(GO) env GOARCH)

GOIMPORTS_LOCAL_ARG := -local github.com/donaldgifford

## Build Directories

BUILD_DIR      := build
BIN_DIR        := $(BUILD_DIR)/bin
CMD            := ./cmd

## Dev Variables
CONFIGS := ./configs
DEV_CONFIG_FILE := $(CONFIGS)/config.dev.yaml

## Version Information

COMMIT_HASH ?= $(shell git rev-parse --short HEAD 2>/dev/null)
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
CUR_VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null || git describe --tags 2>/dev/null || echo "v0.0.0-$(COMMIT_HASH)")

## Build Variables

COVERAGE_OUT := coverage.out

########################################################################
## Self-Documenting Makefile Help                                     ##
## https://marmelab.com/blog/2016/02/29/auto-documented-makefile.html ##
########################################################################

########
##@ Help

.PHONY: help
help:   ## Display this help
	@awk -v "col=\033[36m" -v "nocol=\033[0m" ' \
		BEGIN { FS = ":.*##" ; printf "Usage:\n  make %s<target>%s\n\n", col, nocol } \
		/^[a-zA-Z_0-9-]+:.*?##/ { printf "  %s%-25s%s %s\n", col, $$1, nocol, $$2 } \
		/^##@/ { printf "\n%s%s%s\n", nocol, substr($$0, 5), nocol } \
	' $(MAKEFILE_LIST)

## Log Pattern
## Automatically logs what a target does by extracting its ## comment
log-%:
	@grep -h -E '^$*:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN { FS = ":.*?## " }; { printf "\033[36m==> %s\033[0m\n", $$2 }'
