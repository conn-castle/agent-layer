# Changelog
All notable changes to this project will be documented in this file.

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
