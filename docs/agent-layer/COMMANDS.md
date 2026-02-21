# Commands

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
Canonical, repeatable **development workflow** commands for this repository (setup, build, run, test, coverage, lint/format, typecheck, migrations, scripts). This file is not for application/CLI usage documentation.

## Format
- Prefer commands that are stable and will be used repeatedly. Avoid one-off debugging commands.
- Organize commands using headings that fit the repo. Create headings as needed.
- If the repo is a monorepo, group commands per workspace/package/service and specify the working directory.
- When commands change, update this file and remove stale entries.
- Insert entries (and any needed headings) below `<!-- ENTRIES START -->`.

### Entry template
````text
- <Short purpose>
```bash
<command>
```
Run from: <repo root or path>  
Prerequisites: <only if critical>  
Notes: <optional constraints or tips>
````

<!-- ENTRIES START -->

### Setup

- Setup a fresh clone (installs pinned tools + pre-commit hooks)
```bash
./scripts/setup.sh
```
Run from: repo root  
Prerequisites: Go 1.25.6+, Make  
Notes: Uses versions pinned in `go.mod`. Installs tools into `.tools/bin`.

- Install pinned Go tooling (goimports, golangci-lint, gotestsum, deadcode) only
```bash
make tools
```
Run from: repo root  
Prerequisites: Go 1.25.6+, Make  
Notes: Uses versions pinned in `go.mod`. Installs tools into `.tools/bin`.

- Install pre-commit hooks
```bash
pre-commit install --install-hooks
```
Run from: repo root  
Prerequisites: `pre-commit` installed

- Run pre-commit on all files
```bash
pre-commit run --all-files
```
Run from: repo root  
Prerequisites: `pre-commit` installed

### Format

- Format Go code (gofmt + goimports)
```bash
make fmt
```
Run from: repo root  
Prerequisites: `make tools` has been run  
Notes: Applies formatting in place.

- Check formatting (CI/local)
```bash
make fmt-check
```
Run from: repo root  
Prerequisites: `make tools` has been run  
Notes: Fails if any files need formatting.

### Lint

- Run golangci-lint
```bash
make lint
```
Run from: repo root  
Prerequisites: `make tools` has been run

- Run dead code analysis across all packages (test-aware default)
```bash
make dead-code
```
Run from: repo root  
Prerequisites: `make tools` has been run
Notes: Runs `deadcode -test ./...` for high-signal results that include package test executables.

- Run entrypoint-focused dead code analysis (higher noise, deeper audit)
```bash
make dead-code-entrypoints
```
Run from: repo root  
Prerequisites: `make tools` has been run
Notes: Runs `deadcode -test` from `./cmd/al` and `./cmd/publish-site` roots; useful when auditing CLI-reachability specifically.

### Test

- Run all tests
```bash
make test
```
Run from: repo root
Prerequisites: `make tools` has been run
Notes: Uses `gotestsum` for nicer output.

- Run race detector on concurrency-critical packages
```bash
make test-race
```
Run from: repo root
Prerequisites: Go 1.25.6+
Notes: Covers `internal/sync`, `internal/install`, and `internal/warnings`.

- Run scenario-based end-to-end tests (offline, hermetic)
```bash
make test-e2e
```
Run from: repo root
Prerequisites: Go 1.25.6+, `sha256sum` or `shasum`
Notes: Builds release artifacts and runs all discovered scenarios with mock agent binaries. Auto-detects latest migration manifest version for upgrade testing. Upgrade scenarios use pre-cached binaries from `~/.cache/al-e2e/bin/` (run `make test-e2e-online` once to populate cache). Override version with `AL_E2E_VERSION=vX.Y.Z`. Filter: `AL_E2E_SCENARIOS="upgrade*" make test-e2e`. `defaults.toml` profile fixture is generated at runtime from `internal/templates/config.toml` to prevent drift.

- Run e2e tests with online upgrade binary downloads
```bash
make test-e2e-online
```
Run from: repo root
Prerequisites: Go 1.25.6+, `curl`, `sha256sum` or `shasum`, network access
Notes: Same as `make test-e2e` but sets `AL_E2E_ONLINE=1` to download release binaries from GitHub. Use before releases or to populate the persistent binary cache. Pin the latest release version with `AL_E2E_LATEST_VERSION=X.Y.Z`.

- Run e2e tests for CI (mandatory upgrade scenarios)
```bash
make test-e2e-ci
```
Run from: repo root
Prerequisites: Go 1.25.6+, `curl`, `sha256sum` or `shasum`, network access
Notes: Same as `make test-e2e-online` but also sets `AL_E2E_REQUIRE_UPGRADE=1` to fail hard if upgrade binaries are missing. Used by `make ci`. Ensures 100% of scenarios execute including upgrade paths.

### Modules

- Run go mod tidy
```bash
make tidy
```
Run from: repo root  
Prerequisites: Go 1.25.6+

- Verify go.mod/go.sum are tidy
```bash
make tidy-check
```
Run from: repo root  
Prerequisites: Go 1.25.6+  
Notes: Fails if `go.mod`/`go.sum` would change.

### Coverage

- Enforce coverage threshold (>= 95%)
```bash
make coverage
```
Run from: repo root  
Prerequisites: Go 1.25.6+
Notes: Canonical local/CI parity command for coverage. `make dev` and `make ci` both route through this target, and GitHub Actions runs `make ci`.

### Dev

- Primary local test command (format + fmt-check + lint + coverage + release tests)
```bash
make dev
```
Run from: repo root
Prerequisites: Go 1.25.6+, `make tools` has been run

### CI

- Run CI checks locally
```bash
make ci
```
Run from: repo root
Prerequisites: Go 1.25.6+, `make tools` has been run
Notes: Includes `make tidy-check`, `make test-release`, `make test-e2e-ci` (online e2e with required upgrade scenarios), and `make docs-cta-check`; requires a clean working tree and network access for upgrade binary downloads.

### Release

- Generate an embedded template ownership manifest for a release version
```bash
./scripts/generate-template-manifest.sh --tag vX.Y.Z
```
Run from: repo root
Prerequisites: none (reads from the working tree, no git tag required)
Notes: Writes `internal/templates/manifests/X.Y.Z.json`. Run for each new release version and commit the generated manifest.

- Validate release readiness (run before tagging)
```bash
make release-preflight RELEASE_TAG=vX.Y.Z
```
Run from: repo root
Prerequisites: `rg` (ripgrep) available on PATH, both manifests committed
Notes: Runs `test-release` (workflow consistency + release script tests) then validates upgrade-contract docs for the tag. Catches issues that would fail the release workflow.

- Validate upgrade-contract docs for a target release tag
```bash
make docs-upgrade-check RELEASE_TAG=vX.Y.Z
```
Run from: repo root
Prerequisites: `site/docs/upgrades.mdx` and `CHANGELOG.md` include the target release tag; `rg` (ripgrep) available on PATH
Notes: Also runs upgrade CTA syntax checks across core docs/message surfaces.

- Validate upgrade CTA syntax drift in core docs/messages
```bash
make docs-cta-check
```
Run from: repo root
Prerequisites: `rg` (ripgrep) available on PATH
Notes: Fails on removed/invalid upgrade command surfaces (for example `--force` or `upgrade plan --json`) and on `al upgrade --yes` guidance that omits required apply flags.

- Build release artifacts locally (cross-compile)
```bash
make release-dist AL_VERSION=dev DIST_DIR=dist
```
Run from: repo root
Prerequisites: Go 1.25.6+, git, gzip, tar, `sha256sum` or `shasum`
Notes: Runs `test-release` first to validate release scripts.
