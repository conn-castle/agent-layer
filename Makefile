SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c

.DEFAULT_GOAL := help

ROOT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
TOOL_BIN ?= $(ROOT_DIR)/.tools/bin
GO_CACHE ?= $(ROOT_DIR)/.cache/go-build
GO_MOD_CACHE ?= $(ROOT_DIR)/.cache/go-mod

GO_FILES_FIND_CMD := find . -type f -name '*.go' -not -path './.tools/*' -not -path './.cache/*' -not -path './tmp/*'

COVERAGE_THRESHOLD ?= 95.0

AL_VERSION ?= dev
DIST_DIR ?= dist

.PHONY: help
help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: tools
tools: $(TOOL_BIN)/goimports $(TOOL_BIN)/golangci-lint $(TOOL_BIN)/gotestsum ## Install pinned Go tools into $(TOOL_BIN)

.PHONY: check-goimports
check-goimports: ## Fail if goimports is missing
	@if [[ ! -x "$(TOOL_BIN)/goimports" ]]; then \
	  echo "goimports not found at $(TOOL_BIN)/goimports. Run: make tools" >&2; \
	  exit 1; \
	fi

.PHONY: check-golangci-lint
check-golangci-lint: ## Fail if golangci-lint is missing
	@if [[ ! -x "$(TOOL_BIN)/golangci-lint" ]]; then \
	  echo "golangci-lint not found at $(TOOL_BIN)/golangci-lint. Run: make tools" >&2; \
	  exit 1; \
	fi

.PHONY: check-gotestsum
check-gotestsum: ## Fail if gotestsum is missing
	@if [[ ! -x "$(TOOL_BIN)/gotestsum" ]]; then \
	  echo "gotestsum not found at $(TOOL_BIN)/gotestsum. Run: make tools" >&2; \
	  exit 1; \
	fi

.PHONY: check-tools
check-tools: check-goimports check-golangci-lint check-gotestsum ## Fail if any required tool is missing

$(TOOL_BIN)/goimports: go.mod go.sum
	@mkdir -p "$(TOOL_BIN)" "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@version="$$(go list -m -f '{{.Version}}' golang.org/x/tools)"; \
	  if [[ -z "$$version" ]]; then echo "Failed to resolve golang.org/x/tools version from go.mod" >&2; exit 1; fi; \
	  GOBIN="$(TOOL_BIN)" GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go install "golang.org/x/tools/cmd/goimports@$$version"

$(TOOL_BIN)/golangci-lint: go.mod go.sum
	@mkdir -p "$(TOOL_BIN)" "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@version="$$(go list -m -f '{{.Version}}' github.com/golangci/golangci-lint/v2)"; \
	  if [[ -z "$$version" ]]; then echo "Failed to resolve golangci-lint version from go.mod" >&2; exit 1; fi; \
	  GOBIN="$(TOOL_BIN)" GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$$version"

$(TOOL_BIN)/gotestsum: go.mod go.sum
	@mkdir -p "$(TOOL_BIN)" "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@version="$$(go list -m -f '{{.Version}}' gotest.tools/gotestsum)"; \
	  if [[ -z "$$version" ]]; then echo "Failed to resolve gotestsum version from go.mod" >&2; exit 1; fi; \
	  GOBIN="$(TOOL_BIN)" GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go install "gotest.tools/gotestsum@$$version"

.PHONY: fmt
fmt: check-goimports ## Format Go files (gofmt + goimports)
	@$(GO_FILES_FIND_CMD) -print0 | xargs -0 gofmt -w
	@$(GO_FILES_FIND_CMD) -print0 | xargs -0 "$(TOOL_BIN)/goimports" -local "github.com/conn-castle/agent-layer" -w

.PHONY: fmt-check
fmt-check: check-goimports ## Check Go formatting (gofmt + goimports)
	@out="$$($(GO_FILES_FIND_CMD) -print0 | xargs -0 gofmt -l)"; \
	  if [[ -n "$$out" ]]; then echo "gofmt needed for:" >&2; echo "$$out" >&2; exit 1; fi
	@out="$$($(GO_FILES_FIND_CMD) -print0 | xargs -0 "$(TOOL_BIN)/goimports" -local "github.com/conn-castle/agent-layer" -l)"; \
	  if [[ -n "$$out" ]]; then echo "goimports needed for:" >&2; echo "$$out" >&2; exit 1; fi

.PHONY: lint
lint: check-golangci-lint ## Run golangci-lint
	@GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" "$(TOOL_BIN)/golangci-lint" run ./...

.PHONY: test
test: check-gotestsum ## Run tests
	@mkdir -p "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" "$(TOOL_BIN)/gotestsum" --format testname -- ./...

.PHONY: tidy
tidy: ## Run go mod tidy
	@mkdir -p "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go mod tidy

.PHONY: tidy-check
tidy-check: ## Verify go.mod/go.sum are tidy
	@mkdir -p "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go mod tidy
	@git diff --exit-code

.PHONY: coverage
coverage: check-gotestsum ## Enforce coverage threshold (>= $(COVERAGE_THRESHOLD)) and write coverage.out
	@mkdir -p "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" "$(TOOL_BIN)/gotestsum" --format testname -- ./... -coverprofile=coverage.out
	@total="$$(go tool cover -func=coverage.out | awk '/^total:/ {print $$3}' | tr -d '%')"; \
	  if [[ -z "$$total" ]]; then echo "Failed to read total coverage from coverage.out" >&2; exit 1; fi; \
	  status=0; \
	  awk -v total="$$total" -v threshold="$(COVERAGE_THRESHOLD)" 'BEGIN { \
	    if (total + 0 < threshold + 0) { \
	      printf("Coverage %.2f%% is below threshold %.2f%%\n", total, threshold) > "/dev/stderr"; \
	      exit 1; \
	    } \
	  }' || status=1; \
	  GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go run -tags tools ./internal/tools/coverreport -profile coverage.out -threshold "$(COVERAGE_THRESHOLD)"; \
	  exit $$status

.PHONY: test-release
test-release: ## Run release artifact tests
	@./scripts/test-release.sh

.PHONY: test-e2e
test-e2e: ## Run end-to-end build/install smoke tests
	@./scripts/test-e2e.sh

.PHONY: docs-upgrade-check
docs-upgrade-check: ## Validate upgrade contract docs for a release tag (set RELEASE_TAG=vX.Y.Z)
	@if [[ -z "$${RELEASE_TAG:-}" ]]; then \
	  echo "RELEASE_TAG is required (example: make docs-upgrade-check RELEASE_TAG=v0.7.0)" >&2; \
	  exit 1; \
	fi
	@./scripts/check-upgrade-docs.sh --tag "$${RELEASE_TAG}"

.PHONY: docs-cta-check
docs-cta-check: ## Validate upgrade CTA syntax in core docs/messages
	@./scripts/check-upgrade-ctas.sh

.PHONY: release-dist
release-dist: test-release ## Build release artifacts (cross-compile)
	@AL_VERSION="$(AL_VERSION)" DIST_DIR="$(DIST_DIR)" ./scripts/build-release.sh

.PHONY: setup
setup: ## Run one-time setup for this clone
	@./scripts/setup.sh

.PHONY: ci
ci: tidy-check fmt-check lint coverage test-release test-e2e ## Run CI checks locally

.PHONY: dev
dev: ## Fast local checks during development (format + lint + coverage + release tests)
	@$(MAKE) fmt
	@$(MAKE) fmt-check
	@$(MAKE) lint
	@$(MAKE) coverage
	@$(MAKE) test-release
