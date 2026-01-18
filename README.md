# Agent Layer (Go edition)

Agent Layer keeps AI-assisted development consistent across tools by generating each client’s required config from **one repo-local source of truth**.

In every repo where you install it:

- `./al` is a **repo-local executable** (gitignored)
- `.agent-layer/` contains **only user-editable configuration** (optionally committed)
- `docs/agent-layer/` contains **project memory** the agents rely on (teams can commit or ignore)

Running `./al <client>` always:
1) reads `.agent-layer/` config
2) regenerates (syncs) client files
3) launches the client

---

## Quick start

Install into the current repository (one command):

```bash
curl -fsSL https://github.com/nicholasjconn/agent-layer/releases/latest/download/agent-layer-install.sh | bash
```

Then run an agent:

```bash
./al gemini
```

Note: you must have the target client installed and on your `PATH` (Gemini CLI, Claude Code CLI, Codex, VS Code, etc.).

---

## What gets created in your repo

### Repo-local executable (gitignored)
- `./al`

### User configuration (gitignored by default, but can be committed)
- `.agent-layer/`
  - `config.toml` (main configuration; human-editable)
  - `instructions/` (numbered `*.md` fragments; lexicographic order)
  - `slash-commands/` (workflow markdown; one file per command)
  - `commands.allow` (approved shell commands; line-based)
  - `.env` (tokens/secrets; gitignored)

### Project memory (required; teams can commit or ignore)
Default instructions and slash commands rely on these files existing.

- `docs/agent-layer/`
  - `ISSUES.md`
  - `FEATURES.md`
  - `ROADMAP.md`
  - `DECISIONS.md`
  - `COMMANDS.md`

### Generated client files (gitignored by default)
Generated outputs are written to the repo root in client-specific formats (examples):
- `.gemini/`, `.claude/`, `.vscode/`, `.codex/`
- `.mcp.json`, `AGENTS.md`, etc.

---

## Supported clients

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
- Antigravity support is limited to instructions + slash commands.
- Auto-approval capabilities vary by client; `approvals.mode` is applied on a best-effort basis.

---

## Configuration (human-editable)

### `.agent-layer/config.toml`

This is the **only** structured config file.

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
reasoning_effort = "xhigh" # codex only

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
```

### Approvals modes (`approvals.mode`)

These modes control whether the agent is allowed to run shell commands and/or MCP tools without prompting:

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

### Instructions: `.agent-layer/instructions/`

- Files are concatenated in **lexicographic order**
- Use numeric prefixes for stable priority (e.g., `00_core.md`, `10_style.md`, `20_repo.md`)

### Slash commands: `.agent-layer/slash-commands/`

- One Markdown file per command.
- Filename (without `.md`) is the canonical command name.

### Approved commands: `.agent-layer/commands.allow`

- One command prefix per line.
- Used to generate each client’s “allowed commands” configuration where supported.

---

## MCP prompt server (internal)

Some clients discover slash commands via MCP prompts. Agent Layer provides an **internal MCP prompt server** automatically.

- You do not configure this in `config.toml`.
- It is generated and wired into client configs by `./al sync`.
- External MCP servers (tool/data servers) are configured under `[mcp]` in `config.toml`.

---

## VS Code + Codex extension (CODEX_HOME)

The Codex VS Code extension reads `CODEX_HOME` from the VS Code process environment at startup.

Agent Layer provides a repo-specific launch path that sets `CODEX_HOME` correctly for this repo.  
See `docs/agent-layer/COMMANDS.md` for the canonical VS Code launch instructions for this repo.

---

## Temporary run folders (concurrency-safe)

Some workflows produce artifacts (plans, task lists, reports). Agent Layer assigns each invocation a unique run directory:

- `tmp/agent-layer/runs/<run-id>/`

It exports:
- `AL_RUN_DIR` — the run directory for this invocation

This avoids collisions when multiple agents run concurrently.

---

## CLI overview

Common usage:

```bash
./al gemini
./al claude
./al codex
./al vscode
./al antigravity
```

Other commands:

- `./al install` — initialize `.agent-layer/`, `docs/agent-layer/`, and `.gitignore` (usually run by the installer)
- `./al sync` — regenerate configs without launching a client
- `./al doctor` — check common setup issues (tokens missing, files not writable, etc.)
- `./al wizard` — interactive setup (enable agents, choose models, Codex reasoning)
- `./al completion` — print shell completion scripts
- `./al mcp-prompts` — run the internal MCP prompt server (normally launched by the client)

---

## Shell completion output (tab completion)

“Shell completion output” is a snippet of shell script that enables tab-completion for `./al` in your shell.

Typical behavior:
- `./al completion bash` prints a Bash completion script to stdout
- `./al completion zsh` prints a Zsh completion script to stdout

This enables:
- `./al <TAB>` to complete supported subcommands (gemini/claude/codex/vscode/antigravity/sync/doctor/wizard/…)

---

## Git ignore defaults

Installer adds a managed `.gitignore` block that typically ignores:

- `./al`
- `.agent-layer/` (except if teams choose to commit it)
- `.agent-layer/.env`
- generated client config directories/files (`.gemini/`, `.claude/`, `.vscode/`, `.codex/`, `.mcp.json`, etc.)
- `tmp/agent-layer/`

`docs/agent-layer/` is created by default; teams may choose to commit it or ignore it.

---

## Goal of the Go rewrite

- Make installation and day-to-day usage trivial
- Preserve 100% feature parity with the current system (instructions, slash commands, config generation, sync-on-run)
- Reduce moving parts by shipping a single repo-local executable and keeping `.agent-layer/` config-only
