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

## Phase 6 — Post-v0.3.0 experience improvements

### Goal
- Improve configuration and workflow usability without expanding the core contract.

### Tasks
- [x] Implement `al doctor` to report missing secrets, disabled servers, and common misconfigurations.
- [x] Implement `al wizard` for agent enablement + model selection + Codex reasoning.
- [ ] Implement `al completion bash|zsh|fish|powershell`.
- [ ] Improve `.agent-layer/config.toml` usability (comments, structure, and editing aids).
- [ ] Add interaction monitoring to agent system instructions to self-improve all prompts, rules, and workflows. This should be add as an explicit ask in the finish task workflow.
- [ ] Rename `FEATURES.md` to a backlog name and update references in docs and prompts.
- [ ] Enforce a single blank line between entries in all memory files.
- [ ] Remove the quality audit report file from `find-issues` outputs and switch to a report path that supports concurrent agents.
- [ ] Move `fix-issues` plans into `tmp`, add a “what the human needs to know” section, and relax approval keyword requirements.
- [ ] Provide opt-in guidance for reading gitignored files in VS Code, Claude Code, and Gemini CLI.
- [ ] Enable safe auto-approval for slash-command workflows invoked through the workflow system.
- [ ] Auto-merge client-side approvals or MCP server edits back into agent-layer sources.
- [x] Add macOS VS Code launchers (`.app` bundle and `.command` script with `CODEX_HOME` support).
- [x] Add Windows VS Code launcher (`.bat` script with `CODEX_HOME` support).
- [ ] Add Linux VS Code launcher (desktop entry with `CODEX_HOME` support).

### Exit criteria
- Configuration and workflow ergonomics improve without changing the core contract.

## Phase 7 — Migration + compatibility

### Goal
- Existing users can adopt v2 without rebuilding their mental model.

### Tasks
- [ ] Provide a one-time migration command to translate v1 config layout into v2 layout (where possible).
- [ ] Document “v1 → v2” mapping and breaking changes.
- [ ] Ensure install/upgrade path is stable and reversible.

### Exit criteria
- A v1 repo can migrate to v2 with minimal manual edits and maintain equivalent behavior.

## Phase 8 — Deep future exploration

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

### Exit criteria
- Long-term initiatives are scoped and ready for selection in a future roadmap cycle.
