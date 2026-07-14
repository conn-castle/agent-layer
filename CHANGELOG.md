# Changelog
All notable changes to this project will be documented in this file.

## v0.13.0 - 2026-07-12

Breaking Agent Dispatch redesign. Ordinary dispatches now start fresh provider
conversations, buffer output until semantic completion, and expose explicit
continuation and factual inspection instead of streaming provider traffic.

### Added
- `al dispatch resume <name>`, `inspect`, `list`, and `delete` with atomic
  per-name durable mappings and immutable run-UUID inspection.
- Exact provider compatibility gates for Claude Code 2.1.207, Codex CLI
  0.144.1, and Antigravity 1.1.1; options now report fresh, resume, and inspect
  capabilities separately.
- Private bounded run evidence, process-group supervision, final-answer replay,
  a safe pre-start retry, and Antigravity’s isolated documented log-file ID
  extractor with fail-loud `not resumable` handling.

### Changed
- **Breaking:** `al dispatch` no longer streams answer text, progress, raw
  events, or provider diagnostics. Standard output contains only a successful
  final answer; standard error begins with one compact identity line.
- **Breaking:** the v1 `dispatch_capable` and streaming options fields were
  removed from the public JSON contract. Use separate capability facts instead.
- Version-binary handoff moved from `internal/dispatch` to
  `internal/versiondispatch` to distinguish it from Agent Dispatch.
- `auto-skill-loop` now provides `fix-issue-log`, `implement-backlog`,
  `improve-interfaces`, and `improve-codebase` modes plus repository-added mode
  files. It selects adaptive fresh work, preserves local blockers,
  batches and ships centrally, keeps `/ship-pr` isolated in its shipper
  dispatch, independently gates exact-head merge authorization, and reconciles
  each result without preplanning the full source.
- Instruction-only assets now live under their owning skills' `references/`
  directories; output and machine-readable resources remain under `assets/`.

### Removed
- Retired the standalone `fix-issues` skill after preserving its explicit
  filters, batching, dispositions, and one-delivery behavior in the
  `fix-issue-log` mode.

### Fixed
- Dispatch now waits for provider completion evidence and terminates failed
  process groups, preventing terminal failure while owned descendants continue.
- Projection preparation is serialized before provider launch, avoiding
  concurrent generated-skill writes while allowing independent targets to run
  concurrently afterward.

## v0.12.3 - 2026-07-10

Patch release for the v0.12 line. Adds current Codex model and reasoning-effort suggestions, and fixes `al sync` so generated skill resources stay reconciled safely with their source trees.

### Changed
- Codex model suggestions now include `gpt-5.6-sol`, `gpt-5.6-terra`, and `gpt-5.6-luna` in the wizard and Agent Dispatch options.
- Codex reasoning-effort suggestions now include `max` and `ultra` in the wizard and Agent Dispatch options.

### Fixed
- `al sync` now reconciles generated skill resources (`scripts/`, `references/`, and `assets/`) with their source skill directory: it removes stale resources and handles file/directory transitions without replacing the generated `SKILL.md`.
- Skill-resource sync now detects source symlinks without following them and safely replaces generated destination symlinks rather than writing through them.

### Internal
- Added v0.12.3 migration and template ownership manifests. The migration has no operations; existing configurations need no manual migration.

## v0.12.2 - 2026-07-06

Patch release for the v0.12 line. Tightens bundled skill and instruction contracts, fixes installer prompt routing edge cases, adds public skill-architecture documentation, and hardens the release workflow before signing.

### Added
- Added the Agent Layer-specific Skills approach documentation page, including the target root-skill model, workflow-skill boundaries, and the mapping from target modules to current bundled skills.
- Added template tests covering bundled skill and instruction contract expectations so future wording changes are checked automatically.

### Changed
- Bundled skill templates now state scope, deferral, review, and orchestration rules more precisely across planning, review, issue fixing, PR handling, verification, and autonomous-loop workflows.
- Multi-agent plan review instructions now make reviewer orchestration and synthesis responsibilities clearer, while participant terminology is standardized across skill and instruction docs.
- The Agent Dispatch catalog skill now activates only for explicit external dispatch targets or skill-directed dispatch, leaving generic subagent work to built-in subagent behavior.
- Universal skill, CLI-skill, and instruction design guides were generalized for portable authoring guidance and now separate client/specification requirements from Agent Layer-specific conventions.
- Release preflight documentation now reflects that CI, release-script checks, and upgrade-doc validation run before tagging.
- The release workflow now runs `make ci` before importing signing credentials.

### Fixed
- Installer and upgrade prompt routing now flow through a shared prompt router, preserving required overwrite/delete prompt validation while centralizing optional prompt fallbacks.
- Statusline source replacement prompting is now gated on the actual optional prompt capability, preventing zero-value prompt implementations from being treated as wired callbacks.
- Zero-value prompt fallback behavior is covered for unified overwrite, grouped tmp deletion, statusline source, config defaults, and skills migration prompts.
- Agent Dispatch now forwards SIGINT and SIGTERM to the full dispatched process group, so shell-launched child processes terminate promptly on interruption.
- End-to-end assertions were updated for the retired generated Codex `.codex/AGENTS.md` shim.

### Internal
- Added v0.12.2 migration and template ownership manifests. The migration has no operations; the release updates managed templates and docs only.

## v0.12.1 - 2026-07-04

Patch release for the v0.12 line. Fixes binary Homebrew publishing, preserves Codex hook trust state across chime refreshes, and retires the generated `.codex/AGENTS.md` shim that duplicated root `AGENTS.md` when Codex used repo-local config.

### Changed
- `al sync` no longer writes `.codex/AGENTS.md`. Codex reads root `AGENTS.md` as project instructions, and repo-local `CODEX_HOME` caused `.codex/AGENTS.md` to be loaded as home-level instructions as well.
- Workflow instruction and skill templates were tightened for tradeoff handling, full-workflow spec approval, and prune-new-tests reviewer output.
- Release documentation now clarifies binary Homebrew tap PR handling.

### Fixed
- Codex chime refresh now preserves existing hook trust state stored inside the managed chime markers and remains idempotent across repeated syncs.
- Homebrew formula generation now includes both macOS ARM64 and macOS Intel release assets.
- Homebrew binary formula installs now mark `al` executable before generating shell completions.
- The release workflow now verifies all binary assets needed by the Homebrew tap, including `al-darwin-amd64`.

### Internal
- Added v0.12.1 migration and template ownership manifests. The migration deletes only Agent Layer-generated `.codex/AGENTS.md` files; hand-authored files at that path are left untouched.

## v0.12.0 - 2026-07-03

Consolidates all unreleased work since `v0.11.0` into one coherent release. Adds typed Antigravity model selection, configurable Agent Dispatch depth, repo-local Codex home opt-in, shared Codex TOML patching, provider turn-stop chimes, serialized sync writes, new workflow skills, signed/notarized macOS binaries, and binary Homebrew delivery.

### Added
- Antigravity model selection is now a first-class Agent Layer setting. `agents.antigravity.model` is projected into generated Antigravity settings, `al wizard` can discover choices from live `agy models` output with catalog fallback, and `al dispatch --model` plus `al dispatch options` now report and accept Antigravity model overrides. Antigravity reasoning level remains encoded in the selected model display string; `--reasoning-effort` is still unsupported for Antigravity.
- `dispatch.max_depth` allows nested `al dispatch` chains beyond the default depth of `1`. `AL_DISPATCH_ACTIVE` now carries the current dispatch depth, invalid or empty values fail loudly, and config validation rejects non-positive depths.
- `agents.codex.local_config_dir` controls whether Agent Layer sets `CODEX_HOME=<repo>/.codex` for `al codex`, Codex dispatch, and `al vscode`. The default is `false`, preserving Codex's normal global/project config layering; set it to `true` to keep repo-local Codex auth, sessions, logs, and runtime state.
- `notifications.chime` is a global opt-in for provider turn-stop chimes. Sync projects provider-native stop hooks for Claude and Codex and an Agent Layer-owned Antigravity plugin, while preserving user-owned hooks and plugin content.
- `al sync` now serializes concurrent writes for the same project with a repo-local lock, preventing overlapping generated-file updates during parallel launches or dispatches.
- `.codex/config.toml` is now treated as shared state. Agent Layer patches only managed Codex keys, MCP projection, project trust, feature toggles, and statusline entries while preserving unrelated user or Codex runtime TOML, comments, multiline strings, and plugin settings.
- `al wizard` adds a Git tracking step for `.agent-layer/` and `docs/agent-layer/`, implemented by rewriting the managed `.agent-layer/gitignore.block` source before sync.
- New built-in workflow skills: `auto-skill-loop`, `full-workflow`, `interface-audit`, and `multi-agent-plan-review`. `ship-pr` also gained a bundled `monitor-pr.sh` helper for polling pull request readiness and filtering review-bot noise.
- Release builds now sign Darwin binaries with Developer ID, enable hardened runtime, notarize them with Apple, and write checksums after signing. The release workflow also documents the required signing and notarization secrets.
- Homebrew delivery now uses prebuilt macOS and Linux release binaries with per-platform checksums instead of building from the source tarball.
- Shared provider option catalogs now back wizard and dispatch option suggestions for supported agents, with corrected Codex model/reasoning suggestions and `fable` in the Claude model catalog.

### Changed
- `al claude`, `al codex`, `al copilot`, and `al agy` now replace the `al` process with the target agent CLI instead of spawning a child process. Agent exit codes now pass through directly and the old `Error: <agent> exited with error: ...` wrapper line is gone.
- macOS permission prompts are now attributed to the actual signed Agent Layer binary that launches the agent. Users may see one prompt per agent after upgrading; old `al` entries in Privacy & Security are cosmetic.
- `approvals.mode = "yolo"` no longer emits the VS Code `chat.tools.global.autoApprove` setting. YOLO still sends full-auto controls to Claude, Codex, Copilot CLI, and Antigravity.
- `al wizard` no longer offers a workflow-bundle refresh when Agent Layer workflow files already exist. The workflow-bundle prompt is install-only for missing bundle files and preserves existing files; use `al upgrade` for managed workflow updates.
- Agent-specific passthrough config now uses the shared `ProviderPassthrough` type, and Agent Layer-owned Antigravity model config is rejected under `agents.antigravity.agent_specific.model`.
- The Claude status line now rounds weekly-limit reset time up to the next whole day/hour so a partial remaining unit stays visible, drops the `#` prefix on the session id, and avoids per-untracked-file `git diff --no-index` processes when counting untracked line changes.
- Agent Dispatch target metadata now uses the shared provider option catalog, reports Antigravity model override support, preserves inherited `CODEX_HOME` unless Codex local config is enabled, and wraps stdout write failures with target-specific dispatch exit errors.
- Release tooling now verifies published binary assets before opening the Homebrew tap PR and renders the full binary formula from release checksums.
- CI/local workflow documentation now notes that `make ci` includes `make test-race`, and release docs clarify that tagged migration and ownership manifests are immutable release artifacts.

### Fixed
- Upgrades move `agents.antigravity.agent_specific.model` to `agents.antigravity.model` before strict runtime validation rejects the passthrough key.
- Wizard Git tracking recognizes managed patterns with inline comments and avoids duplicating them.
- Antigravity wizard model options are prefetched before prompting, so the wizard can show live model choices rather than a stale or empty catalog.
- Codex reasoning suggestions and restored shared agent option suggestions now stay valid across CLI, wizard, and dispatch surfaces.
- Codex TOML patching now preserves multiline strings, plugin defaults, inline comments on managed keys, and unrelated shared config while updating Agent Layer-owned entries.
- Strict config validation now fails loudly on malformed nested `AskUserQuestion` overrides and invalid `AL_DISPATCH_ACTIVE` depth values.
- Gitignore management was hardened to prevent managed block data loss and handle inline-comment tracking defaults correctly.
- `al doctor` diagnostics and CI dead-code enforcement were corrected, including loader-error handling that previously weakened the dead-code gate.
- The hidden deprecated `al mcp-prompts` path is now a no-op stub instead of a live prompt delivery surface.
- Release and formula update tests now cover signed binary build outputs, checksum extraction, notarization hooks, and binary Homebrew formula rendering.

### Security
- The Go toolchain/dependency floor was updated to address reachable Go standard-library vulnerabilities found by local vulnerability analysis.
- Codex trust-block generation now rejects invalid UTF-8 repository roots before writing project trust entries.
- The release workflow now checks out code with persisted credentials disabled in the release build job.

### Documentation
- README, site docs, troubleshooting, concepts, reference, FAQ, and security pages were refreshed for current Antigravity, MCP, Codex, upgrade, and release behavior.
- `docs/RELEASE.md` now documents Developer ID signing, notarization, binary Homebrew delivery, release asset verification, and the complete set of required release secrets.
- Agent instruction templates were tightened: production code should validate inputs and returned errors defensively, repeated failed fixes should trigger research before another attempt, and memory files are described as living records of the current working tree.
- Skill design and CLI skill design docs were tightened, and generated workflow skills now include clearer subagent/review constraints.

### Internal
- Shared semver parsing/comparison moved into `internal/version`, and release message helpers were consolidated.
- A reusable TOML patching engine (`internal/tomlpatch`) now powers safer shared-config edits.
- Upgrade migration logic was split to keep skill migration code separate from the main migration coordinator.
- Markdown heading slug generation now avoids quadratic suffix deduplication on large documents.
- Test coverage was expanded across dispatch depth, Antigravity model discovery, Codex shared-config patching, chime cleanup, sync locking, release tooling, wizard option discovery, gitignore tracking, and upgrade migration contracts.

## v0.11.0 - 2026-06-03

Replaces Gemini CLI support with the agy-backed Antigravity integration, adds Agent Dispatch for focused second-agent work, ships explicit opt-in Claude/Codex status lines, expands the wizard into a workflow-bundle, CLI-skill, and deterministic answer-file setup flow, hardens upgrade/CI release workflows, and publishes the public best-practice guides from canonical repository docs.

### Added
- **Agentic status lines for Claude Code and Codex**, explicit opt-in per provider. `al wizard` and interactive `al upgrade` can write `agents.claude.statusline = true` or `agents.codex.statusline = true` and seed missing editable sources once. `al sync` then projects `.agent-layer/claude-statusline.sh` to `.claude/claude-statusline.sh` and wires `statusLine` into `.claude/settings.json`; for Codex it injects a managed `[tui].status_line` block into `.codex/config.toml` from `.agent-layer/codex-statusline.toml` (skipped when you define `agent_specific.tui.status_line` yourself). The Claude line renders model, reasoning effort, context %, weekly usage limit, session, directory, git branch/dirty state, lines changed, and session cost; it requires `jq` on `PATH` and degrades to a one-line hint when absent. Absent or explicit `false` disables the statusline. Existing source files are user-owned: sync, wizard, and non-interactive upgrade never overwrite them; interactive upgrade can show a diff and ask before replacing them.
- `al dispatch` and `al dispatch options` for focused headless second-agent work across Codex, Claude, and Antigravity. Dispatch accepts prompt text from arguments or stdin, can invoke a portable Agent Layer skill with `--skill`, supports target selection via `--agent` (including `random`), reports model/reasoning override support through text or JSON options output, streams target answer text to stdout, writes wrapper status/errors to stderr, and uses stable wrapper-owned exit categories. Agent Layer-launched clients receive `AL_DISPATCH_CALLER_AGENT`; dispatched targets receive `AL_DISPATCH_ACTIVE=1` so nested dispatch is blocked.
- `al wizard` now offers five per-agent **feature disable toggles**, folded into the existing model step (most default to **No**, keeping the client's native behavior; the Codex apps toggle is the exception and defaults to **Yes**, disabling apps): disable Codex browser/computer-use (`features.browser_use`/`in_app_browser`/`computer_use`) and built-in apps (`features.apps`); disable Claude's IDE open-file reading (`env.CLAUDE_CODE_AUTO_CONNECT_IDE`), auto-memory (`autoMemoryEnabled`), claude.ai connectors (`env.ENABLE_CLAUDEAI_MCP_SERVERS`), and the AskUserQuestion tool. The first three Claude toggles and the Codex toggles write their client-native `agent_specific` key only when you opt in; the AskUserQuestion toggle writes a typed `agents.claude.disable_question_tool` flag and `al sync` injects the matching `permissions.deny` entry plus a `PreToolUse` hook into `.claude/settings.json`, **merged with** (never replacing) any deny/hook entries you already have. All toggles read back from existing config so re-running the wizard preserves your choice. The Codex "apps" prompt is reworded to the shared "Disable …?" form (its enabled-state storage is unchanged).
- `al wizard` now asks whether to install or refresh the Agent Layer workflow bundle (instruction files, memory templates/docs, and built-in workflow skills). Bare `al init` creates only operational scaffolding with empty `instructions/` and `skills/`; answering "no" in the wizard leaves existing workflow files untouched. Answering "yes" refreshes managed bundled instruction files and workflow skills, creates missing `04_conventions.md` and memory docs/templates, and preserves existing user-owned conventions and memory files.
- `al wizard` now includes an opt-in CLI skill catalog for `tavily-web`, `playwright-cli`, `find-docs`, and `agent-dispatch`. Selected catalog skills are copied into `.agent-layer/skills/<id>/`; unselected catalog skills are removed. `al doctor` checks installed catalog skills for required binaries and reports missing tools without blocking agent launch.
- `al wizard` now detects MCP servers in `config.toml` that are **not** part of the default catalog and asks about them in a dedicated step, separate from the catalog multiselect. Selected servers stay enabled; unselected servers are set to `enabled = false` with their definition preserved — disabling never deletes a custom server (it has no catalog template to restore from). The step is skipped when there are no custom servers, and the apply summary lists any custom servers being disabled.
- `al wizard --answers <file>` for deterministic JSON-driven wizard runs. Answer files can script select, multi-select, confirm, input, and secret-input prompts; the runner bypasses terminal detection, rejects `--profile`/`--yes` conflicts, validates unknown fields, invalid options, multiple JSON values, missing prompts, and unused answers, and gives e2e coverage a stable way to exercise the real wizard flow without PTY automation.
- `al doctor` now prints a **context size summary** after its checks: estimated instruction tokens, skill catalog-metadata tokens (against a ~4,000-token budget), MCP totals (enabled servers, total tools, total tool-schema tokens), and an estimated **total** of the always-loaded token costs (instructions + skill catalog + MCP tool schemas). Configurable metrics show their configured threshold; the skill catalog shows its fixed token budget. The summary always prints — even when values are under threshold and even with `noise_mode = "quiet"` or `al --quiet doctor` — because it is informational, not a warning. Thresholds left unset show `(no limit set)`; components that can't be measured are named in an `(excludes …)` note on the total, and unreachable MCP servers are excluded from totals with a note. A one-off `al --quiet doctor` run suppresses warning-only doctor output while still surfacing failures.
- Antigravity support via `al agy`, backed by `agy --gemini_dir=<repo>/.agy` and repo-local settings under `.agy/antigravity-cli/`.
- `al probe agy` capability probe reports Antigravity permissions, MCP config migration, and runtime MCP discovery status as JSON.
- Public best-practice website guides for skill design, CLI skill design, and instruction design. The release publisher now generates `/skill-design`, `/cli-skill-design`, and `/instruction-design` from the canonical repository docs with public page headers, and `/best-practices` links the guide set together.
- `config_delete_key` and `config_replace_string` upgrade migration operations and a v0.10.2 migration that moves `agents.gemini.enabled` to `agents.antigravity.enabled`, rewrites MCP client lists from `gemini` to `antigravity`, deletes stale Gemini and retired Antigravity desktop config keys, and defaults Antigravity to disabled when no prior Gemini setting exists.
- `make al-agy` developer convenience target.
- `make lint-ci-local` developer target for no-Docker CI-parity golangci-lint runs. It uses disposable Go build, module, and golangci-lint caches with `GOOS=linux GOARCH=amd64 CGO_ENABLED=0`, and the command is documented in `COMMANDS.md`.

### Changed
- The shipped `al init` template no longer disables Claude Code's **AskUserQuestion** tool by default. Fresh installs now allow the tool (matching Claude Code's native default) instead of seeding `agent_specific.permissions.deny = ["AskUserQuestion"]`. The wizard's new opt-in toggle instead sets `agents.claude.disable_question_tool = true`, and `al sync` injects the `permissions.deny` entry plus a `PreToolUse` hook into `.claude/settings.json` — merged with (never replacing) any deny/hook entries you already have. The hook is what enforces the block under YOLO/`bypassPermissions`, where `permissions.deny` is skipped entirely. Existing repos are unaffected (`config.toml` is never overwritten on upgrade); run `al wizard` to opt back in.
- `al wizard` no longer deletes default MCP servers you unselect. Previously, unticking a catalog default in the wizard pruned its `[[mcp.servers]]` block (and any hand-customization) from `config.toml`. Now an unselected default that already exists is kept with `enabled = false`, matching how custom servers are handled — the wizard never deletes a server block. Selecting a default that is absent still adds it from the embedded catalog; leaving an absent default unselected leaves it absent. Fully removing a server is now a manual `config.toml` edit. Profile/`--yes` runs are unaffected.
- The `al upgrade` migration report no longer prints `[no_op]` rows (migrations that ran but changed nothing because the target was already in the desired state). Only `[applied]` and `[skipped_*]` rows are shown. The report header, target/source versions, and any source-resolution notes still print so diagnostics are preserved. This removes the wall of noise that dominated upgrade output when most migrations were already satisfied.
- The recurring `agent-layer update available` warning (shown on `al sync` and `al <client>` runs) now tells users how to turn it off: set `version_update_on_sync = false` under `[warnings]` in `.agent-layer/config.toml`. The default stays on, and `al doctor`'s update check is unaffected.
- Shipped agent instructions (`01_base.md`) now: direct agents to add real logging/instrumentation to the code to gather evidence on repeated failures instead of guessing; tell agents to zealously preserve context by delegating context-heavy work to subagents; and explicitly encourage scratch scripts and temporary files under `.agent-layer/tmp` for debugging.
- Shipped instruction templates were also tightened: the hard-rule template now leads with grounding unknowns, several generic rules moved out of always-loaded rules, response-style guidance was made explicit, tool-routing now prefers local files/CLIs before MCP or web retrieval when local sources can answer, and relevant skills should be activated automatically from their descriptions.
- The default MCP server catalog now contains the external-tool servers `context7`, `tavily`, `fetch`, and `playwright`. The previous `ripgrep` and `filesystem` catalog entries were removed from the embedded wizard catalog; existing hand-authored MCP server blocks are preserved, and custom servers are now handled by the dedicated custom-server wizard step.
- The release website publisher now stages pages before copying, generates public guide pages from canonical Markdown sources, escapes plain-text angle brackets for MDX safety, builds guide tables of contents from real headings, and keeps versioned-doc publishing idempotent for the target tag.
- CI and release workflows now pin third-party GitHub Actions to immutable commit SHAs with version comments across both workflows.
- Shared skill projection now treats Antigravity as the supported shared-skill consumer in place of Gemini CLI.
- Fresh `al init` now defaults `[agents.antigravity] enabled = false` (the prior `true` default was scoped to the retired Antigravity desktop launcher). Existing repos keep their migrated value from the v0.10.2 migration; users on the retired desktop launcher will have their pre-existing enable flag replaced by the rename from `agents.gemini.enabled` (or the new default if no Gemini config existed). The v0.10.2 row in `site/docs/upgrades.mdx` documents the replacement behavior.
- Claude `reasoning_effort` no longer requires an Opus model. Agent Layer previously failed config validation for `agents.claude.reasoning_effort` unless `agents.claude.model` was an Opus variant — and rejected it outright when the model was unset. That hard error is removed: the value is passed through to Claude Code for any model (including when no model is set), and Claude Code is the authority on which model/effort combinations apply (e.g. Sonnet now supports effort levels). `al wizard` correspondingly offers the reasoning-effort prompt for any enabled Claude model instead of only Opus, and no longer clears the choice when you switch models. Copilot CLI `reasoning_effort` is still rejected, since that client exposes no effort control.
- Wizard and installer status-line source handling now share the same exported source metadata, including canonical paths, legacy Claude source path, template path, and permissions, instead of maintaining separate mappings.

### Fixed
- `al doctor` no longer warns merely because `docs/agent-layer/` is absent in a bare-initialized repo. Bare `al init` intentionally does not create optional workflow memory docs; doctor now warns only when `.agent-layer/instructions/*.md` references `docs/agent-layer` and the directory is missing.
- `al doctor` now reports the configured Copilot CLI enablement state. The default config enables `[agents.copilot_cli]`, but doctor previously omitted that agent row even though sync generated Copilot CLI artifacts.
- Copilot CLI's generated `.copilot/mcp-config.json` now keeps an explicit empty `"mcpServers": {}` object when no Copilot MCP servers are enabled, matching the documented shape and the other MCP client writers.
- `al wizard` now redirects to `al upgrade` when `config.toml` contains a legacy key that only a migration can fix (e.g. a leftover `[agents.gemini]` table). Previously the wizard announced it would help fix the config, then ran `sync`, which re-validated strictly and hard-failed with a raw `config validation failed` error — a dead end, because the wizard's config patch preserves unknown sections verbatim and never runs the rename migration. The wizard now detects this class (a new `ErrConfigNeedsUpgrade` sentinel wrapped by config validation) and prints a clear "run `al upgrade`, then re-run `al wizard`" message, exiting cleanly instead of failing at sync.
- `al doctor`'s Antigravity version check now accepts the bare version string (e.g. `1.0.2`) that `agy --version` prints in Antigravity 1.0.x. Previously the check required an `agy`-prefixed line (`agy 1.0.0`) and reported `[FAIL] Could not parse Antigravity version` against a working install. Multi-line build-timestamp noise is still rejected: the bare form is only accepted when the entire `--version` output is a single version triple.
- Upgrade planning now treats wizard-managed CLI catalog skills as managed files instead of unknown files, and unpinned dev/legacy upgrades can trigger source-agnostic operations across the target-supported manifest chain when legacy Gemini config or missing source-agnostic defaults prove there is migration work to do. Pinned upgrades keep their normal source-to-target chain.
- `al wizard` now reseeds a missing Claude or Codex statusline source when the effective post-wizard config has that provider's statusline enabled, even if you did not re-toggle the setting in the current run. Previously an enabled repo with a deleted source file could complete the wizard and then fail during sync.
- Upgrade diff previews for a missing `.agent-layer/claude-statusline.sh` now show legacy `.agent-layer/statusline.sh` content when that legacy file will seed the new source, and the legacy source is classified as known so upgrade unknown-file scans do not prompt to delete it.
- The Claude statusline now counts untracked-file line changes without spawning one `git diff --no-index` process per untracked file, avoiding prompt stalls in repos with many unignored files.
- The `upgrade-profile-overwrite-claude` e2e scenario now scopes MCP sanitization assertions to the affected server blocks instead of scanning the whole `config.toml`, avoiding future false failures when another server legitimately uses similar fields.

### Removed
- The `al wizard` "Default MCP server entries are missing from config.toml: … Restore them before continuing?" confirm prompt. Missing catalog defaults are now just unselected options in the MCP multiselect — select one to add it, leave it unselected to keep it absent. The prompt was effectively a no-op anyway (it defaulted to "yes" while the multiselect still showed missing defaults unselected, so the rendered config tracked the multiselect, not the answer).
- Gemini CLI sync/client projection, including generated `.gemini/settings.json`, `.gemini/policies/agent-layer.toml`, the global `~/.gemini/trustedFolders.json` write, and the root `GEMINI.md` instruction shim, has been replaced by Antigravity projection. The v0.10.2 migration cleans up any orphan `GEMINI.md` in existing repos. Historical release notes below remain unchanged.
- `al gemini` subcommand removed entirely (no deprecation window). Existing scripts must switch to `al agy`; invoking `al gemini` now produces cobra's standard "unknown command" error.
- Duplicate carry-forward Gemini-to-Antigravity migration operations from the v0.11.0 manifest. Unknown-source upgrades now discover those source-agnostic operations from their original v0.10.2 manifest through the supported-chain planner.
- Internal `buildCodexConfig` test-only shim. Tests now call the System-aware Codex config builder directly.

## v0.10.1 - 2026-05-17

Adds diff-scoped cleanup skills (`prune-new-tests`, `simplify-new-code`) and wires fresh-context reviewer subagents into five existing skills to prevent narrative-driven rationalization. Introduces `al init --here` for in-place installs in subdirectories of existing repos, deep-merges Claude `agent_specific` configuration so `permissions.deny` is additive, auto-writes a Codex per-repo trust stanza, and adds a human-gated merge phase to `ship-pr`.

### Added
- `al init --here` flag installs Agent Layer in the current directory without walking up to an ancestor `.agent-layer/` or `.git`. Lets users add a separate `.agent-layer/` inside a subfolder of an existing repo. When `al init` resolves to an already-initialized ancestor, the error now points at `--here` so the option is discoverable.
- `agent_specific.permissions.deny = ["AskUserQuestion"]` shipped in the install seed (`internal/templates/config.toml`). Fresh `al init` now disables Claude Code's structured clarification-question tool by default; remove the line to keep it. Existing repos are unaffected.
- `prune-new-tests` skill — burden-of-proof pruning of tests added in the current uncommitted diff. Each added test must defend its existence with a concrete production-code mutation that would flip its assertion or it is auto-deleted. Surviving coverage gaps are reported, never backfilled. Uses a fresh-context reviewer subagent so the implementer's narrative cannot rationalize speculative tests into surviving.
- `simplify-new-code` skill — diff-scoped scope-creep removal. Scans the current uncommitted diff for agent-added speculative flexibility, premature abstractions, dead branches, impossible-case error handling, defensive scaffolding, clever patterns, and half-finished work, and auto-applies simplifications while preserving the user-requested behavior. Uses a fresh-context reviewer subagent that identifies scope creep by pattern, not by comparison to the request.

### Changed
- Built-in skill frontmatter descriptions are shorter and more routing-focused, reducing Claude skill listing budget pressure while keeping key trigger language.
- `simplify-code` skill renamed to `simplify-codebase` and scoped explicitly to the codebase (full repository or explicit paths). The implicit "if uncommitted changes exist, scope to the diff" branch is removed — `simplify-new-code` is the diff-scoped sibling. All in-repo callouts updated: diff-context skills (`debug-issue`, `complete-current-phase`, `fix-issues`, `implement-plan`, `audit-and-fix-uncommitted-changes`) now point at `simplify-new-code`; codebase-context skill (`improve-codebase`) points at `simplify-codebase`.
- `implement-plan` skill inserts mandatory cleanup phases between implementation and verification: Phase 4 runs `prune-new-tests` whenever tests were added, Phase 5 runs `simplify-new-code` whenever production code was added or modified, and Phase 6 (renumbered from 4) handles plan-vs-implementation verification.
- `audit-and-fix-uncommitted-changes` skill adds Phase 0.5 (Pre-pass Cleanup) that runs `prune-new-tests` and `simplify-new-code` before any review round, so reviewers don't spend budget on code about to be pruned.
- `verify-against-plan` skill restructured around a plan-anchored, narrative-blind fresh-context reviewer subagent. Phase 2 now delegates plan-vs-implementation comparison to a subagent that sees only the plan and the post-implementation state — never the implementer's narrative, prior conversation, or deviation rationalizations.
- `address-pr-comments` Phase 6 (reply audit) restructured around a fresh-context reviewer subagent. The original author's replies are audited by a subagent that sees only the comment, the reply, and (for `Fixed` verdicts) the named commit's diff — not the agent's prior reasoning when authoring the replies.
- `improve-codebase` Phase 3 per-chunk re-audit restructured around a fresh-context reviewer subagent. The post-fix chunk and the originating findings are the only inputs; the fixer's narrative is excluded so sunk-cost reasoning ("we just fixed that") cannot rubber-stamp incomplete fixes.
- Claude `agent_specific` is now deep-merged into `.claude/settings.json` for object values (arrays and scalars still replace at their key). Previously, top-level objects were replaced wholesale. `permissions.deny` is additive and does not trigger an override warning; `permissions.allow` continues to warn when present.
- Codex sync now writes `[projects."<repo root>"] trust_level = "trusted"` to repo-local `.codex/config.toml`, preserving Codex's exact absolute-path trust semantics without requiring a per-repo `agents.codex.agent_specific.projects` passthrough. The `agent_specific.projects` override warning is path-aware: it only fires when the user's `projects` map contains the managed repo root (real collision), not when it lists unrelated paths that coexist with the managed entry.
- `ship-pr` skill adds a human-gated Phase 9: the agent merges the PR only when the user replies with the exact phrase `I approve merging PR #<N>` matching the run's PR number, using an unambiguous GitHub merge method or pausing for a strategy choice when multiple methods are available; on a successful merge it deletes the source branch locally and remotely. The skill refuses to delete the repository's default branch.
- `ship-pr` skill adds an upfront "Continuation rule" framing sub-skill returns as intermediate, not terminal. Addresses a recurring failure where the orchestrator stopped after `audit-and-fix-uncommitted-changes` returned, mistaking the sub-skill's closeout summary for ship-pr's completion.

## v0.10.0 - 2026-05-07

Consolidates skill projection into a shared `.agents/skills/` directory for non-Claude clients, migrates Gemini sync to the Policy Engine, splits the MCP catalog from the install seed, adds `xhigh` reasoning effort for Claude, and improves the upgrade experience with automatic post-upgrade sync and opt-in diff preview.

### Added
- Skills synced to a shared `.agents/skills/<name>/SKILL.md` tree for non-Claude clients (Codex, Gemini, Antigravity, VS Code/Copilot, Copilot CLI). Per-client directories (`.codex/skills/`, `.gemini/skills/`, `.agent/skills/`, `.vscode/prompts/`, `.github/skills/`) are retired and cleaned automatically by `al sync`. Projection rules and ownership contract documented in `docs/SKILL-CLIENT-SPEC.md`.
- `chat.agentSkillsLocations` written to `.vscode/settings.json` pointing at `.agents/skills/` so VS Code Copilot picks up the consolidated location.
- `xhigh` as a valid `reasoning_effort` value for Claude. Custom (unknown) effort values now pass through with a warning instead of a hard validation error.
- `_generatedBy: agent-layer` provenance field added to generated `.mcp.json` and `.gemini/settings.json` for stronger ownership detection during upgrade readiness checks; replaces the weak `mcpServers`-only signature.
- Internal MCP server catalog (`mcp-catalog.toml`) embedded in the binary and consumed by `al wizard` for its MCP server multiselect; decoupled from the install seed so fresh `config.toml` files start with a minimal `[mcp]` section.

### Changed
- Gemini sync migrates from the deprecated `tools.allowed` field to the Policy Engine. `WriteGeminiSettings` no longer emits `tools.allowed` in `.gemini/settings.json` and instead writes a `policyPaths: [".gemini/policies"]` pointer; `WriteGeminiPolicies` generates `.gemini/policies/agent-layer.toml` with one `[[rule]]` block per allowed command (`toolName = "run_shell_command"`, `commandPrefix`, `decision = "allow"`, `priority = 100`, `allowRedirection = true`). `allowRedirection = true` preserves the previous `tools.allowed` behavior for headless workflows that pipe output (e.g., `git ... > file`). Resolves the Gemini CLI deprecation warning.
- `al upgrade` now runs `al sync` automatically on success so retired projection paths and freshly-introduced templates are reconciled without a manual follow-up. Sync warnings surface on stderr; sync failures are wrapped (`upgrade applied; sync failed: <err> (run \`al sync\` to retry)`) with `errors.Is` preserved, and the "Upgrade successful." banner is suppressed when sync fails so the failure is unmissable.
- `al upgrade` overwrite prompts now show a compact summary (file path with `+N -M` line stats, colorized when output is a terminal) and ask "View the full diff?" (default no) before printing unified diff bodies.
- `.agent-layer/tmp/` excluded from pre-upgrade snapshots; rollback does not restore tmp content. Interactive bulk-delete prompt is scoped to non-tmp content; a separate grouped prompt handles tmp unknowns. Non-interactive deletion requires `--apply-tmp-deletions`; `--apply-deletions` alone never touches tmp.
- `config.toml` install seed no longer includes inline MCP server entries; the `[mcp]` section now points to `al wizard` for server configuration. Existing repos are unaffected.
- Stale `.claude/skills/` and `.gemini/skills/` directories flagged for cleanup during upgrade readiness when those agents are disabled.

### Fixed
- `.mcp.json` no longer omits the `mcpServers` key when no servers are configured.
- Rollback no longer silently wipes `.agent-layer/tmp/` content that was excluded from the snapshot.
- Grouped tmp-unknowns deletion fallback fixed when `DeleteUnknownTmpAllFunc` is not wired in `PromptFuncs`.
- Env preview redaction edge cases in `al wizard` hardened.
- Doctor and wizard previews hardened for additional edge cases.

### Removed
- `tools.allowed` field from generated `.gemini/settings.json`. The next `al sync` rewrites the file in the new shape, removing the deprecated key.
- Per-client skill directories (`.codex/skills/`, `.gemini/skills/`, `.agent/skills/`, `.vscode/prompts/`, `.github/skills/`) retired; `al sync` cleans these paths and writes to `.agents/skills/` instead.

## v0.9.2 - 2026-03-21

Adds GitHub Copilot CLI as a supported agent client, introduces context files for plan/task artifacts, supports `max` reasoning effort for Claude Opus, and consolidates internal abstractions for cleaner dependency injection. Instructions and skills are improved for better autonomy and tradeoff handling.

### Added
- GitHub Copilot CLI integration: new `al copilot` command, sync support, config fields (`[agents.copilot_cli]`), wizard catalog entries, doctor checks, and v0.9.2 migration manifest. Stale Copilot artifacts (`.copilot/mcp-config.json`, managed skill dirs under `.github/skills/`) are cleaned when the agent is disabled.
- Context file (`.context.md`) for plan/task artifact system. Captures key file paths, current state, constraints, and an entry point so implementing agents can orient immediately without re-discovering what the planner found. Produced by `plan-work` (Phase 3b), consumed by `implement-plan`, `review-plan`, `verify-against-plan`, and other plan-aware skills.
- `max` as a valid `reasoning_effort` value for Claude Opus models. Since Claude Code only supports `max` as a session-scoped CLI flag, all effort values are now passed via `--effort` and `max` is excluded from `settings.json` sync.
- Memory hygiene improvements in instruction and doc templates: "What NOT to store" section in `02_memory.md`, character-budget awareness rule (~8k chars / ~2k tokens per file), completed-phase archival guidance in `ROADMAP.md` template, and entry-ID placeholder update from "abcdef" to "short-slug".
- Explicit "Upgrade successful." message when `al upgrade` completes, resolving ambiguous output on no-op completions.
- Nil guard for `sys.HTTPClient()` in `downloadHTTPClientWithSystem`, falling back to `defaultHTTPClient` when the System implementation returns nil.
- `INSTRUCTION-DESIGN.md` internal reference document for instruction authoring principles.

### Changed
- `--effort` CLI flag now respects `agent_specific.effortLevel` override: when the override is set, the managed `--effort` arg is skipped so the user's setting takes precedence.
- Model catalogs updated: added `gemini-3.1-flash-lite`, `opus[1m]`, `gpt-5.4`; removed deprecated `gemini-2.0-*`, `gpt-5/5.1-*`, `claude-sonnet-4.5`.
- Instruction quality improvements: removed rules that duplicate baseline model behavior, strengthened tradeoff protocol to require at least two options with pros/cons, added memory pruning rule for DECISIONS.md and CONTEXT.md.
- `audit-documentation` and `audit-tests` skills rewritten to fix autonomously with human checkpoints for genuine tradeoffs instead of dual report-only/fix-mode pattern. `audit-tests` now autonomously deletes rubber-stamp tests, consolidates duplicates, and combats agent-caused test bloat. All 22 skills receive a tradeoff checkpoint for standalone distribution.
- `ship-pr` skill fixes two edge cases: on a non-default branch with no uncommitted changes, proceeds to create the PR; on the default branch with uncommitted changes, creates a new branch before committing.
- Dispatch System interface expanded: replaced package-level mutable function stubs (`osStat`, `osChmod`, `osRename`, `lockFileFn`, `flockFn`, `dispatchSleep`, `httpClient`, etc.) with methods on the System interface for proper dependency injection.
- Duplicate abstractions consolidated: approval mode constants unified into `config.ApprovalMode*`, agent-enabled helpers unified into `config.IsAgentEnabled`.
- Skills docs page restructured: orchestrator/primary/supporting tiers with recommended workflow section, separated universal skill standard from Agent Layer-specific features.
- Gitignore template updated to include `open-vscode.sh` and compiled `al` binary.

### Fixed
- `--effort` flag no longer shadows user's `agent_specific.effortLevel` override in Claude Code.
- `ship-pr` no longer stalls on a non-default branch with no uncommitted changes and no longer tries to commit on the default branch without creating a feature branch first.
- Nil panic prevented when `System.HTTPClient()` returns nil in download paths.
- **Security:** Updated `github.com/modelcontextprotocol/go-sdk` from v1.4.0 to v1.4.1 to fix improper handling of null Unicode character when parsing JSON (high severity).

### Improved
- Expanded automated test coverage across Copilot CLI sync, dispatch System interface, model catalogs, gitignore templates, effort flag paths, and instruction template assertions.

## v0.9.1 - 2026-03-07

Overhauls the built-in skill library from 10 to 22 structured, workflow-driven skills and introduces a user-managed conventions file so you can tailor project-specific rules without losing them on upgrade. Instructions are reordered, deduplicated, and compressed for better agent compliance.

### Added
- New user-managed `04_conventions.md` instruction template for project-specific conventions (architecture, code quality, data safety, time/data, environment). Seeded on `al init`, never overwritten on `al upgrade`; future convention updates delivered via `append_to_file` migrations with duplicate detection.
- New `append_to_file` migration kind for delivering content to user-managed files without overwriting edits. Supports duplicate-detection via match string, automatic file creation, and atomic writes.
- New `delete_file` migration operations for removing deprecated skill directories during upgrade.
- 17 new built-in skills following a normalized workflow-driven structure with explicit phases, global constraints, guardrails, and human checkpoints: `address-pr-comments`, `audit-and-fix-uncommitted-changes`, `audit-memory`, `audit-tests`, `complete-current-phase`, `debug-issue`, `fix-ci`, `implement-plan`, `improve-codebase`, `plan-work`, `repair-checks`, `resolve-findings`, `review-scope`, `schedule-backlog`, `ship-pr`, `simplify-code`, `verify-against-plan`.
- Site documentation page for built-in skills (`site/docs/skills.mdx`) covering the full 22-skill library organized by category with usage examples and customization guidance.
- Site documentation page for evidence-based skill design (`site/pages/skill-design.mdx`) with 5 core design principles and 18 academic/industry citations.
- Internal skill authoring reference (`docs/SKILL-DESIGN.md`) and audit workflow specification (`docs/SKILL-AUDIT.md`).

### Changed
- Instruction files reordered for primacy effect: `02_rules.md` → `00_rules.md`, `00_base.md` → `01_base.md`, `01_memory.md` → `02_memory.md`. Hard constraints now load first to improve model compliance. `al upgrade` migrates existing repos via `rename_file` operations.
- Cross-file instruction duplicates removed: 6 items that appeared in multiple instruction files consolidated to a single canonical location, reducing ~57 instructions to ~50.
- Verbose instruction sections compressed (~50% fewer tokens in Critical Protocol, Workflow & Safety, and Tools sections) without removing guidance.
- UTC-only internals and No system Python rules moved from `02_rules.md` to `04_conventions.md` as project-specific conventions (delivered to existing users via `append_to_file` migrations).
- 5 existing built-in skills restructured to normalized workflow pattern: `audit-documentation`, `boost-coverage`, `finish-task`, `fix-issues`, `review-plan`.
- Decision hygiene guidance updated: superseded decisions should be replaced (not accumulated), and entries that become self-evident from the codebase should be removed.
- Skill loader error message for directories missing a skill file shortened from "missing SKILL.md or skill.md" to "has no SKILL.md".
- Site docs sidebar positions updated to accommodate new Skills page; upgrade-checklist shell examples consolidated.

### Removed
- 5 built-in skills replaced by normalized workflow equivalents: `cleanup-code` (→ `simplify-code`), `continue-roadmap` (→ `complete-current-phase`), `find-issues` (→ `resolve-findings`), `fix-tests` (→ `repair-checks`), `update-roadmap` (→ `schedule-backlog`). Migration manifest deletes these (plus `mechanical-cleanup`) from user directories during upgrade.

### Improved
- Expanded automated test coverage for user-owned instruction files (seed-on-init, no-overwrite-on-upgrade, excluded-from-diffs), `append_to_file` migration paths (apply, no-op, file creation, rollback), normalized workflow skill structure validation, artifact naming conventions, and skill deletion migration scenarios.

## v0.9.0 - 2026-03-01

### Added
- Added a reusable `internal/skillvalidator` package with parse/validate separation and deterministic findings for agentskills.io-aligned skill validation.
- Added `al doctor` skills diagnostics for standards checks (unknown frontmatter keys, name/path mismatches, and non-canonical directory filenames such as `skill.md`).
- Added release manifests for `v0.9.0`: `internal/templates/migrations/0.9.0.json` and `internal/templates/manifests/0.9.0.json`.
- Added `al doctor` check for stale flat-format skill files (`.md` at skills root) with guidance to run `al upgrade`.
- Added `CONTEXT.md` memory file template for general-purpose project context, domain concepts, naming conventions, and lessons learned.
- Added data-driven breaking-change display: migration manifests now carry `breaking`, `breaking_notice`, and `breaking_details` fields, and the upgrade report renders them generically instead of hardcoding per-kind display logic.
- Added yellow highlighting for readiness warnings, file-removal counts, and review-needed items in upgrade plan and upgrade output for improved scanability.
- Upgrade snapshot rollback now accepts snapshots in `created` status, enabling recovery from interrupted upgrades that failed before reaching `applied` status.

### Changed
- Renamed legacy "slash command" source and output terminology to "skills" across config, sync pipelines, templates, and docs.
- Canonicalized source layout to `.agent-layer/skills/`, with migrations that rename legacy `.agent-layer/slash-commands/` and embedded skill template paths.
- **Breaking:** Flat-format skills (`<name>.md`) are no longer supported by the skill loader. All skills must use directory format (`<name>/SKILL.md`). `al upgrade` migrates both built-in and user-authored skills automatically via a single `migrate_skills_format` operation with pre-flight conflict detection and user confirmation.
- Skill frontmatter parsing/generation now uses YAML (`go.yaml.in/yaml/v3`) with support for `name`, `description`, `license`, `compatibility`, `metadata`, and `allowed-tools`, while keeping unknown fields parse-tolerant for portability.
- Increased skill parser/validator single-line scanner caps to `8 MiB` to reduce token-limit failures on large single-line skill content.
- Documentation now explicitly states that missing or empty skill `description` is load-enforced (fail-loud), while missing `name` remains backward-compatible with doctor warnings.
- Skills migration user-facing copy updated to "Slash-commands renamed to skills" with data-driven breaking-change notices sourced from the migration manifest.
- Unknown-file scanning now covers both `.agent-layer/` and `docs/agent-layer/`, with a fresh post-migration re-scan so the unknown-file prompt reflects actual post-migration state instead of stale pre-migration paths.
- Migration-covered diff suppression now uses ancestor-directory matching, so migrations that own an entire directory (e.g., skills format migration) suppress noisy per-file template diffs in plan output.
- Git safety instruction now clarifies that commit/push authorization applies only to the specific request and does not carry forward.

### Fixed
- `al doctor` lenient-config fallback now best-effort loads skills, preventing false "No skills configured" results when strict config validation fails.
- Skill name handling is now Unicode NFKC-aware across loading and validation paths, preventing false duplicates/mismatches for normalization-equivalent names.
- Skill metadata and text limits now use rune counts (not bytes), and validation now rejects empty names and non-ASCII digit forms in slug normalization.
- Directory-format loading now accepts lowercase `skill.md` as a compatibility fallback while preserving canonical `SKILL.md` precedence.

### Removed
- Removed `docs/agent-layer/SKILLS_WORKFLOWS.md`; workflow guidance is now provided by individual skill sources.

### Improved
- Expanded automated coverage across skill loading/validation, upgrade migrations, prompt generation, install ownership/readiness checks, and docs surfaces touched by the skills-standard migration.
- Updated default embedded skills to canonical agentskills directory format (`skills/<name>/SKILL.md`) and aligned memory/workflow docs for Phase 15 completion.

## v0.8.8 - 2026-02-25

### Added
- Added embedded upgrade migration manifest support for `v0.8.8` to canonicalize the Claude VS Code agent key from `agents.claude-vscode.enabled` to `agents.claude_vscode.enabled` and enforce a default for the canonical key.
- Expanded branch-coverage suites across upgrade/install, dispatch, wizard PTY interactions, and command paths to harden release confidence.

### Changed
- Canonical config/docs naming now uses `[agents.claude_vscode]` (snake_case). Legacy `[agents.claude-vscode]` remains accepted at load time and is migrated during upgrade.
- Release and upgrade-doc validation checks are stricter, including stronger release workflow/docs contract assertions and stable-tag publishing guardrails.
- `cmd/publish-site` release version-retention and versioned-doc pruning behavior is more robust for deterministic stable release publication.

### Fixed
- Made dispatch cache sync-error coverage deterministic by injecting sync failures in tests instead of relying on platform-specific `/dev/null` behavior.
- Aligned upgrade/readiness messaging and docs references to the canonical `claude_vscode` key to avoid mixed-key guidance.

### Improved
- Removed obsolete Claude-specific GitHub workflow files in favor of the current release/verification workflow set.
- Updated README and site docs for key-name consistency and release guidance clarity.

## v0.8.7 - 2026-02-24

### Added
- `al <client>` commands now support `--quiet` / `-q` for one-off quiet runs that suppress Agent Layer informational output while preserving client output and error exit behavior.
- Added end-to-end coverage for quiet Claude runs to ensure `al claude --quiet` emits no Agent Layer output and still launches correctly.
- Wizard back-navigation support: pressing `Esc` now moves to the previous step, with explicit first-step exit confirmation and deterministic state rollback behavior for partial selections.
- Wizard `Ctrl+C` now exits immediately without saving, distinct from `Esc` (back). Both keys are shown as hints in the bottom navigation bar (`esc back • ctrl+c exit`).
- Config guardrail test for required fields: automated enforcement now checks that newly required config fields have matching `config_set_default` migration coverage (with explicit legacy baseline allowlist).
- Claude reasoning-effort support in wizard/config/sync paths (`low|medium|high`), including projection into `.claude/settings.json` as `effortLevel`.

### Changed
- `warnings.noise_mode` now supports `quiet` in addition to `default` and `reduce`.
- Quiet handling is now applied consistently across dispatch and client launch paths, including argument forwarding and no-sync execution.
- Wizard-managed `config.toml` writes now use an explicit preferred section order policy (`approvals`, enabled agents, `mcp`, `warnings`) instead of implicitly coupling ordering to template parse order.
- Upgrade diff output is now colorized in interactive terminals for better scanability (adds/removes/hunks), with plain-text fallback preserved for non-interactive and no-color environments.
- Wizard profile apply flow now warns when replacing an existing TOML-corrupt `.agent-layer/config.toml`.

### Fixed
- `al sync` warning-only outcomes in quiet mode now preserve non-zero exit behavior without printing warning text.
- Quiet-mode behavior now avoids leaking dispatch/update-check banners when quiet is enabled via flag or config.
- Prevented hidden behavior drift in multiline TOML patching by refactoring duplicated state tracking into a shared iterator with regression coverage across call sites.
- Gemini `reasoning_effort` is now rejected explicitly with a clear validation error instead of allowing ambiguous/unsupported behavior.
- Claude reasoning-effort validation now fails loudly for invalid option values and unsupported model selections.

### Improved
- Documentation and default config comments now describe quiet-mode behavior and its interaction with `al doctor` (which always prints warnings).
- Validation and warning messaging now includes `quiet` as a first-class supported noise mode.
- Expanded test coverage for wizard back-navigation state transitions, profile corruption warning paths, reasoning-effort capability validation, Claude sync projection behavior, and upgrade diff color/no-color rendering.
- Added PTY integration tests for wizard Esc/Ctrl+C keystroke classification, validating the full chain from raw terminal bytes through bubbletea to error classification.
- Updated project memory/docs (`ISSUES.md`, `BACKLOG.md`, `ROADMAP.md`, `DECISIONS.md`, `README.md`) to reflect completed sprint scope and release-facing behavior.

## v0.8.6 - 2026-02-23

### Added
- `al wizard` now prompts to enable per-repo Claude settings and caches isolation (`local_config_dir`) when Claude or Claude VS Code is enabled. Default is `false` (shared global config); selecting `true` sets `CLAUDE_CONFIG_DIR` to a repo-local directory for separate settings and caches per repository.

### Fixed
- `.gitignore` template inline comments on `/.claude/` and `/.claude-config/` patterns were treated by Git as part of the literal pattern, causing both directories to not be gitignored. Comments moved to their own lines. Affected users (v0.8.5 installs): run `al upgrade` or `al sync` to pick up the corrected template.

### Improved
- Per-repo isolation is now documented as a core feature in the README comparison table, key properties, and a dedicated [Per-repo credential isolation](https://conn-castle.github.io/agent-layer-web/docs/concepts#per-repo-credential-isolation) section in the site concepts page (Codex auth isolation plus Claude settings and caches isolation; Claude auth remains shared due to an upstream limitation).

## v0.8.5 - 2026-02-23

### Added
- Optional agent-specific passthrough configuration for Claude and Codex via `agents.claude.agent_specific` and `agents.codex.agent_specific`.
- Optional `agents.claude.local_config_dir` support for repo-local Claude config isolation (`.claude-config`). `al vscode` sets `CLAUDE_CONFIG_DIR` only when both `local_config_dir = true` and `agents.claude-vscode.enabled = true`.
- `al upgrade rollback --list` support to inspect available snapshot IDs and statuses before executing rollback.
- `al sync` now auto-adds the repository root to `~/.gemini/trustedFolders.json` when Gemini is enabled so Gemini CLI reliably loads project-level `.gemini/settings.json`. If this write fails, sync still succeeds and emits a non-fatal warning with manual remediation guidance.

### Changed
- Agent-specific passthrough keys intentionally override Agent Layer-managed keys when they collide, with sync warnings to keep overrides explicit.
- `al vscode` now clears `CLAUDE_CONFIG_DIR` only when it points at the repo-local `.claude-config`; user-defined non-repo values are preserved.
- Codex sync now writes `agents.codex.agent_specific` keys before managed MCP tables so top-level overrides remain at the TOML root.

### Fixed
- Config parsing now rejects unrecognized keys during strict decode instead of silently ignoring them, with actionable validation guidance in the returned error.
- `.env` parsing now correctly handles quoted values, escaped newline/carriage-return sequences, and invalid trailing characters after quoted values.
- MCP tool-name collision warnings are deterministic: warning subjects and per-tool server lists are sorted for stable output.
- Upgrade `config_set_default` prompts no longer mark one choice as "recommended"; migration manifest values are still pre-selected but users must make an explicit choice.
- `al upgrade` now accepts zero-byte snapshot file entries, fixing failures where unknown empty files previously produced `requires content_base64` errors.
- `al gemini` no longer fails MCP discovery for the internal `agent-layer` server when PATH `al` is a non-runnable repo-pin shim. Prompt-server command resolution now prefers local source execution (`go run <repo>/cmd/al mcp-prompts`) when available, `al mcp-prompts` bypasses repo-pin dispatch, and prompt-server source roots are validated as the Agent Layer module.

### Improved
- Expanded automated coverage for root/upgrade command paths, Gemini trust flows, upgrade-readiness helpers, warning policy branches, prompt-server root resolution, dispatch behavior, and codex/claude agent-specific config rendering.
- Added e2e coverage for upgrade flows that include empty unknown files and for wizard MCP sanitization behavior when profile defaults disable an injected server block.
- Website documentation now includes `al upgrade rollback --list` guidance, Gemini trusted-folder remediation notes, and expanded agent-specific and repo-local Claude configuration guidance.

## v0.8.4 - 2026-02-20

### Fixed
- Version dispatch no longer prints the version-source diagnostic twice when a pinned or environment-overridden version dispatches to a cached binary. The diagnostic now prints only from the binary that actually runs the command.

### Changed
- Config validation now silently strips transport-incompatible MCP server fields instead of rejecting the config with an error. For example, `headers` on a stdio server or `command`/`args` on an HTTP server are removed during validation rather than causing a load failure. This makes configs more resilient to leftover fields from transport changes or manual editing.

### Improved
- `al wizard` config patching now removes dotted sub-key lines (e.g., `headers.Authorization = "Bearer ..."`) when stripping a parent key like `headers`. Previously, only inline-table syntax (`headers = { ... }`) was handled.
- Scenario-based end-to-end test framework with 26 scenarios and 436 assertions replaces the previous monolithic e2e script. Covers fresh install, wizard profiles, upgrade paths, error propagation, agent launch, and rollback workflows with mock agent binaries.

## v0.8.3 - 2026-02-19

### Fixed
- `al doctor` lenient config fallback now injects built-in environment variables (e.g., `AL_REPO_ROOT`), fixing false "missing environment variables" warnings for MCP servers like `filesystem` that reference `${AL_REPO_ROOT}` in their args.
- `al wizard` now sanitizes transport-incompatible MCP server fields during config patching. For example, leftover `headers` on a stdio server or leftover `command`/`args` on an HTTP server are automatically removed. Previously, the wizard would complete successfully but `al sync` would fail with a validation error, creating a circular "run wizard to fix" loop.

## v0.8.2 - 2026-02-18

### Added
- Migration manifest chaining: `al upgrade` now loads all intermediate migration manifests between the source and target versions during multi-version jumps. Users upgrading from 0.8.0 to 0.8.2 will receive migrations introduced in intermediate releases.
- Config resilience: `al wizard`, `al doctor`, and `al upgrade` now use lenient config parsing so they always work even on broken or incomplete configs. Runtime commands remain strict with actionable guidance.

### Changed
- The `agents.claude-vscode.enabled` config migration has been moved from the v0.8.1 manifest to v0.8.2. This ensures all users (including those who installed v0.8.1 before the migration was added) receive the prompt during upgrade.

### Fixed
- Users jumping multiple versions (e.g., 0.8.0 to 0.8.2) no longer miss intermediate migration operations.

### Removed
- Slash command `auto-approve` frontmatter. Approval permissions are controlled entirely by `approvals.mode`.

### Improved
- Slash command frontmatter now rejects unrecognized keys with a clear error, catching typos and unsupported fields early.

## v0.8.1 - 2026-02-18

### Added
- `[agents.claude-vscode]` config section for Claude Code VS Code Extension support. `al vscode` is the single command for launching VS Code with both Codex and Claude extension settings based on which agents are enabled.
- `approvals.mode = "yolo"` for maximum agent autonomy: skips all permission prompts (Claude `--dangerously-skip-permissions`, Gemini `--approval-mode=yolo`, Codex `approval_policy=never` + `sandbox_mode=danger-full-access`, VS Code `chat.tools.global.autoApprove`). Intended for sandboxed/ephemeral environments.
- Slash command `auto-approve` frontmatter: `auto-approve: true` auto-approves MCP prompt retrieval for that skill in Claude clients. It does not auto-approve agent actions after reading the prompt; those remain governed by `approvals.mode`.
- Upgrade readiness checks now detect stale disabled-agent artifacts for `.claude/settings.json`.

### Changed
- `al vscode` now launches when either `[agents.vscode]` or `[agents.claude-vscode]` is enabled in `config.toml`, unifying both extensions under a single launch command.
- `CODEX_HOME` is now cleared from the VS Code process environment when `[agents.vscode]` is disabled, preventing stale Codex configuration leakage when only Claude VS Code is enabled.
- Release process now includes a preflight gate (`make release-preflight RELEASE_TAG=vX.Y.Z`) that validates upgrade-contract documentation before tagging.

### Fixed
- `al vscode` no longer appends `.` when pass-through arguments include a positional path or file argument. (#51)
- Sync warning exit behavior restored: non-suppressible warnings correctly propagate exit status regardless of `noise_mode` setting.

## v0.8.0 - 2026-02-16

### Added
- New upgrade command surface centered on `al upgrade`: `al upgrade plan` (dry-run preview), `al upgrade rollback <snapshot-id>` (manual restore), `al upgrade prefetch --version X.Y.Z` (cache warm-up), and `al upgrade repair-gitignore-block` (managed block repair).
- `al upgrade plan` now produces plain-language categorized upgrade previews with readiness checks and line-level diff previews (default 40 lines per file, configurable via `--diff-lines`).
- Upgrade planning ownership inference is now deterministic and offline using committed release manifests (`internal/templates/manifests/*.json`) plus repo baseline state (`.agent-layer/state/managed-baseline.json`).
- `al upgrade` now creates managed-file snapshots under `.agent-layer/state/upgrade-snapshots/` and automatically rolls back transactional changes when an upgrade step fails.
- Embedded per-release migration engine: `al upgrade` executes migration manifests (`internal/templates/migrations/<target>.json`) before template writes and emits deterministic migration reports.
- `al wizard` now supports non-interactive profile mode (`--profile`, optional `--yes`) and backup cleanup (`--cleanup-backups`).
- Warning noise control added via `warnings.noise_mode` (`default` or `reduce`) to reduce non-critical suppressible warning output when desired.

### Changed
- **Breaking:** `al init` is now one-time scaffolding only. If `.agent-layer/` already exists, `al init` errors and directs users to `al upgrade plan` + `al upgrade`.
- **Breaking:** `al upgrade` non-interactive execution now requires `--yes` plus explicit apply flags (`--apply-managed-updates`, `--apply-memory-updates`, `--apply-deletions`) to make mutation intent explicit.
- `.agent-layer/.env` and `.agent-layer/config.toml` are now strictly user-owned: seeded only when missing and never overwritten by init/upgrade operations.
- `.agent-layer/.gitignore` is treated as internal agent-owned state: always rewritten from templates and excluded from upgrade plans/diff prompts.
- Default MCP server templates now pin concrete tool versions by default, with inline floating/latest opt-in examples for teams that want automatic upstream updates.
- Update-check handling now degrades gracefully on GitHub API rate limits so init/doctor flows continue with actionable warning output.
- Release process now requires generating and committing a per-tag template ownership manifest before tagging (`./scripts/generate-template-manifest.sh --tag vX.Y.Z`).

### Fixed
- `al upgrade rollback <snapshot-id>` now rejects path separators in snapshot IDs, preventing path traversal attempts during manual rollback resolution.
- `al vscode` launch preflight now fails fast with explicit guidance when the `code` CLI is missing on `PATH` or when `.vscode/settings.json` has a managed-block marker conflict.

### Removed
- **Breaking:** `al init --overwrite` and `al init --force` have been removed. Use `al upgrade plan` and `al upgrade` for upgrades/repairs.
- **Breaking:** `al upgrade --force` has been removed. Use explicit apply flags (plus `--yes` for non-interactive runs) to select mutation categories.
- **Breaking:** `al upgrade plan --json` has been removed; text output is now the only supported plan interface.

## v0.7.0 - 2026-02-07

### Added
- Upgrade contract published at `site/docs/upgrades.mdx`: defines upgrade event categories (`safe auto`, `needs review`, `breaking/manual`), sequential compatibility guarantees (`N-1` to `N`), release-versioned migration rules, and OS/shell capability matrix.
- Release gate validates upgrade documentation for each release tag (`make docs-upgrade-check`), ensuring migration table rows exist and placeholder text is replaced when changelog notes breaking changes.
- `al init --version latest` resolves the latest GitHub release to a semver pin before writing `.agent-layer/al.version`.
- `al init --version X.Y.Z` validates the release exists on GitHub before writing the pin file, failing with a clear "release not found" message instead of writing a pin that 404s on next use.
- `al init` auto-recovers from empty or corrupt `.agent-layer/al.version` pin files with a warning instead of blocking all commands.
- Binary download progress indicator: `ensureCachedBinary` emits "Downloading al vX.Y.Z..." / "Downloaded al vX.Y.Z" to stderr.
- Actionable error messages for binary download failures (404 not-found and timeout scenarios).

### Changed
- **Breaking:** Windows support removed. Deleted `al-install.ps1` installer, Windows release target, `open-vscode.bat` launcher, and all Windows-specific code paths in dispatch, cache, exec, and lock packages. Windows was never tested and best-effort support eroded trust. macOS and Linux remain fully supported.
- `al init` now bypasses repo-pin binary dispatch and always executes on the invoking CLI binary, preventing older pinned versions from running upgrade operations.
- Launcher template writes refactored for reliability with proper macOS path escaping.
- Codex MCP header projection order corrected.
- CI workflow caches pinned tools in GitHub Actions for faster builds.
- Upgrade contract linked from README, site docs, DEVELOPMENT.md, and RELEASE.md.

### Removed
- `al-install.ps1` (Windows PowerShell installer).
- `open-vscode.bat` (Windows VS Code launcher).
- Windows release targets (`windows/amd64`) from build scripts.
- Windows-specific dispatch, cache, exec, and lock code paths.

## v0.6.1 - 2026-02-06

### Added
- CLI argument forwarding: `al <client>` now forwards extra arguments to the underlying client. Use `--` to separate Agent Layer flags from client arguments (e.g., `al claude -- --help` or `al vscode --no-sync -- --reuse-window`).
- VS Code launchers are now created during `al init` in addition to `al sync`, so launchers are available immediately after initialization.
- `.gitignore` managed block is now updated during both `al init` and `al sync` operations for consistency.

### Fixed
- `AL_SHIM_ACTIVE` environment variable no longer leaks into VS Code's integrated terminal when launching via `al vscode`. Previously, this caused subsequent `al` commands in the terminal to fail with "version dispatch already active" errors. (#46)
- Wizard now rewrites `config.toml` sections in the template-defined canonical order, preventing section ordering drift after multiple wizard runs.

### Changed
- Launcher code moved to `internal/launchers` package with exported `EnsureGitignore` for cross-package use.
- Documentation updated with clearer guidance on gitignore template format, wizard behavior, and troubleshooting MCP server startup on macOS.

## v0.6.0 - 2026-02-03

### Added
- Documentation website with comprehensive guides covering getting started, concepts (approvals, MCP servers, project memory, version pinning), reference (CLI, configuration, environment variables), and troubleshooting.
- Website publishing pipeline (`cmd/publish-site`) with automated deployment in the release workflow.
- Playwright MCP server template in default `config.toml` for browser automation workflows.
- Descriptive comments for all default MCP server templates explaining purpose and required credentials.
- Claude Code VS Code Extension added to supported clients table in README.

### Changed
- README rewritten with clearer value proposition, comparison table (manual vs Agent Layer), and improved quick start flow.
- Default MCP server examples in README now use generic `example-api` instead of GitHub-specific config for clarity.
- Documentation structure consolidated from nested pages to flat MDX files for better navigation.

## v0.5.8 - 2026-01-30

### Changed
- **Breaking:** Environment variables now require `AL_` prefix to avoid conflicts with shell environment (e.g., `GITHUB_PERSONAL_ACCESS_TOKEN` → `AL_GITHUB_PERSONAL_ACCESS_TOKEN`). This ensures Agent Layer variables don't override existing environment variables when VS Code terminals inherit the process environment.

### Fixed
- VS Code `open-vscode.app` launcher now uses `osascript` with a login shell (`zsh -l`) instead of hardcoded VS Code CLI paths, fixing launch failures when VS Code is installed via Homebrew, in `~/Applications`, or other non-standard locations. This also fixes MCP server failures where VS Code couldn't find `node` because Finder-launched apps have a minimal PATH.
- All VS Code launchers (`.app`, `.command`, `.bat`, `.desktop`) now delegate to `al vscode` for loading `.agent-layer/.env`, ensuring consistent parsing (KEY=VALUE data, not sourced) across platforms. Only `AL_*` variables with non-empty values are loaded, and existing environment variables take precedence—matching the documented behavior for `al` commands.
- VS Code `.app` launcher now shows a descriptive alert when the `code` command is not found, instead of silently failing.
- Linux `.desktop` launcher simplified to delegate to `.command` script for consistent behavior and maintainability.

## v0.5.7 - 2026-01-29

### Added
- Custom HTTP header support for Codex MCP servers: `bearer_token_env_var` for `Authorization: Bearer ${VAR}`, `env_http_headers` for other env-var-sourced headers, and `http_headers` for static literals.
- `X-MCP-Tools` header in default GitHub MCP server template for server-side tool filtering, reducing projected tool count.
- Detailed per-tool token breakdown in `al doctor` MCP schema bloat warnings, showing top contributors by token count.
- Documentation for MCP HTTP header projection across all supported clients (`docs/MCP_HEADERS_SUPPORT.md`).

### Changed
- Default MCP schema token thresholds increased to accommodate larger MCP servers (server: 7500→20000 tokens, total: 10000→30000 tokens).
- Doctor command now shows real-time discovery progress when checking MCP servers.
- Large internal modules (`install`, `dispatch`, `config`) split into smaller, focused files for maintainability.
- golangci-lint upgraded to v2.8.0 with additional linting rules enabled.

## v0.5.6 - 2026-01-27

### Added
- `http_transport` config option for HTTP MCP servers to specify transport mode (`streamable` or `sse`).
- Three new MCP server templates in default `config.toml`: `fetch` (mcp-server-fetch), `ripgrep` (mcp-ripgrep), and `filesystem` (server-filesystem with repo-scoped access).
- `${AL_REPO_ROOT}` built-in variable for resolving repository root path in MCP server args.
- VS Code settings sync now preserves existing user settings and comments using JSONC-aware block insertion instead of overwriting the entire file.
- Memory file templates (`BACKLOG.md`, `COMMANDS.md`, `DECISIONS.md`, `ISSUES.md`, `ROADMAP.md`) now include detailed formatting guidelines and entry templates.

### Changed
- MCP projection refactored: new `internal/projection/resolvers.go` module centralizes server resolution logic, used by both sync and warning checks.
- Update-available warning now includes full upgrade instructions for Homebrew, macOS/Linux shell script, and Windows PowerShell.
- Instruction templates consolidated and shortened to reduce token count while preserving key guidelines.
- Terminal detection moved to canonical `internal/terminal` package with `IsInteractive()` function.
- Default MCP server templates no longer specify `clients` filter (servers are projected to all clients by default).

### Fixed
- MCP server health checks now properly handle HTTP transport timeout scenarios.

## v0.5.5 - 2026-01-25

### Added
- New `03_tools.md` instruction template with comprehensive tool usage guidelines: time-sensitive information handling, Context7 documentation lookups, MCP tool constraints, approval workflows, and error handling.
- New `fix-tests` slash command runs repo-defined checks (lint/format/pre-commit/tests) in a loop, fixing failures until all checks pass or max iterations reached.

### Changed
- Temporary artifact location moved from `tmp/agent-layer/runs/` to `.agent-layer/tmp/runs/`, keeping all agent artifacts within `.agent-layer/`.
- Slash command artifact naming standardized across workflows: `.agent-layer/tmp/<workflow>.<run-id>.<type>.md` with `run-id = YYYYMMDD-HHMMSS-<short-rand>`. User path overrides removed for consistency.
- `finish-task` workflow now delegates to `fix-tests` when available before falling back to manual repo-defined commands.
- README updated with new artifact naming convention and VS Code reauthentication note for new `CODEX_HOME` environments.

## v0.5.4 - 2026-01-24

### Changed
- Memory file `FEATURES.md` renamed to `BACKLOG.md` to better reflect its purpose (unscheduled user-visible features and tasks vs deferred issues).
- `al init --overwrite` now detects and prompts to delete unknown files under `.agent-layer` that are not tracked by Agent Layer templates.
- `al init --force` now deletes unknown files under `.agent-layer` in addition to overwriting existing files without prompts.
- Memory instruction templates improved with clearer formatting rules and entry layouts.
- Slash command templates (`continue-roadmap.md`, `update-roadmap.md`) simplified and clarified.
- VS Code launcher paths centralized in `internal/launchers` package, consumed by sync and install to prevent drift.
- Sync package refactored with system abstraction layer for improved test isolation and reliability.

## v0.5.3 - 2026-01-24

### Changed
- User-facing strings consolidated into `internal/messages/` package for consistency and maintainability.
- Python release tools (`extract-checksum.py`, `update-formula.py`) replaced with Go implementations in `internal/tools/`.
- Release test script reorganized into modular components (`scripts/test-release/release_tests.sh`, `scripts/test-release/tool_tests.sh`).
- Slash command templates (`find-issues.md`, `finish-task.md`) simplified to reduce duplication with base instructions; formatting rules now delegate to individual memory file templates.

## v0.5.2 - 2026-01-24

### Added
- Automated Homebrew tap updates: release workflow now opens a PR against `conn-castle/homebrew-tap` to update the formula with the new tarball URL and SHA256.

## v0.5.1 - 2026-01-23

### Added
- Source tarball (`agent-layer-<version>.tar.gz`) published with releases for Homebrew formula support.

### Changed
- Release scripts now generate and verify the source tarball via `git archive` + `gzip -n`.
- Documentation cleanup: simplified release process, corrected `make dev` description.

## v0.5.0 - 2026-01-23

Major shift from repo-local binary to globally installed CLI with per-repo version pinning.

### Added
- Global CLI installation via Homebrew (`brew install conn-castle/tap/agent-layer`), shell script (macOS/Linux), or PowerShell (Windows).
- `al init` command initializes `.agent-layer/` and `docs/agent-layer/` in any repo.
- Per-repo version pinning via `.agent-layer/al.version`; global CLI dispatches to the pinned version automatically.
- Cached binary downloads with SHA-256 verification; cached binaries stored in `~/.cache/agent-layer/versions/`.
- Shell completion for bash, zsh, and fish (`al completion <shell>` with optional `--install` flag).
- Update checking: `al init` and `al doctor` warn when a newer release is available.
- Linux desktop entry launcher (`.agent-layer/open-vscode.desktop`).
- E2E test suite (`scripts/test-e2e.sh`) and release test script (`scripts/test-release.sh`).
- Environment variables: `AL_CACHE_DIR` (override cache location), `AL_VERSION` (force version), `AL_NO_NETWORK` (disable downloads).

### Changed
- **Breaking:** Repo-local `./al` executable replaced with globally installed `al` CLI.
- **Breaking:** `al install` renamed to `al init`.
- **Breaking:** Repository moved from `nicholasjconn/agent-layer` to `conn-castle/agent-layer`.
- Install script renamed from `agent-layer-install.sh` to `al-install.sh`.
- `al init --overwrite` now prompts before each overwrite; use `--force` to skip prompts.
- `al init --version <tag>` pins the repo to a specific release version.
- Commands run from any subdirectory now resolve the repo root automatically.
- `.agent-layer/.gitignore` added to ignore launchers, template copies, and backups.

### Removed
- Repo-local `./al` binary; global `al` dispatches to pinned versions as needed.
- `agent-layer-install.sh` (replaced by `al-install.sh`).

## v0.4.0 - 2026-01-21

### Added
- `al doctor` command reports missing secrets, disabled servers, and common misconfigurations.
- `al wizard` command provides interactive setup for approval modes, agent enablement, model selection, MCP servers, secrets, and warning thresholds.
- Configurable warning system with thresholds for instruction token count, MCP server/tool counts, and schema token sizes.
- Antigravity slash commands now generate skills in `.agent/skills/<command>/SKILL.md`.
- VS Code launchers: macOS `.app` bundle (no Terminal window), macOS `.command` script, and Windows `.bat` file, all with `CODEX_HOME` support.
- `al install --no-wizard` flag skips the post-install wizard prompt.
- Atomic file writes across all sync operations prevent partial file corruption.

### Changed
- `al install` now prompts to run the wizard after seeding files (interactive terminals only).
- Gitignore patterns use root-anchored paths (`/AGENTS.md` instead of `AGENTS.md`) for precision.
- Default Codex reasoning effort changed from `xhigh` to `high`.
- Codex config header now warns about potential secrets in generated files.
- Environment variable loading: process environment takes precedence; `.agent-layer/.env` fills missing keys only; empty values in `.env` are ignored.
- Improved instruction and slash-command templates.

### Fixed
- VS Code launcher now works correctly with proper error messages for missing `code` command.
- MCP configuration for Codex HTTP servers now handles bearer token environment variables correctly.

## v0.3.1 - 2026-01-19

### Added
- Installer failure output now includes clear, actionable error messages.

### Fixed
- Installer checksum verification now handles SHA256SUMS entries with "./" prefixes.

### Changed
- Quick start documentation no longer suggests manual install fallback when only `./al` is present.

## v0.3.0 - 2026-01-18

Complete rewrite in Go for simpler installation and fewer moving parts.

### Added
- Single repo-local Go binary (`./al`) replaces the Node.js codebase.
- `al install` command for repository initialization with template seeding.
- `al install --overwrite` flag to reset templates to defaults.
- `al sync` command to regenerate client configs without launching.
- Support for five clients: Gemini CLI, Claude Code CLI, VS Code/Copilot Chat, Codex CLI, and Antigravity.
- Unified `[[mcp.servers]]` configuration in `config.toml` for both HTTP and stdio transports.
- Approval modes (`all`, `mcp`, `commands`, `none`) with per-client projection.
- `${ENV_VAR}` substitution from `.agent-layer/.env` with client-specific placeholder syntax preservation.
- Internal MCP prompt server for slash command discovery (auto-wired into client configs).
- Golden-file tests for deterministic output validation.
- Managed `.gitignore` block with customizable template (`.agent-layer/gitignore.block`).

### Changed
- **Breaking:** Complete rewrite from Node.js to Go.
- **Breaking:** Configuration moved from `config/agents.json` to `.agent-layer/config.toml` (TOML format).
- **Breaking:** MCP servers now configured via `[[mcp.servers]]` arrays in `config.toml`.
- CLI simplified: `./al <client>` always syncs then launches.
- Instructions now in `.agent-layer/instructions/` (numbered markdown files, lexicographic order).
- Slash commands now in `.agent-layer/slash-commands/` (one markdown file per command).
- Approved commands now in `.agent-layer/commands.allow` (one prefix per line).
- Project memory standardized in `docs/agent-layer/` (ISSUES.md, FEATURES.md, ROADMAP.md, DECISIONS.md, COMMANDS.md).

### Removed
- Node.js codebase (`src/lib/*.mjs`, test files, `package.json`).
- `config/agents.json` and separate MCP server configuration files.
- Built-in Tavily MCP server (now configurable as external server in `config.toml`).

## v0.2.0 - 2026-01-17

Major architectural overhaul moving core logic from shell to Node.js.

### Added
- Per-agent opt-in configuration via `config/agents.json` with interactive setup prompt.
- HTTP transport support for MCP servers.
- Tavily MCP server for web search capabilities.
- `./al --version` flag with dirty suffix for non-tagged commits.
- User config preservation and backup during upgrades.

### Changed
- **Breaking:** CLI entrypoint is now `.agent-layer/agent-layer`; `./al` remains as the launcher wrapper in the parent root.
- Root resolution, environment loading, and cleanup moved from shell to Node.js (`src/lib/roots.mjs`, `src/lib/env.mjs`, `src/lib/cleanup.mjs`).
- Test framework migrated from Bats (shell) to Node.js native test runner.
- GitHub MCP server switched to hosted HTTP endpoint with PAT authentication.
- Architecture documentation updated to reflect new layer boundaries.

### Removed
- Shell scripts: `al`, `run.sh`, `setup.sh`, `clean.sh`, `check-updates.sh`, `open-vscode.command`.
- Shell-based root resolution: `src/lib/parent-root.sh`, `src/lib/temp-parent-root.sh`.

## v0.1.0 - 2026-01-12
Initial release.

### Added
- Installer for per-project setup that pins `.agent-layer/` to tagged releases, with upgrade, version, and dev-branch options.
- Repo-local `./al` launcher with sync and environment modes plus local update checks.
- Sync pipeline that generates client configs from `.agent-layer/config` sources.
- MCP prompt server that exposes workflows as prompts.
- Project memory templates and setup/bootstrap helpers.
