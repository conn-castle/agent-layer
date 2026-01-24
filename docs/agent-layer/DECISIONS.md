# Decisions

Purpose: Rolling log of important decisions (brief).

Notes for updates:
- Add an entry when making a significant decision (architecture, storage, data model, interface boundaries, dependency choice).
- Keep entries brief.
- Do not log decisions that have no future ramifications or simply restate best practices or existing instructions.
- Keep the oldest decisions near the top and add new entries at the bottom.
- Lines below the first line must be indented by 4 spaces so they stay associated with the entry.

Entry format:
- Decision YYYY-MM-DD abcdef: Short title
    Decision: <what was chosen>
    Reason: <why it was chosen>
    Tradeoffs: <what is gained and what is lost>

## Decision Log

<!-- ENTRIES START -->

- Decision 2026-01-17 a1b2c3d: Distribution model (repo-local Go binary)
    Decision: Ship as a repo-local Go binary installed via a shell script that downloads platform-specific releases. No global install, no runtime dependencies.
    Reason: Maximizes adoption by avoiding global installs and keeping the tool per-repo; Go binaries eliminate Node.js and Python runtime requirements.
    Tradeoffs: Requires installer step per repo; multiple repos mean multiple binary copies.
    Notes: Superseded by Decision 2026-01-22 f1e2d3c (global CLI with pinning).

- Decision 2026-01-17 b2c3d4e: Configuration approach (TOML in .agent-layer/)
    Decision: All user configuration lives in `.agent-layer/` as human-editable files: `config.toml` for structured settings, numbered `.md` files for instructions, and line-based files for allowlists.
    Reason: TOML supports comments and is readable; separating configuration from code simplifies reasoning and sharing.
    Tradeoffs: Code updates require binary upgrades; schema validation needed to prevent misconfiguration.

- Decision 2026-01-17 c3d4e5f: Project memory required under docs/agent-layer
    Decision: Always ensure project memory exists under `docs/agent-layer/` (create templates when missing).
    Reason: Default instructions and workflows rely on these files.
    Tradeoffs: The installer adds files under `docs/agent-layer/`.

- Decision 2026-01-17 d4e5f6a: Always sync on agent launch
    Decision: `al <client>` always regenerates client configs from `.agent-layer/` sources before launching.
    Reason: "Always up to date" is the core product value; it prevents configuration drift.
    Tradeoffs: Slightly slower launches; optimization can be added later.

- Decision 2026-01-17 e5f6a7b: MCP architecture (external servers + internal prompt server)
    Decision: External MCP servers are user-defined in `config.toml` with HTTP or stdio transports. The internal prompt server (`al mcp-prompts`) exposes slash commands automatically and is not user-configured.
    Reason: Users need arbitrary MCP servers while slash command discovery should be consistent and automatic.
    Tradeoffs: Requires per-client projection logic; some clients may not support all server features.

- Decision 2026-01-17 f6a7b8c: Approvals policy (4-mode system)
    Decision: Implement `approvals.mode` with four options: `all`, `mcp`, `commands`, `none`. Project the closest supported behavior per client.
    Reason: Users must understand what is auto-approved; a small fixed set is easier than per-client knobs.
    Tradeoffs: Some clients cannot support all approval types; behavior may differ slightly across clients.

- Decision 2026-01-17 a7b8c9d: Launchers for CODEX_HOME
    Decision: Provide repo-specific VS Code launchers that set `CODEX_HOME` for the repository.
    Reason: The extension reads `CODEX_HOME` at process start; this must be reliable and easy to use.
    Tradeoffs: Requires operating system-specific launchers and packaging.

- Decision 2026-01-17 b8c9d0e: Concurrency-safe run directories
    Decision: Each invocation uses a unique run directory under `tmp/agent-layer/runs/<run-id>/` and exports `AL_RUN_DIR`.
    Reason: Avoid collisions when multiple agents run concurrently and keep artifacts isolated.
    Tradeoffs: Requires lifecycle guidance for cleanup and retention.

- Decision 2026-01-17 c9d0e1f: Client parity (Antigravity partial support)
    Decision: Full feature parity for Gemini, Claude, VS Code, and Codex. Antigravity supports instructions and slash commands only (no MCP, no approvals).
    Reason: Antigravity integration is best-effort; core clients must have consistent behavior.
    Tradeoffs: Antigravity users have a limited experience.

- Decision 2026-01-18 d0e1f2a: Development tooling (Makefile + 95% coverage gate)
    Decision: Use Makefile targets with repo-local tools in `.tools/bin`. Enforce 95% test coverage in continuous integration.
    Reason: Keeps the workflow consistent and avoids PATH mutations; high coverage ensures reliability.
    Tradeoffs: Requires `make tools` per clone; increases continuous integration runtime.

- Decision 2026-01-18 e1f2a3b: Secret handling (placeholders with Codex exception)
    Decision: Generated configs use client-specific placeholder syntax so secrets are never embedded. Exception: Codex embeds secrets in URLs and stdio environment values and uses `bearer_token_env_var` for Authorization headers.
    Reason: Prevents accidental secret exposure; Codex limitations require an exception.
    Tradeoffs: Users running clients directly need tokens in the shell environment; Codex secrets appear in generated files.

- Decision 2026-01-19 f2a3b4c: Configuration editing internals (go-toml v2 + v1 tree + envfile/fsutil)
    Decision: Parse configuration with `go-toml/v2`; use `go-toml` v1 tree edits for the wizard; centralize env parsing and file writes in `internal/envfile` and `internal/fsutil`.
    Reason: Stable parsing, safe structured edits, and shared input and output logic.
    Tradeoffs: Two TOML dependencies and tighter coupling to template structure; edits may reformat output.

- Decision 2026-01-19 7f3c2a1: Wizard dependencies (charmbracelet/huh with pre-release transitive deps)
    Decision: Use `github.com/charmbracelet/huh` for the wizard UI. Accept its transitive pre-release pseudo-version dependencies until upstream tags stable releases.
    Reason: Provides the best interactive TTY experience; overriding upstream pins risks incompatibility.
    Tradeoffs: go.mod includes pre-release versions; wizard is TTY-only.

- Decision 2026-01-20 3859afb: Environment variable precedence
    Decision: `.agent-layer/.env` fills missing environment variables only; never overrides existing shell environment; empty values are ignored.
    Reason: Prevent template placeholders from shadowing real tokens.
    Tradeoffs: Users must unset shell variables to use `.agent-layer/.env` values.

- Decision 2026-01-20 7c2a9fd: Antigravity slash commands as skills
    Decision: Map slash commands to Antigravity skills at `.agent/skills/<command>/SKILL.md`.
    Reason: Antigravity documents skills as the workspace format for reusable workflows.
    Tradeoffs: Skills are agent-triggered rather than explicit slash-invoked.

- Decision 2026-01-21 bb93bc0: Sync warnings (configurable thresholds with opt-out)
    Decision: Warning thresholds for instruction token count and MCP checks are configurable via `config.toml` with pointer fields (nil disables the warning). Token estimation uses a byte/rune heuristic (max(bytes/3, runes/4) with 10% buffer).
    Reason: Users need control over warning thresholds without exposing estimation internals; nil pointers clearly indicate disabled state.
    Tradeoffs: Pointer fields require careful handling in code; wizard must support "disable" as a selection option.

- Decision 2026-01-22 f1e2d3c: Distribution model (global CLI with per-repo pinning)
    Decision: Ship a single globally installed `al` CLI with per-repo version pinning via `.agent-layer/al.version` and cached binaries.
    Reason: A single entrypoint reduces support burden while pinning keeps multi-repo setups reproducible.
    Tradeoffs: Requires cache management, download verification, and dispatch logic.

- Decision 2026-01-23 0d63d53: Release source tarball for Homebrew
    Decision: Generate and publish a versioned source tarball (`agent-layer-<version>.tar.gz`, without a leading `v`) via `git archive` + `gzip -n`, and include it in `checksums.txt`.
    Reason: GitHub auto-generated source archives can change checksums; Homebrew needs a stable tarball URL.
    Tradeoffs: Release pipeline requires git/gzip and an additional release asset to maintain.
