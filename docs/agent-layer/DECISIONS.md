# Decisions

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
A rolling log of important, non-obvious decisions that materially affect future work (constraints, deferrals, irreversible tradeoffs). Only record decisions that future developers/agents would not learn just by reading the code.

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

- Decision 2026-01-22 f1e2d3c: Distribution model (global CLI with per-repo pinning)
    Decision: Ship a single globally installed `al` CLI with per-repo version pinning via `.agent-layer/al.version` and cached binaries.
    Reason: A single entrypoint reduces support burden while pinning keeps multi-repo setups reproducible.

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

- Decision 2026-01-24 a1b2c3d: Ignore unexpected working tree changes
    Decision: Agents will not pause, warn, or stop due to unexpected working tree changes (unstaged or staged files not created by the agent).
    Reason: The user works in parallel with agents, making concurrent changes a normal operating condition.
    Tradeoffs: Increases risk of edit conflicts if both user and agent modify the same file simultaneously; relies on git for resolution.

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
    Tradeoffs: Legacy blocks with markers/hash now require `al init --overwrite` to regenerate before `al sync` will succeed.

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
    Decision: `readPinnedVersion()` treats empty and non-semver pin files as "no pin" (returns a warning string instead of an error). `writeVersionFile()` auto-repairs empty/corrupt pins without requiring `--overwrite`.
    Reason: A broken pin file should never make the CLI completely unusable. `al init` must always be able to self-heal the pin state.
    Tradeoffs: Corrupt pins silently fall through to the current binary version; users see a warning but may not notice it in noisy terminal output.
