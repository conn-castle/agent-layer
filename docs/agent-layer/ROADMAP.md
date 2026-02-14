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

## Phase 11 — Upgrade lifecycle (explainability, safety, and migration engine)

Covers Upgrade Plan Phases 1–3. Depends on Phase 10 (Upgrade Plan Phase 0).

### Goal
- Users can preview upgrade changes before any file is written, using plain-language summaries plus line-level diffs.
- Upgrades are reversible via automatic snapshots, and destructive operations require explicit, granular opt-in.
- Each release ships a migration manifest that handles file renames, deletions, and config transitions idempotently.
- Upgrade planning remains explainable in human-readable output while keeping internal diagnostics out of the default UX path.

### Tasks

**Explainability (Upgrade Plan Phase 1)**
- [x] Implement `al upgrade plan` dry-run command showing categorized changes: template additions, updates, renames, removals/orphans, config key migrations, and pin version changes (current → target).
- [x] Issue 2026-01-26 j4k5l6 (Priority: Medium, Area: install / UX): Add ownership labels per diff in upgrade and overwrite flows (`upstream template delta`, `local customization`, plus richer ownership states), while keeping ownership diagnostics out of default upgrade-plan text output.
- [x] Keep `al upgrade plan --json` as compatibility-only output during cleanup transition; hide it from help and mark it deprecated for eventual removal.
- [x] Validate GitHub issue #30 (j4k5l6: managed file diff visibility) closure criteria; close only after line-level diff visibility is shipped, otherwise record the remaining gap.
- [x] Add upgrade-readiness checks in dry-run output: flag unrecognized config keys, stale `--no-sync` generated outputs, floating `@latest` external dependency specs, and stale disabled-agent artifacts.
- [x] Gracefully degrade GitHub API update checks: suppress or minimize output on HTTP 403/429 rate limits instead of emitting multi-line warning blocks.
- [x] Simplify default `al upgrade plan` text output to plain-language sections/actions and remove default exposure of ownership reason codes, confidence, and detection metadata.

**Safety and reversibility (Upgrade Plan Phase 2)**
- [x] Add automatic snapshot/rollback for managed files during upgrade operations.
- [x] Replace binary `--force` semantics with explicit flags: `--apply-managed-updates`, `--apply-memory-updates`, `--apply-deletions`.
- [x] Require explicit confirmation for deletions unless `--yes --apply-deletions` is provided.
- [x] Add `al upgrade rollback <snapshot-id>` command to restore a previous managed-file snapshot.
- [x] Add CI-safe non-interactive apply mode (for example `al upgrade --yes --apply-managed-updates`) that applies managed template updates without deleting unknowns, bridging the gap between interactive upgrades and all-in destructive apply behavior.

**Migration engine (Upgrade Plan Phase 3)**
- [ ] Backlog 2026-01-25 8b9c2d1 (Priority: High, Area: lifecycle management): Implement migration manifests per release for file rename/delete mapping, config key rename/default transform, and generated artifact transitions.
- [ ] Execute migrations idempotently before template write; emit deterministic migration report with before/after rationale.
- [ ] Add compatibility shims plus deprecation periods for renamed commands/flags.
- [ ] Add migration guidance/rules for env key transitions (e.g., non-`AL_` to `AL_`).
- [ ] Backlog 2026-02-03 b4c5d6e (Priority: Medium, Area: lifecycle management): Add template-source metadata and pinning rules so non-default template repositories can be upgraded deterministically.

### Task details
- Backlog 2026-01-25 8b9c2d1
  Description: Define how to handle renamed/deleted template files so stale orphans are not left behind in user repos. Migration manifests codify the mapping per release.
  Acceptance criteria: Each release includes a manifest; `al upgrade` executes manifest migrations idempotently before template write; stale managed files are detected and handled.
  Notes: Current behavior adds/updates files but does not remove files that vanished from templates.
- Backlog 2026-02-03 b4c5d6e
  Description: Allow teams to specify a custom Git repository as template source during `al init`.
  Acceptance criteria: `al init` (or a dedicated command) accepts a Git URL and instantiates templates correctly with repeatable upgrade behavior.
  Notes: Requires secure fetch + cache strategy and compatibility with pinning/update notifications.

### Exit criteria
- `al upgrade plan` exists and shows plain-language categorized changes without writing files.
- `al upgrade plan --json` is hidden/deprecated compatibility output (no stable schema contract) and is no longer in the default user guidance path.
- Every upgrade operation creates a snapshot that can be rolled back via `al upgrade rollback`.
- `--force` is replaced by granular flags; no single flag can silently delete unknowns.
- `al upgrade --yes --apply-managed-updates` is CI-safe and does not delete unknown files.
- Migration manifests ship with each release and handle renames, deletions, and config transitions.
- Breaking changes follow a documented deprecation period with compatibility shims.
- Custom template repositories upgrade deterministically with pinning rules.

## Phase 12 — Documentation and website improvements

### Goal
- VS Code launch architecture is documented for contributors.
- Website documentation is searchable from a global header search bar.

### Tasks
- [ ] Backlog 2026-02-06 vsc-launch (Priority: High, Area: documentation / launchers): Produce detailed architecture documentation for the VS Code launch mechanism, including non-obvious launch flow and design decisions.
- [ ] Backlog 2026-02-06 websearch (Priority: Medium, Area: website / documentation): Add a global website search bar for docs/pages discovery (client-side index or service-backed), with relevant results integrated into the site header UX.

### Task details
- Backlog 2026-02-06 vsc-launch
  Description: Document how VS Code is launched by the CLI, especially the unique/odd path that is currently undocumented.
  Acceptance criteria: Docs are added or updated (for example `docs/DEVELOPMENT.md` or a new architecture doc) and clearly explain launch flow/design choices.
  Notes: User identified this as the least documented and most non-obvious part of the system.
- Backlog 2026-02-06 websearch
  Description: Add global docs search on the website to improve navigation and discovery of guides/features/reference content.
  Acceptance criteria: Search bar is integrated in site header and returns relevant results from documentation pages.
  Notes: Consider client-side indexing (for example FlexSearch) versus managed services (for example Algolia).

### Exit criteria
- VS Code launch architecture documentation exists and is linked from contributor-facing docs.
- Website search is available in the header and returns relevant documentation results.
