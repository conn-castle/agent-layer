# VS Code Launch Architecture

This document explains how VS Code launch works in Agent Layer, including the managed settings flow, preflight diagnostics, and performance boundaries.

## Scope

This architecture covers:

- `al vscode` command dispatch and no-sync behavior
- repo-local launcher scripts under `.agent-layer/`
- `CODEX_HOME` wiring for the VS Code Codex extension
- `CLAUDE_CONFIG_DIR` wiring for the VS Code Claude extension
- `GROK_HOME` wiring for Grok when `[agents.grok]` is enabled
- `.vscode/settings.json` managed block lifecycle

## Entry points

User-facing launch entry points:

- `al vscode` (`cmd/al/launch_cmd.go`)
- `.agent-layer/open-vscode.command`, `.agent-layer/open-vscode.sh`, `.agent-layer/open-vscode.app` (templates in `internal/templates/launchers/`)

Launcher scripts call `al vscode --no-sync` after checking that `al` and `code` are available on `PATH`.
They resolve the project's physical directory with `cd -P`, preserving literal
directory names (including trailing newlines) when locating the project.

## End-to-end flow

1. User runs `al vscode` (or a repo-local launcher that calls `al vscode --no-sync`).
2. `cmd/al/launch_cmd.go` parses `--no-sync` and pass-through args.
3. Launch mode (dispatched in `cmd/al/launch_cmd.go`):
   - default mode: `clients.RunWithStderr(...)` performs config load, sync, warnings, then launch
   - no-sync mode: `clients.RunNoSyncWithStderr(...)` performs config load and launch only
4. `internal/clients/vscode/launch.go` runs preflight checks:
   - `code` command exists on `PATH`
   - `.vscode/settings.json` root-level managed marker line comments are not malformed/duplicated; sync and launch share lexical recognition that excludes values, block/header comments, and nested profiles
5. Launch sets environment variables based on enabled agents:
   - `CODEX_HOME=<repo>/.codex` only when `agents.vscode` is enabled and `agents.codex.local_config_dir` is `true` (when local config is disabled, clears values selecting this repo home; when only VS Code is disabled, preserves inherited homes and ensures a selected project home exists; other relative overrides are made absolute from the caller’s directory)
   - `GROK_HOME=<repo>/.grok-config` when `agents.grok` is enabled (when disabled, clears only this repo’s home, honoring shorthand fallback, and makes other relative overrides absolute)
   - `CLAUDE_CONFIG_DIR=<repo>/.claude-config` when **both** `agents.claude_vscode` is enabled **and** `agents.claude.local_config_dir` is `true` (when disabled, clears only stale repo-local values and makes other relative overrides absolute from the caller’s directory)
   - Home comparisons follow symlinks before `..` and use filesystem identity for existing objects, including case aliases. Identical missing leaf names under aliased existing parents match; case-only aliases of a missing leaf are unsupported until the home exists. New `.codex` homes use `0700` without changing existing modes. Home-name shorthand, including dot segments and repeated/trailing separators, selects the repo home from subfolders only if the caller-relative entry is absent; values containing `..` or another component resolve physically.
6. Executes `code ...` with pass-through args, appending `.` only when no positional path/file arg is provided.

For example, `al vscode -- --user-data-dir "/tmp/editor data" --profile "Web Development"`
opens the project with an isolated data directory and named profile. Separate values
for `--user-data-dir`, `--extensions-dir`, `--profile`, and `--locale` do not count as
project targets. Explicit workspace/folder paths and stdin `-` suppress the default
`.`; a trailing native `--` alone does not. Other native arguments pass through
unchanged, with unknown option values conservatively treated as possible paths.

## Managed settings architecture

Sync writes VS Code outputs from `.agent-layer/config.toml`:

- `.vscode/mcp.json` via `internal/sync/vscode_mcp.go`
- `.vscode/settings.json` managed block via `internal/sync/vscode.go` + `internal/sync/vscode_settings_jsonc.go`

The shared `.agents/skills/` projection is also written when VS Code (or any other shared-skill consumer) is enabled, but is not VS-Code-specific. See `docs/SKILL-CLIENT-SPEC.md` for the full projection contract.

`settings.json` uses marker boundaries:

- `// >>> agent-layer`
- `// <<< agent-layer`

When markers exist, sync regenerates only the managed block. Agent Layer owns only the `vscodeSettings` keys and the retired keys in `vscodeRetiredManagedKeys`; sync moves any other property found inside the block, with its comments, to just after the end marker, and fails rather than discarding block content it cannot parse as properties. When missing, sync inserts a managed block into the root JSONC object. Launch preflight now fails fast if markers are malformed (duplicate/mismatched order) so users get repair guidance before VS Code starts.

## Preflight diagnostics

`al vscode` now emits explicit preflight errors for common launch failures:

- missing `code` command on `PATH`
- malformed managed settings markers in `.vscode/settings.json`

Remediation is intentionally explicit:

- install `code` shell command from VS Code command palette
- run `al sync` to repair the managed settings block

## First-launch performance profile (2026-02-15)

Measured on macOS arm64 using this repo:

- `code --version`: `0.158s` total
- `./tmp/al vscode --no-sync -- --version`: `0.339s` total

Interpretation:

- Agent Layer adds sub-second launch overhead before handing off to VS Code.
- The historical "slow first launch" symptom is primarily outside Agent Layer scope (VS Code extension initialization/workspace indexing), not in the launch pipeline itself.

## Contributor guidance

When changing VS Code launch behavior:

1. Keep `al vscode` and launcher-script behavior aligned.
2. Preserve fail-fast diagnostics for `al`/`code` availability and managed marker conflicts.
3. Update this document and `site/docs/troubleshooting.mdx` if launch failure messages or remediation steps change.
