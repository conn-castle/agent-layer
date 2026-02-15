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

- Decision 2026-01-17 f6a7b8c: Approvals policy (4-mode system)
    Decision: Implement `approvals.mode` with four options: `all`, `mcp`, `commands`, `none`. Project the closest supported behavior per client.
    Reason: A small fixed set is easier to understand than per-client knobs; behavior may differ slightly across clients.

- Decision 2026-01-17 a7b8c9d: VS Code launchers for CODEX_HOME
    Decision: Provide repo-specific VS Code launchers that set `CODEX_HOME` at process start.
    Reason: The Codex extension reads `CODEX_HOME` only at startup; launchers ensure correct repo context.

- Decision 2026-01-17 c9d0e1f: Antigravity limited support
    Decision: Antigravity supports instructions and slash commands only (no MCP, no approvals). Slash commands map to skills at `.agent/skills/<command>/SKILL.md`.
    Reason: Antigravity integration is best-effort; core clients (Gemini, Claude, VS Code, Codex) have full parity.

- Decision 2026-01-18 e1f2a3b: Secret handling (Codex exception)
    Decision: Generated configs use client-specific placeholder syntax so secrets are never embedded. Exception: Codex embeds secrets in URLs/env and uses `bearer_token_env_var` for headers. Shell environment takes precedence over `.agent-layer/.env`.
    Reason: Prevents accidental secret exposure; Codex limitations require an exception.

- Decision 2026-01-22 f1e2d3c: Distribution model (global CLI with per-repo pinning)
    Decision: Ship a single globally installed `al` CLI with per-repo version pinning via `.agent-layer/al.version` and cached binaries.
    Reason: A single entrypoint reduces support burden while pinning keeps multi-repo setups reproducible.

- Decision 2026-01-24 a1b2c3d: Ignore unexpected working tree changes
    Decision: Agents will not pause, warn, or stop due to unexpected working tree changes (unstaged or staged files not created by the agent).
    Reason: The user works in parallel with agents, making concurrent changes a normal operating condition.
    Tradeoffs: Increases risk of edit conflicts if both user and agent modify the same file simultaneously; relies on git for resolution.

- Decision 2026-01-25 edefea6: Sync dependency injection for system calls
    Decision: Added a `System` interface with a `RealSystem` implementation and threaded it through `internal/sync` writers and prompt resolution instead of patching globals.
    Reason: Removes test-only global state and enables parallel-safe unit tests.
    Tradeoffs: Adds `sys System` parameters and test stubs for filesystem/process operations.

- Decision 2026-01-25 b4c5d6e: Centralize VS Code launcher paths
    Decision: Centralize VS Code launcher paths in `internal/launchers` and consume them from sync and install.
    Reason: Single source of truth prevents drift and accidental deletion of generated artifacts.
    Tradeoffs: Adds a small shared package dependency for sync and install.

- Decision 2026-01-25 f3a9d1: Freeze repo-local .agent-layer updates
    Decision: Do not manually update `.agent-layer/` in this repo; use a migration later.
    Reason: Preserve the current `.agent-layer/` state for testing migration behavior in a future release.
    Tradeoffs: Repo-local instructions may drift from templates until the migration is exercised.

- Decision 2026-01-25 7e2c9f4: Agent-only workflow artifacts live in `.agent-layer/tmp`
    Decision: Workflow artifacts are written to `.agent-layer/tmp` using a unique per-invocation filename: `.agent-layer/tmp/<workflow>.<run-id>.<type>.md` with `run-id = YYYYMMDD-HHMMSS-<short-rand>`; no path overrides.
    Reason: Keeps artifacts invisible to humans while avoiding collisions for concurrent agents without relying on env vars or per-chat IDs.
    Tradeoffs: Files can accumulate until manually cleaned; agents must echo paths in chat to retain context.

- Decision 2026-01-26 999bc79: Centralize MCP server resolution in projection package
    Decision: MCP server resolution logic and the `ResolvedMCPServer` type now live in `internal/projection`. The warnings package imports projection for MCP resolution instead of maintaining duplicate code.
    Reason: Eliminates DRY violation where identical resolution logic existed in both projection and warnings packages.
    Tradeoffs: Warnings package now depends on projection; acceptable since projection is a lower-level utility.

- Decision 2026-01-27 d4e7a1b: VS Code settings merge scoped to managed block
    Decision: When the managed markers exist in `.vscode/settings.json`, update only the managed block and do not validate unrelated JSONC content; if markers are missing, parse the root object to insert the block.
    Reason: Avoid partial JSONC parsing dependencies while still supporting first-time insertion.
    Tradeoffs: Invalid JSONC outside the managed block is no longer detected once the markers are present.

- Decision 2026-01-28 5c8e2a1: Codex custom MCP headers
    Decision: Codex projects MCP headers using `bearer_token_env_var` for `Authorization: Bearer ${VAR}`, `env_http_headers` for exact `${VAR}` values, and `http_headers` for literals; other placeholder formats error.
    Reason: Support custom headers across clients without embedding secrets or relying on placeholder expansion in Codex.
    Tradeoffs: Headers with mixed literal + env placeholder (for example, `Token ${VAR}`) are rejected for Codex and must be restructured.

- Decision 2026-01-28 2f8a4e1: Breakdown MCP tool token counts in doctor warnings
    Decision: Provide a breakdown of the top tools by token count in `MCP_TOOL_SCHEMA_BLOAT_SERVER` warnings.
    Reason: Better visibility into which tools contribute to schema bloat allows targeted optimization.
    Tradeoffs: Adds per-tool JSON marshaling during discovery, slightly increasing check latency.

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

- Decision 2026-02-05 f7a3c9d: Wizard config output uses canonical template order
    Decision: The wizard always rewrites `config.toml` in the template-defined order, rather than preserving the existing file layout.
    Reason: Produces deterministic output and reinforces that the wizard is the authoritative manager of config structure.
    Tradeoffs: Manual layout tweaks and some inline comment placement may be reordered on each wizard run.

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

- Decision 2026-02-09 p1-upgrade-plan-heuristics: Upgrade planning compares invoking binary templates with conservative labels (superseded by `p1b-ownership-baseline`)
    Decision: `al upgrade` bypasses repo pin dispatch, `al upgrade plan` compares repo state to the invoking binary's embedded templates, rename detection uses unique exact normalized-content hash matches, and ownership labels are best-effort without a managed-file hash manifest.
    Reason: Upgrade previews must reflect the version the user is actively running while still surfacing useful diffs now, before migration manifests and stronger ownership baselines land.
    Tradeoffs: Ambiguous rename/ownership cases fall back to non-rename + `local customization`, so some true upstream deltas may be under-labeled until manifest/baseline infrastructure is added.

- Decision 2026-02-09 p1b-ownership-baseline: Ownership classification now uses embedded manifests plus canonical baseline state
    Decision: `al upgrade plan` ownership classification now uses committed per-release manifests (`internal/templates/manifests/*.json`) and canonical repo baseline state (`.agent-layer/state/managed-baseline.json`) with section-aware policies (`memory_entries_v1`, `memory_roadmap_v1`, `allowlist_lines_v1`), and emits `unknown_no_baseline` when evidence is insufficient.
    Reason: Distinguishes upstream template deltas from true local customization without runtime network/tag lookups and avoids silent guesses in ambiguous cases.
    Tradeoffs: Release workflow must generate/commit manifests for each tag; repos lacking credible baseline evidence may show `unknown no baseline` until a baseline refresh run (for example `al upgrade`).

- Decision 2026-02-10 cov-stab: Replace chmod-based error injection with function-variable stubs
    Decision: Replace all `chmod 0o000`/`0o444` + `t.Skip` error injection in tests with deterministic function-variable stubs (`var osFunc = os.Func` + path-selective overrides + `t.Cleanup` restore).
    Reason: chmod-based tests skipped on platforms that don't enforce permission denial (macOS root, CI), causing non-deterministic coverage between CI and local runs.
    Tradeoffs: Adds package-level function variables to production code; acceptable since the pattern is already established in `cmd/al/` and `internal/install/`.

- Decision 2026-02-10 p1c-init-upgrade-ownership: Init scaffolding only; user-owned config/env; agent-only .gitignore
    Decision: `al init` is one-time scaffolding (errors if `.agent-layer/` already exists). Upgrades/repairs are done via `al upgrade plan` + `al upgrade`. `.agent-layer/.env` and `.agent-layer/config.toml` are user-owned and seeded only when missing (never overwritten by init/upgrade). `.agent-layer/.gitignore` is agent-owned internal and is always overwritten and excluded from upgrade plans/diffs.
    Reason: Avoid accidental clobbering of user-specific configuration, reduce cognitive load in upgrade plans, and simplify init semantics by removing upgrade behavior.
    Tradeoffs: Changes to `.agent-layer/.gitignore` cannot be preserved; repos without baseline evidence will require an `al upgrade` run to establish it (supersedes earlier init-overwrite guidance).

- Decision 2026-02-10 pin-required: Pinning required for supported repos
    Decision: Treat `.agent-layer/al.version` as required and do not support or document an end-user “unpin” workflow.
    Reason: Unpinned repos can silently drift when developers upgrade the global `al` install, causing hard-to-debug mismatches.
    Tradeoffs: Advanced users can still delete the pin file manually, but that is unsupported and reduces reproducibility.

- Decision 2026-02-11 p1d-json-contract: Upgrade-plan JSON is diagnostic, not a stable public schema contract (superseded by `p3b-hard-removals`)
    Decision: Keep `al upgrade plan --json` as optional diagnostic output, but do not guarantee a stable field-level schema for CI automation.
    Reason: There are currently no first-party automation consumers; locking the schema now would add compatibility burden without immediate product value.
    Tradeoffs: External automation must pin CLI versions (or parse defensively) if it chooses to consume `--json` output.

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

- Decision 2026-02-14 p1g-upgrade-everyday-output: Default upgrade UX is plain-language; JSON retained temporarily (superseded by `p3b-hard-removals`)
    Decision: `al upgrade plan` default text output now prioritizes plain-language summaries/actions and no longer surfaces ownership reason codes, confidence/detection metadata, or readiness IDs; at that time `--json` remained only as a hidden temporary compatibility path (supersedes the user-facing part of `p1d-json-contract`).
    Reason: Everyday users needed low-jargon guidance while existing automation had a short transition window before full JSON removal.
    Tradeoffs: Power users lost internal diagnostics in the default path and had to use the temporary hidden JSON mode or source-level inspection during the transition.

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
