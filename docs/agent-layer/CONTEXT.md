# Context

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
Persistent project-specific knowledge that does not belong in ISSUES, BACKLOG, ROADMAP, DECISIONS, or COMMANDS. Read this file before starting work on a task.

Record three categories of information here:
1. **Project context** — domain concepts, architectural invariants, naming conventions, external dependencies, environment setup notes, team norms, and any other stable facts an agent needs to work effectively in this repository.
2. **Project-specific nuances** — non-obvious behaviors, implicit conventions, or user-provided clarifications that an agent would not discover from reading the code alone. When a user corrects a misunderstanding or explains how something actually works in this project, record it here.
3. **Lessons learned** — repeated mistakes, surprising behaviors, non-obvious gotchas, and corrective patterns discovered during development. When an error recurs or a workaround is needed more than once, record it here so future agents avoid the same mistake.

Do not duplicate information that belongs in other memory files:
- Deferred bugs or tech debt → ISSUES.md
- Planned features → BACKLOG.md
- Workflow commands → COMMANDS.md
- Non-obvious decisions → DECISIONS.md
- Phased plans → ROADMAP.md

## Format
- Organize by topic using headings (`##`, `###`).
- Prefer concise bullet points. State facts directly; omit hedging language.
- Before adding an entry, search this file for existing coverage. Merge into or update an existing section instead of creating a near-duplicate.
- Remove or update entries when the underlying facts change.
- Insert all content below `<!-- ENTRIES START -->`.

<!-- ENTRIES START -->

## Secrets

- Codex is the one exception to "never embed secrets in generated configs": it embeds secrets in URLs/env via `bearer_token_env_var`, and shell environment takes precedence over `.agent-layer/.env`. All other clients use placeholder syntax in generated configs.

## VS Code and editor integrations

- The Codex VS Code extension reads `CODEX_HOME` only at process startup. The generated repo-specific launchers exist to set `CODEX_HOME` before launch — there is no in-extension reload path.
- The Claude VS Code extension shares MCP scope with Claude CLI: both read the same `.mcp.json`/`.claude/settings.json`. Configuring separate MCP server sets for the two surfaces is not possible. `[agents.claude_vscode]` is config-only.
- `.vscode/settings.json` updates only validate JSONC content inside the managed markers. Invalid JSONC outside the markers is not detected once the markers are present.

## Codex MCP headers

- Codex MCP header projection accepts exactly three placeholder formats: `bearer_token_env_var` for `Authorization: Bearer ${VAR}`, `env_http_headers` for exact `${VAR}` values, and `http_headers` for literal strings. Mixed literal + env placeholder (e.g. `Token ${VAR}`) is rejected and must be restructured.

## Wizard feature-disable toggles → client keys

- The wizard's per-feature "disable" toggles map to these `agent_specific` keys (written only when the user opts in; absence = client default):
  - Codex (`agents.codex.agent_specific.features`, appended to `.codex/config.toml`): `browser_use`/`in_app_browser`/`computer_use = false` (browser/computer-use), `apps = false` (built-in apps).
  - Claude (`agents.claude.agent_specific`, deep-merged into `.claude/settings.json`): `env.CLAUDE_CODE_AUTO_CONNECT_IDE = "false"` (IDE open-file reading), `env.ENABLE_CLAUDEAI_MCP_SERVERS = "false"` (claude.ai connectors), `autoMemoryEnabled = false` (auto-memory), and `permissions.deny = ["AskUserQuestion"]` + `hooks.PreToolUse` matcher (AskUserQuestion).
- Value types: settings.json `env` values are JSON strings, so the wizard writes the quoted string `"false"`; `autoMemoryEnabled` and the Codex `features.*` flags are booleans (`false`). `CLAUDE_CODE_DISABLE_AUTO_MEMORY` is deliberately not used (it takes `1`/`0`, not `false`).
- The Claude patch writer (`applyClaudeAgentSpecificUpdate`, `internal/wizard/patch.go`) writes dotted `agent_specific.*` keys into `[agents.claude]` unless the user expanded `agent_specific` into explicit sub-tables (`[agents.claude.agent_specific(.env|.permissions|.hooks)]`), in which case it writes the leaf into that section to avoid a TOML duplicate-table error.

## Antigravity

- Antigravity uses the `agy` binary and is launched with `--gemini_dir=<repo>/.agy` plus `AGY_CLI_DISABLE_AUTO_UPDATE=1` for repo-local containment. Agent Layer writes `.agy/antigravity-cli/settings.json` and `.agy/antigravity-cli/mcp_config.json`.
- `agy` v1.0.0 migrates `.agy/antigravity-cli/mcp_config.json` into `<gemini_dir>/config/mcp_config.json`, but runtime MCP discovery remains false in the observed probe baseline. Use `al probe agy` when checking whether upstream behavior has changed.

## Pin file recovery

- Empty or non-semver `.agent-layer/al.version` pin files are treated as "no pin" and auto-repaired by `al init`/`al upgrade` without prompts. The user sees a warning, never a hard error. A broken pin file must never make the CLI unusable.

## Upgrade and migration internals

- When source version cannot be resolved during `al upgrade`, source-agnostic migration operations still execute; source-gated operations are skipped with deterministic report entries. Ambiguous repos may need an explicit follow-up if the skip report flags missed transitions.
- Multi-version upgrades chain migration manifests: all manifests between source (exclusive) and target (inclusive) load in order with per-operation deduplication by ID. When source is unknown, only the target manifest loads (backward compatible). Manifests must have unique operation IDs across the chain or later duplicates are silently skipped.
- The required-field migration guardrail uses a baseline allowlist for fields that predate manifest enforcement (baseline version `0.8.1`). The allowlist must be maintained when introducing new required fields; stale entries can hide drift if not reviewed.

## E2E test harness

- `scripts/test-e2e/harness.sh` authenticates GitHub API calls with `GITHUB_TOKEN`/`GH_TOKEN` when available (raises the limit from 60 req/hr to 5000 req/hr). CI exports the token to `make ci`. Unauthenticated fallback is preserved for local offline runs.

## Test policy

- Do not write tests that assert specific wording, language, headings, or prose contracts in skill and instruction templates. Those checks are tautological and brittle. Tests may verify Agent Layer mechanics such as parsing, validation, sync/projection, resource copying, file existence/absence, and generated artifacts.

## Root resolution in cmd/al tests

- Root resolution (`internal/root` `FindAgentLayerRoot`/`FindRepoRoot`) walks upward from the working dir and stops only at a `.agent-layer/` or the filesystem root — there is no intermediate ceiling. `cmd/al` tests run from `t.TempDir()` (under the OS temp dir) and assume no ancestor holds a `.agent-layer`.
- A stray `.agent-layer` above the temp dir — e.g. `/tmp/.agent-layer` left by running `al init`/`al wizard`/`make al-*` while `cd`'d into `/tmp` — makes resolution escape the test sandbox. Symptoms: `cmd/al` tests fail with `got "/tmp"` or `already initialized in an ancestor directory (/tmp)`. Fix: `rm -rf /tmp/.agent-layer`, and don't run `al init` from `/tmp`. CI runners are clean, so this is a local-only gotcha.
