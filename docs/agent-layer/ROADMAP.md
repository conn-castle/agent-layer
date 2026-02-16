# Roadmap

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
A phased plan of work that guides architecture decisions and sequencing. The roadmap is the “what next” reference; the backlog holds unscheduled items.

## Format
- The roadmap is a single list of numbered phases under `<!-- PHASES START -->`.
- Do not renumber completed phases (phases marked with ✅).
- You may renumber incomplete phases when updating the roadmap (e.g., to insert a new phase).
- Incomplete phases include **Goal**, **Tasks** (checkbox list), and **Exit criteria** sections.
- When a phase is complete:
  - update the heading to: `## Phase N ✅ — <phase name>`
  - replace the phase content with a short bullet summary of what was accomplished (no checkbox list).

### Phase templates

Completed:
```markdown
## Phase N ✅ — <phase name>
- <Accomplishment summary bullet>
- <Accomplishment summary bullet>
```

Incomplete:
```markdown
## Phase N — <phase name>

### Goal
- <What success looks like for this phase, in 1–3 bullet points.>

### Tasks
- [ ] <Concrete deliverable-oriented task>
- [ ] <Concrete deliverable-oriented task>

### Exit criteria
- <Objective condition that must be true to call the phase complete.>
- <Prefer testable statements: “X exists”, “Y passes”, “Z is documented”.>
```

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
- Added `al init --overwrite` flag and warnings for existing files that differ from templates (later superseded by `al upgrade`).
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
- Added per-file overwrite prompts during `al init --overwrite` with `--force` flag to skip prompts (later superseded by `al upgrade`).

## Phase 8 ✅ — v0.5.4 Workflows and instructions
- Added tool instructions guiding models to use search or Context7 for time-sensitive information.
- Implemented `fix-tests` workflow for iterative lint/pre-commit/test fixing until passing.
- Updated `finish-task` and `cleanup-code` workflows to ensure commit-ready state via `fix-tests`.
- Made `find-issues` and `fix-issues` outputs concurrency-safe with temp-directory report paths.
- Renamed `FEATURES.md` to `BACKLOG.md` and updated all references.
- Enforced single blank line between entries in all memory files.
- Documented VS Code reauthentication requirement for new `CODEX_HOME` in README.

## Phase 9 ✅ — MCP defaults + CLI output polish
- Added default MCP entries for Ripgrep, Fetch, and Filesystem (with path restriction) to config templates.
- Enhanced CLI output readability with semantic coloring and distinct success/warning/error formatting.
- Updated upgrade warnings to include concrete commands and safety notes about overwrites.
- Resolved documentation/instruction issues regarding search fallback, uncommitted changes, and decision hygiene.

## Phase 10 ✅ — Upgrade Phase 0 stabilization (immediate)
- Removed unsupported Windows upgrade surface from installers, release targets, launchers, and docs.
- Implemented and hardened pin management (`--version latest` resolution, explicit version validation, init/upgrade dispatch bypass, corrupt pin recovery, and improved download errors/progress).
- Published the canonical upgrade contract in `site/docs/upgrades.mdx` with event categories, sequential compatibility guarantees (`N-1` to `N`), release-versioned migration rules, and macOS/Linux shell capability matrix.
- Linked the upgrade contract from user and contributor docs (`README.md`, site docs, `docs/DEVELOPMENT.md`, `docs/RELEASE.md`, and `docs/UPGRADE_PLAN.md`).

## Phase 11 ✅ — Upgrade lifecycle (explainability, safety, and migration engine)
- Delivered explainable, plain-language `al upgrade plan` output with line-level diffs, ownership labeling, and readiness checks.
- Completed upgrade safety/reversibility with automatic snapshots, `al upgrade rollback`, granular apply flags, and CI-safe managed-only apply mode.
- Implemented release migration manifests with idempotent execution and deterministic migration reporting, including hard removals for breaking upgrade surfaces.
- Enforced `.agent-layer/.env` namespace policy (`AL_`-only) and removed the legacy install `Force` path to keep one explicit apply/prompt flow.
- Superseded Backlog 2026-02-03 `b4c5d6e` in this roadmap phase: embedded templates remain the sole supported template source for deterministic upgrades in this release line.

## Phase 12 — Documentation and website improvements

### Goal
- VS Code launch architecture is documented for contributors.
- Website documentation is searchable from a global header search bar.

### Tasks
- [x] vsc-launch (Priority: High, Area: documentation / launchers): Produced architecture documentation in `docs/architecture/vscode-launch.md`, linked from contributor docs.
- [ ] websearch (Priority: Medium, Area: website / documentation): Add a global website search bar for docs/pages discovery (client-side index or service-backed), with relevant results integrated into the site header UX.

### Task details
- vsc-launch
  Description: Document how VS Code is launched by the CLI, especially the unique/odd path that is currently undocumented.
  Acceptance criteria: Docs are added or updated (for example `docs/DEVELOPMENT.md` or a new architecture doc) and clearly explain launch flow/design choices.
  Notes: User identified this as the least documented and most non-obvious part of the system.
- websearch
  Description: Add global docs search on the website to improve navigation and discovery of guides/features/reference content.
  Acceptance criteria: Search bar is integrated in site header and returns relevant results from documentation pages.
  Notes: Consider client-side indexing (for example FlexSearch) versus managed services (for example Algolia).

### Exit criteria
- VS Code launch architecture documentation exists and is linked from contributor-facing docs.
- Website search is available in the header and returns relevant documentation results.
