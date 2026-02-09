# Changelog
All notable changes to this project will be documented in this file.

## v0.7.0 - 2026-02-07

### Added
- Upgrade contract published at `site/docs/upgrades.mdx`: defines upgrade event categories (`safe auto`, `needs review`, `breaking/manual`), sequential compatibility guarantees (`N-1` to `N`), release-versioned migration rules, and OS/shell capability matrix.
- Release gate validates upgrade documentation for each release tag (`make docs-upgrade-check`), ensuring migration table rows exist and placeholder text is replaced when changelog notes breaking changes.
- `al init --version latest` resolves the latest GitHub release to a semver pin before writing `.agent-layer/al.version`.
- `al init --version X.Y.Z` validates the release exists on GitHub before writing the pin file, failing with a clear "release not found" message instead of writing a pin that 404s on next use.
- `al init` auto-recovers from empty or corrupt `.agent-layer/al.version` pin files with a warning instead of blocking all commands.
- Binary download progress indicator: `ensureCachedBinary` emits "Downloading al vX.Y.Z..." / "Downloaded al vX.Y.Z" to stderr.
- Actionable error messages for binary download failures (404 not-found and timeout scenarios).

### Changed
- **Breaking:** Windows support removed. Deleted `al-install.ps1` installer, Windows release target, `open-vscode.bat` launcher, and all Windows-specific code paths in dispatch, cache, exec, and lock packages. Windows was never tested and best-effort support eroded trust. macOS and Linux remain fully supported.
- `al init` now bypasses repo-pin binary dispatch and always executes on the invoking CLI binary, preventing older pinned versions from running upgrade operations.
- Launcher template writes refactored for reliability with proper macOS path escaping.
- Codex MCP header projection order corrected.
- CI workflow caches pinned tools in GitHub Actions for faster builds.
- Upgrade contract linked from README, site docs, DEVELOPMENT.md, RELEASE.md, and docs/UPGRADE_PLAN.md.

### Removed
- `al-install.ps1` (Windows PowerShell installer).
- `open-vscode.bat` (Windows VS Code launcher).
- Windows release targets (`windows/amd64`) from build scripts.
- Windows-specific dispatch, cache, exec, and lock code paths.

## v0.6.1 - 2026-02-06

### Added
- CLI argument forwarding: `al <client>` now forwards extra arguments to the underlying client. Use `--` to separate Agent Layer flags from client arguments (e.g., `al claude -- --help` or `al vscode --no-sync -- --reuse-window`).
- VS Code launchers are now created during `al init` in addition to `al sync`, so launchers are available immediately after initialization.
- `.gitignore` managed block is now updated during both `al init` and `al sync` operations for consistency.

### Fixed
- `AL_SHIM_ACTIVE` environment variable no longer leaks into VS Code's integrated terminal when launching via `al vscode`. Previously, this caused subsequent `al` commands in the terminal to fail with "version dispatch already active" errors. (#46)
- Wizard now rewrites `config.toml` sections in the template-defined canonical order, preventing section ordering drift after multiple wizard runs.

### Changed
- Launcher code moved to `internal/launchers` package with exported `EnsureGitignore` for cross-package use.
- Documentation updated with clearer guidance on gitignore template format, wizard behavior, and troubleshooting MCP server startup on macOS.

## v0.6.0 - 2026-02-03

### Added
- Documentation website with comprehensive guides covering getting started, concepts (approvals, MCP servers, project memory, version pinning), reference (CLI, configuration, environment variables), and troubleshooting.
- Website publishing pipeline (`cmd/publish-site`) with automated deployment in the release workflow.
- Playwright MCP server template in default `config.toml` for browser automation workflows.
- Descriptive comments for all default MCP server templates explaining purpose and required credentials.
- Claude Code VS Code Extension added to supported clients table in README.

### Changed
- README rewritten with clearer value proposition, comparison table (manual vs Agent Layer), and improved quick start flow.
- Default MCP server examples in README now use generic `example-api` instead of GitHub-specific config for clarity.
- Documentation structure consolidated from nested pages to flat MDX files for better navigation.

## v0.5.8 - 2026-01-30

### Changed
- **Breaking:** Environment variables now require `AL_` prefix to avoid conflicts with shell environment (e.g., `GITHUB_PERSONAL_ACCESS_TOKEN` → `AL_GITHUB_PERSONAL_ACCESS_TOKEN`). This ensures Agent Layer variables don't override existing environment variables when VS Code terminals inherit the process environment.

### Fixed
- VS Code `open-vscode.app` launcher now uses `osascript` with a login shell (`zsh -l`) instead of hardcoded VS Code CLI paths, fixing launch failures when VS Code is installed via Homebrew, in `~/Applications`, or other non-standard locations. This also fixes MCP server failures where VS Code couldn't find `node` because Finder-launched apps have a minimal PATH.
- All VS Code launchers (`.app`, `.command`, `.bat`, `.desktop`) now delegate to `al vscode` for loading `.agent-layer/.env`, ensuring consistent parsing (KEY=VALUE data, not sourced) across platforms. Only `AL_*` variables with non-empty values are loaded, and existing environment variables take precedence—matching the documented behavior for `al` commands.
- VS Code `.app` launcher now shows a descriptive alert when the `code` command is not found, instead of silently failing.
- Linux `.desktop` launcher simplified to delegate to `.command` script for consistent behavior and maintainability.

## v0.5.7 - 2026-01-29

### Added
- Custom HTTP header support for Codex MCP servers: `bearer_token_env_var` for `Authorization: Bearer ${VAR}`, `env_http_headers` for other env-var-sourced headers, and `http_headers` for static literals.
- `X-MCP-Tools` header in default GitHub MCP server template for server-side tool filtering, reducing projected tool count.
- Detailed per-tool token breakdown in `al doctor` MCP schema bloat warnings, showing top contributors by token count.
- Documentation for MCP HTTP header projection across all supported clients (`docs/MCP_HEADERS_SUPPORT.md`).

### Changed
- Default MCP schema token thresholds increased to accommodate larger MCP servers (server: 7500→20000 tokens, total: 10000→30000 tokens).
- Doctor command now shows real-time discovery progress when checking MCP servers.
- Large internal modules (`install`, `dispatch`, `config`) split into smaller, focused files for maintainability.
- golangci-lint upgraded to v2.8.0 with additional linting rules enabled.

## v0.5.6 - 2026-01-27

### Added
- `http_transport` config option for HTTP MCP servers to specify transport mode (`streamable` or `sse`).
- Three new MCP server templates in default `config.toml`: `fetch` (mcp-server-fetch), `ripgrep` (mcp-ripgrep), and `filesystem` (server-filesystem with repo-scoped access).
- `${AL_REPO_ROOT}` built-in variable for resolving repository root path in MCP server args.
- VS Code settings sync now preserves existing user settings and comments using JSONC-aware block insertion instead of overwriting the entire file.
- Memory file templates (`BACKLOG.md`, `COMMANDS.md`, `DECISIONS.md`, `ISSUES.md`, `ROADMAP.md`) now include detailed formatting guidelines and entry templates.

### Changed
- MCP projection refactored: new `internal/projection/resolvers.go` module centralizes server resolution logic, used by both sync and warning checks.
- Update-available warning now includes full upgrade instructions for Homebrew, macOS/Linux shell script, and Windows PowerShell.
- Instruction templates consolidated and shortened to reduce token count while preserving key guidelines.
- Terminal detection moved to canonical `internal/terminal` package with `IsInteractive()` function.
- Default MCP server templates no longer specify `clients` filter (servers are projected to all clients by default).

### Fixed
- MCP server health checks now properly handle HTTP transport timeout scenarios.

## v0.5.5 - 2026-01-25

### Added
- New `03_tools.md` instruction template with comprehensive tool usage guidelines: time-sensitive information handling, Context7 documentation lookups, MCP tool constraints, approval workflows, and error handling.
- New `fix-tests` slash command runs repo-defined checks (lint/format/pre-commit/tests) in a loop, fixing failures until all checks pass or max iterations reached.

### Changed
- Temporary artifact location moved from `tmp/agent-layer/runs/` to `.agent-layer/tmp/runs/`, keeping all agent artifacts within `.agent-layer/`.
- Slash command artifact naming standardized across workflows: `.agent-layer/tmp/<workflow>.<run-id>.<type>.md` with `run-id = YYYYMMDD-HHMMSS-<short-rand>`. User path overrides removed for consistency.
- `finish-task` workflow now delegates to `fix-tests` when available before falling back to manual repo-defined commands.
- README updated with new artifact naming convention and VS Code reauthentication note for new `CODEX_HOME` environments.

## v0.5.4 - 2026-01-24

### Changed
- Memory file `FEATURES.md` renamed to `BACKLOG.md` to better reflect its purpose (unscheduled user-visible features and tasks vs deferred issues).
- `al init --overwrite` now detects and prompts to delete unknown files under `.agent-layer` that are not tracked by Agent Layer templates.
- `al init --force` now deletes unknown files under `.agent-layer` in addition to overwriting existing files without prompts.
- Memory instruction templates improved with clearer formatting rules and entry layouts.
- Slash command templates (`continue-roadmap.md`, `update-roadmap.md`) simplified and clarified.
- VS Code launcher paths centralized in `internal/launchers` package, consumed by sync and install to prevent drift.
- Sync package refactored with system abstraction layer for improved test isolation and reliability.

## v0.5.3 - 2026-01-24

### Changed
- User-facing strings consolidated into `internal/messages/` package for consistency and maintainability.
- Python release tools (`extract-checksum.py`, `update-formula.py`) replaced with Go implementations in `internal/tools/`.
- Release test script reorganized into modular components (`scripts/test-release/release_tests.sh`, `scripts/test-release/tool_tests.sh`).
- Slash command templates (`find-issues.md`, `finish-task.md`) simplified to reduce duplication with base instructions; formatting rules now delegate to individual memory file templates.

## v0.5.2 - 2026-01-24

### Added
- Automated Homebrew tap updates: release workflow now opens a PR against `conn-castle/homebrew-tap` to update the formula with the new tarball URL and SHA256.

## v0.5.1 - 2026-01-23

### Added
- Source tarball (`agent-layer-<version>.tar.gz`) published with releases for Homebrew formula support.

### Changed
- Release scripts now generate and verify the source tarball via `git archive` + `gzip -n`.
- Documentation cleanup: simplified release process, corrected `make dev` description.

## v0.5.0 - 2026-01-23

Major shift from repo-local binary to globally installed CLI with per-repo version pinning.

### Added
- Global CLI installation via Homebrew (`brew install conn-castle/tap/agent-layer`), shell script (macOS/Linux), or PowerShell (Windows).
- `al init` command initializes `.agent-layer/` and `docs/agent-layer/` in any repo.
- Per-repo version pinning via `.agent-layer/al.version`; global CLI dispatches to the pinned version automatically.
- Cached binary downloads with SHA-256 verification; cached binaries stored in `~/.cache/agent-layer/versions/`.
- Shell completion for bash, zsh, and fish (`al completion <shell>` with optional `--install` flag).
- Update checking: `al init` and `al doctor` warn when a newer release is available.
- Linux desktop entry launcher (`.agent-layer/open-vscode.desktop`).
- E2E test suite (`scripts/test-e2e.sh`) and release test script (`scripts/test-release.sh`).
- Environment variables: `AL_CACHE_DIR` (override cache location), `AL_VERSION` (force version), `AL_NO_NETWORK` (disable downloads).

### Changed
- **Breaking:** Repo-local `./al` executable replaced with globally installed `al` CLI.
- **Breaking:** `al install` renamed to `al init`.
- **Breaking:** Repository moved from `nicholasjconn/agent-layer` to `conn-castle/agent-layer`.
- Install script renamed from `agent-layer-install.sh` to `al-install.sh`.
- `al init --overwrite` now prompts before each overwrite; use `--force` to skip prompts.
- `al init --version <tag>` pins the repo to a specific release version.
- Commands run from any subdirectory now resolve the repo root automatically.
- `.agent-layer/.gitignore` added to ignore launchers, template copies, and backups.

### Removed
- Repo-local `./al` binary; global `al` dispatches to pinned versions as needed.
- `agent-layer-install.sh` (replaced by `al-install.sh`).

## v0.4.0 - 2026-01-21

### Added
- `al doctor` command reports missing secrets, disabled servers, and common misconfigurations.
- `al wizard` command provides interactive setup for approval modes, agent enablement, model selection, MCP servers, secrets, and warning thresholds.
- Configurable warning system with thresholds for instruction token count, MCP server/tool counts, and schema token sizes.
- Antigravity slash commands now generate skills in `.agent/skills/<command>/SKILL.md`.
- VS Code launchers: macOS `.app` bundle (no Terminal window), macOS `.command` script, and Windows `.bat` file, all with `CODEX_HOME` support.
- `al install --no-wizard` flag skips the post-install wizard prompt.
- Atomic file writes across all sync operations prevent partial file corruption.

### Changed
- `al install` now prompts to run the wizard after seeding files (interactive terminals only).
- Gitignore patterns use root-anchored paths (`/AGENTS.md` instead of `AGENTS.md`) for precision.
- Default Codex reasoning effort changed from `xhigh` to `high`.
- Codex config header now warns about potential secrets in generated files.
- Environment variable loading: process environment takes precedence; `.agent-layer/.env` fills missing keys only; empty values in `.env` are ignored.
- Improved instruction and slash-command templates.

### Fixed
- VS Code launcher now works correctly with proper error messages for missing `code` command.
- MCP configuration for Codex HTTP servers now handles bearer token environment variables correctly.

## v0.3.1 - 2026-01-19

### Added
- Installer failure output now includes clear, actionable error messages.

### Fixed
- Installer checksum verification now handles SHA256SUMS entries with "./" prefixes.

### Changed
- Quick start documentation no longer suggests manual install fallback when only `./al` is present.

## v0.3.0 - 2026-01-18

Complete rewrite in Go for simpler installation and fewer moving parts.

### Added
- Single repo-local Go binary (`./al`) replaces the Node.js codebase.
- `al install` command for repository initialization with template seeding.
- `al install --overwrite` flag to reset templates to defaults.
- `al sync` command to regenerate client configs without launching.
- Support for five clients: Gemini CLI, Claude Code CLI, VS Code/Copilot Chat, Codex CLI, and Antigravity.
- Unified `[[mcp.servers]]` configuration in `config.toml` for both HTTP and stdio transports.
- Approval modes (`all`, `mcp`, `commands`, `none`) with per-client projection.
- `${ENV_VAR}` substitution from `.agent-layer/.env` with client-specific placeholder syntax preservation.
- Internal MCP prompt server for slash command discovery (auto-wired into client configs).
- Golden-file tests for deterministic output validation.
- Managed `.gitignore` block with customizable template (`.agent-layer/gitignore.block`).

### Changed
- **Breaking:** Complete rewrite from Node.js to Go.
- **Breaking:** Configuration moved from `config/agents.json` to `.agent-layer/config.toml` (TOML format).
- **Breaking:** MCP servers now configured via `[[mcp.servers]]` arrays in `config.toml`.
- CLI simplified: `./al <client>` always syncs then launches.
- Instructions now in `.agent-layer/instructions/` (numbered markdown files, lexicographic order).
- Slash commands now in `.agent-layer/slash-commands/` (one markdown file per command).
- Approved commands now in `.agent-layer/commands.allow` (one prefix per line).
- Project memory standardized in `docs/agent-layer/` (ISSUES.md, FEATURES.md, ROADMAP.md, DECISIONS.md, COMMANDS.md).

### Removed
- Node.js codebase (`src/lib/*.mjs`, test files, `package.json`).
- `config/agents.json` and separate MCP server configuration files.
- Built-in Tavily MCP server (now configurable as external server in `config.toml`).

## v0.2.0 - 2026-01-17

Major architectural overhaul moving core logic from shell to Node.js.

### Added
- Per-agent opt-in configuration via `config/agents.json` with interactive setup prompt.
- HTTP transport support for MCP servers.
- Tavily MCP server for web search capabilities.
- `./al --version` flag with dirty suffix for non-tagged commits.
- User config preservation and backup during upgrades.

### Changed
- **Breaking:** CLI entrypoint is now `.agent-layer/agent-layer`; `./al` remains as the launcher wrapper in the parent root.
- Root resolution, environment loading, and cleanup moved from shell to Node.js (`src/lib/roots.mjs`, `src/lib/env.mjs`, `src/lib/cleanup.mjs`).
- Test framework migrated from Bats (shell) to Node.js native test runner.
- GitHub MCP server switched to hosted HTTP endpoint with PAT authentication.
- Architecture documentation updated to reflect new layer boundaries.

### Removed
- Shell scripts: `al`, `run.sh`, `setup.sh`, `clean.sh`, `check-updates.sh`, `open-vscode.command`.
- Shell-based root resolution: `src/lib/parent-root.sh`, `src/lib/temp-parent-root.sh`.

## v0.1.0 - 2026-01-12
Initial release.

### Added
- Installer for per-project setup that pins `.agent-layer/` to tagged releases, with upgrade, version, and dev-branch options.
- Repo-local `./al` launcher with sync and environment modes plus local update checks.
- Sync pipeline that generates client configs from `.agent-layer/config` sources.
- MCP prompt server that exposes workflows as prompts.
- Project memory templates and setup/bootstrap helpers.
