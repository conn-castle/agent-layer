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

## Phase 8 — v0.5.4 Workflows and instructions

### Goal
- Improve agent effectiveness through better workflows and instruction quality.

### Tasks
- [x] Add tool instruction file that guides models to use search or Context7 for time-sensitive information.
- [x] Implement `fix-tests` workflow that runs all checks (lint, pre-commit, tests) and iteratively fixes failures until passing.
- [x] Update finish-task and cleanup-code to ensure commit-ready state (tests pass, lint passes, precommit hooks pass). Ideally, when completed, they would just call `fix-tests` workflow.
- [x] Remove the quality audit report file from `find-issues` outputs and switch to a report path that supports concurrent agents.
- [x] Move `fix-issues` plans into `tmp`, add a "what the human needs to know" section, and relax approval keyword requirements.
- [x] Rename `FEATURES.md` to a backlog name and update references in docs and prompts.
- [x] Enforce a single blank line between entries in all memory files.
- [x] Add to README that when opening VS Code with a new `CODEX_HOME`, reauthentication is required and expected.

### Exit criteria
- Workflows reliably produce commit-ready code.
- `find-issues` and `fix-issues` outputs are concurrency-safe and documented.

## Phase 9 — Backlog Exploration

### Goal
- Explore longer-term ideas without blocking core delivery.

### Tasks
- [ ] Split this phase into smaller, more manageable sub-phases for focused exploration.
- [ ] Add interaction monitoring to agent system instructions to self-improve all prompts, rules, and workflows.
- [ ] Enable safe auto-approval for slash-command workflows invoked through the workflow system.
- [ ] Auto-merge client-side approvals or MCP server edits back into agent-layer sources.
- [ ] Add a queueing system to chain tasks without interrupting the current task.
- [ ] Add a simple flowchart or rules-based guide for slash-command ordering.
- [ ] Build a Ralph Wiggum-like tool where different agents can chat with each other.
- [ ] Build a unified documentation repository with Model Context Protocol tool access for shared notes.
- [ ] Add indexed chat history in the unified documentation repository for searchable context.
- [ ] Persist conversation history in model-specific local folders (e.g., `.agent-layer/gemini/`, `.agent-layer/openai/`).
- [ ] Implement "full access" mode for all agents with security warnings (similar to Codex full-auto).

### Exit criteria
- Long-term initiatives are scoped and ready for selection in a future roadmap cycle.
- Workflows can run with minimal human intervention where safe.
- Agent-layer sources stay in sync with client-side changes.
- Instruction quality improves through monitoring feedback.
