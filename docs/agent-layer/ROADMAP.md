# Roadmap

Note: This is an agent-layer memory file. It is primarily for agent use.

## Phases

<!-- PHASES START -->

## Phase 1 ✅ — Define the vNext contract (docs-first)
- Defined the vNext product contract: repo-local CLI (later superseded by global CLI), config-first `.agent-layer/` with repo-local launchers, required `docs/agent-layer/` memory, always-sync-on-run.
- Created simplified `README.md`, `DECISIONS.md`, and `ROADMAP.md` as the foundation for the Go rewrite.
- Moved project memory into `docs/agent-layer/` and templated it for installer seeding.

## Phase 2 ✅ — Repository installer + skeleton (single command install)
- Implemented repo initialization (`al init`), gitignore management, and template seeding.
- Added release workflow + installer script for repo-local CLI installation (later superseded by global installers).

## Phase 3 ✅ — Core sync engine (parity with current generators)
- Implemented config parsing/validation, instruction + workflow parsing, and deterministic generators for all clients.
- Wired the internal MCP prompt server into Gemini/Claude configs and added golden-file tests.

## Phase 4 ✅ — Agent launchers (Gemini/Claude/Codex/VS Code/Antigravity)
- Added shared launch pipeline and client launchers with per-agent model/effort wiring.
- Ensured Antigravity runs with generated instructions and slash commands only.

## Phase 5 ✅ — v0.3.0 minimum viable product (first Go release)
- Implemented `[[mcp.servers]]` projection for HTTP and stdio transports with environment variable wiring.
- Added `${ENV_VAR}` substitution from `.agent-layer/.env` with client-specific placeholder syntax preservation.
- Implemented approval modes (`all`, `mcp`, `commands`, `none`) with per-client projections.
- Added `al init --overwrite` flag and warnings for existing files that differ from templates.
- Fixed `go run ./cmd/al <client>` to locate the binary correctly for the internal MCP prompt server.
- Updated default `gitignore.block` to make `.agent-layer/` optional with customization guidance.
- Release workflow now auto-extracts release notes from `CHANGELOG.md`.

## Phase 6 ✅ — v0.4.0 CLI polish and sync warnings
- Implemented `al doctor` for missing secrets, disabled servers, and common misconfigurations.
- Implemented `al wizard` for agent enablement, model selection, and Codex reasoning.
- Added macOS VS Code launchers (`.app` bundle and `.command` script with `CODEX_HOME` support).
- Added Windows VS Code launcher (`.bat` script with `CODEX_HOME` support).
- Added configurable sync warnings for oversized instructions (token count threshold) and excessive MCP servers (per-client server count threshold).

## Phase 7 ✅ — v0.5.0 Global CLI and install improvements
- Transitioned from repo-local binary to globally installed `al` CLI with per-repo version pinning via `.agent-layer/al.version`.
- Published Homebrew tap (`conn-castle/tap/agent-layer`) with automated formula updates on release.
- Added shell completion for bash, zsh, and fish (`al completion <shell>`).
- Added manual installers (`al-install.sh`, `al-install.ps1`) with SHA-256 checksum verification.
- Added Linux VS Code launcher (desktop entry with `CODEX_HOME` support).
- Added per-file overwrite prompts during `al init --overwrite` with `--force` flag to skip prompts.

## Phase 8 ✅ — v0.5.4 Workflows and instructions
- Added tool instructions guiding models to use search or Context7 for time-sensitive information.
- Implemented `fix-tests` workflow for iterative lint/pre-commit/test fixing until passing.
- Updated `finish-task` and `cleanup-code` workflows to ensure commit-ready state via `fix-tests`.
- Made `find-issues` and `fix-issues` outputs concurrency-safe with temp-directory report paths.
- Renamed `FEATURES.md` to `BACKLOG.md` and updated all references.
- Enforced single blank line between entries in all memory files.
- Documented VS Code reauthentication requirement for new `CODEX_HOME` in README.

## Phase 9 — MCP defaults + CLI output polish

### Goal
- Ship a higher-value default MCP catalog and clearer update guidance.
- Improve CLI output readability for the most common commands.

### Tasks
- [x] Add default MCP entry for the Ripgrep server (fast regex search) in `config.toml` template (commented or enabled).
- [x] Add default MCP entry for the official Fetch server in `config.toml` template (commented or enabled).
- [x] Add default MCP entry for the filesystem server with repo path restriction via CLI args; verify the correct restriction argument before shipping.
- [x] Update the “update available” warning to include concrete upgrade commands (brew/curl/PowerShell as applicable) and explicit safety notes about backups and `--force` overwrite risks.
- [x] Improve CLI output readability (wizard/init/doctor) with semantic coloring and spacing; ensure errors/warnings/successes are visually distinct.
- [x] Address Issue 2026-01-25 f1e2d3: documentation-first search fallback strategy for agents.
- [x] Address Issue 2026-01-25 c4d5e6: Codex ignores unexpected repository changes instruction.
- [x] Address Issue 2026-01-25 d7e8f9: decision consolidation in documentation workflows.

### Exit criteria
- Default MCP server entries exist in templates and are documented (Ripgrep, Fetch, filesystem with path restriction).
- Update warning includes concrete upgrade commands and explicit safety notes.
- CLI output uses consistent, readable formatting and semantic emphasis across core commands.
- Issue items above are resolved or explicitly deferred with rationale.
