# Changelog
All notable changes to this project will be documented in this file.

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
