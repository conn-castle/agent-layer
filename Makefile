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

# Prune excluded directory roots before descent; -not -path still traverses them.
GO_FILES_FIND_CMD := find . \( -path './.git' -o -path './.tools' -o -path './.cache' -o -path './.claude' -o -path './.codex' -o -path './.gemini' -o -path './.agy' -o -path './.antigravitycli' -o -path './.agents' -o -path './.agent-layer' -o -path './tmp' \) -prune -o -type f -name '*.go'

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

.PHONY: shell-syntax-check
shell-syntax-check: ## Parse every tracked or untracked, non-ignored *.sh file without executing it
	@git ls-files -z --cached --others --exclude-standard -- '*.sh' | \
	  while IFS= read -r -d '' file; do \
	    [[ ! -e "$$file" ]] || bash -n -- "$$file" || exit $$?; \
	  done

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

.PHONY: test-deepswe-planner
test-deepswe-planner: ## Verify the website task-correlation evidence
	@node --test scripts/test-deepswe-planner.js

.PHONY: refresh-deepswe-planner-data
refresh-deepswe-planner-data: ## Download the official DeepSWE snapshot and regenerate planner data
	@mkdir -p .agent-layer/tmp/deepswe-planner-data
	@curl --fail --location --retry 3 --output .agent-layer/tmp/deepswe-planner-data/trials.json https://deepswe.datacurve.ai/artifacts/v1.1/trials.json
	@curl --fail --location --retry 3 --output .agent-layer/tmp/deepswe-planner-data/tasks.json https://deepswe.datacurve.ai/artifacts/v1.1/tasks.json
	@node scripts/build-deepswe-planner-data.js \
	  --trials .agent-layer/tmp/deepswe-planner-data/trials.json \
	  --tasks .agent-layer/tmp/deepswe-planner-data/tasks.json \
	  --output site/static/deepswe-planner/app/data.js \
	  --retrieved-at "$$(date -u +%Y-%m-%dT%H:%M:%S.000Z)"

.PHONY: test-race
test-race: ## Run race detector for concurrency-critical packages
	@mkdir -p "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go test -race ./internal/agentdispatch/... ./internal/sync/... ./internal/install/... ./internal/warnings/... ./internal/projectlock/... ./internal/skillimport/...

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
	@tmp_mod="$$(mktemp)" && tmp_sum="$$(mktemp)" && \
	  cp go.mod "$$tmp_mod" && cp go.sum "$$tmp_sum" && \
	  trap 'cp "$$tmp_mod" go.mod 2>/dev/null || true; cp "$$tmp_sum" go.sum 2>/dev/null || true; rm -f "$$tmp_mod" "$$tmp_sum"' EXIT && \
	  GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go mod tidy && \
	  if ! cmp -s "$$tmp_mod" go.mod || ! cmp -s "$$tmp_sum" go.sum; then \
	    echo "go mod tidy changed go.mod or go.sum" >&2; \
	    diff -u "$$tmp_mod" go.mod >&2 || true; \
	    diff -u "$$tmp_sum" go.sum >&2 || true; \
	    exit 1; \
	  fi

.PHONY: coverage
coverage: check-gotestsum ## Run tests with coverage reporting and write coverage.out
	@mkdir -p "$(GO_CACHE)" "$(GO_MOD_CACHE)"
	@GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" "$(TOOL_BIN)/gotestsum" --format testname -- ./... -coverprofile=coverage.out
	@GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go run -tags tools ./internal/tools/coverreport -profile coverage.out

.PHONY: test-release
test-release: ## Run release artifact tests
	@./scripts/test-release.sh

.PHONY: test-e2e
test-e2e: ## Run end-to-end tests (offline — uses cached binaries only)
	@./scripts/test-e2e.sh

.PHONY: test-e2e-online
test-e2e-online: ## Run e2e tests with online upgrade binary downloads
	@AL_E2E_ONLINE=1 ./scripts/test-e2e.sh

.PHONY: test-codex-dispatch-wait-live
test-codex-dispatch-wait-live: al-dev-build ## Run paid local Codex dispatch-wait polling test (never CI)
	@AL_LIVE_CODEX_DISPATCH_WAIT=1 go test -tags=live_codex -count=1 -run '^TestCodexDispatchWaitStaysDirect$$' -v ./internal/sync

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
	  echo "SITE_BUILD_TAG is required (example: make website-build-check SITE_BUILD_TAG=v999.0.0 WEBSITE_REPO_DIR=agent-layer-web)" >&2; \
	  exit 1; \
	fi
	@if [[ -z "$${WEBSITE_REPO_DIR:-}" ]]; then \
	  echo "WEBSITE_REPO_DIR is required (example: make website-build-check SITE_BUILD_TAG=v999.0.0 WEBSITE_REPO_DIR=agent-layer-web)" >&2; \
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

.PHONY: release-catalog-certify
release-catalog-certify: ## Certify clean, pushed main for release tagging in hosted CI
	@./scripts/certify-release-catalog.sh

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
ci: tidy-check fmt-check lint shell-syntax-check dead-code coverage test-deepswe-planner test-race test-release test-e2e-harness test-e2e-ci docs-cta-check ## Run CI checks locally

.PHONY: dev
dev: ## Fast local formatting and lint loop
	@$(MAKE) fmt
	@$(MAKE) lint

# Local dev targets — run al subcommands against this repo's own .agent-layer using source
AL_RUN := GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go run ./cmd/al
AL_DEV_BIN_DIR := $(ROOT_DIR)/.agent-layer/tmp/dev-bin
AL_DEV_BIN := $(AL_DEV_BIN_DIR)/al
AL_DEV_LAUNCH_ENV := PATH="$(AL_DEV_BIN_DIR):$$PATH" AL_DEV_BYPASS_VERSION_DISPATCH=1
AL_MANAGED_AGENT_ENV := AL_RUN_DIR AL_RUN_ID AL_DISPATCH_CALLER_AGENT AL_DISPATCH_ACTIVE AL_SHIM_ACTIVE AL_DEV_BYPASS_VERSION_DISPATCH CODEX_HOME CLAUDE_CONFIG_DIR AGY_CLI_DISABLE_AUTO_UPDATE GROK_HOME

.PHONY: al-dev-build
al-dev-build: ## Build source al for development commands that launch child processes
	@mkdir -p "$(AL_DEV_BIN_DIR)"
	@GOCACHE="$(GO_CACHE)" GOMODCACHE="$(GO_MOD_CACHE)" go build -o "$(AL_DEV_BIN)" ./cmd/al

.PHONY: al-upgrade
al-upgrade: ## Upgrade this repo's .agent-layer using current source
	@$(AL_RUN) upgrade

.PHONY: al-sync
al-sync: ## Sync this repo's generated agent files using current source
	@$(AL_RUN) sync

.PHONY: al-wizard
al-wizard: ## Run al wizard against this repo using current source
	@$(AL_RUN) wizard

.PHONY: al-doctor
al-doctor: al-dev-build ## Run al doctor against this repo using current source
	@unset $(AL_MANAGED_AGENT_ENV); $(AL_DEV_LAUNCH_ENV) "$(AL_DEV_BIN)" doctor

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

.PHONY: al-grok
al-grok: al-dev-build ## Run al grok against this repo using current source
	@unset $(AL_MANAGED_AGENT_ENV); $(AL_DEV_LAUNCH_ENV) "$(AL_DEV_BIN)" grok
