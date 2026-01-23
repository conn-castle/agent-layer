# Agent Layer

Agent Layer keeps AI-assisted development consistent across tools by generating each client’s required config from **one repo-local source of truth**.

Install once per machine. `al` is a globally installed CLI on your `PATH`. In each repo, run `al init` to seed `.agent-layer/` and `docs/agent-layer/`. You edit `.agent-layer/`; `al` regenerates the right files for each client and launches it.

Running `al <client>` always:
1) reads `.agent-layer/` config
2) regenerates (syncs) client files
3) launches the client

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

### Windows (PowerShell)

```powershell
iwr -useb https://github.com/conn-castle/agent-layer/releases/latest/download/al-install.ps1 | iex
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

Notes:
- `al init` prompts to run `al wizard` after seeding files. Use `al init --no-wizard` to skip; non-interactive shells skip automatically.
- To refresh template-managed files, use `al init --overwrite` to review each file or `al init --force` to overwrite without prompts.
- Agent Layer does not install clients. Install the target client CLI and ensure it is on your `PATH` (Gemini CLI, Claude Code CLI, Codex, VS Code, etc.).

---

## Version pinning (per repo, optional)

Version pinning keeps everyone on the same Agent Layer release and lets `al` download the right binary automatically.

When a release version is available, `al init` writes `.agent-layer/al.version` (for example, `0.5.0`). You can also edit it manually or pass `--version` to pin a specific release.

When you run `al` inside a repo, it locates `.agent-layer/`, reads the pinned version when present, and dispatches to that version automatically.

Pin format:
- `0.5.0` or `v0.5.0` (both are accepted)

Cache location (per user):
- default: user cache dir (for example `~/.cache/agent-layer/versions/<version>/<os>-<arch>/al-<os>-<arch>` on Linux)
- override: `AL_CACHE_DIR=/path/to/cache`

Overrides:
- `AL_VERSION=0.5.0` forces a version (overrides the repo pin)
- `AL_NO_NETWORK=1` disables downloads (fails if the pinned version is missing)

---

## Updating Agent Layer

Update the global CLI:
- Homebrew: `brew upgrade conn-castle/tap/agent-layer` (updates the installed formula)
- Script (macOS/Linux): re-run the install script from Install (downloads and replaces `al`)
- Windows: re-run the PowerShell install script (downloads and replaces `al`)

If a repo is pinned, edit `.agent-layer/al.version` to the new release (`vX.Y.Z` or `X.Y.Z`) and run `al` to download it.

`al doctor` always checks for newer releases and warns if you're behind. `al init` also warns when your installed CLI is out of date, unless you set `--version`, `AL_VERSION`, or `AL_NO_NETWORK`.

---

## Interactive setup (optional, `al wizard`)

Run `al wizard` any time to interactively configure the most important settings:

- **Approvals Mode** (all, mcp, commands, none)
- **Agent Enablement** (Gemini, Claude, Codex, VS Code, Antigravity)
- **Model Selection** (optional; leave blank to use client defaults, including Codex reasoning effort)
- **MCP Servers & Secrets** (toggle default servers; safely write secrets to `.agent-layer/.env`)
- **Warning Thresholds** (optional; configure warnings for common performance/usage issues)

**Controls:**
- **Arrow keys**: Navigate
- **Space**: Toggle selection (multi-select)
- **Enter**: Confirm/Continue
- **Esc/Ctrl+C**: Cancel

The wizard preserves your configuration’s table structure and key ordering and creates backups (`.bak`) before modifying `.agent-layer/config.toml` or `.agent-layer/.env`. Note that inline comments on modified lines may be moved to leading comments or removed; the original formatting is preserved in the backup files.

---

## What gets created in your repo

`al init` creates three buckets: user configuration, project memory, and generated client files.

### User configuration (gitignored by default, but can be committed)
- `.agent-layer/`
  - `config.toml` (main configuration; human-editable)
  - `al.version` (repo pin; optional but recommended)
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
- `docs/agent-layer/FEATURES.md`
- `docs/agent-layer/ROADMAP.md`
- `docs/agent-layer/DECISIONS.md`

### Generated client files (gitignored by default)
Generated outputs are written to the repo root in client-specific formats (examples):
- `.agent/`, `.gemini/`, `.claude/`, `.vscode/`, `.codex/`
- `.mcp.json`, `AGENTS.md`, etc.

---

## Supported clients

MCP = Model Context Protocol (tool/data servers).

| Client | Instructions | Slash commands | MCP servers | Approved commands |
|---|---:|---:|---:|---:|
| Gemini CLI | ✅ | ✅ | ✅ | ✅ |
| Claude Code CLI | ✅ | ✅ | ✅ | ✅ |
| VS Code / Copilot Chat | ✅ | ✅ | ✅ | ✅ |
| Codex CLI | ✅ | ✅ | ✅ | ✅ |
| Codex VS Code extension | ✅ | ✅ | ✅ | ✅ |
| Antigravity | ✅ | ✅ | ❌ | ❌ |

Notes:
- VS Code/Codex “slash commands” are generated in their native formats (prompt files / skills).
- Antigravity slash commands are generated as skills in `.agent/skills/<command>/SKILL.md`.
- Auto-approval capabilities vary by client; `approvals.mode` is applied on a best-effort basis.
- Antigravity does not support MCP servers because it only reads from the home directory and does not load repo-local `.gemini/` or `.agent/` MCP configs.

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
model = "gpt-5.2-codex"
reasoning_effort = "high" # codex only

[agents.vscode]
enabled = true

[agents.antigravity]
enabled = true

[mcp]
# Secrets belong in .agent-layer/.env (never in config.toml).
# MCP servers here are the *external tool servers* that get projected into client configs.
# Installer seeds a small library of defaults you can edit, disable, or delete.

[[mcp.servers]]
id = "github"
enabled = true
clients = ["gemini", "claude", "vscode", "codex"] # omit = all clients
transport = "http"
url = "https://example.com/mcp"
headers = { Authorization = "Bearer ${GITHUB_PERSONAL_ACCESS_TOKEN}" }

[[mcp.servers]]
id = "local-mcp"
enabled = false
transport = "stdio"
command = "my-mcp-server"
args = ["--flag", "value"]
env = { MY_TOKEN = "${MY_TOKEN}" }

[warnings]
# Optional thresholds for warning checks. Omit or comment out to disable.
instruction_token_threshold = 10000
mcp_server_threshold = 15
mcp_tools_total_threshold = 60
mcp_server_tools_threshold = 25
mcp_schema_tokens_total_threshold = 10000
mcp_schema_tokens_server_threshold = 7500
```

#### Warning thresholds (`[warnings]`)

Warning thresholds are optional. When a threshold is omitted, its warning is disabled. Values must be positive integers (zero/negative are rejected by config validation). `al sync` uses `instruction_token_threshold`, while `al doctor` evaluates all configured MCP warning thresholds.

#### Approvals modes (`approvals.mode`)

These modes control whether the agent is allowed to run shell commands and/or MCP tools without prompting. Edit them to match your team's preferences; `al wizard` can update `approvals.mode`.

- `all`: auto-approve **both** shell commands and MCP tool calls (where supported)
- `mcp`: auto-approve **only** MCP tool calls; shell commands still require approval (or are restricted)
- `commands`: auto-approve **only** shell commands; MCP tool calls still require approval
- `none`: approve **nothing** automatically

Client notes:
- Some clients do not support all approval types; Agent Layer generates the closest supported behavior per client.

### Secrets: `.agent-layer/.env`

API tokens and other secrets live in `.agent-layer/.env` (always gitignored). Example keys:
- `GITHUB_PERSONAL_ACCESS_TOKEN`
- `CONTEXT7_API_KEY`
- `TAVILY_API_KEY`

When launching via `al`, your existing process environment takes precedence. `.agent-layer/.env` fills missing keys only, and empty values in `.agent-layer/.env` are ignored (so template entries cannot override real tokens).

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
- Windows: `open-vscode.bat` (uses `code` CLI)
- Linux: `open-vscode.desktop` (uses `code` CLI; shows a dialog if missing)

If you use the CLI-based launchers, install the `code` command from inside VS Code:
- macOS: Cmd+Shift+P -> "Shell Command: Install 'code' command in PATH"
- Linux: Ctrl+Shift+P -> "Shell Command: Install 'code' command in PATH"

---

## Temporary run folders (concurrency-safe)

Some workflows produce artifacts (plans, task lists, reports). Agent Layer assigns each invocation a unique run directory:

- `tmp/agent-layer/runs/<run-id>/`

It exports:
- `AL_RUN_DIR` — the run directory for this invocation
- `AL_RUN_ID` — the run identifier for this invocation

This avoids collisions when multiple agents run concurrently.

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

Other commands:

- `al init` — initialize `.agent-layer/`, `docs/agent-layer/`, and `.gitignore`
- `al sync` — regenerate configs without launching a client
- `al doctor` — check common setup issues and warn about available updates
- `al wizard` — interactive setup wizard (configure agents, models, MCP secrets)
- `al completion` — generate shell completion scripts (bash/zsh/fish, macOS/Linux only)
- `al mcp-prompts` — internal MCP prompt server (normally launched by the client)

---

## Shell completion (macOS/Linux)

*The completion command is available on macOS and Linux only; Windows completions are not supported.*

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
- `.agent-layer/.env`
- generated client config directories/files (`.gemini/`, `.claude/`, `.vscode/`, `.codex/`, `.mcp.json`, etc.)
- `tmp/agent-layer/`

If you choose to commit `.agent-layer/`, keep `.agent-layer/.gitignore` so repo-local launchers, template copies, and backups stay untracked.

To commit `.agent-layer/`, remove the `/agent-layer/` line in `.agent-layer/gitignore.block` and re-run `al init`.

To customize the managed block, edit `.agent-layer/gitignore.block` and re-run `al init`.

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
