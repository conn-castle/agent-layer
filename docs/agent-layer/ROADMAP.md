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

## Phase 9 ✅ — MCP defaults + CLI output polish
- Added default MCP entries for Ripgrep, Fetch, and Filesystem (with path restriction) to config templates.
- Enhanced CLI output readability with semantic coloring and distinct success/warning/error formatting.
- Updated upgrade warnings to include concrete commands and safety notes about overwrites.
- Resolved documentation/instruction issues regarding search fallback, uncommitted changes, and decision hygiene.

## Phase 10 — Upgrade lifecycle and notification clarity

### Goal
- Make upgrade behavior predictable when template files are renamed, deleted, or sourced from non-default template repositories.
- Make upgrade notifications and overwrite prompts explain exactly what changed and what action the user should take.
- Reduce upgrade risk by separating user customizations from template-version changes in surfaced diffs.
- Improve upgrade-related discoverability by documenting VS Code launcher architecture and adding website search.

### Tasks
- [ ] Backlog 2026-01-25 8b9c2d1 (Priority: High, Area: lifecycle management): Define and implement migration handling for renamed/deleted template files so obsolete managed files are safely detected and handled during upgrades; document the design and behavior.
- [ ] Issue 2026-01-26 j4k5l6 (Priority: Medium, Area: install / UX): Add categorized upgrade diff/notification UX for `al init --overwrite` and related flows so users can distinguish intentional local customizations from upstream template updates.
- [ ] Backlog 2026-02-03 b4c5d6e (Priority: Medium, Area: lifecycle management): Add support for custom Git repositories as template sources with explicit pinning, caching, and deterministic upgrade behavior.
- [ ] Backlog 2026-02-06 vsc-launch (Priority: High, Area: documentation / launchers): Produce detailed architecture documentation for the VS Code launch mechanism, including non-obvious launch flow and design decisions.
- [ ] Backlog 2026-02-06 websearch (Priority: Medium, Area: website / documentation): Add a global website search bar for docs/pages discovery (client-side index or service-backed), with relevant results integrated into the site header UX.

### Task details
- Backlog 2026-01-25 8b9c2d1
  Description: Define how to handle renamed/deleted template files so stale orphans are not left behind in user repos.
  Acceptance criteria: A clear migration design/decision is documented and implemented for detecting/cleaning obsolete managed files during upgrades.
  Notes: Current behavior adds/updates files but does not remove files that vanished from templates.
- Backlog 2026-02-03 b4c5d6e
  Description: Allow teams to specify a custom Git repository as template source during `al init`.
  Acceptance criteria: `al init` (or a dedicated command) accepts a Git URL and instantiates templates correctly with repeatable upgrade behavior.
  Notes: Requires secure fetch + cache strategy and compatibility with pinning/update notifications.
- Backlog 2026-02-06 vsc-launch
  Description: Document how VS Code is launched by the CLI, especially the unique/odd path that is currently undocumented.
  Acceptance criteria: Docs are added or updated (for example `docs/DEVELOPMENT.md` or a new architecture doc) and clearly explain launch flow/design choices.
  Notes: User identified this as the least documented and most non-obvious part of the system.
- Backlog 2026-02-06 websearch
  Description: Add global docs search on the website to improve navigation and discovery of guides/features/reference content.
  Acceptance criteria: Search bar is integrated in site header and returns relevant results from documentation pages.
  Notes: Consider client-side indexing (for example FlexSearch) versus managed services (for example Algolia).

### Exit criteria
- Upgrade flows detect and handle renamed/deleted managed templates without leaving ambiguous orphaned files.
- Upgrade notifications present actionable, categorized change information for overwrite decisions.
- Custom template repository sourcing is documented and validated with deterministic, repeatable upgrade behavior.
- VS Code launch architecture documentation exists and is linked from contributor-facing docs.
- Website search is available in the header and returns relevant documentation results.
