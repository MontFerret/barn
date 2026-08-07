.DEFAULT_GOAL := check

GO ?= go
GOFMT ?= gofmt
PACKAGES ?= ./...
BASE ?=
STAMP_TIMESTAMP ?=

GO_FILES := $(shell find . -type f -name '*.go' -not -path './vendor/*' | sort)

.PHONY: check build test test-race vet fmt fmt-check tidy mod-check validate generate verify check-immutable stamp check-stamped help

check: fmt-check vet test validate ## Run all local source and registry checks.

build: ## Build all Go packages.
	$(GO) build $(PACKAGES)

test: ## Run all tests.
	$(GO) test $(PACKAGES)

test-race: ## Run all tests with the race detector.
	$(GO) test -race $(PACKAGES)

vet: ## Run go vet.
	$(GO) vet $(PACKAGES)

fmt: ## Format all Go files.
	$(GO) fmt $(PACKAGES)

fmt-check: ## Check Go formatting without changing files.
	@unformatted="$$( $(GOFMT) -l $(GO_FILES) )"; \
	if [ -n "$$unformatted" ]; then \
		printf 'Unformatted Go files:\n%s\n' "$$unformatted" >&2; \
		exit 1; \
	fi

tidy: ## Update module metadata.
	$(GO) mod tidy

mod-check: ## Check module metadata without changing files.
	$(GO) mod tidy -diff

validate: ## Validate the complete registry and pinned releases.
	$(GO) run ./cmd/barn validate

generate: ## Generate the complete public distribution under dist/.
	$(GO) run ./cmd/barn generate

verify: ## Verify the complete generated dist/ tree is current.
	$(GO) run ./cmd/barn verify

check-immutable: ## Check published records against BASE.
	@test -n "$(BASE)" || { printf 'BASE is required\n' >&2; exit 2; }
	$(GO) run ./cmd/barn check-immutable --base "$(BASE)"

stamp: ## Stamp canonical versions missing publication metadata.
	$(GO) run ./cmd/barn stamp $(if $(STAMP_TIMESTAMP),--timestamp "$(STAMP_TIMESTAMP)")

check-stamped: ## Check that every canonical version has publication metadata.
	$(GO) run ./cmd/barn stamp --check

help: ## List available targets.
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "%-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
