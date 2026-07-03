# Decisions

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
A rolling log of important, non-obvious decisions that materially affect future work (constraints, deferrals, irreversible tradeoffs). Only record decisions that future developers/agents would not learn just by reading the code. Do not log routine choices or standard best-practice decisions; if it is obvious from the code, leave it out.

## Format
- Keep entries brief and durable (avoid restating obvious defaults).
- Keep the oldest decisions near the top and add new entries at the bottom.
- Insert entries under `<!-- ENTRIES START -->`.
- Line 1 starts with `- Decision YYYY-MM-DD <id>:` and a short title.
- Lines 2–4 are indented by **4 spaces** and use `Key: Value`.
- Keep **exactly one blank line** between entries.
- If a decision is superseded, replace the old entry with the new one. Fold the old entry's tradeoff context into the new entry's `Reason` field when it is still valuable, then remove the old entry.
- Periodically consolidate: remove entries that are now self-evident from the codebase (the decision is embodied in code, tests, or docs and a reader would learn it without the log). When removing, verify the tradeoff information is not uniquely preserved in the log.

### Entry template
```text
- Decision YYYY-MM-DD short-slug: Short title
    Decision: <what was chosen>
    Reason: <why it was chosen>
    Tradeoffs: <what is gained and what is lost>
```

## Decision Log

<!-- ENTRIES START -->

- Decision 2026-01-24 a1b2c3d: Ignore unexpected working tree changes
    Decision: Agents will not pause, warn, or stop due to unexpected working tree changes (unstaged or staged files not created by the agent).
    Reason: The user works in parallel with agents, making concurrent changes a normal operating condition.
    Tradeoffs: Increases risk of edit conflicts if both user and agent modify the same file simultaneously; relies on git for resolution.

- Decision 2026-01-25 7e2c9f4: Agent-only workflow artifacts live in `.agent-layer/tmp`
    Decision: Workflow artifacts are written to `.agent-layer/tmp` using a unique per-invocation filename: `.agent-layer/tmp/<workflow>.<run-id>.<type>.md` with `run-id = YYYYMMDD-HHMMSS-<short-rand>`; no path overrides.
    Reason: Keeps artifacts invisible to humans while avoiding collisions for concurrent agents without relying on env vars or per-chat IDs.
    Tradeoffs: Files can accumulate until manually cleaned; agents must echo paths in chat to retain context.

- Decision 2026-02-10 p1c-init-upgrade-ownership: Init scaffolding only; user-owned config/env; agent-owned `.gitignore`
    Decision: `al init` is one-time scaffolding (errors if `.agent-layer/` exists); upgrades/repairs go through `al upgrade plan` + `al upgrade`. `.env` and `config.toml` are user-owned and never overwritten. `.agent-layer/.gitignore` is agent-owned, always overwritten, and excluded from upgrade diffs.
    Reason: Avoid clobbering user config; reduce upgrade-plan noise; simplify init semantics.
    Tradeoffs: Changes to `.agent-layer/.gitignore` cannot be preserved; repos without baseline evidence need an `al upgrade` to establish it.

- Decision 2026-02-12 chlog-immutable: CHANGELOG entries are historical and immutable
    Decision: Never modify published CHANGELOG entries. They record what happened at the time of release and are treated as fixed historical records, even if terminology or paths have since changed.
    Reason: Changing historical entries undermines trust in the changelog as a factual record and can confuse readers comparing old entries against old tags.
    Tradeoffs: Stale references in old entries (e.g., renamed files) remain; readers must consult current docs for the latest names.

- Decision 2026-02-17 p12-yolo-mode: Approvals policy is a 5-mode system including `yolo` (supersedes f6a7b8c)
    Decision: `approvals.mode = "yolo"` auto-approves commands + MCP (like `all`) and sends full-auto flags to supporting clients: Claude `--dangerously-skip-permissions`, Codex `approval_policy=never` + `sandbox_mode=danger-full-access`, Copilot CLI `--yolo`, Antigravity `--dangerously-skip-permissions`. A one-line `[yolo]` stderr acknowledgement runs on every sync/launch.
    Reason: Sandboxed/ephemeral environments want to skip all prompts without per-client setup.
    Tradeoffs: YOLO bypasses all safety prompts only where clients expose the control. No `al doctor` warning — YOLO is a deliberate choice, not a health issue.

- Decision 2026-02-18 config-resilience: Strict runtime parse + lenient repair path (supersedes config-migrate, unknown-key-repairable)
    Decision: `Config.Migrate()` is removed. Runtime commands parse strictly (`ParseConfig`/`LoadConfig`, reject unknown keys, wrapped as `ErrConfigValidation`); repair tools (`al wizard`, `al doctor`) use `ParseConfigLenient`/`LoadConfigLenient`. `al upgrade` uses `config_set_default` ops that prompt interactively for new required fields.
    Reason: Silent defaults violate the "no silent fallbacks" rule; repair tools must always be runnable; explicit consent is a design principle.
    Tradeoffs: Users must run `al wizard` or `al upgrade` to fix broken configs instead of auto-repair. Exception: configs whose only fault is a legacy key that needs a migration (validation wraps `ErrConfigNeedsUpgrade`) are not wizard-fixable — the wizard's patch preserves unknown sections verbatim, so it detects this class and redirects to `al upgrade` rather than dead-ending at sync.

- Decision 2026-02-22 claude-config-dir: Opt-in repo-local `CLAUDE_CONFIG_DIR` via `local_config_dir` config field
    Decision: Gate `CLAUDE_CONFIG_DIR=<repo>/.claude-config` behind `[agents.claude] local_config_dir = true` (opt-in, default false). Use `.claude-config/` (not `.claude/`) to avoid collision with the project-level `.claude/settings.json` generated by `al sync`; keep `.claude-config/` gitignored unconditionally.
    Reason: Claude Code writes a user-level `settings.json` to `CLAUDE_CONFIG_DIR` that would collide with Agent Layer's project-level file; UX cost (two `.claude*` directories) makes always-on inappropriate.
    Tradeoffs: Disabled by default means no isolation unless configured. Claude Code stores auth in the OS credential store regardless of `CLAUDE_CONFIG_DIR` (upstream limitation), so `local_config_dir` currently isolates settings and caches but not auth.

- Decision 2026-02-24 quiet-supersedes-p7a: Warnings verbosity policy (`reduce`, `quiet`, and `--quiet`)
    Decision: `warnings.noise_mode` supports `reduce` and `quiet`, and `--quiet`/`-q` provides command-line suppression. `reduce` suppresses only non-critical suppressible warnings. Configured `quiet` suppresses warnings, update checks, and dispatch banners for normal runs, while `al doctor` still prints warnings by default; an explicit `al --quiet doctor` suppresses warning-only doctor output while preserving failure output.
    Reason: Users need one coherent verbosity model that serves both safer daily use (`reduce`) and zero-noise scripted flows (`quiet`/`--quiet`).
    Tradeoffs: Quiet mode can hide high-risk warnings by design, and older pinned binaries may ignore quiet behavior until upgraded.

- Decision 2026-03-02 instruction-files: Primacy ordering, dedup, and user-managed `04_conventions.md`
    Decision: Instruction files are `00_rules.md`, `01_base.md`, `02_memory.md` (primacy first), and a user-managed `04_conventions.md` that is never overwritten by `al upgrade`. (Superseded in part by [cli-skill-catalog, 2026-05-28]: bare `al init` no longer seeds any instruction files including `04_conventions.md`; `04_conventions.md` is created only when the wizard installs missing workflow-bundle files.) Project-specific items (frontend rules, coverage thresholds, package policies, typing, schema safety) live in `04_conventions.md`; universal items stay in the managed instruction set. A new `append_to_file` migration kind delivers new managed-side defaults to existing repos.
    Reason: Project-specific conventions add noise where they do not apply; primacy ordering improves model compliance with hard constraints.
    Tradeoffs: Conventions are not auto-updated during upgrades; new defaults require explicit `append_to_file` entries. File renames required a v0.9.1 migration.

- Decision 2026-03-21 native-skill-sync: Native skill directory sync replaces MCP prompt delivery (supersedes 2026-01-17 e5f6a7b, 2026-02-23 mcp-prompts-dispatch-bypass)
    Decision: Removed MCP prompt delivery; `al mcp-prompts` remains only as a hidden deprecation no-op stub. Claude reads `.claude/skills/`; Codex/Antigravity/VS Code/Copilot/Copilot-CLI share `.agents/skills/`. Subdirectory support (`scripts/`, `references/`, `assets/`) is preserved.
    Reason: MCP prompts return flat text only; current client docs support directory-format Agent Skills with `.agents/skills/` as the interoperable path.
    Tradeoffs: Lost the unified MCP prompt API; gained full Agent Skills resource support and fewer duplicate projections.

- Decision 2026-05-07 mcp-catalog-seed-split: MCP server catalog moves to wizard-only embedded file; install seed ships zero `[[mcp.servers]]`
    Decision: Default MCP blocks now live in a wizard-only embedded catalog; the install seed ships only an empty `[mcp]` section with docs guidance. Interactive wizard selection inserts a selected catalog block only when it is absent from config.toml, and disables unselected defaults in place (`enabled = false`, block kept) — see wizard-mcp-disable-in-place 2026-05-29. Profile mode remains verbatim.
    Reason: Fresh `al init` should produce a minimal config instead of ~70 disabled MCP lines, while the wizard remains the place to discover curated defaults.
    Tradeoffs: Profile-mode users keep full responsibility for whatever their profile contains.

- Decision 2026-05-29 reasoning-effort-capability-matrix: Reasoning effort is per-client; model/effort compatibility is delegated to the downstream client
    Decision: Support Codex and Claude `reasoning_effort` as custom-tolerant fields (unknown values pass through with a sync warning, not a hard error); reject Copilot CLI effort until a verified control exists. For Claude, do not gate on the model — pass all values via `--effort` and write settings `effortLevel` only for non-`max` values; Claude Code is the authority on which model/effort combinations apply.
    Reason: Silent omission breaks fail-loud behavior, but strict Claude catalogs broke when upstream added values such as `xhigh`, and the prior Opus-only gate rejected valid combinations (e.g. Sonnet now supports effort) and blocked an unset model; `max` is session-only while low/medium/high/xhigh are cataloged.
    Tradeoffs: Typos surface as sync warnings instead of hard errors for Claude/Codex; effort set against a model that does not support it is applied silently by Claude Code rather than rejected by Agent Layer; Copilot configs still fail fast until support is intentionally added.

- Decision 2026-05-09 claude-antigravity-agent-specific-deep-merge: Claude/Antigravity agent_specific deep-merge; install seed denies AskUserQuestion
    Decision: Claude and Antigravity `agent_specific` are deep-merged into their generated `settings.json` files for object values (arrays/scalars still replace). `permissions.deny` is additive and silent; `permissions.allow` warns on override, and Claude also warns for `effortLevel`. Install seed ships `agent_specific.permissions.deny = ["AskUserQuestion"]` to disable Claude Code's clarification tool by default. Codex `agent_specific` remains shallow.
    Reason: Shallow-replace forced users to repeat the entire `permissions` block (re-listing managed allow entries) to add a deny. Agents in this repo prefer text-only clarifications.
    Tradeoffs: Claude and Antigravity warning helpers intentionally differ only where their managed settings differ. `cloneAgentSpecificValue` only deep-clones the projected types (`map[string]any`, `[]any`, `[]string`); new slice types need explicit support.

- Decision 2026-05-16 skill-architecture: Diff-scoped cleanup is two skills; reviewers are fresh-context with sibling-file prompts
    Decision: (a) Two narrow leaf skills (`prune-new-tests` + `simplify-new-code`) sit between implementation and verification; the codebase-scoped sibling is `simplify-codebase`. (b) Phases that re-grade the same agent's work delegate to a fresh-context reviewer subagent (applied across `prune-new-tests`, `simplify-new-code`, `verify-against-plan` Phase 2, `address-pr-comments` Phase 6, `improve-codebase` Phase 3). Sub-variants: artifact-only (PR-comment audit, per-chunk re-audit), plan-anchored narrative-blind (verify-against-plan), smell-pattern (simplify-new-code). (c) Verbatim reviewer prompts live in sibling `reviewer-prompt.md` files; SKILL.md reads and passes them verbatim.
    Reason: Tests are agent side-effects (delete by default) while added code was user-requested (preserve; remove only scope creep) — opposite dispositions degenerate to a justification framework if one rubric covers both. Self-grading inherits primacy/self-consistency biases; a rubric-only review is the cheapest fix. File-backed prompts enforce the verbatim contract mechanically. See `docs/SKILL-DESIGN.md` for ComplexBench evidence.
    Tradeoffs: Larger skill catalog and more re-reads (wall-clock + tokens) for fresh-context invocations. `audit-and-fix-uncommitted-changes` Round N+1 and `resolve-findings` Phase 5 Auditor are intentionally out of scope (already bounded by external artifacts).

- Decision 2026-05-20 lint-suppression-policy: gosec suppressions inline-with-reason; ST10xx dual-enforces revive `exported`
    Decision: No global `gosec.excludes` in `.golangci.yml`; every suppression is inline `// #nosec <rule> -- <reason>` or path-scoped with reason. Test code tightens to `0o600`/`0o700` where incidental. `staticcheck.checks` re-enables `ST1020`-`ST1022`. `issues.max-issues-per-linter: 0` and `max-same-issues: 0` prevent silent caps.
    Reason: Global excludes hid real findings; dual ST10xx + revive is belt-and-suspenders.
    Tradeoffs: Every new perm-literal callsite must tighten or carry a reason; new exported symbols need canonical Go doc comments.

- Decision 2026-05-21 antigravity-replacement: Replace Gemini CLI and retired Antigravity launcher with agy-backed Antigravity
    Decision: Remove `[agents.gemini]` projection and use `[agents.antigravity]`; write repo-local `.agy/antigravity-cli/` config; launch `agy` with `--gemini_dir=<repo>/.agy`; remove the `al gemini` subcommand entirely in the same release (no deprecation window); expose `al agy` (launcher) and `al probe agy`. Subcommand strings follow the launched binary (`agy`) for consistency with `al claude`/`al codex`; internal Go identifiers, package paths, and `[agents.antigravity]` config key keep the product name "Antigravity".
    Reason: Gemini CLI free/pro/ultra is sunset while `agy` is the verified successor. Repo-local `--gemini_dir` containment avoids the old home-directory trusted-folder write and supersedes the prior Gemini Policy Engine/trust-file decisions. A one-release compatibility alias was rejected because the alias is going away anyway and shipping the breaking rename in the same release that introduces the new client keeps the migration surface to a single upgrade step.
    Tradeoffs: Drops Agent Layer projection for upstream Gemini CLI, its model settings, and the retired Antigravity desktop launcher. Existing scripts that invoke `al gemini` will now fail with cobra's "unknown command" error and must switch to `al agy` in v0.10.2. Antigravity MCP config is written and migrated by `agy` v1.0.0, but runtime MCP registration is still false in the observed probe baseline. Subcommand string (`agy`) does not match the internal client identifier (`antigravity`) used in MCP `clients` lists and `[agents.antigravity]` config — accepted because the surface mismatch is small and the binary-named subcommand keeps the user-facing CLI consistent with the rest of `al`.

- Decision 2026-05-23 dispatch-codex-final-answer-no-ack: No synthetic acknowledgment line for Codex final-answer-only streaming
    Decision: Dispatch's Codex adapter emits no synthetic "answer arrives at the end" acknowledgment line. The final-answer-only behavior is documented in `docs/AGENT-DISPATCH.md` (Runtime Notes); compact stderr forwarding of real Codex stream events (`thread.started`, `turn.started`, command lifecycle) is the only runtime signal.
    Reason: Dispatch output is consumed by other agents; the spec's "no synthetic heartbeats or padding" rule applies. A synthetic line burns caller-side context tokens without conveying information that the docs do not already provide.
    Tradeoffs: Callers seeing sparse Codex stderr while answer text is pending must already know (from instructions or docs) that Codex is final-answer-only. Accepted because generated instructions cover discovery and the alternative (synthetic line on every Codex run) violates the agent-consumer token-economy norm.

- Decision 2026-05-28 cli-skill-catalog: Bare init ships no catalog or workflow bundle; wizard installs optional workflow/catalog content
    Decision: The four CLI-wrapper skills (`tavily-web`, `playwright-cli`, `find-docs`, `agent-dispatch`) live in a wizard-only embedded catalog at `internal/templates/skills-catalog/` keyed by `internal/templates/cli-skills-catalog.toml`. Bare `al init` installs no catalog skills, workflow skills, instruction files, memory docs/templates, or marker file; it creates empty `.agent-layer/instructions/` and `.agent-layer/skills/`. The wizard workflow-bundle prompt is install-only: shown only when no workflow-bundle evidence exists, and it creates missing workflow skills, managed instruction files, `04_conventions.md`, and memory docs/templates without overwriting existing files. The separate catalog multiselect copies selected catalog skill dirs and removes deselected catalog ids.
    Reason: Direct precedent is `mcp-catalog-seed-split` (2026-05-07). CLI-wrapper skills require external binaries the user may not have; bundling them by default produced silent breakage. The older alternate-layout flag, marker file, and workflow deletion semantics created a second install mode and a surprising delete path. Wizard workflow-bundle refresh also overwrote managed instructions and replaced skill dirs, so full refresh is now an explicit reinstall/upgrade concern rather than a routine wizard option.
    Tradeoffs: Existing repos keep their catalog skill directories on upgrade (the ownership classifier marks them `catalog_skills_v1`); deselecting a catalog id in the wizard removes that catalog directory after diff-preview confirmation. The wizard can no longer repair or refresh partial workflow-bundle installs once any workflow evidence exists; users must reinstall or use upgrade flows for that.

- Decision 2026-05-28 mcp-catalog-cli-skill-preference: Drop ripgrep/filesystem from the MCP catalog; steer ordinary CLI-backed tools to CLI skills
    Decision: The wizard MCP catalog (`internal/templates/mcp-catalog.toml`) ships only `context7`, `tavily`, `fetch`, and `playwright`; `ripgrep` and `filesystem` are removed. The MCP multiselect screen warns that MCP servers are not the recommended default for ordinary CLI-backed tools (prefer CLI command-based skills — see https://agent-layer.dev/cli-skill-design) and not to enable both an MCP server and a CLI skill for the same tool (e.g. Tavily). Do not re-add CLI-wrapper servers to the catalog.
    Reason: Ordinary CLI tools (repo search, file access) belong in CLI command-based skills, which keep live `--help` as the source of truth and avoid per-server MCP tool-schema overhead and config drift; ripgrep/filesystem also duplicate capabilities most clients already have natively.
    Tradeoffs: Users who still want those servers must hand-author them (supported, and shown as reference config examples in README/site docs). No remaining catalog default exercises the optional `clients` field.

- Decision 2026-05-29 wizard-mcp-disable-in-place: Wizard disables MCP servers in place (never deletes) and has no restore-missing prompt
    Decision: `al wizard` never deletes a `[[mcp.servers]]` block. Both catalog defaults and non-catalog customs (`customMCPServers`, surfaced in a dedicated step after the catalog multiselect) follow one rule: an unselected server that exists in config.toml is kept with `enabled = false`; a selected server is enabled. The only asymmetry is insertion — a selected *catalog default* absent from config.toml is added from the embedded catalog, while a custom server has no template and so can only be kept/disabled. There is no separate "restore missing defaults?" confirm; missing defaults are simply unselected options in the multiselect that are added when selected and left absent otherwise. The custom step is skipped in both flow directions when there are no customs (mirrors `skipEnableLayerStep`); a no-op screen would otherwise trap back-navigation. `EnabledMCPServersTouched`/`CustomMCPServersTouched = false` (profile/`--yes`/programmatic) preserves existing state unchanged.
    Reason: Deleting on disable permanently destroys user-authored definitions (custom servers especially have no catalog template) and made the restore-missing prompt necessary and confusing — it defaulted to "yes" yet the multiselect still showed missing defaults unselected, so the answer was effectively a no-op. Disable-in-place is reversible, consistent across both server kinds, and removes the prompt entirely.
    Tradeoffs: A disabled server lingers as a dead `enabled = false` block until the user removes it by hand; the wizard can re-enable it later. Fully removing a default from config.toml is now a manual edit rather than a wizard toggle.

- Decision 2026-05-31 wizard-feature-disable-agent-specific: Scalar disable toggles store direct `agent_specific` keys; the array-shaped AskUserQuestion toggle is a typed flag
    Decision: The scalar wizard "disable" toggles (Codex browser/computer-use, Codex apps, Claude IDE open-file reading, Claude auto-memory, Claude connectors) write the client-native key straight into the agent's `agent_specific` passthrough — Codex `features.*`, Claude `env.*`/`autoMemoryEnabled`. No typed config field, no `fields.go` entry, no sync changes. A key is written only when the user opts in (disable intent); leaving a toggle off omits the key so the client keeps its own default (no hidden defaults). Patch writers `applyCodexBrowserUpdate` and `applyClaudeAgentSpecificUpdate` parallel `applyCodexAppsUpdate`. The Claude AskUserQuestion toggle is the deliberate exception — see [[askuserquestion-deny-plus-hook-default-allowed]].
    Reason: Generated client files already derive entirely from `agent_specific` (Claude deep-merge, Codex append), so a typed field would create a second canonical source and duplicate sync logic. This holds for scalar values. It breaks for the AskUserQuestion toggle, whose value is array-shaped (`permissions.deny` + `hooks.PreToolUse`) and must coexist with user entries — which the line-based wizard patcher cannot union, only clobber. Supersedes the BACKLOG `disable-client-memory-config` proposal of a single translated flag.
    Tradeoffs: The feature→client-key mapping lives in wizard code and CONTEXT.md rather than a typed schema; each new client's controls must be added the same way. Codex `memories` is intentionally not pinned (experimental, off by default).

- Decision 2026-05-31 askuserquestion-deny-plus-hook-default-allowed: AskUserQuestion blocked via a typed flag + sync-time deny/hook injection; template no longer disables it by default
    Decision: The shipped template no longer seeds `permissions.deny = ["AskUserQuestion"]`, so fresh `al init` allows the tool (Claude Code's native default). The wizard's opt-in toggle writes a typed `agents.claude.disable_question_tool = true` scalar (a `*bool`, like `local_config_dir`; no `fields.go`/validation entry needed). When true, `buildClaudeSettings` (`internal/sync/claude_question_tool.go`) injects `permissions.deny += "AskUserQuestion"` (array-union, dedup) and appends the `PreToolUse` hook (`matcher = "AskUserQuestion"`, command exits 2; dedup by matcher) into the generated `.claude/settings.json`, AFTER the agent_specific merge so user-supplied deny/hooks are unioned, never replaced. The hook is always emitted regardless of `approvals.mode`.
    Reason: `permissions.deny` is skipped entirely under YOLO/`bypassPermissions` (Claude Code permission-modes docs), so the deny alone silently fails under YOLO; the PreToolUse hook fires in every mode and is the robust block. The earlier design wrote the deny/hook directly into `agent_specific` via the line-based wizard patcher, which clobbered co-listed user entries (replace on enable; whole-line comment-out on default-accept). A typed flag + structured sync injection is the only way to union with user entries, since the patcher cannot edit array elements. Defaulting to allowed avoids shipping a hidden behavior.
    Tradeoffs: New installs lose the previous default-off behavior (CHANGELOG call-out); existing repos keep their deny (config.toml is never overwritten on upgrade) and migrate via `al wizard`. The injection logic is a small Claude-sync-only special case (the scalar toggles in [[wizard-feature-disable-agent-specific]] need none). Unreleased when introduced, so no migration from the old agent_specific form was required.

- Decision 2026-06-01 wizard-feature-toggles-multiselect: Per-agent feature toggles are two positive-framed multi-selects, not six disable confirms
    Decision: The model step renders one Claude multi-select and one Codex multi-select instead of six individual "Disable X?" yes/no prompts. Each checkbox uses positive framing (checked = keep the feature enabled; unchecking disables it) and is pre-checked from current config so a no-edit re-run produces no config churn. Inversion between checkbox state and the disable-sense storage happens only at the prompt boundary (`promptFeatureToggles` in `helpers.go`); the disable-sense `Choices` fields, their `*Touched` flags, the config keys, and all of patch.go stay the canonical source of truth and are unchanged. Codex apps remains the enabled-sense exception (`featureToggle.enabledSense`) that always writes an explicit `features.apps` per [[wizard-feature-disable-agent-specific]]. Option labels are short, stable strings defined once in `messages` and reused on both the option-list and contains-check sides (the label is the load-bearing match identity); per-feature explanations live in the multi-line group title because `MultiSelect` has no per-option description. The MCP per-server enable/disable flow stays separate.
    Reason: Six sequential disable confirms with inverted ("Yes = disable") phrasing were noisy and easy to misread; two positive-framed checkbox groups are scannable and make the current state visible at a glance. Inverting only at the boundary avoids a second canonical source while delivering the friendlier UX.
    Tradeoffs: A label edit must stay byte-identical between the message constant and the invert match or pre-selection silently breaks (mitigated by single-definition reuse + a round-trip/inversion test). Esc on the Codex multi-select still discards the Claude selection too (the whole model step is one back unit, unchanged).

- Decision 2026-06-30 provider-statusline-sources: Statusline config is explicit; wizard defaults absent keys on
    Decision: `agents.claude.statusline` and `agents.codex.statusline` are explicit booleans for sync/install/upgrade paths: absent or explicit `false` disables, explicit `true` enables. In `al wizard`, absent keys precheck the provider feature toggles while explicit `false` remains unchecked and explicit `true` remains checked. Claude uses `.agent-layer/claude-statusline.sh` projected to `.claude/claude-statusline.sh`; legacy `.agent-layer/statusline.sh` migrates write-if-missing. Codex uses `.agent-layer/codex-statusline.toml` as a statusline-only `[tui].status_line` fragment merged into `.codex/config.toml`, unless `agents.codex.agent_specific.tui.status_line` is explicit. Sync never seeds missing sources; wizard and upgrade seed missing enabled sources once. Existing sources are replaced only through interactive upgrade diff review; non-interactive upgrade skips replacements. The `0.11.0` `config_set_default` ops for both keys ship `value: false`, so a non-interactive `al upgrade --yes` writes the explicit disabled default and enables/seeds nothing; interactive `al upgrade` still prompts defaulting off.
    Reason: A single unified statusline schema made end-user customization too hard and would cap Claude-specific script flexibility. The wizard is an interactive consent point, so defaulting absent keys on improves setup without changing bare init or non-interactive upgrade behavior; explicit `false` remains the durable opt-out. Defaulting migrations to `false` keeps `--yes` minimal (no unexpected feature enablement, no settings mutation). Boolean toggles keep Agent Layer responsible for provider wiring while users edit each provider-native source directly.
    Tradeoffs: Default-accepting wizard runs can now enable and seed statusline sources when the provider is enabled and the config key is absent. Hand-edited configs with `statusline = true` fail loudly when the source is missing until the user runs `al wizard`, interactive `al upgrade`, or creates the source manually. Shipped source improvements reach only missing sources or existing sources accepted through interactive upgrade review. Codex cannot mirror Claude script-only fields such as ANSI formatting, cost, lines changed, or worktree label.

- Decision 2026-06-30 release-artifacts-immutable: Tagged release manifests are immutable
    Decision: After a version is tagged, do not modify its `internal/templates/manifests/<version>.json` or `internal/templates/migrations/<version>.json` for later work. Create new manifest and migration files for the forthcoming release version instead, even when the migration file has no operations.
    Reason: These files define the published ownership baseline and upgrade contract for that tag. Rewriting them after release makes the source tree disagree with the released artifact and can change direct-upgrade behavior for historical target versions.
    Tradeoffs: Patch releases may carry empty migration manifests and duplicate-looking release artifacts, but historical upgrade behavior remains auditable and byte-stable.

- Decision 2026-06-30 antigravity-model-first-class: Antigravity model uses typed config, not agent_specific
    Decision: `agents.antigravity.model` is the only supported Antigravity model field. Sync projects it to generated Antigravity `settings.json`; runtime validation rejects `agents.antigravity.agent_specific.model`. The v0.12.0 upgrade migration moves that passthrough key to the typed field before validation.
    Reason: Wizard, dispatch, field catalog, docs, and defaults all treat Antigravity model selection as an Agent Layer-owned concept. Keeping it under `agent_specific` made a known field look like unmanaged passthrough and created two possible sources of truth.
    Tradeoffs: Fresh hand-authored configs that put `model` under `agent_specific` fail loudly and must move the value to `agents.antigravity.model`; upgrading repos get the automatic move first.

- Decision 2026-07-01 codex-local-home-opt-in: Repo-local Codex home requires explicit opt-in
    Decision: Gate `CODEX_HOME=<repo>/.codex` behind `[agents.codex] local_config_dir = true` for `al codex`, Codex dispatch, and `al vscode`; absent/false preserves inherited `CODEX_HOME`.
    Reason: Agent Layer should keep generating repo-local Codex project config while leaving Codex's normal global/project config layering as the default. Users who want per-repo Codex auth, sessions, logs, and runtime state can opt in.
    Tradeoffs: Existing users who relied on automatic per-repo Codex auth must set `agents.codex.local_config_dir = true`; default mode shares Codex home state but better matches user-managed Codex workflows.

- Decision 2026-07-02 codex-config-shared-state: `.codex/config.toml` is shared state patched by sync
    Decision: `al sync` patches only known Agent Layer-owned Codex entries in `.codex/config.toml`, preserves unrelated Codex/user runtime entries, and seeds current repo project trust only when the exact project entry is absent.
    Reason: Codex writes durable runtime/project state into the same file Agent Layer must refresh for model, approval, statusline, feature toggles, and MCP projection.
    Tradeoffs: Invalid existing TOML and shape conflicts now block sync instead of being overwritten; arbitrary removed passthrough paths outside known Agent Layer-owned paths may require manual cleanup.

- Decision 2026-07-02 cli-exec-handoff-signing: Agent launch exec handoff plus signed binary delivery
    Decision: Interactive CLI launchers (`al claude`, `al codex`, `al copilot`, `al agy`) replace `al` with the target agent via `syscall.Exec`; darwin release binaries are Developer ID signed/notarized as `com.conncastle.agent-layer`; Homebrew renders a binary formula for release assets instead of building from source.
    Reason: macOS TCC/keychain prompts must attribute protected access to the actual agent binary, and grants should survive Agent Layer upgrades for install-script and Homebrew users.
    Tradeoffs: Agent exit codes and stderr now pass through without Agent Layer's wrapper; users get a one-time permission prompt under each agent identity, while stale `al` grants remain cosmetic.

- Decision 2026-07-03 global-notifications-chime: One notification toggle projects provider-native Stop hooks
    Decision: `notifications.chime` is a global typed opt-in. Sync projects it to Claude `Stop`, a marked managed Codex `[[hooks.Stop]]` region, and an Agent Layer-owned Antigravity `.agents/plugins/agent-layer-chime` plugin.
    Reason: A single source of truth avoids per-agent hook drift while preserving user-owned provider hooks and shared config.
    Tradeoffs: The default sound command is macOS-specific, and the chime signals lifecycle stop only; Codex needs marked shared-config ownership while Antigravity needs a dedicated plugin directory.
