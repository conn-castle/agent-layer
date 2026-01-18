# Development

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
- Prefer `make` targets (see `docs/agent-layer/COMMANDS.md`) instead of running `goimports` / `golangci-lint` directly; tools are installed repo-locally under `.tools/bin` so you do not need to edit your shell PATH.
- Use `make dev` for a quick local pass (format + fmt-check + lint + test). Run `./scripts/setup.sh` or `make tools` first.
- If you change installer templates (anything under `internal/templates/`), re-run `./al install` in a target repo to re-seed files. Use `./al install --overwrite` to reset template-managed files.

## Run the CLI locally (always uses latest changes)
There are two paths: run from source (`go run`) or build a local `./al` binary.

### Option A: run from source (no local binary)
```bash
# One-time init for a fresh repo (creates .agent-layer/ and docs/agent-layer/)
go run ./cmd/al install

# Generate outputs (optional; client commands already sync on run)
go run ./cmd/al sync

# Launch a client (always runs sync first)
go run ./cmd/al gemini
```

### Option B: build a local ./al binary
```bash
go build -o ./al ./cmd/al
./al install
./al gemini
```

### Run against a scratch repo (recommended for install/sync testing)
```bash
mkdir -p tmp/dev-repo
cd tmp/dev-repo
go run ../../cmd/al install
go run ../../cmd/al gemini
```

Notes:
- `install` is required once per repo to seed `.agent-layer/` and `docs/agent-layer/`.
- `sync` is optional because `./al <client>` always syncs before launch.
- `./scripts/setup.sh` is only for tool + hook setup, not required just to run the CLI.

## Run checks locally
```bash
# Quick pass (format + fmt-check + lint + test)
make dev

# Targeted checks (optional)
make test
make lint
make coverage
```
Notes:
- `make dev` does not run coverage; use `make coverage` when you need the gate locally.
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
