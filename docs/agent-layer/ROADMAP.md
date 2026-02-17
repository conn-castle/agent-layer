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
- Linked the upgrade contract from user and contributor docs (`README.md`, site docs, `docs/DEVELOPMENT.md`, and `docs/RELEASE.md`).

## Phase 11 ✅ — Upgrade lifecycle (explainability, safety, and migration engine)
- Delivered explainable, plain-language `al upgrade plan` output with line-level diffs, ownership labeling, and readiness checks.
- Completed upgrade safety/reversibility with automatic snapshots, `al upgrade rollback`, granular apply flags, and CI-safe managed-only apply mode.
- Implemented release migration manifests with idempotent execution and deterministic migration reporting, including hard removals for breaking upgrade surfaces.
- Enforced `.agent-layer/.env` namespace policy (`AL_`-only) and removed the legacy install `Force` path to keep one explicit apply/prompt flow.
- Superseded Backlog 2026-02-03 `b4c5d6e` in this roadmap phase: embedded templates remain the sole supported template source for deterministic upgrades in this release line.

## Phase 12 — High-leverage quick wins

### Goal
- Ship high-value, easy-to-implement features that improve daily usability and power-user workflows.
- Clear deferred backlog items that are unblocked and have outsized impact relative to effort.

### Tasks
- [x] yolo-mode: Add a "full-auto" (YOLO) config option that configures agents to run with maximum autonomy — skipping permission prompts where the agent supports it. Wire appropriate launch flags per client (e.g., Claude's `--dangerously-skip-permissions`, Codex full-auto sandbox). Include prominent security warnings in CLI output, `al doctor`, and documentation. (From BACKLOG e5f4d3c)
- [ ] claude-vscode: Add support for configuring and launching the Claude extension within VS Code, similar to existing VS Code agent support. (From BACKLOG 7e9f3a1)
- [ ] skill-auto-approval: Enable safe auto-approval for workflow skills, allowing skills invoked through the workflow system to run with minimal human intervention when the operation is deemed safe. Requires clear safety criteria and audit trail. (From BACKLOG b2c3d4e)
- [x] launch-plan-decision: Evaluate whether a standalone `al launch-plan <client>` command is needed now that Phase 11 is complete. Document the decision (keep/remove/reshape) based on final launch sync-mode semantics. (From BACKLOG launch-plan-revisit)
- [x] sys-inst-commit-clarity: Replace rigid "NEVER stage or commit" instructions with clearer "commit only when explicitly asked" language across all agents. (From BACKLOG)
- [x] vscode-positional-args: Fix `al vscode` unconditionally appending `.` when caller provides positional args (e.g., workspace files), causing double windows. Skip `.` when passArgs contains a positional argument. (GitHub #51)

### Task details
- yolo-mode
  Description: Add a config option (e.g., `[approvals] auto_approve = "full"` or a dedicated `[approvals] yolo = true`) that tells `al sync` to generate per-client configs with maximum autonomy. For Claude Code: pass `--dangerously-skip-permissions` or equivalent allowlist. For Codex: configure full-auto sandbox mode. For VS Code: adjust settings accordingly. Display a clear, unmissable security warning on `al sync` and `al <client>` when YOLO mode is active. `al doctor` should flag YOLO mode as a warning.
  Acceptance criteria: Config option exists and is documented. Each supported client launches with reduced/no permission prompts when enabled. Security warning is displayed on sync and launch. `al doctor` warns when YOLO is active.
  Notes: This is a power-user feature. The security warning must be prominent and explain the risks. Consider requiring explicit opt-in (not just a default change).
- claude-vscode
  Description: Add launcher support for the Claude extension in VS Code. The Claude extension does not respect MCP servers defined in the local repo config (similar to Codex), so `al sync` must project MCP server configuration into the Claude extension's settings. This may involve a new agent entry in config.toml (e.g., `[agents.claude-vscode]`), a new sync target for Claude extension MCP/settings, and a launch command or integration with `al vscode`.
  Acceptance criteria: Users can enable the Claude VS Code extension through config and launch/configure it via `al` commands. MCP servers defined in `config.toml` are projected into Claude extension settings by `al sync`.
  Notes: Currently `al vscode` handles VS Code Copilot/Codex configuration. The Claude extension has the same MCP gap as Codex — it needs explicit MCP projection.
- skill-auto-approval
  Description: Extend the approval system so that skills invoked through the workflow system can be auto-approved when they meet safety criteria. Define what "safe" means (e.g., read-only operations, scoped file edits, no network calls). Add audit trail for auto-approved actions.
  Acceptance criteria: Safe workflow skills run without prompts. Safety criteria are documented. Audit log records auto-approved actions.
- launch-plan-decision
  Description: Phase 11 is complete. Evaluate whether `al launch-plan <client>` adds value or is redundant with `al sync` + `al upgrade plan`. Document the decision.
  Acceptance criteria: Decision is recorded in DECISIONS.md with rationale.

### Exit criteria
- YOLO mode config option exists, wires correct flags per client, and displays security warnings.
- Claude VS Code extension is configurable and launchable via `al` CLI.
- Safe workflow skills can run without manual approval prompts.
- Launch-plan decision is documented.
- Agent commit instructions are clarified across all instruction templates.
- `al vscode` does not append `.` when caller provides positional args (GitHub #51).

## Phase 13 — Maintenance and quality sweep

### Goal
- Clean up accumulated tech debt across the codebase.
- Consolidate duplicated test helpers and fix known correctness issues.
- Polish the upgrade subsystem with deferred improvements.

### Tasks

#### Testing and DRY
- [ ] test-coverage-parity: Align local and CI test coverage reporting so developers can verify coverage locally before pushing. (From ISSUES)
- [ ] testutil-consolidate: Create `internal/testutil` package and consolidate duplicated test helpers: `writeStubWithExit` (5+ packages), `boolPtr` (2 packages), `withWorkingDir` (2 packages). (From ISSUES stub-dup, 3c5f958c, 3c5f958d)
- [ ] envfile-roundtrip: Fix asymmetric envfile encode/decode and add round-trip property tests. (From ISSUES envfile-asym)

#### Wizard and config
- [ ] wiz-globals: Convert mutable exported catalog variables in `internal/wizard/catalog.go` to functions returning fresh copies. Remove confirmed dead code in `approval_modes.go` and `helpers.go`. (From ISSUES)
- [ ] upg-config-roundtrip: Preserve user TOML comments and key ordering during config migrations, or document the destructive formatting as intentional. (From ISSUES)

#### Upgrade polish
- [ ] upg-ver-diff-ignore: Suppress `al.version` diffs during upgrade plan/apply since updating the version is the primary goal of an upgrade. (From ISSUES)
- [ ] upg-snapshot-polish: Address snapshot scope (lazy snapshotting), scoped restore filtering, and per-snapshot size guards. (From ISSUES upg-snapshot-scope, upg-scoped-restore, upg-snapshot-size)
- [ ] upg-rollback-audit: Add schema/status extension for manual rollback auditability in snapshot system. (From ISSUES)
- [ ] upg-snapshot-list: Add `al upgrade rollback --list` (or `al upgrade snapshots`) to show available snapshots without manual directory inspection. (From BACKLOG)
- [ ] upg-ver-pinning: Decide on and implement a supported workflow for upgrading to intermediate versions (`al upgrade --version X.Y.Z` or equivalent). (From ISSUES upg-ver)

#### Structural
- [ ] installer-struct: Evaluate whether the `installer` struct (23 fields, 57+ methods) should be split into sub-structs (e.g., `templateManager`, `ownershipClassifier`). Extract if method count has grown. (From ISSUES 3c5f958f)

### Exit criteria
- Duplicated test helpers are consolidated into `internal/testutil`.
- Envfile round-trip property tests pass.
- Local test coverage matches CI coverage.
- Wizard exported globals are immutable; dead code is removed.
- Upgrade plan suppresses `al.version` noise.
- Snapshot list command is available for upgrade rollback discovery.
- Remaining upgrade polish items are addressed or explicitly documented as intentional.

## Phase 14 — Documentation and website improvements

### Goal
- Website is polished, searchable, and makes a strong first impression for new users.
- Documentation is comprehensive, discoverable, and optimized for both humans and agents.

### Tasks
- [x] vsc-launch (Priority: High, Area: documentation / launchers): Produced architecture documentation in `docs/architecture/vscode-launch.md`, linked from contributor docs.
- [ ] websearch (Priority: Medium, Area: website / documentation): Add a global website search bar for docs/pages discovery (client-side index or service-backed), with relevant results integrated into the site header UX.
- [ ] web-seo (Priority: Medium, Area: website / marketing): Update website metadata, SEO tags, Open Graph / social cards, and favicon for professional visibility. (From ISSUES)
- [ ] web-docs-copy-btn (Priority: Medium, Area: website / UX): Add a "Copy for Agent" button to each documentation page that copies clean, LLM-friendly page content to the clipboard. (From BACKLOG)
- [ ] analytics (Priority: Medium, Area: website): Integrate privacy-respecting analytics (e.g., Plausible) to monitor visitor patterns and guide content priorities. (From BACKLOG)
- [ ] public-roadmap (Priority: Medium, Area: documentation): Transform the internal roadmap into a public-facing page on the website that communicates project direction and upcoming features. (From BACKLOG f1a2b3c)

### Task details
- vsc-launch
  Description: Document how VS Code is launched by the CLI, especially the unique/odd path that is currently undocumented.
  Acceptance criteria: Docs are added or updated and clearly explain launch flow/design choices.
  Notes: Completed.
- websearch
  Description: Add global docs search on the website to improve navigation and discovery of guides/features/reference content.
  Acceptance criteria: Search bar is integrated in site header and returns relevant results from documentation pages.
  Notes: Consider client-side indexing (e.g., FlexSearch) versus managed services (e.g., Algolia).
- web-seo
  Description: Audit `site/` for missing meta tags, Open Graph tags, social cards, and favicon, then implement them.
  Acceptance criteria: Website has proper `<meta>` tags, OG tags, social card preview, and favicon.
- web-docs-copy-btn
  Description: Add a button to the top of each documentation page that copies the page text/markdown to the clipboard. Show a brief success indicator (e.g., toast or icon change).
  Acceptance criteria: Documentation pages feature a visible button; clicking it copies clean content to the clipboard.
  Notes: Ensure the copied content is well-formatted for LLM consumption (no nav, no boilerplate).
- analytics
  Description: Integrate tracking and analytics into the project website.
  Acceptance criteria: Visitor data is accessible via an analytics dashboard. Privacy compliance (GDPR/CCPA) is addressed.
  Notes: Prefer privacy-respecting alternatives (Plausible, Fathom) over Google Analytics.
- public-roadmap
  Description: Convert the internal ROADMAP.md into a public-facing documentation page on the website.
  Acceptance criteria: Roadmap is accessible on the website, clearly communicates direction, and remains easy for agents to keep current.

### Exit criteria
- VS Code launch architecture documentation exists and is linked from contributor-facing docs.
- Website search is available in the header and returns relevant documentation results.
- Website has professional metadata, favicon, and SEO optimization.
- Documentation pages have a "Copy for Agent" button.
- Analytics are integrated with privacy compliance.
- Public roadmap is accessible on the website.

## Phase 15 — Skills standard alignment (agentskills.io)

### Goal
- Fully align with the [agentskills.io](https://agentskills.io/specification) open standard for portable agent skills.
- Rename "slash-commands" to "skills" throughout the entire codebase, config, templates, docs, and CLI output.
- Skills are validated, documented, and support supplemental directories for complex skills.

### Tasks
- [ ] skill-rename: Global rename of "slash-commands" → "skills" across codebase (source directory `.agent-layer/slash-commands/` → `.agent-layer/skills/`), config references, template content, CLI output, documentation, and memory files. Add an upgrade migration to move existing user directories.
- [ ] skill-source-format: Support both flat `.md` files (simple skills) and full agentskills.io directory format (`<name>/SKILL.md` with optional `scripts/`, `references/`, `assets/`) as skill sources. Flat files remain supported for simplicity; directory format enables full standard compliance and community skill portability.
- [ ] skill-frontmatter: Extend the skill parser to handle all agentskills.io frontmatter fields (`name`, `description`, `license`, `compatibility`, `metadata`, `allowed-tools`) and pass them through to generated outputs.
- [ ] skill-validation: Update `al doctor` to validate skills against the agentskills.io spec (name conventions, required frontmatter, size recommendations, directory name matching).
- [ ] skill-template-migration: Convert embedded template skills from flat `.md` to agentskills.io directory format (`<name>/SKILL.md`).
- [ ] skill-review-plan: Implement a "review-plan" skill with a standardized mechanism to locate and identify the active plan. (From BACKLOG)
- [ ] skill-ordering-guide: Add a skills workflow guide to documentation explaining recommended skill sequences for common workflows (feature dev, bug fix, code review). (From BACKLOG e5f6a7b)
- [ ] skill-template-refs: Audit and fix template skills that reference non-existent Makefile targets (e.g., `make test-fast`, `make dead-code`). Add stronger guards or conditional checks. (From ISSUES tmpl-mk)

### Task details
- skill-rename
  Description: Rename all references from "slash-commands" to "skills" throughout the project. This includes the source directory (`.agent-layer/slash-commands/` → `.agent-layer/skills/`), Go types/functions, config keys, CLI help text, embedded templates, generated comments, documentation, and memory files. Add an upgrade migration that moves the user's existing `slash-commands/` directory to `skills/`.
  Acceptance criteria: Zero references to "slash-command(s)" remain in codebase, config, templates, docs, or CLI output. Upgrade migration handles existing installations.
- skill-source-format
  Description: The skill loader should support two source formats: (1) flat files `.agent-layer/skills/<name>.md` for simple single-file skills, and (2) directories `.agent-layer/skills/<name>/SKILL.md` for full agentskills.io skills with supplemental `scripts/`, `references/`, and `assets/` subdirectories. Both formats produce the same `Skill` struct for downstream projection.
  Acceptance criteria: Both flat-file and directory skills load correctly. Directory skills can include supplemental files that are referenced from SKILL.md. Generated outputs for all clients are agentskills.io-compliant.
  Notes: This dual-format approach lets simple skills stay simple (one file) while enabling community skills and complex skills to use the full standard. The directory format is the canonical agentskills.io format and should be the recommended default for new skills.
- skill-frontmatter
  Description: Extend the YAML frontmatter parser to extract all agentskills.io fields beyond `description`. Pass additional fields through to generated SKILL.md outputs for Codex, Antigravity, and future clients.
  Acceptance criteria: All agentskills.io frontmatter fields are parsed and preserved in generated outputs.
- skill-validation
  Description: Add skill validation checks to `al doctor`: name conventions (lowercase, hyphens only, no consecutive hyphens, matches directory name), required frontmatter (`name`, `description`), SKILL.md size recommendation (< 500 lines), and directory structure conventions.
  Acceptance criteria: `al doctor` reports warnings for non-compliant skills.
- skill-review-plan
  Description: Create a skill that allows agents to review and critique a development plan. Requires a standardized discovery mechanism to locate the active plan.
  Acceptance criteria: A "review-plan" skill is available. Agents can reliably identify the current plan to be reviewed.

### Exit criteria
- No references to "slash-commands" remain in codebase, config, docs, or CLI output.
- Source skills in `.agent-layer/skills/` support both flat `.md` and directory `SKILL.md` formats.
- Generated skill outputs for all clients are agentskills.io-compliant (valid `name`, `description` frontmatter).
- `al doctor` validates skill format compliance.
- Upgrade migration moves existing `slash-commands/` to `skills/` seamlessly.
- Skills workflow guide is published in documentation.

## Phase 16 — Profiles and multi-config

### Goal
- Users can define named profiles with different instructions, skills, and configuration overrides.
- Profiles enable specialization (e.g., "dev", "review", "ops") with easy switching.
- Architecture supports future use of profiles as subagent personas.

### Tasks
- [ ] profile-structure: Define and implement profile directory structure using an overlay model where profiles extend and override a shared base configuration.
- [ ] profile-loading: Implement profile loading with well-defined merge semantics (config: deep merge with profile overriding base; instructions: profile appended after base; skills: profile added to base, override by name).
- [ ] profile-switching: Add `al profile <name>` command (or equivalent) for selecting the active profile. Store active profile in `.agent-layer/state/`.
- [ ] profile-sync: Ensure `al sync` reads the active profile and produces correct merged outputs for all clients.
- [ ] profile-wizard: Extend `al wizard` to support creating and configuring profiles interactively.
- [ ] profile-doctor: Extend `al doctor` to validate profile structure and detect merge conflicts or shadowed settings.
- [ ] profile-docs: Document the profile system, directory structure, merge semantics, and usage patterns.

### Task details
- profile-structure
  Description: Implement the overlay profile model. Directory layout:
  ```
  .agent-layer/
      config.toml              # Base configuration
      instructions/*.md        # Base instructions
      skills/                  # Base skills
      profiles/
          <name>/
              config.toml      # Optional: merged with base (profile overrides)
              instructions/*.md # Optional: appended after base instructions
              skills/          # Optional: added to base skill set (override by name)
  ```
  The base config is always loaded first. If an active profile is set, the profile's files are merged on top. This keeps profiles DRY — they only contain what differs from the base.
  Acceptance criteria: Profile directories are recognized and loaded when an active profile is set. Base-only operation is unchanged when no profile is active.
  Notes: This overlay model supports the future "profiles as subagents" direction — each profile defines a complete persona with specialized instructions and skills on top of a shared project base. A profile's config.toml can enable/disable agents, change models, add MCP servers, or adjust approval modes independently of the base.
- profile-loading
  Description: Implement merge logic: (1) config.toml — deep merge where profile keys override base keys for matching paths; (2) instructions — profile instruction files are sorted and appended after base instruction files; (3) skills — profile skills are added to the base set, with profile skills taking precedence over base skills of the same name.
  Acceptance criteria: Merge semantics are deterministic and documented. Tests cover all merge scenarios (override, append, shadow).
- profile-switching
  Description: Add a command to select the active profile. Active profile is persisted in `.agent-layer/state/active-profile` (or similar). `al sync` reads this to determine which profile to apply. `al profile` with no arguments shows the current profile. `al profile --list` shows available profiles.
  Acceptance criteria: Users can switch profiles via CLI. Profile selection persists across commands. Switching triggers a sync reminder or auto-sync.
- profile-wizard
  Description: Extend the wizard to support creating a new profile (name, which base settings to override) and switching between profiles.
  Acceptance criteria: `al wizard` includes a profile creation/management flow.

### Exit criteria
- Users can create named profiles under `.agent-layer/profiles/<name>/`.
- Each profile can override config, add instructions, and add/override skills.
- `al profile <name>` switches the active profile and `al sync` respects it.
- Profile merge semantics are deterministic, tested, and documented.
- `al doctor` validates profile structure.
- Profile system is documented with examples and use-case guidance.

## Phase 17 — Onboarding and developer experience

### Goal
- New users can go from zero to productive in under 5 minutes.
- Getting-started experience is best-in-class with clear guides, examples, and helpful error messages.

### Tasks
- [ ] quickstart-guide: Create a comprehensive quickstart guide with step-by-step instructions covering install, `al init`, first sync, and launching an agent.
- [ ] example-repos: Create 2–3 example repository configurations (minimal single-agent, full-featured multi-agent, team/enterprise setup) that users can reference or clone.
- [ ] wizard-polish: Audit and improve `al init` and `al wizard` flows for first-time users — reduce prompts, improve defaults, add contextual help text.
- [ ] error-audit: Audit CLI error messages for clarity and actionability. Ensure every error tells the user what went wrong and what to do next.
- [ ] demo-content: Create demo GIFs or short videos showing key workflows (init, sync, launch, skills) for embedding in documentation and README.

### Task details
- quickstart-guide
  Description: Write a quickstart that takes a user from zero (no agent-layer installed) to productive (first agent launched and working) in clear, numbered steps. Cover macOS and Linux. Include what to expect at each step.
  Acceptance criteria: Quickstart is published on the website and linked from README. A new user can follow it end-to-end without external help.
- example-repos
  Description: Create example `.agent-layer/` configurations that demonstrate common setups. Publish as a separate repository or as part of the docs site.
  Acceptance criteria: At least 2 example configurations exist and are linked from quickstart/docs. Each example includes a README explaining the setup.
- wizard-polish
  Description: Identify friction points in `al init` and `al wizard` by walking through them as a first-time user. Reduce unnecessary prompts, improve default selections, and add inline help.
  Acceptance criteria: First-time `al init` completes in fewer steps with better defaults. Wizard explains what each option does.
- error-audit
  Description: Review all user-facing error messages in the CLI. Ensure each one is actionable (not just "error: failed").
  Acceptance criteria: No error message leaves the user without a next step. Common errors include remediation commands.
- demo-content
  Description: Record short demos of key workflows for documentation.
  Acceptance criteria: At least 3 demos exist (init, sync, launch) and are embedded in docs.

### Exit criteria
- Quickstart guide exists on the website and covers the full zero-to-productive flow.
- At least 2 example repository configurations are published and linked.
- `al init` and `al wizard` flows are audited and improved for first-time users.
- CLI error messages are actionable across all commands.
- Key workflows have visual demos in documentation.
