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
  - replace ALL phase content (Goal, Tasks, Task details, Exit criteria) with a concise bullet summary of what was accomplished (no checkbox list).
- **Archival:** When more than 5 completed phases exist, consolidate the oldest completed phases into a single `## Archived phases` summary. Keep the 5 most recently completed phases as individual entries. The archive section uses one line per phase.

### Phase templates

Archived (compact):
```markdown
## Archived phases (1–N)
- Phase 1 — <name>: <one-line summary>
- Phase 2 — <name>: <one-line summary>
```

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

## Archived phases (1–11)
- Phase 1 — Define the vNext contract (docs-first): Defined the config-first `.agent-layer/` contract, memory docs, and Go rewrite foundation.
- Phase 2 — Repository installer + skeleton (single command install): Implemented `al init`, gitignore management, template seeding, and initial release tooling.
- Phase 3 — Core sync engine (parity with current generators): Implemented config/instruction parsing and deterministic generators for all clients.
- Phase 4 — Agent launchers (Gemini/Claude/Codex/VS Code/Antigravity): Added shared launch plumbing and per-client launcher behavior.
- Phase 5 — v0.3.0 minimum viable product (first Go release): Delivered MCP projection, env substitution, approvals, init overwrite warnings, gitignore guidance, and release-note extraction.
- Phase 6 — v0.4.0 CLI polish and sync warnings: Added `al doctor`, `al wizard`, VS Code launchers, and configurable sync warnings.
- Phase 7 — v0.5.0 Global CLI and install improvements: Moved to a global `al` with per-repo pinning, Homebrew/manual installers, shell completion, and Linux VS Code launcher support.
- Phase 8 — v0.5.4 Workflows and instructions: Added workflow improvements, temp-report concurrency, BACKLOG rename, memory formatting, and VS Code reauth docs.
- Phase 9 — MCP defaults + CLI output polish: Added default MCP entries, semantic CLI output, upgrade warning improvements, and instruction/documentation cleanup.
- Phase 10 — Upgrade Phase 0 stabilization (immediate): Removed unsupported Windows upgrade surfaces, hardened pin management, and published/linked the upgrade contract.
- Phase 11 — Upgrade lifecycle: Delivered explainable upgrade plans, automatic snapshots/rollback, granular apply flags, migration manifests, and `.env` namespace enforcement.

## Phase 12 ✅ — High-leverage quick wins
- Added `approvals.mode = "yolo"` with per-client full-auto projections and single-line sync/launch acknowledgements.
- Added `[agents.claude_vscode]` and unified VS Code launch behavior under `al vscode`.
- Recorded the decision to not implement a standalone `al launch-plan` command.
- Clarified agent commit instructions and fixed `al vscode` positional-arg handling to avoid unconditional `.` appends.

## Phase 13 ✅ — Maintenance and quality sweep
- Completed the testing/config/upgrade maintenance scope, including deterministic warning ordering, helper consolidation in `internal/testutil`, envfile round-trip hardening, strict enable-only config decoding, upgrade snapshot/list/pinning polish, and race-check command coverage.
- Refactored `internal/install` with explicit coordinator components (`templateManager`, `ownershipClassifier`, `upgradeOrchestrator`) while preserving behavior under existing install/upgrade test contracts.
- Reduced direct `installer` method surface from 105 to 70 methods and updated call boundaries throughout install and upgrade flows.

## Phase 14 ✅ — Documentation and website improvements
- Completed VS Code launch architecture documentation and linked it from contributor-facing website docs.
- Completed website polish work in the web repo: global search, SEO/social metadata and favicon updates, and documentation "Copy for Agent" UX.
- Integrated Google Analytics 4 with consent defaults and removed the public-roadmap item from this phase scope.

## Phase 15 ✅ — Skills standard alignment (agentskills.io)
- Added a reusable `internal/skillvalidator` package with parse/validate separation, deterministic findings, Unicode NFKC-aware name checks, rune-based length limits, and normalization-aware name/path matching.
- Integrated skill validation into `al doctor` with dedicated diagnostics and tests, including explicit warnings when directory-format skills use non-canonical lowercase `skill.md`.
- Added directory loader compatibility for lowercase `skill.md` with canonical `SKILL.md` precedence, keeping parser behavior backward-compatible for existing repos.
- Migrated embedded template skills to directory format (`skills/<name>/SKILL.md`), updated embed patterns/tests/manifests/migrations, and added the `review-plan` skill with deterministic `*.plan.md` discovery guidance.
- Aligned public docs (`README.md`, `site/docs/reference.mdx`) with current frontmatter/spec rules and added individual skill-level workflow guidance.

## Phase 16 ✅ — Antigravity replacement
- Replaced Gemini CLI support and the retired Antigravity desktop launcher with a single Antigravity client under `[agents.antigravity]`, including `al antigravity`, repo-local `agy --gemini_dir` launch containment, and generated settings/MCP config. The `al gemini` subcommand was removed entirely in the same release (no deprecation window).
- Added `al probe antigravity` with a stable JSON contract (`agy_config_dir`, `capabilities`, `evidence`, etc.), parser fixtures for the v1.0.0 baseline, non-zero CLI exit on probe failure, and a forensic workspace under `.agent-layer/tmp/probe-antigravity-<ts>-<suffix>/` cleaned by `al upgrade --apply-tmp-deletions`.
- Added the v0.10.2 migration manifest plus `config_delete_key`, `config_replace_string`, and `delete_generated_artifact` (orphan `GEMINI.md`) operations so existing `[agents.gemini]` configs and `mcp.servers[].clients[] = "gemini"` entries move cleanly to Antigravity while stale model/effort keys, retired desktop config, and the orphan instruction shim are removed.
- Updated doctor, templates, gitignore defaults, e2e coverage, docs, and memory to make Antigravity the supported Google CLI path.

## Phase 17 — Onboarding and developer experience

### Goal
- New users can reach initial productivity within five minutes.
- Getting-started experience is exemplary, with clear guides, examples, and actionable error messages.

### Tasks
- [ ] quickstart-guide: Create a comprehensive quickstart guide with step-by-step instructions covering install, `al init`, first sync, and launching an agent.
- [ ] example-repos: Create 2–3 example repository configurations (minimal single-agent, full-featured multi-agent, team/enterprise setup) that users can reference or clone.
- [ ] wizard-polish: Audit and improve `al init` and `al wizard` flows for first-time users — reduce prompts, improve defaults, add contextual help text.
- [ ] error-audit: Audit CLI error messages for clarity and actionability. Ensure every error tells the user what went wrong and what to do next.
- [ ] demo-content: Create demo GIFs or short videos showing key workflows (init, sync, launch, skills) for embedding in documentation and README.

### Task details
- quickstart-guide: Zero-to-productive numbered guide covering install through first agent launch on macOS and Linux; linked from README.
- example-repos: At least two example `.agent-layer/` setups (minimal + multi-agent) published with READMEs and linked from docs.
- wizard-polish: Reduce friction in `al init` / `al wizard` (fewer prompts, better defaults, inline help). Progress: incremental improvements through v0.10.2 — wizard slice (Escape/back, cancel guidance, TOML-corrupt warning), MCP catalog split (fresh `al init` ships zero `[[mcp.servers]]`; interactive prune of disabled catalog entries, profile mode exempt), Phase 16 wizard swap (Antigravity replaces Gemini in the agent catalog, fresh-install default flips to disabled, deprecation notice for legacy `[agents.gemini]`).
- error-audit: Audit every user-facing CLI error so each names a next step.
- demo-content: Record at least three workflow demos (init / sync / launch) and embed in docs.

### Exit criteria
- Quickstart guide exists on the website and covers the full zero-to-productive flow.
- At least 2 example repository configurations are published and linked.
- `al init` and `al wizard` flows are audited and improved for first-time users.
- CLI error messages are actionable across all commands.
- Key workflows have visual demos in documentation.

## Phase 18 — Profiles and multi-config

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
- profile-structure: Overlay model under `.agent-layer/profiles/<name>/{config.toml,instructions/*.md,skills/}` — base loaded first, active profile merged on top so profiles stay DRY. Supports the "profile-as-subagent persona" direction.
- profile-loading: Deterministic merge — config deep-merged, instructions appended after base, skills added with profile overriding base on name collision; tests cover override/append/shadow.
- profile-switching: `al profile <name>` selects (persists under `.agent-layer/state/`), `al profile` shows current, `al profile --list` enumerates; sync respects the active profile.
- profile-wizard: Wizard flow to create / select / delete profiles.

### Exit criteria
- Users can create named profiles under `.agent-layer/profiles/<name>/`.
- Each profile can override config, add instructions, and add/override skills.
- `al profile <name>` switches the active profile and `al sync` respects it.
- Profile merge semantics are deterministic, tested, and documented.
- `al doctor` validates profile structure.
- Profile system is documented with examples and use-case guidance.
