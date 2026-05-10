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

- Decision 2026-01-17 a7b8c9d: VS Code launchers for CODEX_HOME
    Decision: Provide repo-specific VS Code launchers that set `CODEX_HOME` at process start.
    Reason: The Codex extension reads `CODEX_HOME` only at startup; launchers ensure correct repo context.
    Tradeoffs: Launching through generated scripts is required to guarantee repo-scoped Codex context.

- Decision 2026-01-17 c9d0e1f: Antigravity limited support
    Decision: Antigravity supports instructions and skills only (no MCP, no approvals). Skills are projected through shared `.agents/skills/<name>/SKILL.md`; singular `.agent/skills/` is treated as legacy generated output.
    Reason: Antigravity integration is best-effort; core clients (Gemini, Claude, VS Code, Codex, Copilot CLI) have full parity, and current skill support aligns with the shared Agent Skills path.
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

- Decision 2026-02-07 p0a-pin-recovery: Empty/corrupt pin files produce warnings, not errors
    Decision: `readPinnedVersion()` treats empty and non-semver pin files as "no pin" (returns a warning string instead of an error). `writeVersionFile()` auto-repairs empty/corrupt pins without requiring prompts.
    Reason: A broken pin file should never make the CLI completely unusable. `al init`/`al upgrade` must always be able to self-heal the pin state.
    Tradeoffs: Corrupt pins silently fall through to the current binary version; users see a warning but may not notice it in noisy terminal output.

- Decision 2026-02-09 p1b-ownership-baseline: Ownership classification now uses embedded manifests plus canonical baseline state
    Decision: `al upgrade plan` ownership classification now uses committed per-release manifests (`internal/templates/manifests/*.json`) and canonical repo baseline state (`.agent-layer/state/managed-baseline.json`) with section-aware policies (`memory_entries_v1`, `memory_roadmap_v1`, `allowlist_lines_v1`), and emits `unknown_no_baseline` when evidence is insufficient.
    Reason: Distinguishes upstream template deltas from true local customization without runtime network/tag lookups and avoids silent guesses in ambiguous cases.
    Tradeoffs: Release workflow must generate/commit manifests for each tag; repos lacking credible baseline evidence may show `unknown no baseline` until a baseline refresh run (for example `al upgrade`).

- Decision 2026-02-10 p1c-init-upgrade-ownership: Init scaffolding only; user-owned config/env; agent-only .gitignore
    Decision: `al init` is one-time scaffolding (errors if `.agent-layer/` already exists). Upgrades/repairs are done via `al upgrade plan` + `al upgrade`. `.agent-layer/.env` and `.agent-layer/config.toml` are user-owned and seeded only when missing (never overwritten by init/upgrade). `.agent-layer/.gitignore` is agent-owned internal and is always overwritten and excluded from upgrade plans/diffs.
    Reason: Avoid accidental clobbering of user-specific configuration, reduce cognitive load in upgrade plans, and simplify init semantics by removing upgrade behavior.
    Tradeoffs: Changes to `.agent-layer/.gitignore` cannot be preserved; repos without baseline evidence will require an `al upgrade` run to establish it (supersedes earlier init-overwrite guidance).

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

- Decision 2026-02-15 p3a-migration-source-fallback: Upgrade migrations run source-agnostic operations when source version cannot be resolved
    Decision: Release migration manifests are required per supported target version (`internal/templates/migrations/<target>.json`) with `min_prior_version`. During `al upgrade`, source-agnostic operations still execute when source version resolution fails; source-gated operations are skipped with deterministic report entries.
    Reason: Users expect upgrades to continue to the latest version even when legacy repos lack reliable source-version evidence.
    Tradeoffs: Some source-dependent migrations may be deferred in ambiguous repos and require explicit follow-up if skip reports indicate missed transitions.

- Decision 2026-02-15 rel-working-tree-manifest: Manifest generation reads from working tree, not git tags
    Decision: `gentemplatemanifest` reads template files from the working tree via `os.ReadFile`/`filepath.WalkDir` instead of `git show <tag>:<path>`. The `--tag` flag is replaced with `--version`.
    Reason: Eliminates the tag chicken-and-egg problem where the manifest must be committed before tagging but the tool required a tag to generate the manifest.
    Tradeoffs: The manifest is no longer guaranteed to match tag content; the release preflight gate mitigates this risk.

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

- Decision 2026-02-20 e2e-mandatory-upgrade-ci: Upgrade scenarios mandatory in CI via auto-detected manifest version (supersedes e2e-two-lane-hermetic skip behavior)
    Decision: E2E testing uses a scenario-based bash harness with contract-level assertions, authentic upgrade source state from downloaded prior release binaries, mandatory upgrade coverage in CI (`test-e2e-ci` with online + required-upgrade flags), and HOME isolation to avoid host config pollution.
    Reason: The previous monolithic flow allowed silent upgrade-scenario skips and weak "no-crash" checks, reducing confidence in real user workflows (`init -> upgrade -> sync -> launch`).
    Tradeoffs: First CI/local online runs require network access and cache warm-up; scenario assertions are stricter and need upkeep when intentional output changes.

- Decision 2026-02-20 gemini-auto-trust: `al sync` auto-trusts repo in `~/.gemini/trustedFolders.json`
    Decision: When Gemini is enabled, `al sync` writes the repo root as `TRUST_FOLDER` to `~/.gemini/trustedFolders.json` (outside the repo). Failures produce a non-fatal warning, never a sync error.
    Reason: Gemini CLI's Trusted Folders feature silently replaces untrusted project settings with `{}`, discarding all MCP servers. Users already expressed trust by enabling Gemini in `config.toml`; propagating that trust to the Gemini runtime is the expected behavior.
    Tradeoffs: Writes to a file outside the repo boundary (`~/.gemini/`). Acceptable because this is a user-level runtime config (analogous to existing `~/.codex/` writes) and failure is non-fatal.

- Decision 2026-02-20 unknown-key-repairable: Strict unknown-key validation with lenient repair path
    Decision: Runtime config parsing rejects unknown TOML keys via strict decode (including enable-only agent sections), while unknown-key failures are wrapped as `ErrConfigValidation` so `al wizard` and `al doctor` can still load leniently and guide repair.
    Reason: Silent unknown-key acceptance hid invalid config (for example, unsupported agent fields), but hard-failing repair tools made recovery impossible.
    Tradeoffs: Runtime commands fail fast on unknown keys until users repair config through wizard/doctor or manual edits.

- Decision 2026-02-22 claude-config-dir: Opt-in repo-local `CLAUDE_CONFIG_DIR` via `local_config_dir` config field
    Decision: Gate `CLAUDE_CONFIG_DIR=<repo>/.claude-config` behind `[agents.claude] local_config_dir = true` (opt-in, default false). When enabled, `al claude` uses warn-and-preserve on mismatch and `al vscode` uses config-flag-based set/unset (matching `CODEX_HOME` pattern). Use `.claude-config/` (not `.claude/`) to avoid collision with the project-level `.claude/settings.json` generated by `al sync`. Keep `.claude-config/` in the gitignore block unconditionally.
    Reason: Claude Code writes a user-level `settings.json` to `CLAUDE_CONFIG_DIR` that would collide with Agent Layer's project-level `.claude/settings.json`. The UX cost (two `.claude*` directories) makes always-on inappropriate. Users who want per-repo settings and caches isolation enable it explicitly.
    Tradeoffs: Disabled by default means no isolation unless configured. Note: Claude Code stores auth in the OS credential store (macOS Keychain service `"Claude Code-credentials"`; Linux libsecret/gnome-keyring) regardless of `CLAUDE_CONFIG_DIR` (upstream limitation), so `local_config_dir` currently isolates settings and caches but not auth.

- Decision 2026-02-24 quiet-supersedes-p7a: Warnings verbosity policy (`reduce`, `quiet`, and `--quiet`)
    Decision: `warnings.noise_mode` supports `reduce` and `quiet`, and `--quiet`/`-q` provides command-line suppression. `reduce` suppresses only non-critical suppressible warnings; `quiet` suppresses all warnings (including critical) except `al doctor`, which always prints warnings.
    Reason: Users need one coherent verbosity model that serves both safer daily use (`reduce`) and zero-noise scripted flows (`quiet`/`--quiet`).
    Tradeoffs: Quiet mode can hide high-risk warnings by design, and older pinned binaries may ignore quiet behavior until upgraded.

- Decision 2026-02-24 wizard-order-policy: Wizard config writes now use explicit preferred section order (supersedes f7a3c9d)
    Decision: Wizard-managed `config.toml` writes iterate an explicit preferred section order (`approvals`, enabled agents, `mcp`, `warnings`) instead of relying on template parse order as the implicit ordering source.
    Reason: The previous "template order is policy" coupling made ordering intent implicit and brittle when templates were reorganized.
    Tradeoffs: Existing user files are still rewritten to canonical wizard order; manual ordering/layout edits are not preserved.

- Decision 2026-02-24 required-field-migration-guardrail: Required config fields need migration defaults with a legacy baseline allowlist
    Decision: Add an automated guardrail test that enforces migration-manifest `config_set_default` coverage for required config fields introduced after baseline version `0.8.1`, with an explicit allowlist for legacy required fields that predate manifest enforcement.
    Reason: A naive all-fields check would fail forever because historical required fields existed before migration manifests had operations.
    Tradeoffs: The baseline allowlist must be maintained deliberately when introducing new required fields; stale allowlist entries can hide drift if not reviewed.

- Decision 2026-02-25 docs-retention-policy: Website publish enforces docs version retention and artifact pruning
    Decision: `cmd/publish-site` now enforces a bounded stable-release retention policy after `docs:version`: keep up to 4 newest patch releases from the newest minor line plus the newest patch release for each of the newest 4 minor lines, drop prerelease entries, and prune dropped versions from `versions.json`, `versioned_docs/`, and `versioned_sidebars/`. Release publishing currently supports stable tags only (`vX.Y.Z`); prerelease tags are intentionally rejected.
    Reason: Unbounded version snapshots in `agent-layer-web` grow maintenance and deploy footprint over time; retention must be enforced in the publisher because deploy only publishes `main`.
    Tradeoffs: Older historical docs snapshots beyond the retention window are intentionally removed on each release publish; maintainers must run publisher manually if immediate cleanup is needed before the next release. Prerelease docs publication is unavailable until prerelease support is explicitly reintroduced end-to-end.

- Decision 2026-02-26 phase15-skill-frontmatter-validation: Skill frontmatter parsing stays YAML-based and validator-enforced
    Decision: Parse/serialize skill frontmatter with `go.yaml.in/yaml/v3`; keep parsing backward-compatible (path-derived names, unknown frontmatter tolerated), and enforce Agent Skills spec checks in `internal/skillvalidator` / `al doctor`.
    Reason: YAML parsing replaces brittle line scanning and supports metadata serialization; validator-level diagnostics preserve upgradeability for existing flat/legacy skills while still driving standards alignment.
    Tradeoffs: `gopkg.in/yaml.v3` may appear transitively, non-compliant skills can still load/sync until users act on doctor warnings, and strict parse-time enforcement needs an explicit migration path.

- Decision 2026-02-27 e2e-github-api-auth: E2E harness authenticates GitHub API calls with GITHUB_TOKEN
    Decision: `resolve_latest_release_version()` in `scripts/test-e2e/harness.sh` now passes `GITHUB_TOKEN` (or `GH_TOKEN`) as a Bearer token in the Authorization header when available. CI workflow exports `GITHUB_TOKEN` to the `make ci` step.
    Reason: Unauthenticated GitHub API calls are rate-limited to 60 req/hr per IP. GitHub Actions runners share IPs, causing intermittent CI failures when the rate limit is hit during the "Resolve upgrade binaries" phase.
    Tradeoffs: Authenticated requests raise the limit to 5000 req/hr but require a token; unauthenticated fallback is preserved for local offline runs.

- Decision 2026-03-01 breaking-change-metadata: Breaking-change display is data-driven from migration manifests
    Decision: Move breaking-change notices and details into the migration manifest (`breaking`, `breaking_notice`, `breaking_details` fields) instead of hardcoding per-kind display logic in the upgrade report renderer.
    Reason: Hardcoded display logic couples the renderer to specific migration kinds and requires code changes for every new breaking migration. Data-driven display lets future breaking migrations carry their own user-facing copy without touching the renderer.
    Tradeoffs: Migration manifest validation is now stricter (breaking requires notice; notice/details require breaking flag); accepted because manifest authoring is a release-time activity with clear validation feedback.

- Decision 2026-03-02 user-owned-conventions: Extract project-specific conventions to user-managed `04_conventions.md`
    Decision: Move 5 project-specific instruction items (frontend rules, test coverage thresholds, package policies, typing requirements, schema safety) from `00_base.md` into a new `04_conventions.md` that is user-managed: seeded on `al init`, never overwritten on `al upgrade`, and excluded from managed diffs. A new `append_to_file` migration kind enables delivering new conventions to existing users via opt-in migration entries.
    Reason: Project-specific conventions create noise for projects where they don't apply; separating them lets users curate their own conventions while universal instructions remain managed.
    Tradeoffs: Conventions are no longer auto-updated during upgrades; new defaults require explicit migration entries to reach existing users.

- Decision 2026-03-02 instruction-reorder-dedup: Reorder instruction files and deduplicate cross-file instructions
    Decision: Rename instruction files to `00_rules.md`, `01_base.md`, `02_memory.md` (primacy effect: hard constraints load first). Remove 6 cross-file duplicates (keeping one canonical copy each), compress verbose sections (~50% fewer tokens), move UTC-only and No system Python from rules to conventions, add no-over-engineering and verification-closure instructions.
    Reason: Research shows instruction count and ordering affect model compliance; deduplication reduces ~57 to ~50 instructions and primacy-ordered hard constraints improve adherence.
    Tradeoffs: File renames require a v0.9.1 migration with 3 `rename_file` + 2 `append_to_file` operations; existing repos must run `al upgrade` to apply renames.

- Decision 2026-03-06 improve-codebase-separate-skill: Create `improve-codebase` as independent skill, not a mode of `audit-and-fix-uncommitted-changes`
    Decision: Create a new `improve-codebase` skill for whole-repository audit-and-fix sweeps rather than adding a scope-mode parameter to the existing `audit-and-fix-uncommitted-changes` skill. Accept structural duplication of the audit loop between the two skills.
    Reason: Research shows (1) prompt length alone degrades accuracy starting ~3K tokens (ACL 2024, 2025), (2) conditional branching is among the hardest instruction types (ComplexBench 2024), (3) constraint interference is a primary failure mode in multi-constraint prompts (ComplexBench 2024), (4) Anthropic recommends the routing pattern over mode-switching, (5) Rule of Three says wait until N=3 before extracting shared abstractions (Fowler). The two skills have different domain knowledge (target selection, phases, delegation targets) despite sharing an audit loop shape.
    Tradeoffs: ~70% structural similarity between the two skills is accepted as accidental duplication. If a third audit-loop skill appears, that triggers extraction per Rule of Three. See `docs/SKILL-DESIGN.md` for full research references.

- Decision 2026-03-06 skill-review-scope-dual-mode-close: review-scope dual-mode design is intentional and within thresholds
    Decision: Keep `review-scope` as a single skill with explicit-scope and proactive-hotspot modes. Do not split into separate skills.
    Reason: Deep-dive analysis showed: (1) modes share Phases 2-3, diverging only in Phase 1 target selection; splitting would duplicate ~45% of the skill; (2) constraint count (~46) is under the 50-constraint threshold; (3) target-resolution is a sequential 4-step cascade (not nested branching); (4) ComplexBench shows splitting skills has worse interference than conditional branching within thresholds.
    Tradeoffs: Phase 1 has 3 target-type paths, but they are mutually exclusive and well-scoped.

- Decision 2026-03-06 skill-broader-orchestrator-close: "broader orchestrator" wording is precise and contextually determinable
    Decision: Keep the current "when no broader orchestrator already owns closeout" phrasing in implement-plan, fix-issues, and debug-issue. Do not replace with explicit skill enumeration.
    Reason: Deep-dive analysis showed: (1) the phrase is reliably determinable from conversation context (the agent knows whether it was invoked standalone or by a parent skill); (2) enumerating parent skills would couple these skills to the orchestrator roster, requiring updates whenever an orchestrator is added/removed; (3) all three skills use identical phrasing, confirming it is a stable cross-cutting convention.
    Tradeoffs: Requires the model to reason about invocation context, but this is standard MCP/delegation behavior.

- Decision 2026-03-21 native-skill-sync: Replace MCP prompt delivery with native skill directory sync
    Decision: Removed the internal `al mcp-prompts` MCP server. Claude receives skills via `.claude/skills/`; Codex, Gemini, Antigravity, VS Code/Copilot, and Copilot CLI share `.agents/skills/`. Full subdirectory support (`scripts/`, `references/`, `assets/`) is preserved. Supersedes Decision 2026-01-17 e5f6a7b and Decision 2026-02-23 mcp-prompts-dispatch-bypass.
    Reason: MCP prompts return flat text only, while current client docs support directory-format Agent Skills and several clients now document `.agents/skills/` as an interoperable project path. Shared sync avoids duplicate skill catalogs.
    Tradeoffs: Lost the unified MCP prompt API for skill delivery and stopped generating legacy client-specific skill folders; gained full Agent Skills resource support and fewer duplicate projections.

- Decision 2026-05-07 reasoning-effort-capability-matrix: Reasoning effort is per-client and custom-tolerant where upstream cadence demands it
    Decision: Support Codex and Claude `reasoning_effort` as custom-tolerant fields; reject Gemini/Copilot CLI effort until a verified control exists; for Claude, require Opus, pass all values via `--effort`, and write settings `effortLevel` only for non-`max` values.
    Reason: Silent omission breaks fail-loud behavior, but strict Claude catalogs broke when upstream added values such as `xhigh`; `max` is session-only while low/medium/high/xhigh are cataloged.
    Tradeoffs: Typos surface as sync warnings instead of hard errors for Claude/Codex; Gemini/Copilot configs still fail fast until support is intentionally added.

- Decision 2026-05-07 mcp-catalog-seed-split: MCP server catalog moves to wizard-only embedded file; install seed ships zero `[[mcp.servers]]`
    Decision: Default MCP blocks now live in a wizard-only embedded catalog; the install seed ships only an empty `[mcp]` section with docs guidance. Interactive wizard selection inserts selected catalog blocks and prunes disabled catalog IDs, including customized variants. Profile mode remains verbatim and does not prune.
    Reason: Fresh `al init` should produce a minimal config instead of ~70 disabled MCP lines, while the wizard remains the place to discover curated defaults.
    Tradeoffs: Unticking a customized catalog ID in the interactive wizard removes that block after diff preview confirmation. Profile-mode users keep full responsibility for whatever their profile contains.

- Decision 2026-05-09 claude-agent-specific-deep-merge: Claude agent_specific deep-merge with additive permissions.deny default
    Decision: Claude `agent_specific` is deep-merged into `.claude/settings.json` for object values (arrays and scalars still replace at their key). `permissions.deny` is additive and silent; `permissions.allow` and `effortLevel` still trigger override warnings. Install seed ships `agent_specific.permissions.deny = ["AskUserQuestion"]` so new repos disable Claude Code's structured clarification tool by default.
    Reason: Shallow-replace forced users to repeat the entire `permissions` block (re-listing managed `allow` entries) just to add a `deny`. The default deny enforces text-only clarifications, which agents in this repo prefer. Asymmetry vs Codex `agent_specific` is intentional: Codex projection still emits TOML at the root and remains shallow.
    Tradeoffs: Override-warning logic for Claude is bespoke (`claudeAgentSpecificOverrideWarning`) instead of the generic reserved-keys helper. Slices in custom values are not deep-cloned (only maps are); safe today because settings are marshaled and discarded, but future mutators would alias the user's config map.

- Decision 2026-05-07 gemini-policy-engine: Migrate Gemini allowlist to Policy Engine TOML with explicit `policyPaths`
    Decision: Stop emitting `tools.allowed` in `.gemini/settings.json`. Generate `.gemini/policies/agent-layer.toml` with one `[[rule]]` block per allowed shell command (`toolName = "run_shell_command"`, `commandPrefix = <cmd>`, `decision = "allow"`, `priority = 100`, `allowRedirection = true`), and write `policyPaths: [".gemini/policies"]` in settings.json so Gemini CLI loads the file.
    Reason: Gemini CLI v0.30+ deprecates `tools.allowed` ("removed in 1.0"). Workspace tier `.gemini/policies/` is NOT auto-loaded by Gemini CLI 0.41.2 (verified by deliberately corrupting a TOML — Gemini ignored it without `policyPaths` and parsed-and-erred with it set), so an explicit pointer is required. `commandPrefix` mirrors the previous `run_shell_command(<cmd>)` substring semantics; `commandRegex` would have required escaping plain user-provided strings. `allowRedirection = true` preserves the prior `tools.allowed` behavior for headless workflows that pipe output (`git ... > out.txt`); without it the engine would prompt for confirmation even when a rule matches.
    Tradeoffs: Two managed artifacts per repo instead of one; users on Gemini CLI < v0.18 (pre-policy-engine) lose the auto-allow behavior, but that release is over six months old.
