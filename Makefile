SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c

.DEFAULT_GOAL := help

ROOT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
GIT_COMMON_DIR := $(shell git rev-parse --path-format=absolute --git-common-dir 2>/dev/null)
CACHE_ROOT ?= $(if $(GIT_COMMON_DIR),$(abspath $(GIT_COMMON_DIR)/../.cache),$(ROOT_DIR)/.cache)
TOOL_BIN ?= $(ROOT_DIR)/.tools/bin
GOLANGCI_LINT_VERSION := v2.12.2
GO_CACHE ?= $(CACHE_ROOT)/go-build
GO_MOD_CACHE ?= $(CACHE_ROOT)/go-mod
GOLANGCI_LINT_CACHE ?= $(ROOT_DIR)/.cache/golangci-lint

GO_FILES_FIND_CMD := find . -type f -name '*.go' -not -path './.tools/*' -not -path './.cache/*' -not -path './.claude/*' -not -path './.codex/*' -not -path './.gemini/*' -not -path './.agy/*' -not -path './.antigravitycli/*' -not -path './.agents/*' -not -path './.agent-layer/*' -not -path './tmp/*'

COVERAGE_THRESHOLD ?= 90.0

AL_VERSION ?= dev
DIST_DIR ?= dist
RELEASE_BINARIES := al-darwin-arm64 al-darwin-amd64 al-linux-arm64 al-linux-amd64

.PHONY: help
help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: tools
tools: $(TOOL_BIN)/goimports $(TOOL_BIN)/golangci-lint $(TOOL_BIN)/gotestsum $(TOOL_BIN)/deadcode ## Install pinned Go tools into $(TOOL_BIN)

.PHONY: release-tools
release-tools: $(TOOL_BIN)/govulncheck ## Install pinned release-only Go tools into $(TOOL_BIN)

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

.PHONY: check-deadcode
check-deadcode: ## Fail if deadcode is missing
	@if [[ ! -x "$(TOOL_BIN)/deadcode" ]]; then \
	  echo "deadcode not found at $(TOOL_BIN)/deadcode. Run: make tools" >&2; \
	  exit 1; \
	fi

.PHONY: check-govulncheck
check-govulncheck: ## Fail if govulncheck is missing
	@if [[ ! -x "$(TOOL_BIN)/govulncheck" ]]; then \
	  echo "govulncheck not found at $(TOOL_BIN)/govulncheck. Run: make release-tools" >&2; \
	  exit 1; \
	fi

.PHONY: check-tools
check-tools: check-goimports check-golangci-lint check-gotestsum check-deadcode ## Fail if any required tool is missing

$(TOOL_BIN)/goimports: go.mod go.sum
	@mkdir -p "$(TOOL_BIN)" "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@version="$$(go list -m -f '{{.Version}}' golang.org/x/tools)"; \
	  if [[ -z "$$version" ]]; then echo "Failed to resolve golang.org/x/tools version from go.mod" >&2; exit 1; fi; \
	  GOBIN="$(TOOL_BIN)" GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go install "golang.org/x/tools/cmd/goimports@$$version"

$(TOOL_BIN)/golangci-lint: Makefile
	@mkdir -p "$(TOOL_BIN)" "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@GOBIN="$(TOOL_BIN)" GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)"

$(TOOL_BIN)/gotestsum: go.mod go.sum
	@mkdir -p "$(TOOL_BIN)" "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@version="$$(go list -m -f '{{.Version}}' gotest.tools/gotestsum)"; \
	  if [[ -z "$$version" ]]; then echo "Failed to resolve gotestsum version from go.mod" >&2; exit 1; fi; \
	  GOBIN="$(TOOL_BIN)" GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go install "gotest.tools/gotestsum@$$version"

$(TOOL_BIN)/deadcode: go.mod go.sum
	@mkdir -p "$(TOOL_BIN)" "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@version="$$(go list -m -f '{{.Version}}' golang.org/x/tools)"; \
	  if [[ -z "$$version" ]]; then echo "Failed to resolve golang.org/x/tools version from go.mod" >&2; exit 1; fi; \
	  GOBIN="$(TOOL_BIN)" GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go install "golang.org/x/tools/cmd/deadcode@$$version"

$(TOOL_BIN)/govulncheck: go.mod go.sum
	@mkdir -p "$(TOOL_BIN)" "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@version="$$(go list -m -f '{{.Version}}' golang.org/x/vuln)"; \
	  if [[ -z "$$version" ]]; then echo "Failed to resolve golang.org/x/vuln version from go.mod" >&2; exit 1; fi; \
	  GOBIN="$(TOOL_BIN)" GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go install "golang.org/x/vuln/cmd/govulncheck@$$version"

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
	@mkdir -p "$(GOLANGCI_LINT_CACHE)"
	@GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" GOLANGCI_LINT_CACHE="$(GOLANGCI_LINT_CACHE)" "$(TOOL_BIN)/golangci-lint" run ./...

.PHONY: lint-ci-local
lint-ci-local: check-golangci-lint ## Run fresh-cache Linux-targeted and native-host lint
	@tmp_root="$$(mktemp -d "$${TMPDIR:-/tmp}/agent-layer-lint-ci-local.XXXXXX")"; \
	  trap 'chmod -R u+w "$$tmp_root" 2>/dev/null || true; rm -rf "$$tmp_root"' EXIT; \
	  mkdir -p "$$tmp_root/go-build" "$$tmp_root/go-mod" "$$tmp_root/golangci-lint"; \
	  GOCACHE="$$tmp_root/go-build" GOMODCACHE="$$tmp_root/go-mod" go mod download; \
	  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	    GOCACHE="$$tmp_root/go-build" \
	    GOMODCACHE="$$tmp_root/go-mod" \
	    GOLANGCI_LINT_CACHE="$$tmp_root/golangci-lint" \
	    "$(TOOL_BIN)/golangci-lint" run ./...; \
	  GOCACHE="$$tmp_root/go-build" \
	    GOMODCACHE="$$tmp_root/go-mod" \
	    GOLANGCI_LINT_CACHE="$$tmp_root/golangci-lint" \
	    "$(TOOL_BIN)/golangci-lint" run ./...

.PHONY: test
test: check-gotestsum ## Run tests
	@mkdir -p "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" "$(TOOL_BIN)/gotestsum" --format testname -- ./...

.PHONY: test-race
test-race: ## Run race detector for concurrency-critical packages
	@mkdir -p "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go test -race ./internal/agentdispatch/... ./internal/sync/... ./internal/install/... ./internal/warnings/...

.PHONY: dead-code
dead-code: check-deadcode ## Run dead code analysis across all packages (test-aware); fails on findings
	@mkdir -p "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@out="$$(GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" "$(TOOL_BIN)/deadcode" -test ./... 2>&1)"; rc=$$?; \
	  if [[ $$rc -ne 0 ]]; then echo "$$out" >&2; echo "deadcode failed (exit $$rc); see output above" >&2; exit $$rc; fi; \
	  if [[ -n "$$out" ]]; then echo "$$out" >&2; echo "dead code detected (deadcode always exits 0; non-empty output fails this target)" >&2; exit 1; fi

.PHONY: dead-code-entrypoints
dead-code-entrypoints: check-deadcode ## Run dead code analysis from CLI entrypoints only; fails on findings
	@mkdir -p "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@out="$$(GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" "$(TOOL_BIN)/deadcode" -test ./cmd/al ./cmd/publish-site 2>&1)"; rc=$$?; \
	  if [[ $$rc -ne 0 ]]; then echo "$$out" >&2; echo "deadcode failed (exit $$rc); see output above" >&2; exit $$rc; fi; \
	  if [[ -n "$$out" ]]; then echo "$$out" >&2; echo "dead code detected (deadcode always exits 0; non-empty output fails this target)" >&2; exit 1; fi

.PHONY: tidy
tidy: ## Run go mod tidy
	@mkdir -p "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go mod tidy

.PHONY: tidy-check
tidy-check: ## Verify go.mod/go.sum are tidy
	@mkdir -p "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@before_mod="$$(git hash-object go.mod)"; before_sum="$$(git hash-object go.sum)"; \
	  GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go mod tidy; \
	  after_mod="$$(git hash-object go.mod)"; after_sum="$$(git hash-object go.sum)"; \
	  if [[ "$$before_mod" != "$$after_mod" || "$$before_sum" != "$$after_sum" ]]; then \
	    echo "go mod tidy changed go.mod or go.sum" >&2; \
	    git diff -- go.mod go.sum >&2; \
	    exit 1; \
	  fi

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
test-e2e: ## Run end-to-end tests (offline — uses cached binaries only)
	@./scripts/test-e2e.sh

.PHONY: test-e2e-online
test-e2e-online: ## Run e2e tests with online upgrade binary downloads
	@AL_E2E_ONLINE=1 ./scripts/test-e2e.sh

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

.PHONY: website-build-check
website-build-check: ## Publish site into a website checkout and run Docusaurus build (set SITE_BUILD_TAG=vX.Y.Z WEBSITE_REPO_DIR=path)
	@if [[ -z "$${SITE_BUILD_TAG:-}" ]]; then \
	  echo "SITE_BUILD_TAG is required (example: make website-build-check SITE_BUILD_TAG=v0.0.0 WEBSITE_REPO_DIR=agent-layer-web)" >&2; \
	  exit 1; \
	fi
	@if [[ -z "$${WEBSITE_REPO_DIR:-}" ]]; then \
	  echo "WEBSITE_REPO_DIR is required (example: make website-build-check SITE_BUILD_TAG=v0.0.0 WEBSITE_REPO_DIR=agent-layer-web)" >&2; \
	  exit 1; \
	fi
	@npm --prefix "$${WEBSITE_REPO_DIR}" ci
	@GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go run ./cmd/publish-site \
	  --tag "$${SITE_BUILD_TAG}" \
	  --repo-b-dir "$${WEBSITE_REPO_DIR}"
	@npm --prefix "$${WEBSITE_REPO_DIR}" run build

.PHONY: release-preflight
release-preflight: ci test-release ## Validate release readiness (set RELEASE_TAG=vX.Y.Z)
	@if [[ -z "$${RELEASE_TAG:-}" ]]; then \
	  echo "RELEASE_TAG is required (example: make release-preflight RELEASE_TAG=v0.8.0)" >&2; \
	  exit 1; \
	fi
	@./scripts/check-upgrade-docs.sh --tag "$${RELEASE_TAG}"

.PHONY: release-dist
release-dist: test-release ## Build release artifacts (cross-compile)
	@AL_VERSION="$(AL_VERSION)" DIST_DIR="$(DIST_DIR)" ./scripts/build-release.sh

.PHONY: release-vuln-check
release-vuln-check: check-govulncheck ## Scan every release executable for known vulnerable symbols (set DIST_DIR=dist)
	@for binary in $(RELEASE_BINARIES); do \
	  path="$(DIST_DIR)/$$binary"; \
	  if [[ ! -f "$$path" ]]; then echo "Release binary not found: $$path" >&2; exit 1; fi; \
	done
	@for binary in $(RELEASE_BINARIES); do \
	  "$(TOOL_BIN)/govulncheck" -mode=binary "$(DIST_DIR)/$$binary" || exit $$?; \
	done

.PHONY: setup
setup: ## Run one-time setup for this clone
	@./scripts/setup.sh

.PHONY: test-e2e-harness
test-e2e-harness: ## Run e2e harness self-tests (auth, helpers)
	@./scripts/test-e2e/test-harness-auth.sh

.PHONY: test-e2e-ci
test-e2e-ci: ## Run e2e tests for CI (online downloads, upgrade scenarios required)
	@AL_E2E_ONLINE=1 AL_E2E_REQUIRE_UPGRADE=1 ./scripts/test-e2e.sh

.PHONY: ci
ci: tidy-check fmt-check lint dead-code coverage test-race test-release test-e2e-harness test-e2e-ci docs-cta-check ## Run CI checks locally

.PHONY: dev
dev: ## Fast local checks during development (format + lint + coverage + release tests)
	@$(MAKE) fmt
	@$(MAKE) fmt-check
	@$(MAKE) lint
	@$(MAKE) coverage
	@$(MAKE) test-release

# Local dev targets — run al subcommands against this repo's own .agent-layer using source
AL_RUN := GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go run ./cmd/al
AL_DEV_BIN_DIR := $(ROOT_DIR)/.agent-layer/tmp/dev-bin
AL_DEV_BIN := $(AL_DEV_BIN_DIR)/al
AL_DEV_LAUNCH_ENV := PATH="$(AL_DEV_BIN_DIR):$$PATH" AL_DEV_BYPASS_VERSION_DISPATCH=1
AL_MANAGED_AGENT_ENV := AL_RUN_DIR AL_RUN_ID AL_DISPATCH_CALLER_AGENT AL_DISPATCH_ACTIVE AL_SHIM_ACTIVE AL_DEV_BYPASS_VERSION_DISPATCH CODEX_HOME CLAUDE_CONFIG_DIR AGY_CLI_DISABLE_AUTO_UPDATE

.PHONY: al-dev-build
al-dev-build: ## Build source al for interactive development launchers
	@mkdir -p "$(AL_DEV_BIN_DIR)"
	@GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go build -o "$(AL_DEV_BIN)" ./cmd/al

.PHONY: al-upgrade
al-upgrade: ## Upgrade this repo's .agent-layer using current source
	@$(AL_RUN) upgrade

.PHONY: al-wizard
al-wizard: ## Run al wizard against this repo using current source
	@$(AL_RUN) wizard

.PHONY: al-doctor
al-doctor: ## Run al doctor against this repo using current source
	@$(AL_RUN) doctor

.PHONY: al-claude
al-claude: al-dev-build ## Run al claude against this repo using current source
	@unset $(AL_MANAGED_AGENT_ENV); $(AL_DEV_LAUNCH_ENV) "$(AL_DEV_BIN)" claude

.PHONY: al-codex
al-codex: al-dev-build ## Run al codex against this repo using current source
	@unset $(AL_MANAGED_AGENT_ENV); $(AL_DEV_LAUNCH_ENV) "$(AL_DEV_BIN)" codex

.PHONY: al-agy
al-agy: al-dev-build ## Run al agy against this repo using current source
	@unset $(AL_MANAGED_AGENT_ENV); $(AL_DEV_LAUNCH_ENV) "$(AL_DEV_BIN)" agy

.PHONY: al-copilot
al-copilot: al-dev-build ## Run al copilot against this repo using current source
	@unset $(AL_MANAGED_AGENT_ENV); $(AL_DEV_LAUNCH_ENV) "$(AL_DEV_BIN)" copilot
