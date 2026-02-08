# Development

This repo is built around determinism: the same inputs should produce the same client outputs locally and in CI. These steps keep tooling pinned, workflows repeatable, and changes reviewable so you can trust what ships.

## Prerequisites
- Go 1.25.6+
- Git
- Make (via Xcode Command Line Tools on macOS)
- Recommended: `pre-commit` for local hooks

## macOS quick start (fresh machine)
1. Install Xcode Command Line Tools (includes Git):
   ```bash
   xcode-select --install
   ```
2. Install Go 1.25.6+ (from https://go.dev/dl/) and confirm it works:
   ```bash
   go version
   ```
3. Install `pre-commit` (recommended) and confirm it works:
   ```bash
   pre-commit --version
   ```

## One-time setup (per clone)
```bash
./scripts/setup.sh
```

## Daily workflow
- Use the commands in `docs/agent-layer/COMMANDS.md` for format, lint, test, coverage, and release builds.
- Prefer `make` targets (see `docs/agent-layer/COMMANDS.md`) instead of running `goimports` / `golangci-lint` directly; tools are installed repo-locally under `.tools/bin` so you do not need to edit your shell PATH. This avoids “works on my machine” drift and keeps local output aligned with CI.
- Use `make dev` for a quick local pass (format + fmt-check + lint + coverage + release tests). Run `./scripts/setup.sh` or `make tools` first.
- If you change installer templates (anything under `internal/templates/`), re-run `go run ./cmd/al init` in a target repo to re-seed files. See the README Quick start for overwrite/force flag guidance.
- If you change upgrade behavior or upgrade-facing guidance, update the canonical upgrade contract page at `site/docs/upgrades.mdx` and keep release notes/docs links aligned.

## Go Tooling & Environment
Agent Layer uses several light shell wrappers and `make` targets around standard Go commands (`go fmt`, `go test`, etc.). This is intentional to ensure consistent behavior across local development and CI (GitHub Actions). It also keeps tool versions explicit, which makes regressions easier to trace.

### Common Issues
- **`go mod tidy` or `go run` fails with network errors**: If your environment restricts access to `proxy.golang.org`, you can try setting `GOPROXY=direct` or ensure you have a working internet connection.
- **Permission errors on Go cache**: If you see errors related to `GOCACHE` or `GOMODCACHE`, you can override them to a local directory:
  ```bash
  export GOCACHE=$PWD/tmp/gocache
  export GOMODCACHE=$PWD/tmp/gomodcache
  go mod tidy
  ```
- **Tools not found**: If `make lint` fails because `golangci-lint` is missing, run `make tools` to install all pinned dependencies into `.tools/bin`.

## Run the CLI locally (always uses latest changes)
Run from source (`go run`):
```bash
# One-time init for a fresh repo (creates .agent-layer/ and docs/agent-layer/)
go run ./cmd/al init

# Generate outputs (optional; client commands already sync on run)
go run ./cmd/al sync

# Launch a client (always runs sync first)
go run ./cmd/al gemini
```

Optional: build a local dev binary from the current source (no global install):
```bash
go build -o ./tmp/al ./cmd/al
./tmp/al init
./tmp/al gemini
```

### Run against a scratch repo (recommended for install/sync testing)
```bash
mkdir -p tmp/dev-repo
cd tmp/dev-repo
go run ../../cmd/al init
go run ../../cmd/al gemini
```

Notes:
- `init` is required once per repo to seed `.agent-layer/` and `docs/agent-layer/`.
- `init` prompts to run the setup wizard by default; pass `--no-wizard` to skip (non-interactive shells skip automatically).
- `sync` is optional because `al <client>` always syncs before launch.
- `./scripts/setup.sh` is only for tool + hook setup, not required just to run the CLI.

## Run checks locally
```bash
# Quick pass (format + fmt-check + lint + coverage)
make dev

# Targeted checks (optional)
make test
make lint
make coverage
```
Notes:
- `make dev` includes coverage; use `make test` when you want a faster loop without the coverage gate.
- `make test` uses `gotestsum` for more readable output (installed via `make tools`).
- `make lint` and `make test` fail fast if tools are missing; run `make tools` once per clone.

## Full local verification (CI-equivalent)
```bash
make ci
```
Note: `make ci` includes `make tidy-check`, which fails if `go.mod` or `go.sum` would change. While you are actively editing dependencies, use `make test`, `make lint`, and `make coverage` instead. `make ci` expects tools to be installed via `make tools`.

## Troubleshooting
- If you see `golangci-lint: command not found` or `goimports: command not found`, run:
  ```bash
  make tools
  ```

- If `pre-commit install` fails with:
  - `[ERROR] Cowardly refusing to install hooks with core.hooksPath set.`

  Unset it for this repo and retry:
  ```bash
  git config --show-origin --get core.hooksPath
  git config --unset-all core.hooksPath
  # If it was set globally instead:
  # git config --global --unset-all core.hooksPath
  ./scripts/setup.sh
  ```

- If `go mod download` fails due to proxy access, ensure your network can reach `proxy.golang.org` or set `GOPROXY=direct` temporarily.
- If Go reports a cache permission error, the scripts use a repo-local cache by default. To set it manually:
  ```bash
  GOCACHE=.cache/go-build GOMODCACHE=.cache/go-mod go mod tidy
  ```
