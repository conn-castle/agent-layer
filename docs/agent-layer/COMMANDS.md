# Commands

Purpose: Canonical, repeatable commands for this repository (tests, coverage, lint/format, typecheck, build, run, migrations, scripts).

Notes for updates:
- Keep entries concise and practical.
- Prefer commands that will be used repeatedly.
- Organize commands using headings that fit the repository. Create headings as needed.
- For each command, document purpose, command, where to run, and prerequisites.
- When commands change, update this file and remove stale entries.
- If the repository is a monorepo, group commands per workspace/package/service and specify the working directory.

Entry format:
- <Short purpose>
```bash
<command>
```
Run from: <repo root or path>  
Prerequisites: <only if critical>  
Notes: <optional constraints or tips>

## Commands

<!-- ENTRIES START -->

### Setup

- Setup a fresh clone (installs pinned tools + pre-commit hooks)
```bash
./scripts/setup.sh
```
Run from: repo root  
Prerequisites: Go 1.25.6+, Make  
Notes: Uses versions pinned in `go.mod`. Installs tools into `.tools/bin`.

- Install pinned Go tooling (goimports, golangci-lint, gotestsum) only
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

### Test

- Run all tests
```bash
make test
```
Run from: repo root
Prerequisites: `make tools` has been run  
Notes: Uses `gotestsum` for nicer output.

- Run end-to-end build/install smoke tests
```bash
make test-e2e
```
Run from: repo root  
Prerequisites: Go 1.25.6+, `curl`, `sha256sum` or `shasum`  
Notes: Builds release artifacts and exercises `al-install.sh` against a local dist. Set `AL_E2E_VERSION` to override the test version.

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

### Dev

- Fast local checks (format + fmt-check + lint + coverage + release tests)
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
Notes: Includes `make tidy-check`, `make test-release`, and `make test-e2e`; requires a clean working tree.

### Release

- Build release artifacts locally (cross-compile)
```bash
make release-dist AL_VERSION=dev DIST_DIR=dist
```
Run from: repo root
Prerequisites: Go 1.25.6+, git, gzip, tar, `sha256sum` or `shasum`
Notes: Runs `test-release` first to validate release scripts.
