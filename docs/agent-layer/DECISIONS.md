# Decisions

Purpose: Rolling log of important decisions (brief).

Notes for updates:
- Add an entry when making a significant decision (architecture, storage, data model, interface boundaries, dependency choice).
- Keep entries brief.
- Keep the oldest decisions near the top and add new entries at the bottom.
- Lines below the first line must be indented by 4 spaces so they stay associated with the entry.

Entry format:
- Decision YYYY-MM-DD abcdef: Short title
    Decision: <what was chosen>
    Reason: <why it was chosen>
    Tradeoffs: <what is gained and what is lost>

## Decision Log

<!-- ENTRIES START -->

- Decision 2026-01-17: Rewrite Agent Layer in Go
    Decision: Implement Agent Layer as a Go CLI with the same feature set as the existing system.
    Reason: Improve end-user adoption via repo-local binary distribution, simpler setup, and fewer runtime dependencies.
    Tradeoffs: Requires re-implementing generators and launch behavior; changes internal architecture.

- Decision 2026-01-17: Repo-local executable in repo root
    Decision: Install a repo-local executable at `./al` and gitignore it by default.
    Reason: Maximizes adoption by avoiding global installs and keeping the tool “per repo”.
    Tradeoffs: Requires an installer step per repo; multiple repos mean multiple copies of the binary.

- Decision 2026-01-17: Installation via explicit shell installer + repo-local init
    Decision: Publish a single installer script named `agent-layer-install.sh` that downloads the correct platform binary into `./al` and then runs `./al install`.
    Reason: A one-command install that does not require Go on user machines is the highest-adoption path for a repo-local binary.
    Tradeoffs: Requires maintaining a shell installer and per-platform release artifacts.

- Decision 2026-01-17: `.agent-layer/` is configuration only
    Decision: `.agent-layer/` contains only user-editable configuration (no runtime code).
    Reason: Removes nested-repo and root-resolution complexity; makes config easy to reason about and optionally share.
    Tradeoffs: Code updates happen via binary upgrades rather than git-pulling `.agent-layer/`.

- Decision 2026-01-17: Single human-editable config file
    Decision: Use `.agent-layer/config.toml` as the single structured configuration file (agents, models, approvals, MCP server definitions).
    Reason: One file is easier to learn, review, and edit than multiple JSON configs.
    Tradeoffs: Requires schema validation and clear comments to prevent misconfiguration.

- Decision 2026-01-17: Human-friendly formats
    Decision: Use TOML for structured config and line-based files for allowlists.
    Reason: JSON is error-prone to edit by hand; TOML supports comments and is readable.
    Tradeoffs: Adds TOML parsing dependency and migration considerations.

- Decision 2026-01-17: Approvals use a 4-mode policy
    Decision: Implement approvals via `approvals.mode ∈ {all, mcp, commands, none}` and project the closest supported behavior per client.
    Reason: Users must understand what is auto-approved; a small fixed set of modes is easier than per-client knobs.
    Tradeoffs: Some clients cannot support all approval types; behavior may differ slightly across clients.

- Decision 2026-01-17: Project memory lives under `docs/agent-layer/` and is required
    Decision: Always ensure project memory exists at `docs/agent-layer/` (templates created if missing).
    Reason: Default instructions and workflows rely on these files; teams can choose whether to commit or ignore them.
    Tradeoffs: Installer modifies the repo by adding files under `docs/agent-layer/`.

- Decision 2026-01-17: Always sync on agent launch
    Decision: `./al <client>` always regenerates configs from `.agent-layer/` sources before launching the client.
    Reason: “Always up to date” is the core product value and prevents drift.
    Tradeoffs: Slightly slower launches; optimization can be added later.

- Decision 2026-01-17: MCP servers are user-defined and support any HTTP or stdio server
    Decision: Define external MCP servers in `config.toml` as a list (`[[mcp.servers]]`) supporting `transport ∈ {http, stdio}`; installer seeds a small library of defaults that users can edit, disable, or delete.
    Reason: Users must be able to use arbitrary local/remote MCP servers while still having good defaults.
    Tradeoffs: Requires robust validation and per-client projection logic; some clients may not support all server features (e.g., custom HTTP headers).

- Decision 2026-01-17: MCP prompt server is internal and automatic
    Decision: Implement an internal MCP prompt server inside `./al` (e.g., `./al mcp-prompts`) to expose slash commands via MCP prompts where needed; do not expose it as a user-configured MCP server.
    Reason: Keeps external MCP server configuration focused on tool/data servers; prompt discovery becomes consistent and automatic.
    Tradeoffs: Requires careful stdio server behavior and stable prompt schema.

- Decision 2026-01-17: CODEX_HOME support via a dedicated VS Code launcher path
    Decision: Provide a first-class way to launch VS Code with `CODEX_HOME` set for the repo (CLI subcommand and optional OS-native launcher artifacts).
    Reason: The Codex VS Code extension reads CODEX_HOME at process start; this must be reliable and easy.
    Tradeoffs: OS-specific launchers require additional packaging/testing.

- Decision 2026-01-17: Concurrency-safe run directories for workflow artifacts
    Decision: Each invocation gets a unique run directory under `tmp/agent-layer/runs/<run-id>/` and exports `AL_RUN_DIR`.
    Reason: Prevent collisions when multiple agents run concurrently and keep tmp artifacts isolated.
    Tradeoffs: Requires lifecycle guidance (cleanup, retention policy).

- Decision 2026-01-17: Full parity across clients + Antigravity partial support
    Decision: Maintain 100% feature parity with the current system for Gemini/Claude/VS Code/Codex; include Antigravity support for instructions and slash commands only.
    Reason: Adoption requires consistent behavior and avoiding regressions; Antigravity remains intentionally limited.
    Tradeoffs: Antigravity remains a best-effort integration rather than a fully supported client.

- Decision 2026-01-17: Cobra for CLI command structure
    Decision: Use `github.com/spf13/cobra` for the `al` CLI command structure.
    Reason: Cobra is the de facto standard in Go CLIs, supports subcommands and help formatting, and keeps the command tree readable.
    Tradeoffs: Adds a dependency and a small amount of boilerplate.

- Decision 2026-01-17: go-toml/v2 for config parsing
    Decision: Use `github.com/pelletier/go-toml/v2` to parse `.agent-layer/config.toml`.
    Reason: It is stable, widely used, and supports modern TOML features with good error messages.
    Tradeoffs: Adds a dependency and couples the config schema to TOML parsing behavior.

- Decision 2026-01-17: MCP prompt server via go-sdk
    Decision: Implement the internal MCP prompt server using `github.com/modelcontextprotocol/go-sdk`.
    Reason: Keeps the Go implementation aligned with the official MCP protocol and avoids maintaining a custom server.
    Tradeoffs: Adds a dependency that must track MCP protocol changes.

- Decision 2026-01-17: Shared projection and launch pipeline
    Decision: Centralize MCP/approvals projection and client launch into shared helpers.
    Reason: Keeps client outputs consistent and reduces duplicate logic across generators and launchers.
    Tradeoffs: Adds abstraction layers that require clear testing.

- Decision 2026-01-17: Tooling baseline with CI enforcement
    Decision: Standardize on gofmt/goimports, golangci-lint, pre-commit hooks, and a CI coverage gate (>= 95%).
    Reason: Keeps formatting, linting, and test quality consistent across contributions.
    Tradeoffs: Requires tool installation and increases CI runtime.

- Decision 2026-01-18: Makefile-based workflow with repo-local tools
    Decision: Use Makefile targets for format/lint/test/ci and install pinned tools into `.tools/bin` via `make tools`, with checks failing fast if tools are missing.
    Reason: Keeps the workflow consistent, repo-local, and avoids PATH mutations or hidden installs.
    Tradeoffs: Requires a one-time tools install per clone and explicit tool setup in CI.

- Decision 2026-01-18: Preserve env var placeholders in generated client configs
    Decision: Never embed actual secret values in generated config files; use client-specific placeholder syntax that each client resolves at runtime (Gemini: `${VAR}`, Claude: `${VAR}`, VS Code: `${env:VAR}`, Codex: `bearer_token_env_var`).
    Reason: Prevents accidental secret exposure if generated configs are committed; aligns with each client's documented best practice.
    Tradeoffs: Users running clients directly (not via `./al <client>`) must have tokens in their shell environment.

- Decision 2026-01-19: Use charmbracelet/huh for Wizard UI
    Decision: Use `github.com/charmbracelet/huh` for the `al wizard` interactive TTY interface.
    Reason: Provides a polished, accessible, and declarative API for forms, selects, and inputs in Go.
    Tradeoffs: Adds a dependency; restricted to TTY environments (wizard is interactive-only).

- Decision 2026-01-19 7f3c2a1: Centralize env parsing and file writes
    Decision: Add shared `internal/envfile` and `internal/fsutil` helpers, and derive wizard defaults from the embedded config template.
    Reason: Removes duplicated parsing/writing logic and keeps defaults and required secrets aligned with the template source of truth.
    Tradeoffs: Adds internal packages and tighter coupling to template config structure; introduces breaking internal API changes.

- Decision 2026-01-19 a4b7c2d: Use go-toml v1 tree editor for wizard patching
    Decision: Use `github.com/pelletier/go-toml` v1 Tree APIs for wizard-driven config edits while keeping go-toml/v2 for schema parsing.
    Reason: go-toml/v2 has no stable document model; v1 Tree allows structured edits without brittle string scanning.
    Tradeoffs: Adds a second TOML dependency and may reformat output during edits.

- Decision 2026-01-19 b7d3f2e: Keep huh-pinned Charmbracelet dependency versions
    Decision: Retain the Charmbracelet bubbles/colorprofile versions required by `github.com/charmbracelet/huh` v0.8.0 instead of overriding to newer tags.
    Reason: Upstream pins pseudo versions; overriding them risks incompatibility without upstream support.
    Tradeoffs: go.mod includes pre-release pseudo versions until upstream tags stable releases.

- Decision 2026-01-20 e327f69: Prompt to run wizard after install
    Decision: After `al install`, prompt to launch the setup wizard by default; skip in non-interactive shells or with `--no-wizard`.
    Reason: Keep defaults safe while guiding users through setup without reading docs.
    Tradeoffs: Adds an extra prompt during install and requires an opt-out flag.

- Decision 2026-01-20 488b743: Memory file references use filenames after initial path list
    Decision: List full memory file paths once in the memory instruction file, then use filename-only references in instructions and slash commands.
    Reason: Reduce visual clutter while keeping a single canonical location for memory file paths.
    Tradeoffs: Readers must consult the memory instruction file to see the full paths.

- Decision 2026-01-20 3859afb: `.agent-layer/.env` does not override shell environment
    Decision: When launching via `./al`, use `.agent-layer/.env` to fill missing environment variables only and ignore empty values; never override existing process environment variables.
    Reason: Prevent empty template keys (and `.agent-layer/.env` values) from shadowing real tokens set in the shell while still allowing placeholder-based config generation.
    Tradeoffs: Users must unset a shell variable if they want the `.agent-layer/.env` value to take effect.

- Decision 2026-01-20 f9a8b7c: Remove unsupported `--reasoning-effort` flag for Codex CLI
    Decision: Removed the `--reasoning-effort` flag from the `al codex` launch command.
    Reason: The current Codex CLI (v0.80.0) does not support this flag directly; the configuration is already correctly handled via the `.codex/config.toml` file generated during the sync phase.
    Tradeoffs: Simplifies the launch arguments; ensures compatibility with the existing Codex CLI while maintaining the desired configuration via the config file.

- Decision 2026-01-20 7c2a9fd: Antigravity slash commands exported as skills
    Decision: Map slash command definitions to Antigravity workspace skills at `.agent/skills/<command>/SKILL.md`.
    Reason: Antigravity documents skills as the workspace-scoped format for reusable command workflows.
    Tradeoffs: Skills are agent-triggered rather than explicit slash-invoked workflows, so behavior may differ from other clients.

- Decision 2026-01-20 b9212c2: Embed resolved secrets in Codex config (reverses part of 2026-01-18 decision)
    Decision: For Codex specifically, resolve and embed actual secret values in `.codex/config.toml` for URLs and stdio env vars. Keep `bearer_token_env_var` for Authorization headers. Add a warning header about secrets.
    Reason: Codex CLI does not support `${VAR}` placeholder syntax in URLs or env values like Claude Code does. The `.codex/` directory is gitignored, so embedding secrets is acceptable.
    Tradeoffs: Secrets appear in the generated file; the warning header and gitignore mitigate accidental exposure.
