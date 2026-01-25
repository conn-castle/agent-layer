# Decisions

Note: This is an agent-layer memory file. It is primarily for agent use.

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
