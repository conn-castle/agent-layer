# VS Code Launch Architecture

This document explains how VS Code launch works in Agent Layer, including the managed settings flow, preflight diagnostics, and performance boundaries.

## Scope

This architecture covers:

- `al vscode` command dispatch and no-sync behavior
- repo-local launcher scripts under `.agent-layer/`
- `CODEX_HOME` wiring for the VS Code Codex extension
- `.vscode/settings.json` managed block lifecycle

## Entry points

User-facing launch entry points:

- `al vscode` (`cmd/al/vscode.go`)
- `.agent-layer/open-vscode.command`, `.agent-layer/open-vscode.sh`, `.agent-layer/open-vscode.app` (templates in `internal/templates/launchers/`)

Launcher scripts call `al vscode --no-sync` after checking that `al` and `code` are available on `PATH`.

## End-to-end flow

1. User runs `al vscode` (or a repo-local launcher that calls `al vscode --no-sync`).
2. `cmd/al/vscode.go` parses `--no-sync` and pass-through args.
3. Launch mode:
   - default mode: `clients.Run(...)` performs config load, sync, warnings, then launch
   - no-sync mode: `clients.RunNoSync(...)` performs config load and launch only
4. `internal/clients/vscode/launch.go` runs preflight checks:
   - `code` command exists on `PATH`
   - `.vscode/settings.json` managed markers are not malformed/duplicated
5. Launch sets `CODEX_HOME=<repo>/.codex` and executes `code ... .`.

## Managed settings architecture

Sync writes VS Code outputs from `.agent-layer/config.toml`:

- `.vscode/mcp.json` via `internal/sync/vscode_mcp.go`
- `.vscode/settings.json` managed block via `internal/sync/vscode.go` + `internal/sync/vscode_settings_jsonc.go`
- `.vscode/prompts/` via prompt writers in `internal/sync/`

`settings.json` uses marker boundaries:

- `// >>> agent-layer`
- `// <<< agent-layer`

When markers exist, sync replaces only the managed block. When missing, sync inserts a managed block into the root JSONC object. Launch preflight now fails fast if markers are malformed (duplicate/mismatched order) so users get repair guidance before VS Code starts.

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
