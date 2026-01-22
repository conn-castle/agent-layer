# Roadmap

Purpose: Phased plan of work; guides architecture decisions and sequencing.

Update rules:
- The roadmap is a single list of numbered phases.
- Do not renumber completed phases (phases marked with ✅).
- You may renumber incomplete phases when updating the roadmap (for example, to insert a new phase).
- Incomplete phases include Goal, Tasks (checkbox list), and Exit criteria.
- When a phase is complete:
  - Add a green check emoji to the phase heading: `## Phase N ✅ — <name>`
  - Replace the phase content with a single bullet list summarizing what was accomplished (no checkbox list).
- There is no separate "current" or "upcoming" section. The phase list itself shows what is done vs not done.


Phase template (completed):
## Phase N ✅ — <phase name>
- <Accomplishment summary bullet>
- <Accomplishment summary bullet>


Phase template (incomplete):
## Phase N — <phase name>

### Goal
- <What success looks like for this phase, in 1 to 3 bullet points.>

### Tasks
- [ ] <Concrete deliverable-oriented task>
- [ ] <Concrete deliverable-oriented task>

### Exit criteria
- <Objective condition that must be true to call the phase complete.>
- <Prefer testable statements: “X exists”, “Y passes”, “Z is documented”.>


## Phases

<!-- PHASES START -->

## Phase 1 ✅ — Define the vNext contract (docs-first)
- Defined the vNext product contract: repo-local `./al`, config-only `.agent-layer/`, required `docs/agent-layer/` memory, always-sync-on-run.
- Created simplified `README.md`, `DECISIONS.md`, and `ROADMAP.md` as the foundation for the Go rewrite.
- Moved project memory into `docs/agent-layer/` and templated it for installer seeding.

## Phase 2 ✅ — Repository installer + skeleton (single command install)
- Implemented repo initialization (`al install`), gitignore management, and template seeding.
- Added release workflow + installer script for repo-local binary installation.

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
- Added `al install --overwrite` flag and warnings for existing files that differ from templates.
- Fixed `go run ./cmd/al <client>` to locate the binary correctly for the internal MCP prompt server.
- Updated default `gitignore.block` to make `.agent-layer/` optional with customization guidance.
- Release workflow now auto-extracts release notes from `CHANGELOG.md`.

## Phase 6 ✅ — v0.4.0 CLI polish and sync warnings
- Implemented `al doctor` for missing secrets, disabled servers, and common misconfigurations.
- Implemented `al wizard` for agent enablement, model selection, and Codex reasoning.
- Added macOS VS Code launchers (`.app` bundle and `.command` script with `CODEX_HOME` support).
- Added Windows VS Code launcher (`.bat` script with `CODEX_HOME` support).
- Added configurable sync warnings for oversized instructions (token count threshold) and excessive MCP servers (per-client server count threshold).

## Phase 7 — v0.5.0 CLI completion and install improvements

### Goal
- Complete CLI experience with shell completion and improved install flow.

### Tasks
- [ ] Implement `al completion bash|zsh|fish|powershell`.
- [ ] Add Linux VS Code launcher (desktop entry with `CODEX_HOME` support).
- [ ] Add commented-out gitignore entry for `docs/agent-layer/` with explanatory note.
- [ ] Add per-file overwrite prompts during `al install --overwrite`; add `--force` flag to skip prompts. Update user facing language to make this clear.

### Exit criteria
- Shell completion works for all major shells.
- Linux launcher ships with `CODEX_HOME` support.
- Install flow offers an optional `docs/agent-layer/` gitignore entry.
- Install overwrite prompts allow per-file decisions or force override.

## Phase 8 — v0.6.0 Workflows and instructions

### Goal
- Improve agent effectiveness through better workflows and instruction quality.

### Tasks
- [ ] Add tool instruction file that guides models to use search or Context7 for time-sensitive information.
- [ ] Implement `fix-tests` workflow that runs all checks (lint, pre-commit, tests) and iteratively fixes failures until passing.
- [ ] Update finish-task and cleanup-code to ensure commit-ready state (tests pass, lint passes, precommit hooks pass).
- [ ] Remove the quality audit report file from `find-issues` outputs and switch to a report path that supports concurrent agents.
- [ ] Move `fix-issues` plans into `tmp`, add a "what the human needs to know" section, and relax approval keyword requirements.
- [ ] Rename `FEATURES.md` to a backlog name and update references in docs and prompts.
- [ ] Enforce a single blank line between entries in all memory files.
- [ ] Improve `.agent-layer/config.toml` usability (comments, structure, and editing aids).

### Exit criteria
- Workflows reliably produce commit-ready code.
- `find-issues` and `fix-issues` outputs are concurrency-safe and documented.

## Phase 9 — v0.7.0 Advanced automation

### Goal
- Enable sophisticated automation and integration patterns.

### Tasks
- [ ] Provide opt-in guidance for reading gitignored files in VS Code, Claude Code, and Gemini CLI.
- [ ] Enable safe auto-approval for slash-command workflows invoked through the workflow system.
- [ ] Auto-merge client-side approvals or MCP server edits back into agent-layer sources.
- [ ] Identify, document, or integrate an MCP server for SQL databases.
- [ ] Add interaction monitoring to agent system instructions to self-improve all prompts, rules, and workflows.


### Exit criteria
- Workflows can run with minimal human intervention where safe.
- Agent-layer sources stay in sync with client-side changes.
- An MCP server for SQL databases is documented or integrated.
- Instruction quality improves through monitoring feedback.

## Phase 10 — Deep future exploration

### Goal
- Explore longer-term ideas without blocking core delivery.

### Tasks
- [ ] Add a queueing system to chain tasks without interrupting the current task.
- [ ] Add a simple flowchart or rules-based guide for slash-command ordering.
- [ ] Add bashcov and c8 coverage tooling, and restore coverage for Node and shell scripts.
- [ ] Decide whether to prefer a code-workspace file over settings.json, and where that file should live.
- [ ] Build a Ralph Wiggum-like tool where different agents can chat with each other.
- [ ] Build a unified documentation repository with Model Context Protocol tool access for shared notes.
- [ ] Add indexed chat history in the unified documentation repository for searchable context.
- [ ] Persist conversation history in model-specific local folders (e.g., `.agent-layer/gemini/`, `.agent-layer/openai/`).
- [ ] Implement "full access" mode for all agents with security warnings (similar to Codex full-auto).

### Exit criteria
- Long-term initiatives are scoped and ready for selection in a future roadmap cycle.
