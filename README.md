# Agent Layer

One repo-local source of truth for instructions, slash commands, MCP servers, and approvals across coding agents.

Agent Layer keeps AI-assisted development consistent across tools by generating each client’s required config from a single `.agent-layer/` folder. Install `al` once per machine, run `al init` per repo, then run `al <client>` (e.g., `al claude`) to sync and launch.

Key properties:
- Local-first, no telemetry
- Deterministic outputs from canonical inputs
- Explicit approvals and command allowlists

Comparison:

| Manual per-client setup | Agent Layer |
| --- | --- |
| duplicate instructions across multiple formats | one canonical source under `.agent-layer/` |
| inconsistent approvals and command policies | consistent approvals and allowlists |
| MCP servers added in one client and forgotten in another | generated MCP config for every supported client |
| no single place to review or audit changes | audit in version control |

If this saves you time, please star the repo. Stars help new users find the project.

---

## Supported clients

MCP = Model Context Protocol (tool/data servers).

| Client | Instructions | Slash commands | MCP servers | Approved commands |
|---|---:|---:|---:|---:|
| Gemini CLI | ✅ | ✅ | ✅ | ✅ |
| Claude Code CLI | ✅ | ✅ | ✅ | ✅ |
| Claude Code VS Code Extension | ✅ | ✅ | ✅ | ✅ |
| VS Code / Copilot Chat | ✅ | ✅ | ✅ | ✅ |
| Codex CLI | ✅ | ✅ | ✅ | ✅ |
| Codex VS Code extension | ✅ | ✅ | ✅ | ✅ |
| Antigravity | ✅ | ✅ | ❌ | ❌ |

Notes:
- VS Code/Codex "slash commands" are generated in their native formats (prompt files / skills).
- Antigravity slash commands are generated as skills in `.agent/skills/<command>/SKILL.md`.
- Auto-approval capabilities vary by client; `approvals.mode` is applied on a best-effort basis.
- Antigravity does not support MCP servers because it only reads from the home directory and does not load repo-local `.gemini/` or `.agent/` MCP configs.

---

## Install

Install once per machine; choose one:

### Homebrew (macOS/Linux)

```bash
brew install conn-castle/tap/agent-layer
```

### Script (macOS/Linux)

```bash
curl -fsSL https://github.com/conn-castle/agent-layer/releases/latest/download/al-install.sh | bash
```

Verify:

```bash
al --version
```

---

## Quick start

Initialize a repo (run from any subdirectory):

```bash
cd /path/to/repo
al init
```

Then run an agent:

```bash
al gemini
```

Optional health check:

```bash
al doctor
```

Notes:
- `al init` prompts to run `al wizard` after seeding files. Use `al init --no-wizard` to skip; non-interactive shells skip automatically.
- `al init` is intended to be run once per repo. If the repo is already initialized, use `al upgrade plan` and `al upgrade` to refresh template-managed files.
- `al upgrade` is the recommended path. For CI-safe non-interactive apply, use `al upgrade --yes --apply-managed-updates`. Add `--apply-memory-updates` and/or `--apply-deletions` only when you explicitly want those categories.
- `al upgrade` automatically creates a managed-file snapshot and rolls changes back if an upgrade step fails. Snapshots are written under `.agent-layer/state/upgrade-snapshots/`.
- Agent Layer does not install clients. Install the target client CLI and ensure it is on your `PATH` (Gemini CLI, Claude Code CLI, Codex, VS Code, etc.).

---

## MCP server requirements (external tools)

Some MCP servers require a specific runtime or launcher to be installed locally. Agent Layer does not install these dependencies; it only runs the command you configure.

Examples:
- **Node-based servers** often use `npx` in the `command` field (requires Node.js + npm).
- **Python/uv-based servers** often use `uvx` in the `command` field (requires `uv`/`uvx` on your `PATH`).

If a server fails to start with “No such file or directory,” verify the `command` exists and is on your `PATH`, or set `command` to the full path of the executable.

### Doctor MCP checks

`al doctor` connects to each enabled MCP server and lists tools. It waits up to **30 seconds per server** before warning about connectivity, and prints a short progress indicator while checks run.

---

## FAQ

### Why do MCP servers fail to start in VS Code on macOS?

If MCP servers that use `npx` are failing in VS Code, your GUI environment may not see a user-directory Node install. Install Node via Homebrew (`brew install node`) so VS Code can find `node` and `npx`, and avoid per-user installs that only exist in shell profiles.

---

## Version pinning (per repo, required)

Version pinning keeps everyone on the same Agent Layer release and lets `al` download the right binary automatically.

Upgrade contract details (event model, compatibility guarantees, migration rules, OS/shell matrix) are maintained in one canonical location: the [upgrade contract](https://conn-castle.github.io/agent-layer-web/docs/upgrades) (source: `site/docs/upgrades.mdx`).

When a release version is available, `al init` writes `.agent-layer/al.version` (for example, `0.6.0`). You can also edit it manually, or set the initial pin with `al init --version X.Y.Z` (or `--version latest`).

When you run `al` inside a repo, it locates `.agent-layer/`, reads the pinned version when present, and dispatches to that version automatically. `al init` and `al upgrade` are exceptions: they run on the invoking CLI version so pin updates and upgrade planning are not blocked by an older repo pin.

By default, `al init` enables pinning in release builds by writing `.agent-layer/al.version`. Think of this file like a lockfile: it prevents the repo from silently changing behavior when you update your globally installed `al`.

Agent Layer treats `.agent-layer/al.version` as required for supported usage. Do not delete it. If it is missing or invalid, install a release build and run `al upgrade` to repair it.

Pin format:
- `0.6.0` or `v0.6.0` (both are accepted)

Cache location (per user):
- default: user cache dir (for example `~/.cache/agent-layer/versions/<version>/<os>-<arch>/al-<os>-<arch>` on Linux)
- override: `AL_CACHE_DIR=/path/to/cache`

Overrides:
- `AL_VERSION=0.6.0` forces a version (overrides the repo pin)
- `AL_NO_NETWORK=1` disables downloads (fails if the pinned version is missing)

---

## Updating Agent Layer

Update the global CLI:
- Homebrew: `brew upgrade conn-castle/tap/agent-layer` (updates the installed formula)
- Script (macOS/Linux): re-run the install script from Install (downloads and replaces `al`)

Run `al upgrade plan` and then `al upgrade` inside the repo to apply template-managed updates. This updates `.agent-layer/al.version` to match the currently installed `al` binary and refreshes template-managed files.

Each `al upgrade` run writes an automatic snapshot of managed upgrade targets and auto-rolls back if an upgrade step fails. Snapshots are retained under `.agent-layer/state/upgrade-snapshots/` for rollback history.
To manually restore an applied snapshot, run `al upgrade rollback <snapshot-id>` (use the JSON filename stem from `.agent-layer/state/upgrade-snapshots/` as `<snapshot-id>`).

`al upgrade` also executes embedded per-release migration manifests before template writes (for example file renames/deletes and config key transitions). If the prior source version cannot be resolved, source-agnostic migrations still run and source-gated migrations are skipped with explicit report output.

Upgrade previews now include line-level diffs in both `al upgrade plan` and interactive `al upgrade` prompts. By default, each file preview is truncated to 40 lines; raise this with `--diff-lines N` when needed.


`al doctor` always checks for newer releases and warns if you're behind. `al init` also warns when your installed CLI is out of date, unless you set `--version`, `AL_VERSION`, or `AL_NO_NETWORK`.

Compatibility guarantee:
- Guaranteed upgrade path is sequential release lines only (`N-1` to `N`; example: `0.6.x` to `0.7.x`).
- Skipping release lines is best effort and may require additional manual migration.
- See the [upgrade contract](https://conn-castle.github.io/agent-layer-web/docs/upgrades) (source: `site/docs/upgrades.mdx`) for event categories and release-versioned migration rules.

---

## Interactive setup (optional, `al wizard`)

Run `al wizard` any time to interactively configure the most important settings:

- **Approvals Mode** (all, mcp, commands, none)
- **Agent Enablement** (Gemini, Claude, Codex, VS Code, Antigravity)
- **Model Selection** (optional; leave blank to use client defaults, including Codex reasoning effort)
- **MCP Servers & Secrets** (toggle default servers; safely write secrets to `.agent-layer/.env`)
- **Warnings** (enable/disable warning checks; threshold values use template defaults)

**Controls:**
- **Arrow keys**: Navigate
- **Space**: Toggle selection (multi-select)
- **Enter**: Confirm/Continue
- **Esc/Ctrl+C**: Cancel

The wizard rewrites `config.toml` in the template-defined order and creates backups (`.bak`) before modifying `.agent-layer/config.toml` or `.agent-layer/.env`. Inline comments on modified lines may be moved to leading comments or removed; the original formatting is preserved in the backup files.

---

## What gets created in your repo

`al init` creates three buckets: user configuration, project memory, and generated client files.

### User configuration (gitignored by default, but can be committed)
  - `.agent-layer/`
  - `config.toml` (main configuration; human-editable)
  - `al.version` (repo pin; required)
  - `instructions/` (numbered `*.md` fragments; lexicographic order)
  - `slash-commands/` (workflow markdown; one file per command)
  - `commands.allow` (approved shell commands; line-based)
  - `gitignore.block` (managed `.gitignore` block template; customize here)
  - `.gitignore` (ignores repo-local launchers, template copies, and backups inside `.agent-layer/`)
  - `.env` (tokens/secrets; gitignored)

Repo-local launchers and template copies live under `.agent-layer/` and are ignored by `.agent-layer/.gitignore`.

### Project memory (required; teams can commit or ignore)
Default instructions and slash commands rely on these files existing, along with any additional memory files your team adopts.

Common memory files include:
- `docs/agent-layer/ISSUES.md`
- `docs/agent-layer/BACKLOG.md`
- `docs/agent-layer/ROADMAP.md`
- `docs/agent-layer/DECISIONS.md`
- `docs/agent-layer/COMMANDS.md`

### Generated client files (gitignored by default)
Generated outputs are written into the repo in client-specific formats (examples):

- Instruction shims: `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `.github/copilot-instructions.md`
- MCP + client configs: `.mcp.json`, `.gemini/settings.json`, `.claude/settings.json`, `.codex/`
- Antigravity skills: `.agent/skills/`
- VS Code integration: `.vscode/mcp.json`, `.vscode/prompts/`, and an Agent Layer-managed block in `.vscode/settings.json`

---

## Configuration (human-editable)

You can edit all configuration files by hand. `al wizard` updates `config.toml` (approvals, agents/models, MCP servers, warnings) and `.agent-layer/.env` (secrets); it does not touch instructions, slash commands, or `commands.allow`.

### `.agent-layer/config.toml`

Edit this file directly or use `al wizard` to update it. This is the **only** structured config file.

Example:

```toml
[approvals]
# one of: "all", "mcp", "commands", "none"
mode = "all"

[agents.gemini]
enabled = true
# model is optional; when omitted, Agent Layer does not pass a model flag and the client uses its default.
# model = "..."

[agents.claude]
enabled = true
# model is optional; when omitted, Agent Layer does not pass a model flag and the client uses its default.
# model = "..."

[agents.codex]
enabled = true
# model is optional; when omitted, Agent Layer does not pass a model setting and the client uses its default.
# model = "gpt-5.2-codex"
# reasoning_effort is optional; when omitted, the client uses its default.
# reasoning_effort = "xhigh" # codex only

[agents.vscode]
enabled = true

[agents.antigravity]
enabled = true

[mcp]
# Secrets belong in .agent-layer/.env (never in config.toml).
# MCP servers here are the *external tool servers* that get projected into client configs.
# Installer seeds a small library of defaults you can edit, disable, or delete.

[[mcp.servers]]
id = "example-api"
enabled = true
transport = "http"
# http_transport = "sse" # optional: "sse" (default) or "streamable"
url = "https://example.com/mcp"
headers = { Authorization = "Bearer ${AL_EXAMPLE_TOKEN}" }

[[mcp.servers]]
id = "local-mcp"
enabled = false
transport = "stdio"
command = "my-mcp-server"
args = ["--flag", "value"]
env = { MY_TOKEN = "${AL_MY_TOKEN}" }

[warnings]
# Optional thresholds for warning checks. Omit or comment out to disable.
# Warn when a newer Agent Layer version is available during sync runs.
version_update_on_sync = true
instruction_token_threshold = 10000
mcp_server_threshold = 15
mcp_tools_total_threshold = 60
mcp_server_tools_threshold = 25
mcp_schema_tokens_total_threshold = 30000
mcp_schema_tokens_server_threshold = 20000
```

#### Built-in placeholders

Agent Layer provides a built-in `${AL_REPO_ROOT}` placeholder for file paths in MCP server configs.
It expands to the absolute repo root during `al sync` and `al doctor`, and it does **not** need to be in `.env`.
Paths that start with `${AL_REPO_ROOT}` or `~` are expanded and normalized; other relative paths are passed through as-is.

Example:

```toml
[[mcp.servers]]
id = "filesystem"
enabled = false
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "${AL_REPO_ROOT}/."]
```

#### Default MCP server client exclusions

Some default MCP servers exclude VS Code via the `clients` field:

- **ripgrep** and **filesystem**: Excluded from VS Code because VS Code/Copilot Chat has native file search and access capabilities. Adding these servers would duplicate functionality and increase context window usage.

You can override these exclusions by editing `clients` in your `config.toml`.

#### HTTP transport (`http_transport`)

For HTTP MCP servers, `http_transport` controls how `al doctor` connects:

- `sse` (default)
- `streamable`

Omit `http_transport` to default to `sse`.

#### Warning thresholds (`[warnings]`)

Warning thresholds are optional. When a threshold is omitted, its warning is disabled. Values must be positive integers (zero/negative are rejected by config validation). `al sync` uses `instruction_token_threshold` and, when `version_update_on_sync = true`, warns if a newer Agent Layer release is available. `al doctor` evaluates all configured MCP warning thresholds.

Set `version_update_on_sync = true` to opt in to update warnings during `al sync` and `al <client>`; omit it or set it to `false` to keep update warnings limited to `al init`, `al doctor`, and `al wizard`.

#### Approvals modes (`approvals.mode`)

These modes control whether the agent is allowed to run shell commands and/or MCP tools without prompting. Edit them to match your team's preferences; `al wizard` can update `approvals.mode`.

- `all`: auto-approve **both** shell commands and MCP tool calls (where supported)
- `mcp`: auto-approve **only** MCP tool calls; shell commands still require approval (or are restricted)
- `commands`: auto-approve **only** shell commands; MCP tool calls still require approval
- `none`: approve **nothing** automatically

Client notes:
- Some clients do not support all approval types; Agent Layer generates the closest supported behavior per client.

### Secrets: `.agent-layer/.env`

API tokens and other secrets live in `.agent-layer/.env` (always gitignored).

**Important:** Only environment variables that start with the `AL_` prefix are sourced from `.env` (others are ignored). This convention avoids conflicts with your shell environment and ensures Agent Layer's variables don't override existing environment variables when VS Code terminals inherit the process environment.

Example keys:
- `AL_GITHUB_PERSONAL_ACCESS_TOKEN`
- `AL_CONTEXT7_API_KEY`
- `AL_TAVILY_API_KEY`

Your existing process environment takes precedence. `.agent-layer/.env` fills missing keys only, and empty values in `.agent-layer/.env` are ignored (so template entries cannot override real tokens). This behavior is consistent whether launching via `al` commands or repo-local launchers like `open-vscode.app`, `open-vscode.sh`, or `open-vscode.command`.

### Instructions: `.agent-layer/instructions/`

These files are user-editable; customize them for your team's preferences.

- Files are concatenated in **lexicographic order**
- Use numeric prefixes for stable priority (e.g., `00_core.md`, `10_style.md`, `20_repo.md`)

### Slash commands: `.agent-layer/slash-commands/`

These files are user-editable; define the workflows you want your agents to run.

- One Markdown file per command.
- Filename (without `.md`) is the canonical command name.
- Antigravity consumes these as skills in `.agent/skills/<command>/SKILL.md`.

### Approved commands: `.agent-layer/commands.allow`

- One command prefix per line.
- Used to generate each client’s “allowed commands” configuration where supported.

---

## MCP prompt server (internal)

Some clients discover slash commands via MCP prompts. Agent Layer provides an **internal MCP prompt server** automatically.

- You do not configure this in `config.toml`.
- It is generated and wired into client configs by `al sync`.
- External MCP servers (tool/data servers) are configured under `[mcp]` in `config.toml`.
- There is no config toggle to disable it; the server is always included for clients that support MCP prompts.

---

## VS Code + Codex extension (CODEX_HOME)

The Codex VS Code extension reads `CODEX_HOME` from the VS Code process environment at startup.

Agent Layer provides repo-specific launchers in `.agent-layer/` that set `CODEX_HOME` correctly for this repo:

Launchers:
- macOS: `open-vscode.app` (recommended; VS Code in `/Applications` or `~/Applications`) or `open-vscode.command` (uses `code` CLI)
- Linux: `open-vscode.desktop` or `open-vscode.sh` (uses `code` CLI; shows a dialog if missing)

These launchers invoke `al vscode`, so the `al` CLI must be available on your PATH.

If you use the CLI-based launchers, install the `code` command from inside VS Code:
- macOS: Cmd+Shift+P -> "Shell Command: Install 'code' command in PATH"
- Linux: Ctrl+Shift+P -> "Shell Command: Install 'code' command in PATH"

**Note:** Codex authentication is per repo because each repo uses its own `CODEX_HOME`. When you open VS Code with a different `CODEX_HOME`, you will need to reauthenticate. This is expected behavior and keeps credentials isolated per repo. It also enables you to use different Codex accounts across repositories, such as one for personal projects and one for work, without credential overlap.

---

## Temporary artifacts (agent-only)

Some workflows write **agent-only** artifacts (plans, task lists, reports). These are not meant for humans to open.

Artifacts always live under `.agent-layer/tmp/` and use a unique, concurrency-safe name:

- `.agent-layer/tmp/<workflow>.<run-id>.<type>.md`
- `run-id = YYYYMMDD-HHMMSS-<short-rand>` (uses bash `$RANDOM`; bash is required)
- Multi-file workflows reuse the same `run-id` for all files.
- Common `type` values: `report`, `plan`, `task`.

Workflows echo the artifact path in the chat output. There are no path overrides or environment variables for this. Artifacts are agent-only and can be ignored; agents may clean up their own plan/task files when a workflow completes. If a run is interrupted, leftover files are harmless and optional to delete.

---

## CLI overview

Common usage:

```bash
al gemini
al claude
al codex
al vscode
al antigravity
```

### Passing flags to clients

`al <client>` forwards any extra arguments to the underlying client. If you need to use Agent Layer flags as well, place `--` before the client arguments.

Examples:

```bash
al claude -- --help
al vscode --no-sync -- --reuse-window
```

`--no-sync` is an Agent Layer flag and must appear before `--`; anything after `--` is passed directly to the client. For an explicit false value, use `--no-sync=false` (space-separated values like `--no-sync false` are not supported and will be passed through).

Other commands:

- `al init` — initialize `.agent-layer/`, `docs/agent-layer/`, and `.gitignore`
- `al upgrade` — apply template-managed updates and update the repo pin (interactive by default; non-interactive requires `--yes` plus one or more apply flags; line-level diff previews shown by default, `--diff-lines N` to raise per-file preview size; automatic snapshot + rollback on failure)
- `al upgrade plan` — preview plain-language categorized template/pin changes and readiness actions with line-level diff previews (`--diff-lines N` to raise per-file preview size)
- `al upgrade rollback <snapshot-id>` — restore an applied upgrade snapshot (snapshot IDs are JSON filenames in `.agent-layer/state/upgrade-snapshots/`)
- `al sync` — regenerate configs without launching a client
- `al doctor` — check common setup issues and warn about available updates
- `al wizard` — interactive setup wizard (configure agents, models, MCP secrets)
- `al completion` — generate shell completion scripts (bash/zsh/fish, macOS/Linux only)

---

## Shell completion (macOS/Linux)

*The completion command is available on macOS and Linux only.*

“Shell completion output” is a snippet of shell script that enables tab-completion for `al` in your shell.

Typical behavior:
- `al completion bash` prints a Bash completion script to stdout
- `al completion zsh` prints a Zsh completion script to stdout
- `al completion fish` prints a Fish completion script to stdout
- `al completion <shell> --install` writes the completion file to the standard user location

This enables:
- `al <TAB>` to complete supported subcommands (gemini/claude/codex/vscode/antigravity/sync/…)

Notes:
- Zsh may require adding the install directory to `$fpath` before `compinit` (the command prints a snippet when needed).
- Bash completion requires bash-completion to be enabled in your shell.

---

## Git ignore defaults

Installer adds a managed `.gitignore` block that typically ignores:
- `.agent-layer/` (except if teams choose to commit it)
- generated client config files/directories (for example `.gemini/settings.json`, `.claude/settings.json`, `.mcp.json`, `.codex/`, `.agent/skills/`, `.vscode/mcp.json`, `.vscode/prompts/`, and `.github/copilot-instructions.md`)

If you choose to commit `.agent-layer/`, keep `.agent-layer/.gitignore` so repo-local launchers, template copies, and backups stay untracked.

To commit `.agent-layer/`, remove the `/agent-layer/` line in `.agent-layer/gitignore.block` and re-run `al sync`.

To customize the managed block, edit `.agent-layer/gitignore.block` and re-run `al sync`.

`.agent-layer/.env` is ignored by `.agent-layer/.gitignore`, not the parent repo `.gitignore`.

`docs/agent-layer/` is created by default; teams may choose to commit it or ignore it.

---

## Design goals

- Make installation and day-to-day usage trivial
- Provide consistent core features across clients (instructions, slash commands, config generation, MCP servers, sync-on-run)
- Reduce moving parts by shipping a single global CLI and keeping `.agent-layer/` config-first with minimal repo-local launchers

## Changelog

See `CHANGELOG.md` for release history.

## Contributing

Contributions are welcome. Please use the project's issue tracker and pull requests.

Contributor workflows live in `docs/DEVELOPMENT.md`.

## License

See `LICENSE.md`.

## Attributions

- Nicholas Conn, PhD - Conn Castle Studios
