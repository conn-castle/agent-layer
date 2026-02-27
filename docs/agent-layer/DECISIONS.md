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
- If a decision is superseded, add a new entry describing the change (do not delete history unless explicitly asked).

### Entry template
```text
- Decision YYYY-MM-DD abcdef: Short title
    Decision: <what was chosen>
    Reason: <why it was chosen>
    Tradeoffs: <what is gained and what is lost>
```

## Decision Log

<!-- ENTRIES START -->

- Decision 2026-01-17 e5f6a7b: MCP architecture (external servers + internal prompt server)
    Decision: External MCP servers are user-defined in `config.toml`. The internal prompt server (`al mcp-prompts`) exposes slash commands automatically and is not user-configured.
    Reason: Users need arbitrary MCP servers while slash command discovery should be consistent and automatic.
    Tradeoffs: Internal prompt server behavior is less customizable than external servers, but stays consistent across clients.

- Decision 2026-01-17 a7b8c9d: VS Code launchers for CODEX_HOME
    Decision: Provide repo-specific VS Code launchers that set `CODEX_HOME` at process start.
    Reason: The Codex extension reads `CODEX_HOME` only at startup; launchers ensure correct repo context.
    Tradeoffs: Launching through generated scripts is required to guarantee repo-scoped Codex context.

- Decision 2026-01-17 c9d0e1f: Antigravity limited support
    Decision: Antigravity supports instructions and slash commands only (no MCP, no approvals). Slash commands map to skills at `.agent/skills/<command>/SKILL.md`.
    Reason: Antigravity integration is best-effort; core clients (Gemini, Claude, VS Code, Codex) have full parity.
    Tradeoffs: Antigravity users get reduced functionality compared with core clients.

- Decision 2026-01-18 e1f2a3b: Secret handling (Codex exception)
    Decision: Generated configs use client-specific placeholder syntax so secrets are never embedded. Exception: Codex embeds secrets in URLs/env and uses `bearer_token_env_var` for headers. Shell environment takes precedence over `.agent-layer/.env`.
    Reason: Prevents accidental secret exposure; Codex limitations require an exception.
    Tradeoffs: Cross-client secret behavior is not fully uniform because Codex has transport-specific constraints.

- Decision 2026-01-22 f1e2d3c: Distribution model (global CLI with per-repo pinning)
    Decision: Ship a single globally installed `al` CLI with per-repo version pinning via `.agent-layer/al.version` and cached binaries.
    Reason: A single entrypoint reduces support burden while pinning keeps multi-repo setups reproducible.
    Tradeoffs: Pin management adds operational complexity and requires explicit upgrade flows.

- Decision 2026-01-24 a1b2c3d: Ignore unexpected working tree changes
    Decision: Agents will not pause, warn, or stop due to unexpected working tree changes (unstaged or staged files not created by the agent).
    Reason: The user works in parallel with agents, making concurrent changes a normal operating condition.
    Tradeoffs: Increases risk of edit conflicts if both user and agent modify the same file simultaneously; relies on git for resolution.

- Decision 2026-01-25 7e2c9f4: Agent-only workflow artifacts live in `.agent-layer/tmp`
    Decision: Workflow artifacts are written to `.agent-layer/tmp` using a unique per-invocation filename: `.agent-layer/tmp/<workflow>.<run-id>.<type>.md` with `run-id = YYYYMMDD-HHMMSS-<short-rand>`; no path overrides.
    Reason: Keeps artifacts invisible to humans while avoiding collisions for concurrent agents without relying on env vars or per-chat IDs.
    Tradeoffs: Files can accumulate until manually cleaned; agents must echo paths in chat to retain context.

- Decision 2026-01-27 d4e7a1b: VS Code settings merge scoped to managed block
    Decision: When the managed markers exist in `.vscode/settings.json`, update only the managed block and do not validate unrelated JSONC content; if markers are missing, parse the root object to insert the block.
    Reason: Avoid partial JSONC parsing dependencies while still supporting first-time insertion.
    Tradeoffs: Invalid JSONC outside the managed block is no longer detected once the markers are present.

- Decision 2026-01-28 5c8e2a1: Codex custom MCP headers
    Decision: Codex projects MCP headers using `bearer_token_env_var` for `Authorization: Bearer ${VAR}`, `env_http_headers` for exact `${VAR}` values, and `http_headers` for literals; other placeholder formats error.
    Reason: Support custom headers across clients without embedding secrets or relying on placeholder expansion in Codex.
    Tradeoffs: Headers with mixed literal + env placeholder (for example, `Token ${VAR}`) are rejected for Codex and must be restructured.

- Decision 2026-02-03 c7d2a1f: Curated CLI docs in site/
    Decision: Stop generating CLI docs during website publish; use the curated `site/docs/reference.mdx` section as the source of truth.
    Reason: Help-output dumps duplicated content and reduced usability compared to a curated guide.
    Tradeoffs: The guide can drift from exact flags; users should rely on `al --help` for authoritative flag output.

- Decision 2026-02-03 d9e3a7b: Consolidate docs into single-page sections
    Decision: Merge Concepts, Getting started, Reference, and Troubleshooting into single pages under `site/docs/`.
    Reason: Reduce fragmentation and make the docs feel cohesive and professional, with fewer small pages.
    Tradeoffs: Breaking URLs for old per-topic pages; cross-links must use anchors on the consolidated pages.

- Decision 2026-02-05 b6c1d2e: Gitignore block templating and validation
    Decision: Store `.agent-layer/gitignore.block` as the verbatim template content; inject managed markers, hash, and header only when writing the root `.gitignore`, and error if the block contains managed markers or a hash line.
    Reason: Keep the template file clean and user-editable while ensuring the root `.gitignore` stays managed and consistent.
    Tradeoffs: Legacy blocks with markers/hash now require `al upgrade` to restore the template file before `al sync` will succeed.

- Decision 2026-02-07 p0a-init-dispatch: Bypass repo-pin dispatch for `al init`
    Decision: `al init` now bypasses repo-pin binary dispatch and always executes on the invoking CLI binary.
    Reason: Upgrade operations must not be executed by an older repo-pinned version that is being replaced.
    Tradeoffs: `al init` behavior can differ from other subcommands in pinned repos when global and pinned versions diverge.

- Decision 2026-02-07 p0a-pin-validation: Resolve and validate explicit init pin targets
    Decision: `al init --version latest` resolves via the latest release API to a normalized semver pin, and explicit `--version` targets are validated against upstream release tags before writing `.agent-layer/al.version`.
    Reason: Upgrade guidance must be executable as written, and typo/nonexistent versions should fail before mutating repo pin state.
    Tradeoffs: Explicit pinning now depends on network access to validate release existence and can fail in fully offline workflows.

- Decision 2026-02-07 p0a-pin-recovery: Empty/corrupt pin files produce warnings, not errors
    Decision: `readPinnedVersion()` treats empty and non-semver pin files as "no pin" (returns a warning string instead of an error). `writeVersionFile()` auto-repairs empty/corrupt pins without requiring prompts.
    Reason: A broken pin file should never make the CLI completely unusable. `al init`/`al upgrade` must always be able to self-heal the pin state.
    Tradeoffs: Corrupt pins silently fall through to the current binary version; users see a warning but may not notice it in noisy terminal output.

- Decision 2026-02-08 p0b-upgrade-contract: Sequential guarantee + three-tier upgrade taxonomy
    Decision: Publish upgrade policy with three event categories (`safe auto`, `needs review`, `breaking/manual`) and a sequential compatibility guarantee (`N-1` to `N` release-line upgrades only, for example `0.6.x` -> `0.7.x`).
    Reason: Provides a clear, enforceable public contract without overpromising broad multi-line migration support before lifecycle tooling lands.
    Tradeoffs: Skipped-line upgrades remain best effort and may require additional manual migration guidance per release.

- Decision 2026-02-09 p1b-ownership-baseline: Ownership classification now uses embedded manifests plus canonical baseline state
    Decision: `al upgrade plan` ownership classification now uses committed per-release manifests (`internal/templates/manifests/*.json`) and canonical repo baseline state (`.agent-layer/state/managed-baseline.json`) with section-aware policies (`memory_entries_v1`, `memory_roadmap_v1`, `allowlist_lines_v1`), and emits `unknown_no_baseline` when evidence is insufficient.
    Reason: Distinguishes upstream template deltas from true local customization without runtime network/tag lookups and avoids silent guesses in ambiguous cases.
    Tradeoffs: Release workflow must generate/commit manifests for each tag; repos lacking credible baseline evidence may show `unknown no baseline` until a baseline refresh run (for example `al upgrade`).

- Decision 2026-02-10 p1c-init-upgrade-ownership: Init scaffolding only; user-owned config/env; agent-only .gitignore
    Decision: `al init` is one-time scaffolding (errors if `.agent-layer/` already exists). Upgrades/repairs are done via `al upgrade plan` + `al upgrade`. `.agent-layer/.env` and `.agent-layer/config.toml` are user-owned and seeded only when missing (never overwritten by init/upgrade). `.agent-layer/.gitignore` is agent-owned internal and is always overwritten and excluded from upgrade plans/diffs.
    Reason: Avoid accidental clobbering of user-specific configuration, reduce cognitive load in upgrade plans, and simplify init semantics by removing upgrade behavior.
    Tradeoffs: Changes to `.agent-layer/.gitignore` cannot be preserved; repos without baseline evidence will require an `al upgrade` run to establish it (supersedes earlier init-overwrite guidance).

- Decision 2026-02-10 pin-required: Pinning required for supported repos
    Decision: Treat `.agent-layer/al.version` as required and do not support or document an end-user “unpin” workflow.
    Reason: Unpinned repos can silently drift when developers upgrade the global `al` install, causing hard-to-debug mismatches.
    Tradeoffs: Advanced users can still delete the pin file manually, but that is unsupported and reduces reproducibility.

- Decision 2026-02-11 p1e-readiness-heuristics: Readiness checks are text-first with VS Code mtime heuristic
    Decision: Implement upgrade readiness checks in text dry-run output; detect stale `--no-sync` state using VS Code generated-output presence plus config-vs-output mtime comparison (instead of full sync-content diffing).
    Reason: Keeps checks decoupled from sync internals while surfacing practical risk before upgrade apply.
    Tradeoffs: mtime-based detection can produce false positives after non-functional config file touches; accepted for lower complexity and maintenance.

- Decision 2026-02-12 chlog-immutable: CHANGELOG entries are historical and immutable
    Decision: Never modify published CHANGELOG entries. They record what happened at the time of release and are treated as fixed historical records, even if terminology or paths have since changed.
    Reason: Changing historical entries undermines trust in the changelog as a factual record and can confuse readers comparing old entries against old tags.
    Tradeoffs: Stale references in old entries (e.g., renamed files) remain; readers must consult current docs for the latest names.

- Decision 2026-02-12 p1f-upgrade-diff-previews: Always show line-level diffs in upgrade previews and prompts
    Decision: `al upgrade plan` and interactive `al upgrade` overwrite prompts now render unified line-level diffs by default, with per-file truncation at 40 lines and an explicit `--diff-lines` override.
    Reason: Issue #30 required users to see specific line changes before accepting/rejecting overwrite decisions; always-on previews remove blind yes/no prompts.
    Tradeoffs: Default output is noisier for large files; users who need deeper context must opt in with a larger `--diff-lines` value.

- Decision 2026-02-14 p2a-upgrade-snapshot-transaction: Upgrade snapshot/rollback boundary and retention policy
    Decision: `al upgrade` now captures full-byte snapshots for the transactional upgrade mutation set (managed files/dirs, memory files, `.gitignore`, launcher outputs, and scanned unknown deletion targets), runs rollback on transactional-step failure, and retains the newest 20 snapshots under `.agent-layer/state/upgrade-snapshots/`.
    Reason: Deliver Phase 11 safety guarantees now while keeping snapshot artifacts available for the upcoming explicit rollback command.
    Tradeoffs: Snapshot files can grow in large repos because payloads are full-content; retention is bounded but no per-snapshot size budget is enforced yet.

- Decision 2026-02-14 p2b-upgrade-apply-flags: Remove `--force`; require explicit apply categories
    Decision: `al upgrade` no longer supports `--force`. Non-interactive runs require `--yes` plus one or more explicit apply flags (`--apply-managed-updates`, `--apply-memory-updates`, `--apply-deletions`), and deletion remains gated behind explicit `--apply-deletions`.
    Reason: Prevent accidental destructive upgrades and make non-interactive intent explicit per mutation category.
    Tradeoffs: Existing `al upgrade --force` automation breaks and must be migrated to explicit apply flags.

- Decision 2026-02-14 p2c-manual-rollback-status: Manual rollback eligibility and status handling
    Decision: `al upgrade rollback <snapshot-id>` accepts only snapshots in `applied` status; successful manual rollback preserves `applied`, while manual rollback failures set `rollback_failed` with `failure_step=manual_rollback`.
    Reason: Keep rollback semantics deterministic with the current snapshot schema while preserving a clear failure trail for failed manual restores.
    Tradeoffs: Snapshot metadata cannot currently distinguish "applied and later manually restored" from "applied and never manually restored" without a future schema extension.

- Decision 2026-02-15 p3a-migration-source-fallback: Upgrade migrations run source-agnostic operations when source version cannot be resolved
    Decision: Release migration manifests are required per supported target version (`internal/templates/migrations/<target>.json`) with `min_prior_version`. During `al upgrade`, source-agnostic operations still execute when source version resolution fails; source-gated operations are skipped with deterministic report entries.
    Reason: Users expect upgrades to continue to the latest version even when legacy repos lack reliable source-version evidence.
    Tradeoffs: Some source-dependent migrations may be deferred in ambiguous repos and require explicit follow-up if skip reports indicate missed transitions.

- Decision 2026-02-15 p3b-hard-removals: Breaking surfaces are removed immediately without compatibility shims
    Decision: Upgrade-related command/flag removals use clean breaks with explicit migration guidance; no deprecation windows or compatibility shims are kept. `al upgrade plan --json` is removed, and text output is the only supported plan interface.
    Reason: Reduces long-term maintenance burden and avoids carrying legacy compatibility branches.
    Tradeoffs: Existing automation that depended on removed surfaces must migrate in the same release window.

- Decision 2026-02-15 p3c-env-namespace: `.agent-layer/.env` is AL_-only with no key-migration path
    Decision: Only `AL_`-prefixed keys are loaded from `.agent-layer/.env`. Non-`AL_` keys are intentionally ignored, and upgrades do not provide env-key namespace migration.
    Reason: Keeps secret loading deterministic and avoids perpetually supporting mixed env-key conventions.
    Tradeoffs: Repositories that previously used non-`AL_` keys must rename them manually when adopting Agent Layer conventions.

- Decision 2026-02-15 p3d-embedded-template-source: Embedded templates are the only supported template source in this release line
    Decision: Agent Layer upgrade/init workflows support embedded templates only; non-default template repositories and template-source pinning metadata are out of scope.
    Reason: Keep upgrade behavior clear, deterministic, and maintainable by avoiding parallel template-source paths.
    Tradeoffs: Teams cannot use first-class custom template repositories in this release line and must revisit this in a future scoped backlog item if needed.

- Decision 2026-02-15 p6a-mcp-default-version-lane: Default MCP template dependencies are pinned with explicit floating opt-in
    Decision: Seeded MCP server commands now pin concrete dependency versions by default (`npx` and `uvx` surfaces), with inline commented examples showing the explicit floating/latest opt-in lane.
    Reason: Keep default sync/upgrade behavior deterministic while preserving an intentional path for teams that want fastest-updating external MCP tools.
    Tradeoffs: Pinned defaults can lag upstream MCP releases until Agent Layer updates the template versions.

- Decision 2026-02-15 p7b-wizard-profile-preview-default: Profile mode is preview-first with explicit apply
    Decision: `al wizard --profile` defaults to preview-only rewrite diffs and requires `--yes` for writes; secret prompts support explicit `skip` and `cancel`; backups are cleaned only via explicit `--cleanup-backups`.
    Reason: Keep non-interactive wizard usage safe in CI/automation and avoid accidental config rewrites.
    Tradeoffs: Adds extra flags for scripted flows and requires explicit cleanup for backup files.

- Decision 2026-02-15 rel-working-tree-manifest: Manifest generation reads from working tree, not git tags
    Decision: `gentemplatemanifest` reads template files from the working tree via `os.ReadFile`/`filepath.WalkDir` instead of `git show <tag>:<path>`. The `--tag` flag is replaced with `--version`.
    Reason: Eliminates the tag chicken-and-egg problem where the manifest must be committed before tagging but the tool required a tag to generate the manifest.
    Tradeoffs: The manifest is no longer guaranteed to match tag content; the release preflight gate mitigates this risk.

- Decision 2026-02-16 p12-no-launch-plan: Do not implement `al launch-plan`
    Decision: `al launch-plan <client>` will not be implemented as a separate command. If dry-run sync demand emerges, add `--dry-run` to `al sync` instead.
    Reason: `al sync` already writes deterministic, gitignored outputs that are safe to regenerate and inspect. `al upgrade plan` handles preview for upgrade mutations. `--no-sync` exists only on `al vscode`; other clients always sync before launch. No `launch-plan` code was ever built, and a separate command would duplicate sync semantics.
    Tradeoffs: No dedicated pre-sync preview exists today; users who want to inspect outputs must run `al sync` and review the generated files.

- Decision 2026-02-17 p12-yolo-mode: Approvals policy expanded to 5-mode system (supersedes f6a7b8c)
    Decision: Add `yolo` as a fifth `approvals.mode` value. YOLO mode auto-approves commands and MCP (like `all`) and also sends full-auto flags to each client: Claude `--dangerously-skip-permissions`, Gemini `--approval-mode=yolo`, Codex `approval_policy=never` + `sandbox_mode=danger-full-access`, VS Code `chat.tools.global.autoApprove=true`.
    Reason: Users running in sandboxed/ephemeral environments want to skip all permission prompts without per-client manual configuration.
    Tradeoffs: YOLO bypasses all safety prompts; a single-line `[yolo]` acknowledgement on stderr (not a structured warning) informs users on every sync and launch. The template config comment and documentation carry the risk explanation. No `al doctor` warning — YOLO is a deliberate choice, not a health issue.

- Decision 2026-02-17 p12-unified-vscode-launcher: Unified VS Code launcher and shared Claude MCP scope
    Decision: Use a single `al vscode` command for Codex and Claude VS Code extension launches. The Claude VS Code config section remains config-only (`[agents.claude_vscode]`), and Claude MCP filtering is shared with CLI via the `"claude"` client filter because both surfaces read the same `.mcp.json`/`.claude/settings.json`.
    Reason: VS Code is a single runtime surface, and separate launcher/MCP-filter paths would duplicate behavior while conflicting with extension file-path expectations.
    Tradeoffs: Users cannot configure separate MCP server sets for Claude CLI vs Claude VS Code extension; both use the same Claude-scoped MCP configuration.

- Decision 2026-02-18 config-resilience: Replace silent Migrate() with lenient parsing + interactive upgrade prompts (supersedes config-migrate)
    Decision: Remove `Config.Migrate()` entirely. Instead: (1) add `ParseConfigLenient`/`LoadConfigLenient` that unmarshal without validation, (2) `al wizard` and `al doctor` fall back to lenient loading so they always work on broken configs, (3) `al upgrade` uses `config_set_default` migration operations that prompt the user interactively for new required field values, (4) runtime commands (`al sync`, `al claude`, etc.) remain strict and fail with actionable guidance.
    Reason: Silent defaults violate the "no silent fallbacks" rule. Repair tools must always be runnable. Every config value should come from an explicit user choice.
    Tradeoffs: Users must run `al wizard` or `al upgrade` to fix broken configs instead of having them auto-repaired; accepted because explicit consent is a design principle.

- Decision 2026-02-18 migration-chain: Migration manifests are chained during multi-version upgrades
    Decision: When source version is known, all manifests between source (exclusive) and target (inclusive) are loaded and applied in order with per-operation deduplication by ID. When source is unknown, only the target manifest is loaded (backward compatible). Migrations that missed a release are placed in the next release's manifest with an expanded `min_prior_version`.
    Reason: Without chaining, users jumping multiple versions (e.g., 0.8.0 → 0.8.2) miss intermediate migrations. The v0.8.1 config migration shipped after the binary, so it was moved to 0.8.2's manifest to catch all users.
    Tradeoffs: Manifest ordering depends on semver sort of filenames; manifests must have unique operation IDs across the chain or later duplicates are silently skipped.

- Decision 2026-02-18 config-field-catalog: Shared config field catalog in `internal/config/fields.go`
    Decision: Centralize config field metadata (type, valid options, required flag, allow-custom) in a single registry. Wizard and upgrade prompts derive option lists from the catalog instead of maintaining separate hardcoded slices. `validate.go` derives approval mode validation from the catalog. Upgrade `config_set_default` prompts receive field metadata to show type-aware numbered choices (bool true/false, enum options) instead of yes/no.
    Reason: Config field options were duplicated across `internal/wizard/catalog.go` (option lists), `validate.go` (valid value maps), and had no way to flow to the upgrade prompter. A shared catalog provides a single source of truth and enables richer upgrade prompts.
    Tradeoffs: Adding a new config field now requires updating `internal/config/fields.go` in addition to `types.go`/`validate.go`; accepted because the catalog is the natural place to document field constraints.

- Decision 2026-02-19 validate-mcp-sanitize: Validation silently strips transport-incompatible MCP fields (supersedes wizard-only approach)
    Decision: `Validate()` in `internal/config/validate.go` now silently clears transport-incompatible fields (headers/url/http_transport on stdio; command/args/env on http) before checking required fields. This runs at config load time, so every code path that loads config benefits — not just the wizard.
    Reason: The v0.8.3 wizard-mcp-sanitize fix only ran during `al wizard`, meaning existing configs with stale fields still blocked all commands (`al claude`, `al sync`, etc.) until the user manually ran the wizard. Users expected the fix to be automatic.
    Tradeoffs: Validation now mutates the config (pointer receiver), which is slightly unusual for a method named Validate; accepted because the alternative (separate Sanitize call) risks code paths that forget to call it. The wizard sanitization remains as a belt-and-suspenders layer that also cleans the file on disk.

- Decision 2026-02-19 wizard-dotted-key-sanitize: Wizard removeKeyFromBlock handles TOML dotted keys
    Decision: `removeKeyFromBlock` in `internal/wizard/patch.go` now detects and removes TOML dotted sub-key lines (e.g., `headers.Authorization = "val"`) in addition to inline table format (`headers = { ... }`). A new `parseDottedPrefixLine` helper matches lines where the key is a dotted prefix.
    Reason: The v0.8.3 wizard sanitization only worked for inline table format. TOML dotted keys (`headers.Foo = "val"`) were invisible to `parseKeyLineWithState` because it requires `key =` (equals after key), not `key.` (dot after key). Users with dotted-key headers on stdio servers would run the wizard and still get validation errors.
    Tradeoffs: Quoted root keys (`"headers".Foo = "val"`) remain unsupported; accepted because this format is extremely rare and the validation-level sanitization provides a safety net regardless of TOML syntax.

- Decision 2026-02-20 e2e-mandatory-upgrade-ci: Upgrade scenarios mandatory in CI via auto-detected manifest version (supersedes e2e-two-lane-hermetic skip behavior)
    Decision: E2E testing uses a scenario-based bash harness with contract-level assertions, authentic upgrade source state from downloaded prior release binaries, mandatory upgrade coverage in CI (`test-e2e-ci` with online + required-upgrade flags), and HOME isolation to avoid host config pollution.
    Reason: The previous monolithic flow allowed silent upgrade-scenario skips and weak "no-crash" checks, reducing confidence in real user workflows (`init -> upgrade -> sync -> launch`).
    Tradeoffs: First CI/local online runs require network access and cache warm-up; scenario assertions are stricter and need upkeep when intentional output changes.

- Decision 2026-02-20 gemini-auto-trust: `al sync` auto-trusts repo in `~/.gemini/trustedFolders.json`
    Decision: When Gemini is enabled, `al sync` writes the repo root as `TRUST_FOLDER` to `~/.gemini/trustedFolders.json` (outside the repo). Failures produce a non-fatal warning, never a sync error.
    Reason: Gemini CLI's Trusted Folders feature silently replaces untrusted project settings with `{}`, discarding all MCP servers. Users already expressed trust by enabling Gemini in `config.toml`; propagating that trust to the Gemini runtime is the expected behavior.
    Tradeoffs: Writes to a file outside the repo boundary (`~/.gemini/`). Acceptable because this is a user-level runtime config (analogous to existing `~/.codex/` writes) and failure is non-fatal.

- Decision 2026-02-20 config-catalog-scope: Field catalog is wizard-managed fields, not full schema
    Decision: The field catalog (fields.go) covers fields that the wizard prompts for and upgrade migrations reference. It is not a complete TOML schema inventory. Fields like warnings.noise_mode and warnings.version_update_on_sync are valid config keys but not in the catalog because they are not wizard-managed.
    Reason: Catalog entries carry wizard UI metadata (options, descriptions, AllowCustom). Adding entries for non-interactive fields would add maintenance burden with no UX benefit.
    Tradeoffs: Catalog metadata and strict schema validation must remain aligned when adding new wizard-managed fields.

- Decision 2026-02-20 unknown-key-repairable: Strict unknown-key validation with lenient repair path
    Decision: Runtime config parsing rejects unknown TOML keys via strict decode (including enable-only agent sections), while unknown-key failures are wrapped as `ErrConfigValidation` so `al wizard` and `al doctor` can still load leniently and guide repair.
    Reason: Silent unknown-key acceptance hid invalid config (for example, unsupported agent fields), but hard-failing repair tools made recovery impossible.
    Tradeoffs: Runtime commands fail fast on unknown keys until users repair config through wizard/doctor or manual edits.

- Decision 2026-02-21 upg-config-toml-destructive: Config migrations use destructive TOML formatting
    Decision: `upgrade_migrations.go` continues to use `tomlv2.Marshal` for config updates, which strips user comments and reorders keys.
    Reason: Full TOML preservation is complex and the wizard/profile flows already provide previews and backups, mitigating the risk of unexpected data loss.
    Tradeoffs: Users lose manual formatting/comments in `config.toml` when a migration executes; accepted for implementation simplicity and deterministic output.

- Decision 2026-02-22 agent-specific-key-rename: Rename agent passthrough key from `custom` to `agent_specific`
    Decision: Replace `agents.<client>.custom` with `agents.<client>.agent_specific` across schema, sync/warning logic, templates, docs, tests, and e2e scenarios; no backward-compatibility alias is kept.
    Reason: Standardize the naming to explicit intent ("agent-specific passthrough") and remove ambiguous "custom" terminology.
    Tradeoffs: Existing configs using `custom` now fail strict parsing and must be updated to `agent_specific`.

- Decision 2026-02-22 claude-config-dir: Opt-in repo-local `CLAUDE_CONFIG_DIR` via `local_config_dir` config field
    Decision: Gate `CLAUDE_CONFIG_DIR=<repo>/.claude-config` behind `[agents.claude] local_config_dir = true` (opt-in, default false). When enabled, `al claude` uses warn-and-preserve on mismatch and `al vscode` uses config-flag-based set/unset (matching `CODEX_HOME` pattern). Use `.claude-config/` (not `.claude/`) to avoid collision with the project-level `.claude/settings.json` generated by `al sync`. Keep `.claude-config/` in the gitignore block unconditionally.
    Reason: Claude Code writes a user-level `settings.json` to `CLAUDE_CONFIG_DIR` that would collide with Agent Layer's project-level `.claude/settings.json`. The UX cost (two `.claude*` directories) makes always-on inappropriate. Users who want per-repo settings and caches isolation enable it explicitly.
    Tradeoffs: Disabled by default means no isolation unless configured. Note: Claude Code stores auth in the OS credential store (macOS Keychain service `"Claude Code-credentials"`; Linux libsecret/gnome-keyring) regardless of `CLAUDE_CONFIG_DIR` (upstream limitation), so `local_config_dir` currently isolates settings and caches but not auth.

- Decision 2026-02-23 mcp-prompts-dispatch-bypass: Prompt server runs on invoking CLI binary
    Decision: `al mcp-prompts` now bypasses repo-pin dispatch (same as `al init`/`al upgrade`), and sync prefers local source `go run <repo>/cmd/al mcp-prompts` when available.
    Reason: MCP stdio startup must not depend on a PATH shim or cached pinned binary that may be missing or non-runnable for the requested pin.
    Tradeoffs: In source repos, prompt-server execution may use local `go run` behavior instead of the globally installed pinned binary path.

- Decision 2026-02-24 quiet-supersedes-p7a: Warnings verbosity policy (`reduce`, `quiet`, and `--quiet`)
    Decision: `warnings.noise_mode` supports `reduce` and `quiet`, and `--quiet`/`-q` provides command-line suppression. `reduce` suppresses only non-critical suppressible warnings; `quiet` suppresses all warnings (including critical) except `al doctor`, which always prints warnings.
    Reason: Users need one coherent verbosity model that serves both safer daily use (`reduce`) and zero-noise scripted flows (`quiet`/`--quiet`).
    Tradeoffs: Quiet mode can hide high-risk warnings by design, and older pinned binaries may ignore quiet behavior until upgraded.

- Decision 2026-02-24 wizard-order-policy: Wizard config writes now use explicit preferred section order (supersedes f7a3c9d)
    Decision: Wizard-managed `config.toml` writes iterate an explicit preferred section order (`approvals`, enabled agents, `mcp`, `warnings`) instead of relying on template parse order as the implicit ordering source.
    Reason: The previous "template order is policy" coupling made ordering intent implicit and brittle when templates were reorganized.
    Tradeoffs: Existing user files are still rewritten to canonical wizard order; manual ordering/layout edits are not preserved.

- Decision 2026-02-24 reasoning-effort-support-matrix: Reasoning effort is explicit per-client capability, not universal
    Decision: Keep `reasoning_effort` supported for Codex and add Claude support (validated options, projected to `.claude/settings.json` as `effortLevel`); Gemini `reasoning_effort` is rejected with a hard validation error until a supported upstream control exists.
    Reason: Claude exposes a stable settings surface for effort control now, while Gemini currently has no verified equivalent in this integration path; silent omission would violate fail-loud policy.
    Tradeoffs: Configs attempting Gemini reasoning fail fast and must remove that key; future Gemini support requires explicit schema/validation/prompt updates.

- Decision 2026-02-24 required-field-migration-guardrail: Required config fields need migration defaults with a legacy baseline allowlist
    Decision: Add an automated guardrail test that enforces migration-manifest `config_set_default` coverage for required config fields introduced after baseline version `0.8.1`, with an explicit allowlist for legacy required fields that predate manifest enforcement.
    Reason: A naive all-fields check would fail forever because historical required fields existed before migration manifests had operations.
    Tradeoffs: The baseline allowlist must be maintained deliberately when introducing new required fields; stale allowlist entries can hide drift if not reviewed.

- Decision 2026-02-25 config-key-style-snake-case: Canonical config keys use snake_case
    Decision: Use snake_case for `.agent-layer/config.toml` keys and table names; rename `[agents.claude-vscode]` to `[agents.claude_vscode]` and ship a source-agnostic v0.8.8 migration (`config_rename_key` + `config_set_default`) for compatibility.
    Reason: Mixed kebab-case and snake_case in one schema increased learning friction and caused avoidable drift across templates, docs, and tests.
    Tradeoffs: Repos that skip `al upgrade`/`al wizard` will fail strict parsing until the key is renamed, while historical release artifacts still reference the legacy key for provenance.

- Decision 2026-02-25 docs-retention-policy: Website publish enforces docs version retention and artifact pruning
    Decision: `cmd/publish-site` now enforces a bounded stable-release retention policy after `docs:version`: keep up to 4 newest patch releases from the newest minor line plus the newest patch release for each of the newest 4 minor lines, drop prerelease entries, and prune dropped versions from `versions.json`, `versioned_docs/`, and `versioned_sidebars/`. Release publishing currently supports stable tags only (`vX.Y.Z`); prerelease tags are intentionally rejected.
    Reason: Unbounded version snapshots in `agent-layer-web` grow maintenance and deploy footprint over time; retention must be enforced in the publisher because deploy only publishes `main`.
    Tradeoffs: Older historical docs snapshots beyond the retention window are intentionally removed on each release publish; maintainers must run publisher manually if immediate cleanup is needed before the next release. Prerelease docs publication is unavailable until prerelease support is explicitly reintroduced end-to-end.

- Decision 2026-02-26 phase15-frontmatter-parser-yaml-v3: Skill frontmatter parsing uses `go.yaml.in/yaml/v3` with strict per-field validation
    Decision: For Phase 15 `skill-frontmatter`, parse and serialize skill frontmatter with `go.yaml.in/yaml/v3`, enforce required/typed fields in code (`description` required, `metadata` string-map, `compatibility`/`allowed-tools` string), and keep unknown keys parse-tolerant until validator work lands.
    Reason: YAML parsing replaces brittle line-scanning while preserving fail-loud behavior and enabling metadata serialization needed for generated Codex/Antigravity SKILL.md outputs.
    Tradeoffs: `go.yaml.in/yaml/v3` keeps us on the stable v3 API surface; migration to `go.yaml.in/yaml/v4` is deferred to a separate follow-up issue to avoid expanding Phase 15 scope.

- Decision 2026-02-26 phase15-frontmatter-parser-yaml-org-path: Skill frontmatter uses maintained YAML module path directly (supersedes phase15-frontmatter-parser-yaml-v3)
    Decision: Replace first-party YAML usage with `go.yaml.in/yaml/v3` and disallow direct imports of `gopkg.in/yaml.v3` in this repository.
    Reason: Avoid direct reliance on the archived `gopkg.in` YAML module path while preserving stable behavior and avoiding pre-release `v4` adoption.
    Tradeoffs: `gopkg.in/yaml.v3` may still appear transitively until upstream dependencies migrate; full graph removal requires broader dependency upgrades outside this scoped change.

- Decision 2026-02-26 phase15-skill-parse-validate-separation: Keep skill parsing tolerant and enforce spec in validator/doctor
    Decision: `internal/config` skill parsing remains backward-compatible (path-derived canonical names, unknown frontmatter tolerated, no hard requirement for frontmatter `name` at parse time), while Phase 15 spec checks are enforced in `internal/skillvalidator` and surfaced as `al doctor` skill warnings.
    Reason: Existing repos contain flat legacy skills that would break under strict parse-time enforcement; validator-level diagnostics preserve upgradeability while still driving standards alignment.
    Tradeoffs: Non-compliant skills can still load/sync until users act on doctor warnings; strict enforcement can be added later only with an explicit migration path.

- Decision 2026-02-27 e2e-github-api-auth: E2E harness authenticates GitHub API calls with GITHUB_TOKEN
    Decision: `resolve_latest_release_version()` in `scripts/test-e2e/harness.sh` now passes `GITHUB_TOKEN` (or `GH_TOKEN`) as a Bearer token in the Authorization header when available. CI workflow exports `GITHUB_TOKEN` to the `make ci` step.
    Reason: Unauthenticated GitHub API calls are rate-limited to 60 req/hr per IP. GitHub Actions runners share IPs, causing intermittent CI failures when the rate limit is hit during the "Resolve upgrade binaries" phase.
    Tradeoffs: Authenticated requests raise the limit to 5000 req/hr but require a token; unauthenticated fallback is preserved for local offline runs.
